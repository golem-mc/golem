package entity

import (
	"math"
	"math/rand"
	"testing"
)

// phantomSpawnWorld is a spawnTestWorld that can be a dimension without sky
// light.
type phantomSpawnWorld struct {
	spawnTestWorld
	noSky bool
}

func (w phantomSpawnWorld) HasSkyLight() bool { return !w.noSky }

// motionWorld is a testWorld whose MOTION_BLOCKING height is set apart from
// the rest.
type motionWorld struct {
	testWorld
	motion int32
}

func (w motionWorld) MotionBlockingHeight(x, z int32) int32 { return w.motion }

// darkOverworld is the night the spawner works under: a sky darkened past
// five and open sky light at the player.
func darkOverworld() phantomSpawnWorld {
	w := phantomSpawnWorld{spawnTestWorld: newSpawnTestWorld()}
	w.darken = 11
	w.sky = 15
	return w
}

// sleepless is a player over the sea level whose rest roll always lands and
// whose local difficulty always beats the roll against it.
func sleepless() PhantomPlayer {
	return PhantomPlayer{X: 0.5, Y: 64, Z: 0.5, TimeSinceRest: math.MaxInt32, Effective: 3}
}

// phantomsSpawned runs the spawner for an hour of ticks and counts the
// phantoms it asks for.
func phantomsSpawned(w PhantomWorld, p PhantomPlayer) int {
	var s PhantomSpawner
	rng := rand.New(rand.NewSource(1))
	n := 0
	for range 72000 {
		s.Tick(w, rng, []PhantomPlayer{p}, func(SpawnRequest) { n++ })
	}
	return n
}

// TestThePhantomSpawnersChecks runs the spawner's checks one at a time
// against the sleepless player under a dark open sky.
// vanilla: PhantomSpawner.tick
func TestThePhantomSpawnersChecks(t *testing.T) {
	if n := phantomsSpawned(darkOverworld(), sleepless()); n == 0 {
		t.Fatal("the control: no phantom over a sleepless player under a dark open sky")
	}

	spectator := sleepless()
	spectator.Spectator = true
	if n := phantomsSpawned(darkOverworld(), spectator); n != 0 {
		t.Errorf("%d phantoms over a spectator, want none", n)
	}

	below := sleepless()
	below.Y = 62
	if n := phantomsSpawned(darkOverworld(), below); n != 0 {
		t.Errorf("%d phantoms over a player under the sea level, want none", n)
	}

	covered := darkOverworld()
	covered.sky = 14
	if n := phantomsSpawned(covered, sleepless()); n != 0 {
		t.Errorf("%d phantoms over a player who cannot see the sky, want none", n)
	}

	day := darkOverworld()
	day.darken = 4
	if n := phantomsSpawned(day, sleepless()); n != 0 {
		t.Errorf("%d phantoms under a sky darkened by 4, want none", n)
	}

	rested := sleepless()
	rested.TimeSinceRest = phantomRestThreshold
	if n := phantomsSpawned(darkOverworld(), rested); n != 0 {
		t.Errorf("%d phantoms over a player rested %d ticks ago, want none", n, phantomRestThreshold)
	}

	// Without sky light none of the darkness, sea-level or sky checks is
	// asked.
	skyless := darkOverworld()
	skyless.noSky = true
	skyless.darken = 0
	skyless.sky = 0
	if n := phantomsSpawned(skyless, below); n == 0 {
		t.Error("no phantom in a dimension without sky light, where the sky checks are not asked")
	}
}
