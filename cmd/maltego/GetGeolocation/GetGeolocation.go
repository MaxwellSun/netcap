package main

import (
	"flag"
	"github.com/dreadl0ck/netcap"
	maltego "github.com/dreadl0ck/netcap/cmd/maltego/maltego"
	"fmt"
	"github.com/dreadl0ck/netcap/types"
	"github.com/gogo/protobuf/proto"
	"io"
	"log"
	"net"
	"os"
	"strconv"

	//"strconv"
	"strings"
)

var (
	flagVersion = flag.Bool("version", false, "print version and exit")
)

func main() {

	lt := maltego.ParseLocalArguments(os.Args)
	profilesFile := lt.Values["path"]
	mac := lt.Values["mac"]
	ipaddr := lt.Values["ipaddr"]

	// print version and exit
	if *flagVersion {
		fmt.Println(netcap.Version)
		os.Exit(0)
	}

	f, err := os.Open(profilesFile)
	if err != nil {
		log.Fatal(err)
	}

	// check if its an audit record file
	if !strings.HasSuffix(f.Name(), ".ncap.gz") && !strings.HasSuffix(f.Name(), ".ncap") {
		log.Fatal("input file must be an audit record file")
	}

	r, err := netcap.Open(profilesFile, netcap.DefaultBufferSize)
	if err != nil {
		panic(err)
	}

	// read netcap header
	header := r.ReadHeader()
	if header.Type != types.Type_NC_DeviceProfile {
		panic("file does not contain DeviceProfile records: " + header.Type.String())
	}

	var (
		profile = new(types.DeviceProfile)
		pm  proto.Message
		ok  bool
		trx = maltego.MaltegoTransform{}
	)
	pm = profile

	if _, ok = pm.(types.AuditRecord); !ok {
		panic("type does not implement types.AuditRecord interface")
	}

	for {
		err := r.Next(profile)
		if err == io.EOF || err == io.ErrUnexpectedEOF {
			break
		} else if err != nil {
			panic(err)
		}

		if profile.MacAddr == mac {

			for _, ip := range profile.Contacts {

				var (
					ent *maltego.MaltegoEntityObj
					addr = net.ParseIP(ip.Addr)
				)
				if addr == nil {
					fmt.Println(err)
					continue
				}
				if v4 := addr.To4(); v4 == nil {
					// v6
					ent = trx.AddEntity("maltego.IPv6Address", ip.Addr)
					ent.SetType("maltego.IPv6Address")
				} else {
					ent = trx.AddEntity("maltego.IPv4Address", ip.Addr)
					ent.SetType("maltego.IPv4Address")
				}
				ent.SetValue(ip.Addr)

				di := "<h3>Heading</h3><p>Timestamp: " + profile.Timestamp + "</p>"
				ent.AddDisplayInformation(di, "Other")

				ent.AddProperty("numPackets", "Num Packets", "strict", strconv.FormatInt(profile.NumPackets, 10))

				ent.SetLinkLabel("GetDeviceIPs")
				ent.SetLinkColor("#000000")

				note := strings.ReplaceAll(proto.MarshalTextString(ip), "\"", "'")
				note = strings.ReplaceAll(note, "<", "")
				note = strings.ReplaceAll(note, ">", "")
				ent.SetNote(note)
			}
		}
	}

	trx.AddUIMessage("completed!","Inform")
	fmt.Println(trx.ReturnOutput())
}