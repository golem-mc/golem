package entity

import (
	"testing"

	"github.com/google/uuid"
)

func TestRiddenVehiclesUseTheirStepSounds(t *testing.T) {
	for _, tc := range []struct {
		kind MobType
		want string
	}{
		{TypePig, "minecraft:entity.pig.step"},
		{TypeHorse, "minecraft:entity.horse.step_wood"},
		{TypeSkeletonHorse, "minecraft:entity.horse.step_wood"},
		{TypeZombieHorse, "minecraft:entity.horse.step_wood"},
		{TypeDonkey, "minecraft:entity.horse.step"},
		{TypeMule, "minecraft:entity.horse.step"},
		{TypeLlama, "minecraft:entity.llama.step"},
		{TypeTraderLlama, "minecraft:entity.llama.step"},
		{TypeCamel, "minecraft:entity.camel.step"},
		{TypeCamelHusk, "minecraft:entity.camel_husk.step"},
		{TypeStrider, "minecraft:entity.strider.step"},
		{TypeNautilus, ""},
		{TypeZombieNautilus, ""},
		{TypeHappyGhast, ""},
	} {
		t.Run(tc.kind.Name(), func(t *testing.T) {
			m, w := stepper(t, tc.kind, "minecraft:stone")
			m.Board(uuid.New())
			var heard []string
			for range 4 {
				from := m.X
				m.X += 0.5
				for _, ev := range m.RiddenStepSounds(w, from, m.Y, m.Z) {
					if ev.Kind == MobSounded {
						heard = append(heard, ev.SoundID.Name())
					}
				}
			}
			if tc.want == "" {
				if len(heard) != 0 {
					t.Fatalf("silent step override produced %v", heard)
				}
			} else if len(heard) != 1 || heard[0] != tc.want {
				t.Fatalf("heard %v, want one %s", heard, tc.want)
			}
		})
	}
}

// vanilla: AbstractHorse.playStepSound, Horse.playGallopSound
func TestRiddenHorseGallopCadenceAndBreathing(t *testing.T) {
	for _, kind := range []MobType{TypeHorse, TypeSkeletonHorse, TypeZombieHorse} {
		for _, baby := range []bool{false, true} {
			m, w := stepper(t, kind, "minecraft:anvil")
			m.SetBaby(baby)
			// Mob passengers use the same gallop branch as players.
			m.Passengers = []*Mob{NewMob(TypeZombie, 2, 0, 0, 0, 2)}
			breaths := 0
			for step := 1; step <= 120; step++ {
				events := m.stepEvent(w, 0, 63, 0, w.BlockAt(0, 63, 0), nil)
				want := ""
				switch {
				case step <= 5:
					want = "minecraft:entity.horse.step_wood"
				case step%3 == 0:
					want = "minecraft:entity.horse.gallop"
				}
				if want == "" {
					if len(events) != 0 {
						t.Fatalf("%s baby=%v step %d: unexpected %+v", kind, baby, step, events)
					}
					continue
				}
				if len(events) == 0 || events[0].SoundID.Name() != want || events[0].Volume != float32(0.3)*stepVolume || events[0].Pitch != 1 {
					t.Fatalf("%s baby=%v step %d: %+v, want %s at 0.045/1", kind, baby, step, events, want)
				}
				if len(events) > 1 {
					breathe := "minecraft:entity.horse.breathe"
					if baby {
						breathe = "minecraft:entity.baby_horse.breathe"
					}
					if kind != TypeHorse || want != "minecraft:entity.horse.gallop" || len(events) != 2 || events[1].SoundID.Name() != breathe || events[1].Volume != float32(0.3)*0.6 || events[1].Pitch != 1 {
						t.Fatalf("unexpected breath: %+v", events)
					}
					breaths++
				}
			}
			if kind == TypeHorse && breaths == 0 {
				t.Fatal("fixed seed produced no horse breaths")
			}
		}
	}
}

func TestChestedHorsesKeepTheirHoofSoundsWhenRidden(t *testing.T) {
	for _, kind := range []MobType{TypeDonkey, TypeMule, TypeLlama, TypeTraderLlama} {
		m, w := stepper(t, kind, "minecraft:stone")
		m.Board(uuid.New())
		want := "minecraft:entity.horse.step"
		if IsLlama(kind) {
			want = "minecraft:entity.llama.step"
		}
		for range 20 {
			events := m.stepEvent(w, 0, 63, 0, w.BlockAt(0, 63, 0), nil)
			if len(events) != 1 || events[0].SoundID.Name() != want {
				t.Fatalf("%s: %+v, want %s without galloping", kind, events, want)
			}
		}
	}
}

func TestRiddenNautilusSwimsOnEnteringWater(t *testing.T) {
	for _, kind := range []MobType{TypeNautilus, TypeZombieNautilus} {
		m, w := stepper(t, kind, "minecraft:stone")
		m.Board(uuid.New())
		m.OnGround, m.VX = false, 0.5
		water := defaultState(t, "minecraft:water")
		for x := int32(0); x <= 4; x++ {
			w.set(x, 64, 0, water)
		}
		var sounds []MobEvent
		for range 4 {
			from := m.X
			m.X += 0.5
			for _, ev := range m.RiddenStepSounds(w, from, m.Y, m.Z) {
				if ev.Kind == MobSounded {
					sounds = append(sounds, ev)
				}
			}
		}
		want := "minecraft:entity.nautilus.swim"
		if kind == TypeZombieNautilus {
			want = "minecraft:entity.zombie_nautilus.swim"
		}
		if len(sounds) != 1 || sounds[0].SoundID.Name() != want || sounds[0].Volume <= 0 {
			t.Fatalf("%s entering water: %+v, want %s", kind, sounds, want)
		}
	}
}

func TestSkeletonHorseWaterStepsUseItsOwnSounds(t *testing.T) {
	m, _ := stepper(t, TypeSkeletonHorse, "minecraft:stone")
	m.Board(uuid.New())
	for step := 1; step <= 12; step++ {
		want := "minecraft:entity.skeleton_horse.swim"
		if step <= 5 {
			want = "minecraft:entity.skeleton_horse.step_water"
		} else if step%3 == 0 {
			want = "minecraft:entity.skeleton_horse.gallop_water"
		}
		events := m.swimEvent(nil)
		if len(events) != 1 || events[0].SoundID.Name() != want || events[0].Volume != 0.3 {
			t.Fatalf("water step %d: %+v, want %s at 0.3", step, events, want)
		}
	}
	m.OnGround, m.VX = false, 1
	if events := m.swimEvent(nil); events[0].SoundID.Name() != "minecraft:entity.skeleton_horse.swim" || events[0].Volume != 0.1 {
		t.Fatalf("swimming: %+v", events)
	}
}
