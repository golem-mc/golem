package server

import (
	"testing"

	"github.com/golem-mc/golem/registry"
)

// TestEveryEffectParticleHasAWireID keeps a new effect's own particle from
// being written as particle 0.
func TestEveryEffectParticleHasAWireID(t *testing.T) {
	for id := range registry.MobEffects {
		p := registry.MobEffectOf(int32(id)).Particle
		if _, ok := effectParticleTypes[p]; p != "" && !ok {
			t.Errorf("effect %s: particle %s missing from effectParticleTypes", registry.MobEffects[id], p)
		}
	}
}
