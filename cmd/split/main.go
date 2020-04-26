package main

import (
	"fmt"
	"github.com/dreadl0ck/gopacket"
	"github.com/dreadl0ck/gopacket/layers"
	"github.com/dreadl0ck/netcap/collector"
	"github.com/dustin/go-humanize"
	"github.com/namsral/flag"
	"io"
	"log"
	"os"
)

var (
	flagInput = flag.String("read", "", "input pcap file")
)

func main() {

	// stat input file
	stat, err := os.Stat(*flagInput)
	if err != nil {
		log.Fatal("failed to open pcap:", err)
	}

	// file exists.
	println("opening", *flagInput+" | size:", humanize.Bytes(uint64(stat.Size())))

	// TODO: display progress
	// display total packet count
	//print("counting packets...")
	//start := time.Now()
	//c.numPackets, err = countPackets(path)
	//if err != nil {
	//	return err
	//}
	//clearLine()
	//fmt.Println("counting packets... done.", c.numPackets, "packets found in", time.Since(start))

	r, f, err := collector.OpenPCAP(*flagInput)
	if err != nil {
		log.Fatal("failed to open pcap:", err)
	}
	defer f.Close()

	fmt.Println("detected link type:", r.LinkType())

	print("processing packets... ")
	for {

		// fetch the next packetdata and packetheader
		// for pcap, currently ZeroCopyReadPacketData() is not supported
		data, ci, err := r.ReadPacketData()
		if err != nil {
			if err == io.EOF {
				break
			}
			log.Fatal("reading pcaps failed: ", err)
		}

		// TODO: parse timestamp from ci
		//  create a directory for each day
		//  and write the corresponding packets there
		p := gopacket.NewPacket(data, layers.LayerTypeEthernet, gopacket.Lazy)
		fmt.Println(p, ci)
	}
}