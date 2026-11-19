package explosions

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// TestABlastWakesTheWaterBesideTheCrater blows a dirt block out from in
// front of a water source and holds that the water flows into the hole: the
// blast's removals wake their neighbours the way any block change does,
// vanilla's setBlock and its neighbour updates, so the source standing
// beside the crater learns the cell next to it opened up (issue #9).
func TestABlastWakesTheWaterBesideTheCrater(t *testing.T) {
	t.Parallel()
	srv, _ := servertest.Join(t, servertest.BlockTestConfig(), "Bystander")

	dirt := servertest.BlockState(t, "minecraft:dirt")
	water := servertest.BlockState(t, "minecraft:water")
	// A water source walled in by dirt on the side the blast comes from;
	// set without a wake, the source stands still until something wakes it.
	if !srv.SetBlock(6, 64, 0, water) || !srv.SetBlock(5, 64, 0, dirt) {
		t.Fatal("the world did not take the pool")
	}

	srv.SpawnTNT(3.5, 64, 0.5, 1)
	servertest.WaitFor(t, srv, 200, func() bool { return srv.BlockAt(5, 64, 0) != dirt })

	// Water spreads a cell every five ticks; a second is plenty.
	servertest.WaitFor(t, srv, 100, func() bool {
		name, _, _ := registry.BlockStateName(srv.BlockAt(5, 64, 0))
		return name == "minecraft:water"
	})
}
