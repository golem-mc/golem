package mobs

import (
	"github.com/golem-mc/golem/internal/server/servertest"

	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/registry"
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

// TestFallingSandBuriesAMobAndTheMobSuffocates checks the landing's own
// answer to the chicken under it: the falling block passes the body by — an
// entity is no place to land on — and places its block in the very cell the
// body's box fills, exactly as it would into empty air. The buried body is
// not pushed anywhere, and the crush finishes it off where it stands. The
// loop runs as the falling tests let it, and the shaft's walls keep the
// chicken under the fall for the ticks the fall needs.
func TestFallingSandBuriesAMobAndTheMobSuffocates(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.SurvivalTestConfig(), "Burier", servertest.OpsListing(t, "Burier"))

	sand, ok := registry.FindState("minecraft:sand", nil)
	if !ok {
		t.Fatal("the registry has no sand")
	}
	stone := servertest.BlockState(t, "minecraft:stone")
	// A one-cell shaft: a ring of stone a chicken has no way out of and a
	// fall has no way past, standing open at the top for the sand.
	for _, pos := range [][3]int32{
		{4, 64, 2}, {5, 64, 2}, {6, 64, 2}, {4, 64, 3}, {6, 64, 3},
		{4, 64, 4}, {5, 64, 4}, {6, 64, 4},
	} {
		if !srv.SetBlock(pos[0], pos[1], pos[2], stone) {
			t.Fatalf("the shaft's chunk was not loaded for (%d, %d, %d)", pos[0], pos[1], pos[2])
		}
	}
	chickenID := srv.SpawnMob("minecraft:chicken", 5.5, 64, 3.5)
	srv.SetMobNoAI(chickenID)
	c.Settle()

	// A sand block left hanging above the shaft.
	c.ChatCommand("setblock 5 66 3 sand")

	// The sand falls through the chicken — a falling block lands on
	// blocks, not bodies — and becomes the block where the chicken
	// stands.
	servertest.UntilTicks(t, srv, 100, "the sand landing", func() bool { return srv.BlockAt(5, 64, 3) == sand })

	// The trapped body stays in the fallen block's cell; the nudge out is
	// an item's and a player's, not a mob's.
	if x, _, z, ok := srv.MobPos(chickenID); !ok || x < 5 || x > 6 || z < 3 || z > 4 {
		t.Errorf("the buried chicken spilled out of its cell (x=%v z=%v ok=%v)", x, z, ok)
	}

	// The crush bites, and four health being a chicken's, the fourth bite
	// ends it where it stands.
	// BurialNoise must allow the landing's Add Transient Block while
	// keeping damage and death events visible to these assertions.
	c.ExpectBodyDamage(chickenID, "minecraft:in_wall")
	dead := pumpTicks(c, srv, 160, func(id int32) bool {
		return servertest.BurialNoise(id) || id == protocol.IDPlayDamageEvent
	}, func(id int32, data []byte) bool {
		if id != protocol.IDPlayEntityEvent {
			return false
		}
		r := protocol.NewReader(data)
		return r.ReadI32() == chickenID && r.ReadU8() == protocol.EntityStatusDeath
	})
	if !dead {
		t.Fatal("the chicken the falling sand buried never suffocated")
	}
}

// TestABuriedMobIsSparedInGlassAndLeaves checks the blocks vanilla never
// suffocates with however full their shapes: the glasses and the leaves
// are isSuffocating(Blocks::never)'s to exempt, so a body sealed in
// glass — placed by hand, by /fill or by leaves growing over it — takes no
// in_wall bite where the same seal of stone would have crushed it on the
// ten-tick rhythm.
func TestABuriedMobIsSparedInGlassAndLeaves(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.SurvivalTestConfig())
	srv.StopTick()
	t.Cleanup(srv.HoldChunk(5, 60))

	c := connect().Join("Gilder")

	// Three seals, one per exempt block, all in the held chunk: glass,
	// oak leaves and the white stained glass.
	seals := []struct {
		block string
		cx    int32
	}{
		{"minecraft:glass", 2},
		{"minecraft:oak_leaves", 5},
		{"minecraft:white_stained_glass", 8},
	}
	for _, seal := range seals {
		srv.SpawnMob("minecraft:chicken", float64(seal.cx)+0.5, 64, 60.5)
	}
	c.Settle()

	stone := servertest.BlockState(t, "minecraft:stone")
	for _, seal := range seals {
		// The chicken's own cell sealed in the exempt block, and the
		// four about it in stone, so the buried body cannot walk its
		// way out; a seal that stayed open would not prove the spare.
		state := servertest.BlockState(t, seal.block)
		if !srv.SetBlock(seal.cx, 64, 60, state) {
			t.Fatalf("the seal of %s at (%d, 64, 60) met no loaded chunk", seal.block, seal.cx)
		}
		for _, pos := range [][3]int32{
			{seal.cx - 1, 64, 60}, {seal.cx + 1, 64, 60},
			{seal.cx, 64, 59}, {seal.cx, 64, 61},
		} {
			if !srv.SetBlock(pos[0], pos[1], pos[2], stone) {
				t.Fatalf("the seal's ring at (%d, %d, %d) met no loaded chunk", pos[0], pos[1], pos[2])
			}
		}
	}

	// Twenty-five ticks would have bitten a stone-sealed body three
	// times; the exempt blocks bite not at all.
	srv.TickMobs(25)
	if n := servertest.DrainDamage(c); n != 0 {
		t.Errorf("%d in_wall bites arrived through glass and leaves in 25 ticks, want none", n)
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
