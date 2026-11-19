package survival

import (
	"fmt"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
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
