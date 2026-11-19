package entity

import (
	"math/rand"
	"testing"
)

// vanilla: Entity.shouldPlayAmethystStepSound, Entity.walkingStepSound
func TestMobCrystalChimeUsesBodyAge(t *testing.T) {
	for _, kind := range []MobType{TypeZombie, TypeWarden, TypeNautilus} {
		t.Run(kind.Name(), func(t *testing.T) {
			m, w := stepper(t, kind, "minecraft:amethyst_block")
			m.GameTime = 100000 // A newborn in an old world still waits twenty ticks.
			for _, age := range []int{19, 20, 39, 40} {
				m.Age = age
				chimes := 0
				for _, ev := range m.stepEvent(w, 0, 63, 0, w.BlockAt(0, 63, 0), nil) {
					if ev.SoundID.Name() == "minecraft:block.amethyst_block.chime" {
						chimes++
						if ev.Volume < 0.1 || ev.Volume > 1.3 || ev.Pitch < 0.5 || ev.Pitch > 1.7 {
							t.Errorf("age %d: chime volume/pitch = %g/%g", age, ev.Volume, ev.Pitch)
						}
					}
				}
				want := 0
				if age%20 == 0 {
					want = 1
				}
				if chimes != want {
					t.Errorf("age %d: %d chimes, want %d", age, chimes, want)
				}
			}
		})
	}
}

// vanilla: Entity.vibrationAndSoundEffectsFromBlock, Parrot.playStepSound
func TestAirborneParrotDoesNotStep(t *testing.T) {
	m, w := stepper(t, TypeParrot, "minecraft:stone")
	m.Y, m.OnGround, m.VX = 80, false, 1.8
	mobs := Mobs{m}
	for range 5 {
		for _, ev := range mobs.Tick(w, nil, nil) {
			if ev.SoundID.Name() == "minecraft:entity.parrot.step" {
				t.Fatal("parrot stepped while flying")
			}
		}
	}
	if m.X < 2.5 || m.OnGround {
		t.Fatalf("parrot did not fly: position (%g,%g,%g), ground %t", m.X, m.Y, m.Z, m.OnGround)
	}
}

// vanilla: Entity.walkingStepSound, Entity.playAmethystStepSound
func TestMobCrystalChimeDrawsRandomOnlyWhenDue(t *testing.T) {
	m, w := stepper(t, TypeZombie, "minecraft:amethyst_block")
	m.Rand = rand.New(rand.NewSource(7))
	control := rand.New(rand.NewSource(7))
	for _, tc := range []struct {
		block string
		age   int
		chime bool
	}{
		{"minecraft:stone", 20, false},
		{"minecraft:amethyst_block", 19, false},
		{"minecraft:amethyst_block", 20, true},
		{"minecraft:amethyst_block", 39, false},
		{"minecraft:stone", 40, false},
		{"minecraft:amethyst_block", 40, true},
	} {
		m.Age = tc.age
		m.stepEvent(w, 0, 63, 0, defaultState(t, tc.block), nil)
		if tc.chime {
			control.Float32()
		}
		if got, want := m.Rand.Int63(), control.Int63(); got != want {
			t.Fatalf("%s at age %d shifted the random stream: got %d, want %d", tc.block, tc.age, got, want)
		}
	}
}
