package entity

import "testing"

// strollClock records when the stroll it wraps sets off on a path and when
// that walk ends.
type strollClock struct {
	Goal
	m              *Mob
	tick           *int
	walking        bool
	walks, lastEnd int
	gaps, gapTicks int
}

func (c *strollClock) Start(t *tickView) {
	c.Goal.Start(t)
	if len(c.m.navOf().nav.path) == 0 {
		return
	}
	if c.walks > 0 {
		c.gaps++
		c.gapTicks += *c.tick - c.lastEnd
	}
	c.walks++
	c.walking = true
}

// watch notes the tick a walk under way runs out of path.
func (c *strollClock) watch() {
	if c.walking && len(c.m.navOf().nav.path) == 0 {
		c.walking = false
		c.lastEnd = *c.tick
	}
}

// An idle hoglin's walk is followed by the rest of the DoNothing span the
// RunOne chained while it walked, then by a weighted pick that pauses again
// one time in three: about 24 + 22.5 ticks from a walk's end to the next.
// vanilla: HoglinAi.createIdleMovementBehaviors, OneShot.tickOrStop, DoNothing
func TestAnIdleHoglinStrollsAgainWithinAPause(t *testing.T) {
	t.Parallel()
	w := newTestWorld()
	// Seven layers, so a stroll pick below the surface climbs out of solid
	// ground rather than failing over a void.
	stone := defaultState(t, "minecraft:stone")
	for y := int32(57); y <= 63; y++ {
		floor(w, -40, -40, 40, 40, y, stone)
	}
	// Six hoglins in turn, one per seed, pool enough gaps for a mean.
	gaps, gapTicks := 0, 0
	for seed := range int64(6) {
		m := NewMob(TypeHoglin, 1, 0.5, 64, 0.5, seed+1)
		m.ZombificationImmune = true
		var idle *runOneGoal
		for _, g := range m.ai.goals {
			if r, ok := g.(*runOneGoal); ok {
				idle = r
			}
		}
		if idle == nil {
			t.Fatal("the hoglin has no idle RunOne")
		}
		walk, ok := idle.choices[0].(*walkPause)
		if !ok {
			t.Fatalf("the idle RunOne's first choice is %T, want the stroll's walkPause", idle.choices[0])
		}
		tick := 0
		clock := &strollClock{Goal: walk.Goal, m: m, tick: &tick}
		walk.Goal = clock
		for tick = range 4000 {
			m.Tick(w, nil)
			clock.watch()
		}
		gaps += clock.gaps
		gapTicks += clock.gapTicks
	}
	if gaps < 60 {
		t.Fatalf("six hoglins strolled again %d times in 4000 ticks each", gaps)
	}
	// A pick on the walk's last tick, with no pause after it, averages about
	// 22.5 ticks; vanilla's pause after the walk averages about 47.
	if mean := float64(gapTicks) / float64(gaps); mean < 35 || mean > 60 {
		t.Errorf("mean gap from a walk's end to the next stroll = %.1f ticks over %d gaps, want 35 to 60", mean, gaps)
	}
}
