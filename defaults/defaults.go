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

package defaults

import (
	"compress/flate"
	"time"
)

const (
	// BufferSize is the size for memory buffering before feeding data into compressor.
	BufferSize = 1024 * 1024 * 12 // 12 MB

	// PacketBuffer is the size of the channel for feeding packets into workers.
	PacketBuffer = 100

	// SnapLen is the default length for an ethernet frame:
	// 1500 Ethernet MTU + 14 bytes Ethernet header.
	SnapLen = 1514

	// ConnFlushInterval configures how often the connections are flushed for Flow and Connection audit record generation.
	// TODO: refactor to flush periodically, instead of every n packets?
	ConnFlushInterval = 0

	// FlowFlushInterval configures how often the connections are flushed for Flow and Connection audit record generation.
	// TODO: refactor to flush periodically, instead of every n packets?
	FlowFlushInterval = 0

	// ConnTimeOut will be used to set age threshold if the corresponding FlushInterval > 0.
	ConnTimeOut = 0 * time.Second

	// FlowTimeOut will be used to set age threshold if the corresponding FlushInterval > 0.
	FlowTimeOut = 0 * time.Second

	// CompressionBlockSize will determine the block size used for parallel compression.
	CompressionBlockSize = 1024 * 1024 * 1 // 1 MB

	// CompressionLevel will determine the compression level to use by default.
	CompressionLevel = flate.BestSpeed

	// TCP Stream Reassembly:
	// default settings are meant to be forgiving in terms of TCP state machine correctness
	// in order to capture as much information as possible.

	// ReassemblyTimeout How long to wait for remaining open streams to close, before initiating teardown.
	ReassemblyTimeout = 5 * time.Second

	// FlushEvery TODO: refactor to flush periodically, instead of every n packets?
	// controls how often collected reassembly data is flushed to consumers.
	FlushEvery = 100

	// ClosePendingTimeout Close streams with pending bytes after.
	ClosePendingTimeout = 1 * time.Hour

	// CloseInactiveTimeout Close inactive streams after.
	CloseInactiveTimeout = 1 * time.Hour

	// AllowMissingInit TCP State Machine.
	AllowMissingInit = true

	// DefragIPv4 controls defragmentation for IPv4.
	DefragIPv4 = true

	// NoOptCheck controls TCP option checking for the reassembly state machine.
	NoOptCheck = true

	// Checksum controls whether the TCP checksum shall be validated.
	Checksum = false

	// IgnoreFSMErr controls if TCP state machine errors should be ignored.
	IgnoreFSMErr = true

	// FileStorage is the default location for storing extracted files.
	FileStorage = "files"

	// DirectoryPermission = 0o755
	DirectoryPermission = 0o700
	FilePermission      = 0o700
)
