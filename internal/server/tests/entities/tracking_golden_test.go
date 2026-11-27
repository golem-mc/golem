package entities

import (
	"encoding/hex"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
)

// TestTrackingGolden records the entity packets two clients receive while one
// walks across a field of displays; GOLDEN=write rewrites the file.
func TestTrackingGolden(t *testing.T) {
	t.Parallel()
	cfg := servertest.HostileTestConfig()
	srv, connect := servertest.Start(t, cfg)
	walker := connect().Join("Walker")
	still := connect().Join("Still")
	servertest.HoldTicks(t, srv)
	for i := range 30 {
		srv.RunLine(fmt.Sprintf("summon block_display %d 70 0.5 {UUID:[I;1,2,3,%d]}", i*4, i+1))
	}
	var log strings.Builder
	record := func(who string, tick int) func(int32, []byte) bool {
		return func(id int32, data []byte) bool {
			switch id {
			case protocol.IDPlaySpawnEntity, protocol.IDPlayRemoveEntities, protocol.IDPlayEntityPosition,
				protocol.IDPlayTeleportEntity, protocol.IDPlayEntityMetadata, protocol.IDPlaySetEntityMotion:
				fmt.Fprintf(&log, "%s %d %02x %s\n", who, tick, id, hex.EncodeToString(data))
			}
			return false
		}
	}
	for tick := range 50 {
		x := float64(tick) * 3
		walker.MoveTo(x, float64(world.GroundLevel), 0.5, 0, 0)
		walker.ReadTicks(srv, 1, record("W", tick))
		still.ReadTicks(srv, 0, record("S", tick))
	}
	const file = "testdata/tracking_golden.txt"
	if os.Getenv("GOLDEN") == "write" {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(file, []byte(log.String()), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if string(want) != log.String() {
		t.Errorf("packets differ from %s (%d bytes, want %d)", file, log.Len(), len(want))
	}
}
