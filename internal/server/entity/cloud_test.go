package entity

import (
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/server/effect"
	"github.com/golem-mc/golem/registry"
)

// The cloud simulation as ordinary unit tests, driving the pass over the
// bodies a plain step is handed, the way the thrown projectile's own are.
// The black-box tests in internal/server/splash_potions_test.go remain the proof of
// the whole life of a lingering potion's cloud; these are the cheapest
// possible checks of the rhythm itself.

// cloudBody is one body the tests hand the pass: a cow-sized box of the
// type at the feet given, in an entity ID the ticks can tell apart.
func cloudBody(id int32, x, z float64) CloudTarget {
	const half, height = 0.45, 1.4
	return CloudTarget{
		EntityID: id,
		Type:     cowUnitType,
		MinX:     x - half, MinY: 64, MinZ: z - half,
		MaxX: x + half, MaxY: 64 + height, MaxZ: z + half,
		FeetX: x, FeetZ: z,
	}
}

var cowUnitType = registryEntityType("minecraft:cow")

func registryEntityType(name string) int32 {
	id, ok := registry.EntityTypeID(name)
	if !ok {
		panic("entity: no such type " + name)
	}
	return id
}

// poisonCloud is one cloud raining a 900-tick poison.
func poisonCloud() *Cloud {
	poison := effect.New(registry.MustMobEffect("minecraft:poison"), 900, 0, false, true, true)
	return NewCloud(1, 0.5, 64, 0.5, []effect.Instance{poison})
}

// TestACloudWaitsThenPoisonsEveryFifthTick runs the pass against a body
// standing at the cloud's own place and reads the rhythm out: nothing
// until the wait is spent, then the first touch on the tick the wait lets
// pass, and the twenty ticks the victim is held off for - the
// reapplication the bodies standing in it stay poisoned by.
func TestACloudWaitsThenPoisonsEveryFifthTick(t *testing.T) {
	c := poisonCloud()
	body := cloudBody(2, 0.5, 0.5)

	for i := 1; i < CloudWaitTime; i++ {
		if reached := c.Step([]CloudTarget{body}); len(reached) != 0 {
			t.Fatalf("tick %d reached %d bodies, want the wait's none", i, len(reached))
		}
		if !c.Waiting() {
			t.Fatalf("tick %d was not waiting, want the wait still holding", i)
		}
	}
	// The wait lets its first pass through on its own last tick, which
	// the rhythm's five lands on.
	if reached := c.Step([]CloudTarget{body}); len(reached) != 1 || reached[0].EntityID != 2 {
		t.Fatalf("tick %d reached %+v, want the standing body's one", CloudWaitTime, reached)
	}

	// The victim is held off twenty ticks: three passes pass it up, and
	// the fourth - the tick its last touch plus twenty - takes it again.
	for i := CloudWaitTime + 1; i < CloudWaitTime+CloudReapplication; i++ {
		if reached := c.Step([]CloudTarget{body}); len(reached) != 0 {
			t.Fatalf("tick %d reached %d bodies, want the held-off victim's none", i, len(reached))
		}
	}
	if reached := c.Step([]CloudTarget{body}); len(reached) != 1 {
		t.Fatalf("tick %d reached %d bodies, want the reapplication's one", CloudWaitTime+CloudReapplication, len(reached))
	}
}

// TestACloudsTouchTakesHalfItsRadius steps a fresh cloud onto a body
// standing at its place and watches every touch take half a block off the
// radius, until one of them leaves it under the floor and the cloud dies
// on it.
func TestACloudsTouchTakesHalfItsRadius(t *testing.T) {
	c := poisonCloud()
	body := cloudBody(2, 0.5, 0.5)

	stepTo := func(n int) {
		for c.TickCount < n {
			c.Step([]CloudTarget{body})
		}
	}
	stepTo(CloudWaitTime)
	// The first touch: the wait's own share of shrink - a twentieth of the
	// radius's own - spent before it.
	if r := c.Radius() - CloudRadiusOnUse; r < CloudStartRadius-0.05 || r > CloudStartRadius {
		t.Fatalf("the touched radius = %v, want the first touch's half a block off three, less the shrink's share", float64(c.Radius()))
	}
	// A touch every reapplication, another half a block off each: the
	// fifth's is the one that leaves the cloud under the floor.
	touches := 1
	for c.TickCount < CloudDuration {
		stepTo(c.TickCount + CloudReapplication)
		touches++
		if c.Dead {
			break
		}
	}
	if touches != 5 {
		t.Fatalf("the touched cloud died on touch %d, want the fifth", touches)
	}
	// The death keeps the floor intact: the sub-floor radius is discarded
	// with the cloud, not told.
	if c.Radius() < cloudMinRadius {
		t.Fatalf("the cloud kept the radius %v it died under, want the floor never gone past", float64(c.Radius()))
	}
}

// TestASplashReachesWhatItsBoxesMakeOfIt shatters a quarter-block potion
// box among three bodies: the one whose box holds the shatter at scale
// one, the one a gap inside the range away at the fraction its distance
// leaves, and the one beyond the four blocks carrying nothing.
func TestASplashReachesWhatItsBoxesMakeOfIt(t *testing.T) {
	potion := NewThrown(1, ThrownSnowball, 0, 64, 0, 0, 0, 0, 0, 1)
	potion.TickCount = 4
	margin := thrownHitMargin(4)

	in := cloudBody(2, 0.5, 0.5)
	near := cloudBody(3, 3.0, 0.5)
	far := cloudBody(4, 8.0, 0.5)
	hits := potion.SplashHits(0.5, 64, 0.5, []ArrowTarget{in.ArrowTarget, near.ArrowTarget, far.ArrowTarget})
	if len(hits) != 2 {
		t.Fatalf("the splash reached %d bodies, want the two the range covers", len(hits))
	}
	by := map[int32]float64{}
	for _, hit := range hits {
		by[hit.Target.EntityID] = hit.Scale
	}
	if s, ok := by[2]; !ok || math.Abs(s-1) > 1e-9 {
		t.Errorf("the body holding the shatter scaled at %v, want one", s)
	}
	// The near body: its box a margin and its half-width out from the
	// potion's own, the gap floored at nought over the squared four.
	gap := 3.0 - 0.45 - (0.5 + ThrownHalfSize) - margin
	if s, ok := by[3]; !ok || math.Abs(s-(1-gap/4)) > 1e-9 {
		t.Errorf("the body at 3 scaled %v, want %v", s, 1-gap/4)
	}
	if _, ok := by[4]; ok {
		t.Error("the body eight blocks out was reached")
	}
}
