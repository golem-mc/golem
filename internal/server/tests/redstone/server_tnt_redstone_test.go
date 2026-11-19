package redstone

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/registry"
)

// The redstone-primed TNT tests: a TNT block that redstone powers must be
// taken out with the normal flags-3 removal — so water flows in, a wire or
// door resting on it reacts and an observer watching it fires — and replaced
// by a primed body shown to the client.
// vanilla: TntBlock.neighborChanged

// primeTNTX/Y/Z is where the neighbour tests put their TNT, near spawn so the
// player's own chunks keep it loaded.
const primeTNTX, primeTNTY, primeTNTZ = 6, 64, 5

// TestPressurePlatePrimesTNTBesideIt stands four TNT blocks around the block
// a pressure plate sits on, then steps a player onto the plate: every block
// must go to air, a primed body must stand in its place and be shown to the
// client, and the four must explode at the end of the fuse.
func TestPressurePlatePrimesTNTBesideIt(t *testing.T) {
	t.Parallel()
	srv, c, _ := servertest.JoinAs(t, servertest.BlockTestConfig(), "Plater")
	srv.StopTick()

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

	srv.StartTick()
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

// TestWaterFlowsIntoTheCellPrimedTNTLeft lays a raw water source beside a TNT
// block and primes it with a redstone block: the removal must update its
// neighbours, so the water books its flow and runs into the emptied cell. The
// water is set without the wake a placement runs, so only the removal's own
// update can start it flowing.
func TestWaterFlowsIntoTheCellPrimedTNTLeft(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)

	if !srv.SetBlock(primeTNTX, primeTNTY, primeTNTZ, servertest.BlockState(t, "minecraft:tnt")) {
		t.Fatal("could not place the TNT")
	}
	if !srv.SetBlock(primeTNTX, primeTNTY, primeTNTZ+1, servertest.MustState(t, "minecraft:water", nil)) {
		t.Fatal("could not place the water")
	}
	servertest.Setblock(t, c, primeTNTX-1, primeTNTY, primeTNTZ, "redstone_block")
	c.Settle()

	servertest.WaitForTicks(t, srv, 40, func() bool {
		name, _, ok := registry.BlockStateName(srv.BlockAt(primeTNTX, primeTNTY, primeTNTZ))
		return ok && name == "minecraft:water"
	})
}

// TestRedstoneWireOnPrimedTNTBreaks rests a redstone wire on a TNT block and
// primes it with a redstone block beside it: the removal must update the wire,
// whose support is gone, so it breaks.
func TestRedstoneWireOnPrimedTNTBreaks(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)

	if !srv.SetBlock(primeTNTX, primeTNTY, primeTNTZ, servertest.BlockState(t, "minecraft:tnt")) {
		t.Fatal("could not place the TNT")
	}
	if !srv.SetBlock(primeTNTX, primeTNTY+1, primeTNTZ, servertest.BlockState(t, "minecraft:redstone_wire")) {
		t.Fatal("could not place the wire")
	}
	servertest.Setblock(t, c, primeTNTX-1, primeTNTY, primeTNTZ, "redstone_block")
	c.Settle()

	servertest.WaitForTicks(t, srv, 40, func() bool {
		name, _, ok := registry.BlockStateName(srv.BlockAt(primeTNTX, primeTNTY+1, primeTNTZ))
		return ok && name == "minecraft:air"
	})
}

// TestLeverOnTNTPrimesPastABlockWrite stops the tick, pulls a lever mounted
// on a TNT block, then writes an unrelated block before the tick resumes: the
// pull's own wake primes the TNT, and the later write's wake must not clear
// the queued body before the server spawns it.
func TestLeverOnTNTPrimesPastABlockWrite(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)
	srv.StopTick()

	lever, ok := registry.FindState("minecraft:lever", map[string]string{"face": "floor", "facing": "north"})
	if !ok {
		t.Fatal("the block registry has no floor lever facing north")
	}
	if !srv.SetBlock(5, 64, 0, servertest.BlockState(t, "minecraft:tnt")) {
		t.Fatal("could not place the TNT")
	}
	if !srv.SetBlock(5, 65, 0, lever) {
		t.Fatal("could not place the lever on the TNT")
	}

	// The click pulls the lever, which powers and primes the TNT below it.
	c.PlaceBlockAtCursor(5, 65, 0, protocol.FaceTop, 0.5, 0.5, 0.5)
	// An unrelated block write before the tick resumes must not drop the
	// queued primed body.
	servertest.Setblock(t, c, 5, 70, 0, "stone")

	srv.StartTick()
	servertest.WaitForTicks(t, srv, 10, func() bool {
		return len(srv.TNTsInDimension("minecraft:overworld")) > 0
	})
	if got := srv.BlockAt(5, 64, 0); got != 0 {
		t.Errorf("the TNT = %d, want air after the pull primed it", got)
	}
}

// TestDoorOnPrimedTNTBreaks rests a door on a TNT block and primes it with a
// redstone block beside it: the removal's shape update must break the door,
// which lost the block that held it up.
func TestDoorOnPrimedTNTBreaks(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)

	door, ok := registry.FindState("minecraft:oak_door", map[string]string{"half": "lower", "facing": "north"})
	if !ok {
		t.Fatal("the block registry has no lower oak door facing north")
	}
	if !srv.SetBlock(primeTNTX, primeTNTY, primeTNTZ, servertest.BlockState(t, "minecraft:tnt")) {
		t.Fatal("could not place the TNT")
	}
	if !srv.SetBlock(primeTNTX, primeTNTY+1, primeTNTZ, door) {
		t.Fatal("could not place the door")
	}
	servertest.Setblock(t, c, primeTNTX-1, primeTNTY, primeTNTZ, "redstone_block")
	c.Settle()

	servertest.WaitForTicks(t, srv, 40, func() bool {
		name, _, ok := registry.BlockStateName(srv.BlockAt(primeTNTX, primeTNTY+1, primeTNTZ))
		return ok && name == "minecraft:air"
	})
}

// TestObserverWatchingPrimedTNTFires points an observer at a TNT block and
// primes it with a redstone block beside it: the removal's shape update must
// book the observer's pulse.
func TestObserverWatchingPrimedTNTFires(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)

	observer, ok := registry.FindState("minecraft:observer", map[string]string{"facing": "north"})
	if !ok {
		t.Fatal("the block registry has no observer facing north")
	}
	if !srv.SetBlock(primeTNTX, primeTNTY, primeTNTZ, servertest.BlockState(t, "minecraft:tnt")) {
		t.Fatal("could not place the TNT")
	}
	// The observer south of the TNT, watching north into it.
	if !srv.SetBlock(primeTNTX, primeTNTY, primeTNTZ+1, observer) {
		t.Fatal("could not place the observer")
	}
	servertest.Setblock(t, c, primeTNTX-1, primeTNTY, primeTNTZ, "redstone_block")

	fired := c.PumpTicks(srv, 40, func(id int32, data []byte) bool {
		if id != protocol.IDPlayBlockUpdate {
			return false
		}
		r := protocol.NewReader(data)
		x, y, z := r.ReadPosition()
		if x != primeTNTX || y != primeTNTY || z != primeTNTZ+1 {
			return false
		}
		_, props := servertest.StateProps(uint16(r.ReadVarInt()))
		return props["powered"] == "true"
	})
	if !fired {
		t.Error("the observer watching the primed TNT never pulsed")
	}
}
