package mobs

import (
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
)

// TestASwimmingFishSplashes: a cod that swims a few blocks plays the fish swim
// sound in the neutral category, once for each block of way scaled by 0.6.
// vanilla: Entity.applyMovementEmissionAndPlaySound, AbstractFish.getSwimSound
func TestASwimmingFishSplashes(t *testing.T) {
	t.Parallel()
	srv, c := squidTests(t)
	id := summonSquid(t, c, "minecraft:cod", fishType(t, "minecraft:cod"), pondCentreX, -8.5)
	start, _ := fishAt(srv, id)
	last, travelled := start, 0.0
	splashes := 0
	for range 300 {
		srv.TickMobs(1)
		m, _ := fishAt(srv, id)
		travelled += math.Hypot(m.X-last.X, m.Z-last.Z)
		last = m
		for _, h := range collectSounds(c) {
			if h.name == "minecraft:entity.fish.swim" {
				splashes++
				if h.category != protocol.SoundCategoryNeutral {
					t.Errorf("the swim sound's category = %d, want neutral", h.category)
				}
			}
		}
	}
	if travelled < 3 {
		t.Fatalf("the cod swam %.2f blocks, want it wandering", travelled)
	}
	// The first splash comes with the first movement, then one per whole
	// 0.6-scaled block of way.
	if limit := int(math.Floor(travelled*0.6)) + 1; splashes == 0 || splashes > limit {
		t.Errorf("%d splashes over %.2f blocks swum, want some but no more than %d", splashes, travelled, limit)
	}
}
