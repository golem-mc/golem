package server

import (
	"log/slog"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/pluginruntime"
)

// BenchmarkConnectionPacketHooks measures decoding and the no-listener hook gates.
func BenchmarkConnectionPacketHooks(b *testing.B) {
	for _, enabled := range []bool{false, true} {
		name := "no-plugins"
		if enabled {
			name = "no-listeners"
		}
		b.Run(name, func(b *testing.B) {
			s := &Server{Config: config.Default()}
			if enabled {
				s.plugins = pluginruntime.New(nil, nil)
			}
			c := &client{srv: s, log: slog.New(slog.DiscardHandler)}
			var settings, payload, pack protocol.Buffer
			settings.PutString("en_us")
			settings.PutI8(10)
			settings.PutVarInt(0)
			settings.PutBool(true)
			settings.PutU8(127)
			settings.PutVarInt(1)
			settings.PutBool(false)
			settings.PutBool(true)
			settings.PutVarInt(0)
			payload.PutString("bench:channel")
			payload.PutRaw([]byte{1, 2, 3})
			pack.PutUUID(protocol.OfflineUUID("pack"))
			pack.PutVarInt(3)
			for _, packet := range []struct {
				name   string
				data   []byte
				handle func(*protocol.Reader) error
			}{
				{"settings", settings.Bytes(), c.handleClientInformation},
				{"payload", payload.Bytes(), c.logPluginMessage},
				{"pack", pack.Bytes(), c.handleResourcePackResponse},
			} {
				b.Run(packet.name, func(b *testing.B) {
					b.ReportAllocs()
					for b.Loop() {
						if err := packet.handle(protocol.NewReader(packet.data)); err != nil {
							b.Fatal(err)
						}
					}
				})
			}
		})
	}
}
