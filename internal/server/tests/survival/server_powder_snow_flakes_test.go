package survival

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestWalkingThroughPowderSnowKicksUpSnowflakes: a player moving sideways
// with its feet in powder snow kicks up a snowflake above the cell on about
// half the ticks, rising at 0.05 with ALTERNATIVE randomization; standing
// still kicks up none.
// vanilla: PowderSnowBlock.entityInside
func TestWalkingThroughPowderSnowKicksUpSnowflakes(t *testing.T) {
	t.Parallel()
	_, c, tick, _ := inPowderSnow(t, "Trudger", 104)

	tick(10)
	if flakes := collectSnowflakes(c); len(flakes) != 0 {
		t.Errorf("standing still kicked up %d snowflakes, want none", len(flakes))
	}

	var flakes []servertest.LevelParticles
	for i := range 20 {
		c.MoveStep(0.5+0.01*float64(i+1), 64, 104.5, true)
		c.EndTick()
		c.Settle()
		tick(1)
		flakes = append(flakes, collectSnowflakes(c)...)
	}
	// 20 ticks at even odds: at least one, and not every tick.
	if len(flakes) == 0 || len(flakes) == 20 {
		t.Fatalf("walking 20 ticks kicked up %d snowflakes, want about half", len(flakes))
	}
	for _, p := range flakes {
		if !isSnowflakeAbove(p, 65, 65) {
			t.Errorf("snowflake = %+v, want one at y 65 rising 0.05, sideways under 1/12, ALTERNATIVE", p)
		}
	}
}

// TestACowPushedThroughPowderSnowKicksUpSnowflakes: a cow standing in the
// two-deep pit kicks up none; pushed sideways each tick, it kicks up
// snowflakes above the cells its box overlaps (Y 65 and 66) on about half
// of them, rising at 0.05 with ALTERNATIVE randomization.
// vanilla: PowderSnowBlock.entityInside
func TestACowPushedThroughPowderSnowKicksUpSnowflakes(t *testing.T) {
	t.Parallel()
	srv, c := inPowderSnowPit(t)
	cowID := summonIntoPowderSnow(t, srv, c, "cow")

	srv.TickMobs(10)
	if flakes := collectSnowflakes(c); len(flakes) != 0 {
		t.Errorf("a standing cow kicked up %d snowflakes, want none", len(flakes))
	}

	// Pushes alternate in sign so the cow stays in the pit.
	var flakes []servertest.LevelParticles
	for i := range 20 {
		push := 0.1
		if i%2 == 1 {
			push = -0.1
		}
		if !srv.PushMob(cowID, push, 0, 0) {
			t.Fatal("the cow was not found to push")
		}
		srv.TickMobs(1)
		flakes = append(flakes, collectSnowflakes(c)...)
	}
	// Two cells over 20 ticks at even odds: at least one, and not all 40.
	if len(flakes) == 0 || len(flakes) == 40 {
		t.Fatalf("pushing the cow 20 ticks kicked up %d snowflakes, want about half of 40", len(flakes))
	}
	for _, p := range flakes {
		if !isSnowflakeAbove(p, 65, 66) {
			t.Errorf("snowflake = %+v, want one at y 65 or 66 rising 0.05, sideways under 1/12, ALTERNATIVE", p)
		}
	}
}

// collectSnowflakes settles c and returns the snowflakes it was sent.
func collectSnowflakes(c *servertest.Client) []servertest.LevelParticles {
	var flakes []servertest.LevelParticles
	c.SettleAndCollect(func(id int32, data []byte) {
		if p, ok := servertest.ParseLevelParticles(data); id == protocol.IDPlayLevelParticles && ok && p.Type == protocol.ParticleSnowflake {
			flakes = append(flakes, p)
		}
	})
	return flakes
}

// isSnowflakeAbove reports whether p is one snowflake rising 0.05 from Y
// lo or hi, drifting sideways under 1/12, with ALTERNATIVE randomization.
func isSnowflakeAbove(p servertest.LevelParticles, lo, hi float64) bool {
	return (p.Y == lo || p.Y == hi) && p.SpeedY == 0.05 && p.Count == 1 && p.Randomization == 1 && p.DX == 0 &&
		p.SpeedX >= -0.084 && p.SpeedX <= 0.084 && p.SpeedZ >= -0.084 && p.SpeedZ <= 0.084
}

// TestItemsTossedIntoPowderSnowKickUpSnowflakes: ten items flying sideways
// into the pit kick up snowflakes at their X above the cells their boxes
// overlap (Y 65 and 66) on about half of them; held by the snow from then
// on, their movement is spent and they kick up none.
// vanilla: PowderSnowBlock.entityInside, Entity.move
func TestItemsTossedIntoPowderSnowKickUpSnowflakes(t *testing.T) {
	t.Parallel()
	srv, c := inPowderSnowPit(t)
	// Distinct items, so none merges into another.
	for _, item := range []string{"stone", "dirt", "cobblestone", "oak_planks", "sand",
		"gravel", "glass", "bone", "stick", "apple"} {
		c.ChatCommand(`summon minecraft:item 3.6 65 5.5 {Item:{id:"minecraft:` + item + `"},Motion:[0.6d,0.0d,0.0d],PickupDelay:32767s}`)
		c.AwaitSpawn(protocol.EntityTypeItem)
	}
	c.Settle()

	srv.TickEntities(1)
	flakes := collectSnowflakes(c)
	// Two cells for each of ten items at even odds: at least one, not all 20.
	if len(flakes) == 0 || len(flakes) == 20 {
		t.Fatalf("ten items flying into the snow kicked up %d snowflakes, want about half of 20", len(flakes))
	}
	for _, p := range flakes {
		if !isSnowflakeAbove(p, 65, 66) || p.X < 4 || p.X > 4.5 {
			t.Errorf("snowflake = %+v, want one at the item's x past 4 and y 65 or 66 rising 0.05, sideways under 1/12, ALTERNATIVE", p)
		}
	}

	srv.TickEntities(5)
	if flakes := collectSnowflakes(c); len(flakes) != 0 {
		t.Errorf("items held in the snow kicked up %d snowflakes, want none", len(flakes))
	}
}

// TestARiddenHorseKicksUpSnowflakesFromItsRidersMovement: a saddled horse
// in the pit, steered by its rider's Move Vehicle reports, has no movement
// of its own; it kicks up snowflakes above the cells its box overlaps (Y 65
// and 66) by the rider's known movement, and none while the rider holds
// still.
// vanilla: PowderSnowBlock.entityInside, Entity.getKnownMovement
func TestARiddenHorseKicksUpSnowflakesFromItsRidersMovement(t *testing.T) {
	t.Parallel()
	srv, c := inPowderSnowPit(t)
	c.MoveToGround(2.5, 64, 5.5, true)
	c.ChatCommand(`summon minecraft:horse 5.5 64 5.5 {Tame:1b,equipment:{saddle:{id:"minecraft:saddle",count:1}}}`)
	horseID := c.AwaitSpawn(protocol.EntityTypeHorse).EntityID
	c.Settle()
	c.UseOn(horseID)
	// Two tick ends with no move between clear the walk's movement.
	c.EndTick()
	c.EndTick()
	c.Settle()
	collectSnowflakes(c)

	srv.TickMobs(5)
	if flakes := collectSnowflakes(c); len(flakes) != 0 {
		t.Errorf("a ridden horse held still kicked up %d snowflakes, want none", len(flakes))
	}

	// Steps alternate in sign so the horse stays in the pit.
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
		srv.TickMobs(1)
		flakes = append(flakes, collectSnowflakes(c)...)
	}
	// The box overlaps eight snow cells, on even odds each tick.
	if len(flakes) == 0 || len(flakes) == 80 {
		t.Fatalf("riding the horse 10 ticks kicked up %d snowflakes, want about half of 80", len(flakes))
	}
	for _, p := range flakes {
		if !isSnowflakeAbove(p, 65, 66) {
			t.Errorf("snowflake = %+v, want one at y 65 or 66 rising 0.05, sideways under 1/12, ALTERNATIVE", p)
		}
	}
}
