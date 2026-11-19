package registry

import "testing"

// TestMobEffectsAreAllDescribed checks the hand-kept effect table names
// exactly the effects of the generated registry.
func TestMobEffectsAreAllDescribed(t *testing.T) {
	if len(mobEffects) != len(MobEffects) {
		t.Errorf("%d effects described, %d in the registry", len(mobEffects), len(MobEffects))
	}
	for _, name := range MobEffects {
		if _, ok := mobEffects[name]; !ok {
			t.Errorf("%s has no colour", name)
		}
	}
}
