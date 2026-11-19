package entity

import (
	"testing"

	"github.com/golem-mc/golem/registry"
)

// vanilla: CopperGolemOxidationLevels, Camel.playStepSound, CamelHusk.playStepSound
func TestMovementSoundOverrides(t *testing.T) {
	copper := NewMob(TypeCopperGolem, 1, 0, 64, 0, 1)
	for stage, want := range []registry.Sound{registry.SoundEntityCopperGolemStep, registry.SoundEntityCopperGolemStep, registry.SoundEntityCopperGolemWeatheredStep, registry.SoundEntityCopperGolemOxidizedStep} {
		copper.SetCopperWeather(stage)
		if got := copper.Sounds().Step; got != want {
			t.Errorf("copper stage %d: %s, want %s", stage, got.Name(), want.Name())
		}
	}
	for _, tc := range []struct {
		kind       MobType
		step, sand registry.Sound
	}{
		{TypeCamel, registry.SoundEntityCamelStep, registry.SoundEntityCamelStepSand},
		{TypeCamelHusk, registry.SoundEntityCamelHuskStep, registry.SoundEntityCamelHuskStepSand},
	} {
		m := NewMob(tc.kind, 1, 0, 64, 0, 1)
		for _, baby := range []bool{false, true} {
			m.SetBaby(baby)
			if got := m.camelStepVoice(registry.BlockStone.DefaultState()); got != tc.step {
				t.Errorf("%s baby %v: %s", tc.kind, baby, got.Name())
			}
			if got := m.camelStepVoice(registry.BlockSand.DefaultState()); got != tc.sand {
				t.Errorf("%s baby %v sand: %s", tc.kind, baby, got.Name())
			}
		}
	}
}
