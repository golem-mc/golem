package blocks

import (
	"bytes"
	"strconv"
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/nbt"
)

func TestLargeBlockEntityBroadcastsReachTwoClientsInOrder(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.ViewDistance = config.MinDistance
	cfg.SimulationDistance = config.MinDistance
	cfg.NetworkCompressionThreshold = 256
	srv, a := servertest.Join(t, cfg, "Alice")
	b := servertest.DialServer(t, srv).Join("Bob")
	servertest.HoldTicks(t, srv)
	srv.RunLine("setblock 4 64 7 oak_sign")
	servertest.RunTicks(t, srv, 1)
	a.Settle()
	b.Settle()
	texts := []string{"before", strings.Repeat("shared broadcast ", 1200), "after"}
	for _, text := range texts {
		srv.RunLine("data merge block 4 64 7 {front_text:{messages:[" + strconv.Quote(text) + ",\"\",\"\",\"\"]}}")
		servertest.RunTicks(t, srv, 1)
	}
	var first [][]byte
	for viewer, c := range []*servertest.Client{a, b} {
		var got [][]byte
		c.SettleAndCollect(func(id int32, data []byte) {
			if id == protocol.IDPlayBlockEntityData {
				got = append(got, data)
			}
		})
		if len(got) != len(texts) {
			t.Fatalf("viewer %d received %d block entities, want %d", viewer, len(got), len(texts))
		}
		for i, data := range got {
			r := protocol.NewReader(data)
			x, y, z := r.ReadPosition()
			kind := r.ReadVarInt()
			if x != 4 || y != 64 || z != 7 || kind != protocol.BlockEntitySign || r.Err() != nil {
				t.Fatal("wrong block entity")
			}
			tag, _, err := nbt.DecodeNetwork(r.Rest())
			if err != nil {
				t.Fatal(err)
			}
			lines, _, _ := signSide(t, tag.(nbt.Compound), "front_text")
			if lines[0] != texts[i] {
				t.Fatalf("viewer %d, update %d: text changed or arrived out of order", viewer, i)
			}
			if viewer == 1 && !bytes.Equal(data, first[i]) {
				t.Fatalf("viewer payloads differ for update %d", i)
			}
		}
		if viewer == 0 {
			first = got
		}
	}
	a.Disconnect()
	b.Disconnect()
}
