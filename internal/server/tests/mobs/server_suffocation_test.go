package mobs

import (
	"github.com/golem-mc/golem/internal/server/servertest"

	"testing"
)

// The walls against the bodies that are not players: a mob whose eyes a
// block fills is crushed by it — vanilla LivingEntity.baseTick's isInWall,
// the same bite a buried player already takes — and a falling block that
// lands on one becomes the block in its cell, which is what leaves the body
// buried in the first place. A mob is not nudged out of it, though; the
// moveTowardsClosestSpace the items take is the player's and the item's
// alone, so the buried mob's way out is the death the crush ends in.

// TestABuriedMobSuffocates checks the crush a buried player already takes,
// on the mob's body: a block filling the cells a chicken's box stands in —
// its eyes included — bites a point of health every hurt cooldown, and
// taking the wall away stops the bites. The stone stands in a ring so the
// buried body has nowhere to walk out to.
func TestABuriedMobSuffocates(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.SurvivalTestConfig())
	// The mob pass runs by hand: the tick that would have bitten is read
	// as the client hears it, and nothing wanders the chicken off between
	// the ticks the test counts.
	srv.StopTick()
	t.Cleanup(srv.HoldChunk(10, 60))

	c := connect().Join("Waller")

	chickenID := srv.SpawnMob("minecraft:chicken", 10.5, 64, 60.5)
	// A chicken in a one-block pit strolls out of it; the AI is off.
	srv.SetMobNoAI(chickenID)
	c.Settle()

	// The chicken's own cell filled — the crush is the eyes' — and the
	// four about it, so the buried body cannot walk its way out of the
	// wall's reach.
	stone := servertest.BlockState(t, "minecraft:stone")
	ring := [][3]int32{
		{10, 64, 60}, {9, 64, 60}, {11, 64, 60}, {10, 64, 59}, {10, 64, 61},
	}
	for _, pos := range ring {
		if !srv.SetBlock(pos[0], pos[1], pos[2], stone) {
			t.Fatalf("the wall's chunk was not loaded for (%d, %d, %d)", pos[0], pos[1], pos[2])
		}
	}

	srv.TickMobs(1)
	c.ExpectBodyDamage(chickenID, "minecraft:in_wall")

	srv.TickMobs(9)
	if n := servertest.DrainDamage(c); n != 0 {
		t.Errorf("%d damage events arrived inside the hurt cooldown, want none", n)
	}

	srv.TickMobs(1)
	c.ExpectBodyDamage(chickenID, "minecraft:in_wall")

	// The wall away, the bites stop: the buried body's eyes are in the
	// open air again, wherever it stands.
	for _, pos := range ring {
		if !srv.SetBlock(pos[0], pos[1], pos[2], 0) {
			t.Fatalf("the wall's chunk was not loaded for (%d, %d, %d)", pos[0], pos[1], pos[2])
		}
	}
	srv.TickMobs(20)
	if n := servertest.DrainDamage(c); n != 0 {
		t.Errorf("%d damage events arrived with the wall gone, want none", n)
	}
}

// TestABuriedMobSuffocatesInARetractedPiston checks the piston that
// vanilla declares a body-crusher only retracted (Blocks'
// NOT_EXTENDED_PISTON): the folded base is the full cube, while the
// extended one leaves its cell filled with an arm and a plate, which the
// eyes fit around.
func TestABuriedMobSuffocatesInARetractedPiston(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.SurvivalTestConfig())
	srv.StopTick()
	t.Cleanup(srv.HoldChunk(13, 60))

	c := connect().Join("Pistonne")

	// A chicken standing in a retracted piston's cell, and one standing
	// in an extended piston's, each ringed so they stay where the seals
	// put them; any bite below belongs to whichever the piston at its
	// cell is wrong about, so the expected bite names its chicken.
	retracted := srv.SpawnMob("minecraft:chicken", 10.5, 64, 60.5)
	srv.SpawnMob("minecraft:chicken", 13.5, 64, 60.5)
	c.Settle()

	stone := servertest.BlockState(t, "minecraft:stone")
	for _, pos := range [][3]int32{
		{9, 64, 60}, {11, 64, 60}, {10, 64, 59}, {10, 64, 61},
		{12, 64, 60}, {14, 64, 60}, {13, 64, 59}, {13, 64, 61},
	} {
		if !srv.SetBlock(pos[0], pos[1], pos[2], stone) {
			t.Fatalf("the seal's ring at (%d, %d, %d) met no loaded chunk", pos[0], pos[1], pos[2])
		}
	}
	if !srv.SetBlock(10, 64, 60, servertest.BlockStateWith(t, "minecraft:piston", "extended=false,facing=up")) {
		t.Fatal("the retracted piston's cell met no loaded chunk")
	}
	if !srv.SetBlock(13, 64, 60, servertest.BlockStateWith(t, "minecraft:piston", "extended=true,facing=up")) {
		t.Fatal("the extended piston's cell met no loaded chunk")
	}

	// The retracted base bites; silenced by clearing its cell — a body
	// whose eyes are in air cannot bite — the extended one takes the
	// whole of the remaining ticks without biting at all, its cell no
	// more than the base plate the eyes stand above.
	srv.TickMobs(1)
	c.ExpectBodyDamage(retracted, "minecraft:in_wall")
	if !srv.SetBlock(10, 64, 60, 0) {
		t.Fatal("the retracted piston's cell met no loaded chunk")
	}
	srv.TickMobs(24)
	if n := servertest.DrainDamage(c); n != 0 {
		t.Errorf("%d in_wall bites arrived, want none from the extended piston over 24 ticks", n)
	}
}
