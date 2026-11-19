package entity

import (
	"math/rand"
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

// vanilla: Entity.doWaterSplashEffect
func TestMobSplashSoundPreservesPitchDraws(t *testing.T) {
	for _, kind := range []MobType{TypeCow, TypeZombie, TypeDolphin, TypeAxolotl} {
		for _, speed := range []float64{0.1, 4} {
			m := NewMob(kind, 1, 0, 64, 0, 1)
			m.Rand = rand.New(rand.NewSource(17))
			control := rand.New(rand.NewSource(17))
			m.VY = speed
			pitch := 1 + (control.Float32()-control.Float32())*0.4
			events := m.waterSplash(nil)
			want := registry.SoundEntityGenericSplash
			if speed < 1 {
				switch kind {
				case TypeZombie:
					want = registry.SoundEntityHostileSplash
				case TypeDolphin:
					want = registry.SoundEntityDolphinSplash
				case TypeAxolotl:
					want = registry.SoundEntityAxolotlSplash
				}
			}
			if len(events) != 1 || events[0].SoundID != want || events[0].Pitch != pitch {
				t.Fatalf("%s speed %g: %+v", kind, speed, events)
			}
			if m.Rand.Int63() != control.Int63() {
				t.Fatal("splash changed random stream")
			}
		}
	}
}

// vanilla: Cod/Salmon/TropicalFish/Pufferfish/Tadpole.getFlopSound
func TestFishFlopSoundIDs(t *testing.T) {
	for _, tc := range []struct {
		kind MobType
		want string
	}{
		{TypeCod, "cod"}, {TypeSalmon, "salmon"}, {TypeTropicalFish, "tropical_fish"}, {TypePufferfish, "puffer_fish"}, {TypeTadpole, "tadpole"},
	} {
		if got := FishFlopSound(tc.kind).Name(); got != "minecraft:entity."+tc.want+".flop" {
			t.Errorf("%s: %s", tc.kind, got)
		}
	}
	if FishFlopSound(registry.EntityTypeCount) != registry.SoundNone {
		t.Fatal("invalid fish has a flop sound")
	}
}
