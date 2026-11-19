package survival

import (
	"fmt"
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestOrbsFlyingIntoPowderSnowAreHeldAndKickUpSnowflakes: ten orbs flying
// sideways into the pit kick up snowflakes at their X above the cells their
// boxes overlap (Y 65 and 66) on about half of them; held by the snow from
// then on, their movement is spent, so they stop short and kick up none.
// vanilla: PowderSnowBlock.entityInside, Entity.move, ExperienceOrb.tick
func TestOrbsFlyingIntoPowderSnowAreHeldAndKickUpSnowflakes(t *testing.T) {
	t.Parallel()
	srv, c := inPowderSnowPit(t)
	// A spectator draws no orb toward them.
	c.ChatCommand("gamemode spectator")
	ids := make([]int32, 10)
	for i := range ids {
		// Distinct values, so none merges into another.
		c.ChatCommand(fmt.Sprintf("summon minecraft:experience_orb 3.6 64.9 %.2f {Value:%d,Motion:[0.6d,0.0d,0.0d]}",
			4.3+0.25*float64(i), i+1))
		ids[i] = c.AwaitSpawn(protocol.EntityTypeExperienceOrb).EntityID
	}
	c.Settle()

	srv.TickEntities(1)
	flakes := collectSnowflakes(c)
	// Two cells for each of ten orbs at even odds: at least one, not all 20.
	if len(flakes) == 0 || len(flakes) == 20 {
		t.Fatalf("ten orbs flying into the snow kicked up %d snowflakes, want about half of 20", len(flakes))
	}
	for _, p := range flakes {
		if !isSnowflakeAbove(p, 65, 66) || p.X < 4 || p.X > 4.5 {
			t.Errorf("snowflake = %+v, want one at the orb's x past 4 and y 65 or 66 rising 0.05, sideways under 1/12, ALTERNATIVE", p)
		}
	}

	srv.TickEntities(2)
	held := make([]float64, len(ids))
	for i, id := range ids {
		held[i], _, _, _ = srv.OrbPos(id)
	}
	srv.TickEntities(4)
	if flakes := collectSnowflakes(c); len(flakes) != 0 {
		t.Errorf("orbs held in the snow kicked up %d snowflakes, want none", len(flakes))
	}
	// Unheld, 0.6 a tick under 0.98 drag would carry each past x 7.
	for i, id := range ids {
		x, _, _, ok := srv.OrbPos(id)
		if !ok {
			t.Fatalf("orb %d is gone", id)
		}
		if x != held[i] || x > 5 {
			t.Errorf("orb %d at x %v after %v, want it held still short of x 5", id, x, held[i])
		}
	}
}

// TestMinecartsRollingIntoPowderSnowAreHeldAndKickUpSnowflakes: three
// derailed carts rolling sideways into the pit kick up snowflakes, twice a
// tick (after the move and at the tick's end), above the cells their boxes
// overlap (Y 65 and 66) on about half of them; held by the snow from then
// on, they stop short and kick up none.
// vanilla: PowderSnowBlock.entityInside, AbstractMinecart.move,
// OldMinecartBehavior.tick
func TestMinecartsRollingIntoPowderSnowAreHeldAndKickUpSnowflakes(t *testing.T) {
	t.Parallel()
	srv, c := inPowderSnowPit(t)

	var flakes []servertest.LevelParticles
	// Two rounds, so 24 chances decide whether any flake went out.
	for round := range 2 {
		ids := make([]int32, 3)
		for i := range ids {
			c.ChatCommand(fmt.Sprintf("summon minecraft:minecart 3.6 64.5 %.1f {Motion:[0.4d,0.0d,0.0d]}", 4.5+float64(i)))
			ids[i] = c.AwaitSpawn(protocol.EntityTypeMinecart).EntityID
		}
		c.Settle()

		srv.TickMinecarts(1)
		flakes = append(flakes, collectSnowflakes(c)...)

		srv.TickMinecarts(2)
		held := make([]float64, len(ids))
		for i, id := range ids {
			held[i], _, _, _, _ = srv.MinecartState(id)
		}
		srv.TickMinecarts(4)
		if got := collectSnowflakes(c); len(got) != 0 {
			t.Errorf("round %d: carts held in the snow kicked up %d snowflakes, want none", round, len(got))
		}
		// Unheld, a cart rolls on at about 0.38 a tick.
		for i, id := range ids {
			x, _, _, _, ok := srv.MinecartState(id)
			if !ok {
				t.Fatalf("cart %d is gone", id)
			}
			if x != held[i] || x > 5 {
				t.Errorf("round %d: cart %d at x %v after %v, want it held still short of x 5", round, id, x, held[i])
			}
		}
		c.ChatCommand("kill @e[type=minecart]")
		c.Settle()
		srv.TickMinecarts(1)
		collectSnowflakes(c)
	}
	// Three carts, two cells, two checks, two rounds: at least one, not all 24.
	if len(flakes) == 0 || len(flakes) == 24 {
		t.Fatalf("carts rolling into the snow kicked up %d snowflakes, want about half of 24", len(flakes))
	}
	for _, p := range flakes {
		if !isSnowflakeAbove(p, 65, 66) || p.X < 3.9 || p.X > 4.5 {
			t.Errorf("snowflake = %+v, want one at the cart's x and y 65 or 66 rising 0.05, sideways under 1/12, ALTERNATIVE", p)
		}
	}
}

// TestABoatSlidingIntoPowderSnowIsHeld: a boat sliding sideways into the
// pit is held by the snow on the next move and stops short of where the
// ground alone would let it slide.
// vanilla: PowderSnowBlock.entityInside, Entity.move
func TestABoatSlidingIntoPowderSnowIsHeld(t *testing.T) {
	t.Parallel()
	srv, c := inPowderSnowPit(t)
	c.ChatCommand("summon minecraft:oak_boat 3.0 64 5.5 {Motion:[1.5d,0.0d,0.0d]}")
	boatID := c.AwaitSpawn(protocol.EntityTypeOakBoat).EntityID
	c.Settle()

	srv.TickBoats(3)
	held, _, _, _, _, ok := srv.BoatState(boatID)
	if !ok {
		t.Fatal("the boat is gone")
	}
	srv.TickBoats(4)
	// Unheld, the ground's friction would let it slide past x 6.
	x, _, _, _, _, _ := srv.BoatState(boatID)
	if x != held || x > 5.5 {
		t.Errorf("the boat at x %v after %v, want it held still short of x 5.5", x, held)
	}
}

// TestADrivenBoatKicksUpSnowflakesFromItsDriversMovement: a boat in the pit,
// steered by its driver's Move Vehicle reports, kicks up snowflakes twice a
// tick above the cells its box overlaps (Y 65) by the driver's known
// movement, and none while the driver holds still.
// vanilla: PowderSnowBlock.entityInside, AbstractBoat.tick,
// ServerPlayer.getKnownMovement
func TestADrivenBoatKicksUpSnowflakesFromItsDriversMovement(t *testing.T) {
	t.Parallel()
	srv, c := inPowderSnowPit(t)
	c.MoveToGround(2.5, 64, 5.5, true)
	boatID := srv.SpawnBoat(5.5, 64, 5.5, "minecraft:oak_boat")
	if boatID == 0 {
		t.Fatal("SpawnBoat came to nothing")
	}
	c.UseOn(boatID)
	servertest.AwaitPassengersFor(c, boatID)
	// Two tick ends with no move between clear the walk's movement.
	c.EndTick()
	c.EndTick()
	c.Settle()
	collectSnowflakes(c)

	srv.TickBoats(5)
	if flakes := collectSnowflakes(c); len(flakes) != 0 {
		t.Errorf("a driven boat held still kicked up %d snowflakes, want none", len(flakes))
	}

	// Steps alternate in sign so the boat stays in the pit.
	var flakes []servertest.LevelParticles
	for i := range 10 {
		x := 5.6
		if i%2 == 1 {
			x = 5.5
		}
		c.Send(protocol.IDPlayMoveVehicleIn, func(b *protocol.Buffer) {
			b.PutF64(x)
			b.PutF64(64)
			b.PutF64(5.5)
			b.PutF32(0)
			b.PutF32(0)
			b.PutBool(true)
		})
		c.Settle()
		srv.TickBoats(1)
		flakes = append(flakes, collectSnowflakes(c)...)
	}
	// The hull overlaps nine snow cells, checked twice a tick on even odds.
	if len(flakes) == 0 || len(flakes) == 180 {
		t.Fatalf("driving the boat 10 ticks kicked up %d snowflakes, want about half of 180", len(flakes))
	}
	for _, p := range flakes {
		if !isSnowflakeAbove(p, 65, 65) {
			t.Errorf("snowflake = %+v, want one at y 65 rising 0.05, sideways under 1/12, ALTERNATIVE", p)
		}
	}
}

// TestArrowsFlyingThroughPowderSnowKickUpSnowflakes: five arrows flying
// sideways through the pit kick up snowflakes at their X above the cells
// their boxes overlap (Y 65 and 66) on about half of them. An arrow's flight
// never runs Entity.move, which is what spends the snow's multiplier, so it
// flies on at the air's drag alone.
// vanilla: PowderSnowBlock.entityInside, AbstractArrow.tick
func TestArrowsFlyingThroughPowderSnowKickUpSnowflakes(t *testing.T) {
	t.Parallel()
	srv, c := inPowderSnowPit(t)
	for i := range 5 {
		c.ChatCommand(fmt.Sprintf("summon minecraft:arrow 3.5 64.6 %.1f {Motion:[0.5d,0.0d,0.0d]}", 4.5+0.5*float64(i)))
		c.AwaitSpawn(protocol.EntityTypeArrow)
	}
	c.Settle()

	srv.TickEntities(3)
	for _, a := range srv.ArrowsInDimension("minecraft:overworld") {
		if want := 0.5 * 0.99 * 0.99 * 0.99; math.Abs(a.VX-want) > 1e-6 {
			t.Errorf("arrow %d in the snow flies at %v, want the air's drag alone (%v)", a.ID, a.VX, want)
		}
	}
	flakes := collectSnowflakes(c)
	// Five arrows, two or more cells across two ticks in the snow: at least one.
	if len(flakes) == 0 {
		t.Fatal("arrows flying through the snow kicked up no snowflakes")
	}
	for _, p := range flakes {
		if !isSnowflakeAbove(p, 65, 66) || p.X < 4 || p.X > 6 {
			t.Errorf("snowflake = %+v, want one at the arrow's x in the pit and y 65 or 66 rising 0.05, sideways under 1/12, ALTERNATIVE", p)
		}
	}
}

// TestAnArrowCrossingPowderSnowWithinATickKicksUpSnowflakes: arrows at three
// blocks a tick from x 8 to x 5 never end a tick with their box over the
// powder snow cell at x 6, but pass through it, so the cells swept between
// the two positions kick up snowflakes, at the arrow's x where the tick ends.
// vanilla: Entity.checkInsideBlocks, BlockGetter.forEachBlockIntersectedBetween
func TestAnArrowCrossingPowderSnowWithinATickKicksUpSnowflakes(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.MobTestConfig(), "Watcher", servertest.OpsListing(t, "Watcher"))
	c.Settle()
	srv.StopTick()
	snow := servertest.BlockState(t, "minecraft:powder_snow")
	// One cell for each arrow, so about half of the sixteen flakes come
	// through the snowflake's coin flip.
	for z := range int32(16) {
		if !srv.SetBlock(6, 65, z, snow) {
			t.Fatal("the chunk was not loaded")
		}
		srv.ShootArrow(8.0, 65.2, float64(z)+0.5, -3, 0, 0)
	}
	c.Settle()

	srv.TickEntities(1)
	flakes := collectSnowflakes(c)
	if len(flakes) == 0 {
		t.Fatal("sixteen arrows crossing powder snow within a tick kicked up no snowflakes")
	}
	for _, p := range flakes {
		if !isSnowflakeAbove(p, 66, 66) || p.X != 5.0 {
			t.Errorf("snowflake = %+v, want one at the arrow's x 5 and y 66 rising 0.05, sideways under 1/12, ALTERNATIVE", p)
		}
	}
}
