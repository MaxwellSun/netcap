/*
 * NETCAP - Traffic Analysis Framework
 * Copyright (c) 2017-2020 Philipp Mieden <dreadl0ck [at] protonmail [dot] ch>
 *
 * THE SOFTWARE IS PROVIDED "AS IS" AND THE AUTHOR DISCLAIMS ALL WARRANTIES
 * WITH REGARD TO THIS SOFTWARE INCLUDING ALL IMPLIED WARRANTIES OF
 * MERCHANTABILITY AND FITNESS. IN NO EVENT SHALL THE AUTHOR BE LIABLE FOR
 * ANY SPECIAL, DIRECT, INDIRECT, OR CONSEQUENTIAL DAMAGES OR ANY DAMAGES
 * WHATSOEVER RESULTING FROM LOSS OF USE, DATA OR PROFITS, WHETHER IN AN
 * ACTION OF CONTRACT, NEGLIGENCE OR OTHER TORTIOUS ACTION, ARISING OUT OF
 * OR IN CONNECTION WITH THE USE OR PERFORMANCE OF THIS SOFTWARE.
 */

// Provides a mechanism to collect network packets from a network interface on macOS, linux and windows
package collector

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"runtime/debug"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/dreadl0ck/gopacket"
	"github.com/dreadl0ck/netcap/dpi"

	"sync"

	"github.com/dreadl0ck/netcap"
	"github.com/dreadl0ck/netcap/resolvers"
	"github.com/dreadl0ck/netcap/utils"

	"github.com/dreadl0ck/netcap/encoder"
	humanize "github.com/dustin/go-humanize"
	"github.com/evilsocket/islazy/tui"
	"github.com/mgutz/ansi"
)

// Collector provides an interface to collect data from PCAP or a network interface.
type Collector struct {

	// input channels for the worker pool
	workers []chan *packet

	// synchronization
	statMutex sync.Mutex
	wg        sync.WaitGroup
	next      int
	current   int64

	// unknown protocol pcap writer
	unkownPcapWriterBuffered *bufio.Writer
	unkownPcapWriterAtomic   *AtomicPcapGoWriter
	unknownPcapFile          *os.File

	// error pcap writer
	errorsPcapWriterBuffered *bufio.Writer
	errorsPcapWriterAtomic   *AtomicPcapGoWriter
	errorsPcapFile           *os.File

	// error log file handle
	errorLogFile *os.File

	// protocol maps
	unknownProtosAtomic *encoder.AtomicCounterMap
	allProtosAtomic     *encoder.AtomicCounterMap

	// processing errors
	errorMap *encoder.AtomicCounterMap

	// stats
	start             time.Time
	numPackets        int64
	totalBytesWritten int64
	files             map[string]string
	inputSize         int64

	// configuration
	config *Config

	isLive bool
}

// New returns a new Collector instance.
func New(config Config) *Collector {

	if config.OutDirPermission == 0 {
		config.OutDirPermission = os.FileMode(outDirPermissionDefault)
	}

	return &Collector{
		next:                1,
		unknownProtosAtomic: encoder.NewAtomicCounterMap(),
		allProtosAtomic:     encoder.NewAtomicCounterMap(),
		errorMap:            encoder.NewAtomicCounterMap(),
		files:               map[string]string{},
		config:              &config,
		start:               time.Now(),
	}
}

// stopWorkers halts all workers.
func (c *Collector) stopWorkers() {
	// wait until all packets have been decoded
	for _, w := range c.workers {
		w <- nil
		// TODO closing here produces a data race
		// close(w)
	}
}

// cleanup before leaving. closes all buffers and displays stats.
func (c *Collector) cleanup(force bool) {

	// stop all workers.
	// this will block until all workers are stopped
	// all packets left in the packet queues will be processed
	c.stopWorkers()

	if c.config.ReassembleConnections {
		// teardown the TCP stream reassembly and print stats
		encoder.CleanupReassembly(!force)
	}
	encoder.Cleanup()

	c.statMutex.Lock()
	c.wg.Wait()
	c.statMutex.Unlock()

	// flush all layer encoders
	for _, encoders := range encoder.LayerEncoders {
		for _, e := range encoders {
			name, size := e.Destroy()
			if size != 0 {
				c.totalBytesWritten += size
				c.files[name] = humanize.Bytes(uint64(size))
			}
		}
	}

	// flush all custom encoders
	for _, e := range encoder.CustomEncoders {
		name, size := e.Destroy()
		if size != 0 {
			c.totalBytesWritten += size
			c.files[name] = humanize.Bytes(uint64(size))
		}
	}

	if c.isLive {
		c.statMutex.Lock()
		c.numPackets = c.current
		c.statMutex.Unlock()
	}

	// sync pcap file
	if err := c.closePcapFiles(); err != nil {
		log.Fatal("failed to close pcap files: ", err)
	}

	c.closeErrorLogFile()
	c.Stats()

	// encoder.DumpTop5LinkFlows()
	// encoder.DumpTop5NetworkFlows()
	// encoder.DumpTop5TransportFlows()

	if c.config.EncoderConfig.Debug {
		c.printErrors()
	}

	if logFileHandle != nil {
		err := logFileHandle.Close()
		if err != nil {
			c.printStdOut("failed to close logfile:", err)
		}
	}

	clearLine()
	c.printlnStdOut("done.")
}

// handleSignals catches signals and runs the cleanup
// SIGQUIT is not catched, to allow debugging by producing a stack and goroutine trace.
func (c *Collector) handleSignals() {
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)

	// start signal handler and cleanup routine
	go func() {
		sig := <-sigs

		fmt.Println("received signal:", sig)
		fmt.Println("exiting")

		go func() {
			sig := <-sigs
			fmt.Println("force quitting, signal:", sig)
			os.Exit(0)
		}()

		c.cleanup(true)
		os.Exit(0)
	}()
}

// to decode incoming packets in parallel
// they are passed to several worker goroutines in round robin style.
func (c *Collector) handlePacket(p *packet) {

	// make it work for 1 worker only, can be used for debugging
	if len(c.workers) == 1 {
		c.workers[0] <- p
		return
	}

	// send the packetInfo to the encoder routine
	c.workers[c.next] <- p

	// increment or reset next
	if c.config.Workers >= c.next+1 {
		// reset
		c.next = 1
	} else {
		c.next++
	}
}

// to decode incoming packets in parallel
// they are passed to several worker goroutines in round robin style.
func (c *Collector) handlePacketTimeout(p *packet) {

	select {
	// send the packetInfo to the encoder routine
	case c.workers[c.next] <- p:
	case <-time.After(3 * time.Second):
		p := gopacket.NewPacket(p.data, c.config.BaseLayer, gopacket.Default)
		fmt.Println("handle packet timeout", p.NetworkLayer().NetworkFlow(), p.TransportLayer().TransportFlow())
	}

	// increment or reset next
	if c.config.Workers >= c.next+1 {
		// reset
		c.next = 1
	} else {
		c.next++
	}
}

// print errors to stdout in red.
func (c *Collector) printErrors() {
	c.errorMap.Lock()
	if len(c.errorMap.Items) > 0 {
		fmt.Println("")
		for msg, count := range c.errorMap.Items {
			fmt.Println(ansi.Red, "[ERROR]", msg, "COUNT:", count, ansi.Reset)
		}
		fmt.Println("")
	}
	c.errorMap.Unlock()
}

// closes the logfile for errors.
func (c *Collector) closeErrorLogFile() {

	// append  stats
	var stats string
	for msg, count := range c.errorMap.Items {
		stats += fmt.Sprintln("[ERROR]", msg, "COUNT:", count)
	}

	_, err := c.errorLogFile.WriteString(stats)
	if err != nil {
		panic(err)
	}

	// sync
	err = c.errorLogFile.Sync()
	if err != nil {
		panic(err)
	}

	// close file handle
	err = c.errorLogFile.Close()
	if err != nil {
		panic(err)
	}
}

// Stats prints collector statistics.
func (c *Collector) Stats() {

	var target io.Writer
	if c.config.Quiet {
		target = logFileHandle
	} else {
		target = os.Stderr
	}

	rows := [][]string{}
	c.unknownProtosAtomic.Lock()
	for k, v := range c.allProtosAtomic.Items {
		if k == "Payload" {
			rows = append(rows, []string{k, fmt.Sprint(v), share(v, c.numPackets)})
			continue
		}
		if _, ok := c.unknownProtosAtomic.Items[k]; ok {
			rows = append(rows, []string{"*" + k, fmt.Sprint(v), share(v, c.numPackets)})
		} else {
			rows = append(rows, []string{k, fmt.Sprint(v), share(v, c.numPackets)})
		}
	}
	numUnknown := len(c.allProtosAtomic.Items)
	c.unknownProtosAtomic.Unlock()
	tui.Table(target, []string{"Layer", "NumRecords", "Share"}, rows)

	// print legend if there are unknown protos
	// -1 for "Payload" layer
	if numUnknown-1 > 0 {
		if !c.config.Quiet {
			fmt.Println("* protocol supported by gopacket, but not implemented in netcap")
		}
	}

	if len(encoder.CustomEncoders) > 0 {
		rows = [][]string{}
		for _, e := range encoder.CustomEncoders {
			rows = append(rows, []string{e.Name, strconv.FormatInt(e.NumRecords(), 10), share(e.NumRecords(), c.numPackets)})
		}
		tui.Table(target, []string{"CustomEncoder", "NumRecords", "Share"}, rows)
	}

	res := "\n-> total bytes of data written to disk: " + humanize.Bytes(uint64(c.totalBytesWritten)) + "\n"
	if c.unkownPcapWriterAtomic.count > 0 {
		res += "-> " + share(c.unkownPcapWriterAtomic.count, c.numPackets) + " of packets (" + strconv.FormatInt(c.unkownPcapWriterAtomic.count, 10) + ") written to unknown.pcap\n"
	}

	if c.errorsPcapWriterAtomic.count > 0 {
		res += "-> " + share(c.errorsPcapWriterAtomic.count, c.numPackets) + " of packets (" + strconv.FormatInt(c.errorsPcapWriterAtomic.count, 10) + ") written to errors.pcap\n"
	}

	fmt.Fprintln(target, res)
}

// updates the progress indicator and writes to stdout.
func (c *Collector) printProgress() {

	// must be locked, otherwise a race occurs when sending a SIGINT
	//  and triggering wg.Wait() in another goroutine...
	c.statMutex.Lock()
	// increment wait group for packet processing
	c.wg.Add(1)
	c.statMutex.Unlock()

	// increment atomic packet counter
	atomic.AddInt64(&c.current, 1)
	if c.current%100 == 0 {
		if !c.config.Quiet {
			// using a strings.Builder for assembling string for performance
			// TODO: could be refactored to use a byte slice with a fixed length instead
			// TODO: add Builder to collector and flush it every cycle to reduce allocations
			// also only print flows and collections when the corresponding encoders are active
			var b strings.Builder
			b.Grow(65)
			b.WriteString("decoding packets... (")
			b.WriteString(utils.Progress(c.current, c.numPackets))
			b.WriteString(") flows: ")
			b.WriteString(strconv.Itoa(encoder.Flows.Size()))
			b.WriteString(" connections: ")
			b.WriteString(strconv.Itoa(encoder.Connections.Size()))
			b.WriteString(" profiles: ")
			b.WriteString(strconv.Itoa(encoder.Profiles.Size()))
			b.WriteString(" packets: ")
			b.WriteString(strconv.Itoa(int(c.current)))

			// print
			clearLine()
			os.Stdout.WriteString(b.String())
		}
	}
}

// Init sets up the collector and starts the configured number of workers
// must be called prior to usage of the collector instance.
func (c *Collector) Init() (err error) {

	// start workers
	c.workers = c.initWorkers()
	c.printlnStdOut("spawned", c.config.Workers, "workers")

	// create full output directory path if set
	if c.config.EncoderConfig.Out != "" {
		err = os.MkdirAll(c.config.EncoderConfig.Out, c.config.OutDirPermission)
		if err != nil {
			return err
		}
	}

	// set file storage
	encoder.FileStorage = c.config.FileStorage

	// init deep packet inspection
	if c.config.DPI {
		dpi.Init()
	}

	// initialize resolvers
	resolvers.Init(c.config.ResolverConfig, c.config.Quiet)

	if c.config.ResolverConfig.LocalDNS {
		encoder.LocalDNS = true
	}

	encoder.SetConfig(c.config.EncoderConfig)

	// set quiet mode for other subpackages
	encoder.Quiet = c.config.Quiet

	if logFileHandle == nil && c.config.Quiet {
		err = c.InitLogging()
		if err != nil {
			return err
		}
	}

	// initialize encoders
	encoder.InitLayerEncoders(c.config.EncoderConfig, c.config.Quiet)
	encoder.InitCustomEncoders(c.config.EncoderConfig, c.config.Quiet)

	// set payload capture
	encoder.CapturePayload = c.config.EncoderConfig.IncludePayloads

	// set pointer of collectors atomic counter map in encoder pkg
	encoder.SetErrorMap(c.errorMap)

	// create pcap files for packets
	// with unknown protocols or errors while decoding
	if err := c.createUnknownPcap(); err != nil {
		log.Fatal("failed to create pcap file for unkown packets: ", err)
	}
	if err := c.createErrorsPcap(); err != nil {
		log.Fatal("failed to create pcap decoding errors file: ", err)
	}

	// handle signal for a clean exit
	c.handleSignals()

	if c.config.FreeOSMem != 0 {
		fmt.Println("will free the OS memory every", c.config.FreeOSMem, "minutes")
		go c.FreeOSMemory()
	}

	// create log file
	c.errorLogFile, err = os.Create(filepath.Join(c.config.EncoderConfig.Out, "errors.log"))

	return
}

// GetNumPackets returns the current number of processed packets
func (c *Collector) GetNumPackets() int64 {
	return atomic.LoadInt64(&c.current)
}

// FreeOSMemory forces freeing memory
func (c *Collector) FreeOSMemory() {
	for {
		select {
		case <-time.After(time.Duration(c.config.FreeOSMem) * time.Minute):
			debug.FreeOSMemory()
		}
	}
}

// PrintConfiguration dumps the current collector config to stdout
func (c *Collector) PrintConfiguration() {

	// ensure the logfile handle gets openend
	err := c.InitLogging()
	if err != nil {
		log.Fatal("failed to open logfile:", err)
	}

	var target io.Writer
	if c.config.Quiet {
		target = logFileHandle
	} else {
		target = os.Stdout
	}

	netcap.FPrintBuildInfo(target)
	fmt.Fprintln(target, "> PID:", os.Getpid())

	// print configuration as table
	tui.Table(target, []string{"Setting", "Value"}, [][]string{
		{"Workers", strconv.Itoa(c.config.Workers)},
		{"MemBuffer", strconv.FormatBool(c.config.EncoderConfig.Buffer)},
		{"MemBufferSize", strconv.Itoa(c.config.EncoderConfig.MemBufferSize) + " bytes"},
		{"Compression", strconv.FormatBool(c.config.EncoderConfig.Compression)},
		{"PacketBuffer", strconv.Itoa(c.config.PacketBufferSize) + " packets"},
		{"PacketContext", strconv.FormatBool(c.config.EncoderConfig.AddContext)},
		{"Payloads", strconv.FormatBool(c.config.EncoderConfig.IncludePayloads)},
		{"FileStorage", c.config.FileStorage},
	})
	fmt.Fprintln(target) // add a newline
}

// InitLogging can be used to open the logfile before calling Init()
// this is used to be able to dump the collector configuration into the netcap.log in quiet mode
// following calls to Init() will not open the filehandle again
func (c *Collector) InitLogging() error {

	// prevent reopen
	if logFileHandle != nil {
		return nil
	}

	var err error
	logFileHandle, err = os.OpenFile("netcap.log", os.O_CREATE|os.O_TRUNC|os.O_WRONLY, c.config.OutDirPermission)
	if err != nil {
		return err
	}
	return nil
}

// Stop will halt packet collection and wait for all processing to finish
func (c *Collector) Stop() {
	c.cleanup(false)
}

// Stop will halt packet collection immediately without waiting for processing to finish
func (c *Collector) ForceStop() {
	c.cleanup(true)
}
