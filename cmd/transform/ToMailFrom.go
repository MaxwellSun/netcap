package transform

import (
	maltego "github.com/dreadl0ck/netcap/maltego"
	"github.com/dreadl0ck/netcap/types"
)

func ToMailFrom() {
	maltego.POP3Transform(
		nil,
		func(lt maltego.LocalTransform, trx *maltego.MaltegoTransform, pop3 *types.POP3, min, max uint64, profilesFile string, ipaddr string) {
			if pop3.ClientIP == ipaddr {
				for _, m := range pop3.Mails {
					if m.From != "" {
						ent := trx.AddEntity("netcap.Email", m.From)
						ent.AddProperty("ipaddr", "IPAddress", "strict", ipaddr)
						ent.AddProperty("path", "Path", "strict", profilesFile)
					}
				}
			}
		},
	)
}
