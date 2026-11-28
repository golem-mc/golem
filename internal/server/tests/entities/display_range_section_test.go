package entities

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
)

// A display stays shown while a player walks inside one section, even past
// its tracking range, and is removed within two ticks of the step into the
// next section.
// vanilla: ChunkMap.tick (movedPlayers)
func TestDisplayRangeIsRecheckedWhenTheWalkerChangesSection(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.HostileTestConfig(), "Walker")
	servertest.HoldTicks(t, srv)
	srv.RunLine("summon block_display 0 70 0.5")
	servertest.RunTicks(t, srv, 2)
	removals := 0
	count := func(id int32, _ []byte) bool {
		if id == protocol.IDPlayRemoveEntities {
			removals++
		}
		return false
	}
	c.ReadTicks(srv, 0, count)
	step := func(x float64) {
		c.MoveTo(x, float64(world.GroundLevel), 0.5, 0, 0)
		c.ReadTicks(srv, 1, count)
	}
	// 64 is the last block inside the range: the walk enters x section 4 there.
	for x := 1.0; x <= 64; x += 3 {
		step(x)
	}
	step(64)
	// Past the range edge, inside the same section.
	for x := 65.0; x < 80; x += 3 {
		step(x)
		if removals != 0 {
			t.Fatalf("display removed at x=%v, inside the section the range was last checked in", x)
		}
	}
	step(80)
	c.ReadTicks(srv, 2, count)
	if removals != 1 {
		t.Fatalf("removals after stepping into the next section = %d, want 1", removals)
	}
}
