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
