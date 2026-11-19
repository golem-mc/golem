package entity

import (
	"math/rand"
	"testing"

	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

func TestParrotVariantsAreRolledAndSaved(t *testing.T) {
	seen := map[int]bool{}
	for i := range 200 {
		m := NewMob(TypeParrot, int32(i+1), 0, 64, 0, int64(i))
		m.FinalizeSpawn(GoDice{m.Rand}, testBiome("minecraft:jungle"), nil, rules.LocalDifficulty{})
		seen[m.ParrotVariant()] = true
	}
	if len(seen) != parrotVariants {
		t.Fatalf("rolled variants %v, want all %d", seen, parrotVariants)
	}
	m := NewMob(TypeParrot, 1, 0, 64, 0, 1)
	m.SetParrotVariant(4)
	saved := nbt.Compound{}
	m.SaveTo(saved, nil)
	back := NewMob(TypeParrot, 2, 0, 64, 0, 2)
	back.LoadFrom(saved, nil, nil)
	if back.ParrotVariant() != 4 {
		t.Fatalf("loaded variant %d, want 4", back.ParrotVariant())
	}
}

// TestAWanderingParrotFliesAndStaysAlive runs the parrot's goals and flight
// over open air: it must leave its start and take no fall damage.
func TestAWanderingParrotFliesAndStaysAlive(t *testing.T) {
	w := flatWorld(t)
	m := NewMob(TypeParrot, 1, 0.5, 64, 0.5, 1)
	for range 600 {
		m.tick(w, nil, []*Mob{m})
	}
	if m.Dead || m.Health != m.MaxHealth {
		t.Fatalf("health %v of %v, dead %v", m.Health, m.MaxHealth, m.Dead)
	}
	if m.X == 0.5 && m.Z == 0.5 {
		t.Error("the parrot never moved")
	}
}

// vanilla: Parrot.imitateNearbyMobs, Parrot.getPitch
func TestParrotImitationConsumesPitchDraws(t *testing.T) {
	w := flatWorld(t)
	for _, tc := range []struct {
		name   string
		kind   MobType
		silent bool
		sound  registry.Sound
	}{
		{"happy ghast", TypeHappyGhast, false, registry.SoundIntentionallyEmpty},
		{"zombie", TypeZombie, false, registry.SoundEntityParrotImitateZombie},
		{"silent happy ghast", TypeHappyGhast, true, registry.SoundNone},
		{"cow", TypeCow, false, registry.SoundNone},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := NewMob(TypeParrot, 1, 0.5, 64, 0.5, 1)
			other := NewMob(tc.kind, 2, 5.5, 64, 0.5, 1)
			other.Silent = tc.silent
			// Seed 516 opens both imitation gates on the first tick.
			m.SetParrotSoundRandom(rand.New(rand.NewSource(516)))
			m.Rand = rand.New(rand.NewSource(1))
			own := rand.New(rand.NewSource(1))
			want := rand.New(rand.NewSource(516))
			if want.Intn(400) != 0 || want.Intn(2) != 0 {
				t.Fatal("seed did not open the imitation gates")
			}
			if tc.kind != TypeCow {
				want.Intn(1)
			}
			var pitch float32
			if tc.sound != registry.SoundNone {
				pitch = (want.Float32()-want.Float32())*0.2 + 1
			}
			tick := tickView{w: w, others: []*Mob{m, other}}
			m.parrotStep(&tick)
			if tc.sound == registry.SoundNone {
				if len(tick.events) != 0 {
					t.Fatalf("unexpected imitation: %+v", tick.events)
				}
			} else if len(tick.events) != 1 {
				t.Errorf("got %d imitation events, want 1", len(tick.events))
			} else if ev := tick.events[0]; ev.Kind != MobSounded || ev.SoundID != tc.sound || ev.Volume != 0.7 || ev.Pitch != pitch {
				t.Errorf("imitation = %+v, want sound %v at pitch %g", ev, tc.sound, pitch)
			}
			if got, next := m.parrot.rng.Int63(), want.Int63(); got != next {
				t.Errorf("next level random draw = %d, want %d", got, next)
			}
			if got, next := m.Rand.Int63(), own.Int63(); got != next {
				t.Errorf("next entity random draw = %d, want %d", got, next)
			}
		})
	}
}

// vanilla: Parrot.aiStep, Parrot.imitateNearbyMobs
func TestParrotImitationUsesLevelRandom(t *testing.T) {
	w := flatWorld(t)
	for _, tc := range []struct {
		name                       string
		seed                       int64
		bounds                     []int
		silent, dead, targetSilent bool
		sound                      registry.Sound
	}{
		{name: "outer gate", seed: 1, bounds: []int{400}},
		{name: "inner gate", seed: 162, bounds: []int{400, 2}},
		{name: "silent parrot", seed: 516, bounds: []int{400}, silent: true},
		{name: "dead parrot", seed: 516, bounds: []int{400}, dead: true},
		{name: "first target", seed: 516, bounds: []int{400, 2, 2}, sound: registry.SoundEntityParrotImitateCreeper},
		{name: "second target", seed: 1562, bounds: []int{400, 2, 2}, sound: registry.SoundEntityParrotImitateZombie},
		{name: "silent target", seed: 1562, bounds: []int{400, 2, 2}, targetSilent: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, entitySeed := range []int64{1, 516} {
				m := NewMob(TypeParrot, 1, 0.5, 64, 0.5, 1)
				m.Rand = rand.New(rand.NewSource(entitySeed))
				own := rand.New(rand.NewSource(entitySeed))
				m.SetParrotSoundRandom(rand.New(rand.NewSource(tc.seed)))
				m.Silent, m.Dead = tc.silent, tc.dead
				creeper := NewMob(TypeCreeper, 2, 5.5, 64, 0.5, 1)
				zombie := NewMob(TypeZombie, 3, 6.5, 64, 0.5, 1)
				zombie.Silent = tc.targetSilent
				view := tickView{w: w, others: []*Mob{m, creeper, zombie}}
				want := rand.New(rand.NewSource(tc.seed))
				for _, bound := range tc.bounds {
					want.Intn(bound)
				}
				var pitch float32
				if tc.sound != registry.SoundNone {
					pitch = (want.Float32()-want.Float32())*0.2 + 1
				}
				m.parrotStep(&view)
				if tc.sound == registry.SoundNone {
					if len(view.events) != 0 {
						t.Fatalf("unexpected imitation: %+v", view.events)
					}
				} else if len(view.events) != 1 || view.events[0].SoundID != tc.sound || view.events[0].Pitch != pitch {
					t.Fatalf("imitation = %+v, want sound %v at pitch %g", view.events, tc.sound, pitch)
				}
				if got, next := m.parrot.rng.Int63(), want.Int63(); got != next {
					t.Errorf("next level draw = %d, want %d", got, next)
				}
				if got, next := m.Rand.Int63(), own.Int63(); got != next {
					t.Errorf("next entity draw = %d, want %d", got, next)
				}
			}
		})
	}
}

// vanilla: Parrot.getAmbientSound, Parrot.getVoicePitch
func TestParrotAmbientUsesLevelRandomAndEntityPitch(t *testing.T) {
	for _, tc := range []struct {
		name              string
		seed              int64
		peaceful, imitate bool
	}{
		{name: "ambient", seed: 1},
		{name: "imitation", seed: 1744, imitate: true},
		{name: "peaceful", seed: 1744, peaceful: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, entitySeed := range []int64{1, 1744} {
				m := NewMob(TypeParrot, 1, 0, 64, 0, 1)
				m.SetParrotSoundRandom(rand.New(rand.NewSource(tc.seed)))
				m.parrot.quiet = tc.peaceful
				m.Rand = rand.New(rand.NewSource(entitySeed))
				level := rand.New(rand.NewSource(tc.seed))
				own := rand.New(rand.NewSource(entitySeed))
				want := registry.SoundEntityParrotAmbient
				if !tc.peaceful {
					roll := level.Intn(1000)
					if (roll == 0) != tc.imitate {
						t.Fatal("seed did not choose expected ambient branch")
					}
				}
				if tc.imitate {
					want = parrotImitationSounds[level.Intn(len(parrotImitationSounds))]
				}
				// Reading hurt, death or step sounds must not roll an idle voice.
				m.Sounds()
				sound, volume, pitch := m.AmbientVoice()
				wantPitch := (own.Float32()-own.Float32())*0.2 + 1
				if sound != want || volume != 1 || pitch != wantPitch {
					t.Errorf("ambient = %v/%g/%g, want %v/1/%g", sound, volume, pitch, want, wantPitch)
				}
				if got, next := m.parrot.rng.Int63(), level.Int63(); got != next {
					t.Errorf("next level draw = %d, want %d", got, next)
				}
				if got, next := m.Rand.Int63(), own.Int63(); got != next {
					t.Errorf("next entity draw = %d, want %d", got, next)
				}
			}
		})
	}
}
