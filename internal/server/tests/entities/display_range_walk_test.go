package entities

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
)

// A display that stays put is removed from a player who walks out of its
// tracking range and spawned afresh when the player walks back in.
// vanilla: ChunkMap.TrackedEntity.updatePlayer
func TestDisplayRespawnsAfterPlayerLeavesAndReentersRange(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.HostileTestConfig(), "Walker")
	servertest.HoldTicks(t, srv)
	srv.RunLine("summon block_display 0 70 0.5 {UUID:[I;1,2,3,4]}")
	// Until the tick that told them, so the walk starts from a known state.
	servertest.RunTicks(t, srv, 2)
	spawns, removals := 0, 0
	count := func(id int32, _ []byte) bool {
		switch id {
		case protocol.IDPlaySpawnEntity:
			spawns++
		case protocol.IDPlayRemoveEntities:
			removals++
		}
		return false
	}
	c.ReadTicks(srv, 0, count)
	if spawns != 1 {
		t.Fatalf("spawns standing beside the display = %d, want 1", spawns)
	}
	x := 0.0
	walk := func(step float64, until func() bool) {
		for range 200 {
			if until() {
				return
			}
			x += step
			c.MoveTo(x, float64(world.GroundLevel), 0.5, 0, 0)
			c.ReadTicks(srv, 1, count)
		}
	}
	walk(3, func() bool { return removals > 0 })
	if removals != 1 {
		t.Fatalf("removals after walking away = %d, want 1", removals)
	}
	walk(-3, func() bool { return spawns > 1 })
	if spawns != 2 {
		t.Fatalf("spawns after walking back = %d, want 2", spawns)
	}
}
