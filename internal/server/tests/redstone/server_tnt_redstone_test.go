package redstone

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"

	"github.com/golem-mc/golem/internal/protocol"
)

// The redstone-primed TNT tests: a TNT block that redstone powers must be
// taken out with the normal flags-3 removal — so water flows in, a wire or
// door resting on it reacts and an observer watching it fires — and replaced
// by a primed body shown to the client.
// vanilla: TntBlock.neighborChanged

// TestPressurePlatePrimesTNTBesideIt stands four TNT blocks around the block
// a pressure plate sits on, then steps a player onto the plate: every block
// must go to air, a primed body must stand in its place and be shown to the
// client, and the four must explode at the end of the fuse.
func TestPressurePlatePrimesTNTBesideIt(t *testing.T) {
	t.Parallel()
	srv, c, _ := servertest.JoinAs(t, servertest.BlockTestConfig(), "Plater")
	servertest.HoldTicks(t, srv)

	const plateX, plateY, plateZ = 12, 64, 0
	if !srv.SetBlock(plateX, plateY, plateZ, servertest.BlockState(t, "minecraft:stone")) {
		t.Fatal("could not place the plate's support")
	}
	if !srv.SetBlock(plateX, plateY+1, plateZ, servertest.BlockState(t, "minecraft:oak_pressure_plate")) {
		t.Fatal("could not place the plate")
	}
	tnt := servertest.BlockState(t, "minecraft:tnt")
	cells := [][3]int32{
		{plateX - 1, plateY, plateZ},
		{plateX + 1, plateY, plateZ},
		{plateX, plateY, plateZ - 1},
		{plateX, plateY, plateZ + 1},
	}
	for _, cell := range cells {
		if !srv.SetBlock(cell[0], cell[1], cell[2], tnt) {
			t.Fatalf("could not place the TNT at (%d, %d, %d)", cell[0], cell[1], cell[2])
		}
	}

	c.MoveTo(float64(plateX)+0.5, float64(plateY+1), float64(plateZ)+0.5, 0, 0)
	c.Settle()

	spawns := 0
	shown := c.PumpTicks(srv, 30, func(id int32, data []byte) bool {
		if id == protocol.IDPlaySpawnEntity {
			if _, typ := servertest.SpawnEntityHead(data); typ == protocol.EntityTypeTnt {
				spawns++
			}
		}
		return spawns == 4
	})
	if !shown {
		t.Errorf("the client was shown %d primed TNT, want the four beside the plate", spawns)
	}
	for _, cell := range cells {
		if got := srv.BlockAt(cell[0], cell[1], cell[2]); got != 0 {
			t.Errorf("the TNT at (%d, %d, %d) = %d, want air after the prime", cell[0], cell[1], cell[2], got)
		}
	}
	if n := len(srv.TNTsInDimension("minecraft:overworld")); n != 4 {
		t.Fatalf("primed TNT in the overworld = %d, want the four beside the plate", n)
	}

	exploded := false
	c.PumpTicks(srv, 100, func(id int32, data []byte) bool {
		if id == protocol.IDPlayExplosion {
			exploded = true
		}
		return false
	})
	if !exploded {
		t.Error("the primed TNT never exploded at the end of the fuse")
	}
}
