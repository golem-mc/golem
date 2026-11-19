package sounds

import (
	"github.com/golem-mc/golem/internal/server/servertest"

	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/registry"
)

// The ambient sound tests: the idle mutter a mob plays while it lives, over a
// real connection with the tick loop stopped so the test drives the mobs by
// hand and waits out the ticks the ambient timer needs. The moan travels as a
// Sound Effect in the kind's own category, at the kind's own volume and the
// pitch the voice rolls: 1.0 ± 0.2, a baby's 1.5 ± 0.2.

// ambientKind is one mob kind whose idle sound a test waits for: the kind to
// plant, where to plant it, the entity type its spawn carries, the sound
// event its moan travels as, the category the moan plays in and the volume
// the kind's own voice carries.
type ambientKind struct {
	name       string
	at         [3]float64
	spawnType  int32
	sound      string
	category   int32
	wantVolume float32
}

// ambientTestKinds is every kind this server simulates whose idle sound
// plays: the four animals, whose idle sound travels in the neutral bracket,
// and the zombie, whose own travels in the hostile one.
var ambientTestKinds = []ambientKind{
	{"cow", [3]float64{2.5, 64, 1.5}, protocol.EntityTypeCow,
		"minecraft:entity.cow.ambient", protocol.SoundCategoryNeutral, 0.4},
	{"pig", [3]float64{-2.5, 64, 1.5}, protocol.EntityTypePig,
		"minecraft:entity.pig.ambient", protocol.SoundCategoryNeutral, 1.0},
	{"sheep", [3]float64{1.5, 64, -2.5}, protocol.EntityTypeSheep,
		"minecraft:entity.sheep.ambient", protocol.SoundCategoryNeutral, 1.0},
	{"chicken", [3]float64{-1.5, 64, -2.5}, protocol.EntityTypeChicken,
		"minecraft:entity.chicken.ambient", protocol.SoundCategoryNeutral, 1.0},
	{"zombie", [3]float64{2.5, 64, -14.5}, protocol.EntityTypeZombie,
		"minecraft:entity.zombie.ambient", protocol.SoundCategoryHostile, 1.0},
}

// checkAmbient asserts one decoded moan against what its kind carries: the
// category, the volume, the pitch within the voice's own spread, and the
// position at the body's own height near where it was planted.
func checkAmbient(t *testing.T, what string, k ambientKind, x, y, z, category int32, volume, pitch float32) {
	t.Helper()
	if category != k.category {
		t.Errorf("the %s's moan played in category %d, want %d", what, category, k.category)
	}
	if volume != k.wantVolume {
		t.Errorf("the %s's moan played at volume %v, want the kind's %v", what, volume, k.wantVolume)
	}
	// The voice's spread, LivingEntity.getVoicePitch: 1.0 ± 0.2, a hair
	// either side left for the rounding.
	if pitch < 0.79 || pitch > 1.21 {
		t.Errorf("the %s's moan played at pitch %v, want the voice's own spread around one", what, pitch)
	}
	// The moan plays where the body stands: at the height of the floor it
	// was planted on, wherever the walk since has carried it.
	if y < 64*8-16 || y > 64*8+16 {
		t.Errorf("the %s's moan played at y=%d eighths, want the floor it stands on", what, y)
	}
	// Sixty-four blocks of slack around the planting spot: the stroll
	// carries a body about, and the zombie's hunt carries it toward the
	// player, but neither out of the neighbourhood.
	if dx := x - int32(k.at[0]*8); dx < -64*8 || dx > 64*8 {
		t.Errorf("the %s's moan played %d eighths from where it was planted, want it near the body", what, dx)
	}
	if dz := z - int32(k.at[2]*8); dz < -64*8 || dz > 64*8 {
		t.Errorf("the %s's moan played %d eighths from where it was planted, want it near the body", what, dz)
	}
}

// TestAPlantedAnimalMoansWithTheAmbientSoundOfItsKind plants every kind whose
// idle sound plays, walks their ticks by hand and waits for each one's moan:
// the sound event of its own, in the category its sort plays in, at the
// volume its kind carries and the pitch its voice rolls.
func TestAPlantedAnimalMoansWithTheAmbientSoundOfItsKind(t *testing.T) {
	t.Parallel()

	for _, k := range ambientTestKinds {
		if _, ok := registry.SoundEventID(k.sound); !ok {
			t.Fatalf("the sound registry carries no %s", k.sound)
		}
	}

	srv, connect := servertest.Start(t, servertest.MobTestConfig())
	srv.StopTick()
	// Midnight: a zombie in the noonday sun burns, and a landed hurt
	// restarts the idle timer, so the burn would hold its moan off.
	srv.SetDayTime(18000)
	c := connect().Join("Shepherd")

	pending := map[string]bool{}
	for _, k := range ambientTestKinds {
		id := srv.SpawnMob("minecraft:"+k.name, k.at[0], k.at[1], k.at[2])
		c.ExpectMobSpawn(k.spawnType)
		// The classic voice, whatever the finalize's dice chose; the
		// variants get their own test.
		srv.SetMobSoundVariant(id, "minecraft:classic")
		pending[k.name] = true
	}

	// The wait: the mobs' ticks driven by hand, the moans read off the wire
	// as they come, until every kind has been heard or the stretch the
	// ambient timer needs has run out.
	for tick := 0; tick < servertest.AmbientTicks && len(pending) > 0; tick += 10 {
		srv.TickMobs(10)
		c.ReadTicks(srv, 0, func(id int32, data []byte) bool {
			if id != protocol.IDPlaySoundEffect {
				return false
			}
			name, category, x, y, z, volume, pitch := servertest.ReadAmbient(t, data)
			for _, k := range ambientTestKinds {
				if !pending[k.name] || name != k.sound {
					continue
				}
				delete(pending, k.name)
				checkAmbient(t, k.name, k, x, y, z, category, volume, pitch)
			}
			return false
		})
	}
	for _, k := range ambientTestKinds {
		if pending[k.name] {
			t.Errorf("the %s never moaned within %d ticks, want its %s", k.name, servertest.AmbientTicks, k.sound)
		}
	}
}

// TestABabyCowsMoanPlaysAroundOnePointFive turns the planted cow's age down
// and waits for its moan: the same quiet sound event its kind carries, at the
// pitch a baby's voice rolls, 1.5 ± 0.2.
func TestABabyCowsMoanPlaysAroundOnePointFive(t *testing.T) {
	t.Parallel()

	srv, connect := servertest.Start(t, servertest.MobTestConfig())
	srv.StopTick()
	c := connect().Join("Calfkeeper")

	cowID := srv.SpawnMob("minecraft:cow", 2.5, 64, 1.5)
	c.ExpectMobSpawn(protocol.EntityTypeCow)
	srv.SetMobBaby(cowID, true)

	moaned := false
	for tick := 0; tick < servertest.AmbientTicks && !moaned; tick += 10 {
		srv.TickMobs(10)
		c.ReadTicks(srv, 0, func(id int32, data []byte) bool {
			if id != protocol.IDPlaySoundEffect {
				return false
			}
			name, category, _, _, _, volume, pitch := servertest.ReadAmbient(t, data)
			if name != "minecraft:entity.cow.ambient" {
				return false
			}
			moaned = true
			if category != protocol.SoundCategoryNeutral {
				t.Errorf("the calf's moan played in category %d, want the neutral bracket", category)
			}
			if volume != 0.4 {
				t.Errorf("the calf's moan played at volume %v, want the kind's 0.4", volume)
			}
			// The baby voice's spread, LivingEntity.getVoicePitch's baby
			// half: 1.5 ± 0.2.
			if pitch < 1.29 || pitch > 1.71 {
				t.Errorf("the calf's moan played at pitch %v, want the baby voice's spread around one and a half", pitch)
			}
			return false
		})
	}
	if !moaned {
		t.Errorf("the calf never moaned within %d ticks", servertest.AmbientTicks)
	}
}

// TestABabyPigMoansItsBabySound turns the planted piglet's age down and
// waits for its moan: the baby event its kind's getSoundSet swaps in while
// it isBaby, not the grown one, at the pitch a baby's voice rolls.
func TestABabyPigMoansItsBabySound(t *testing.T) {
	t.Parallel()

	if _, ok := registry.SoundEventID("minecraft:entity.baby_pig.ambient"); !ok {
		t.Fatal("the sound registry carries no minecraft:entity.baby_pig.ambient")
	}

	srv, connect := servertest.Start(t, servertest.MobTestConfig())
	srv.StopTick()
	c := connect().Join("Pigkeeper")

	pigID := srv.SpawnMob("minecraft:pig", 2.5, 64, 1.5)
	c.ExpectMobSpawn(protocol.EntityTypePig)
	srv.SetMobBaby(pigID, true)

	moaned := false
	for tick := 0; tick < servertest.AmbientTicks && !moaned; tick += 10 {
		srv.TickMobs(10)
		c.ReadTicks(srv, 0, func(id int32, data []byte) bool {
			if id != protocol.IDPlaySoundEffect {
				return false
			}
			name, category, _, _, _, volume, pitch := servertest.ReadAmbient(t, data)
			if name != "minecraft:entity.baby_pig.ambient" {
				return false
			}
			moaned = true
			if category != protocol.SoundCategoryNeutral {
				t.Errorf("the piglet's moan played in category %d, want the neutral bracket", category)
			}
			if volume != 1.0 {
				t.Errorf("the piglet's moan played at volume %v, want the kind's 1.0", volume)
			}
			// The baby voice's spread, LivingEntity.getVoicePitch's baby
			// half: 1.5 ± 0.2.
			if pitch < 1.29 || pitch > 1.71 {
				t.Errorf("the piglet's moan played at pitch %v, want the baby voice's spread around one and a half", pitch)
			}
			return false
		})
	}
	if !moaned {
		t.Errorf("the piglet never moaned within %d ticks", servertest.AmbientTicks)
	}
}

// TestABabyChickenMoansItsBabySound turns the planted chick's age down and
// waits for its moan: the baby event its kind's getSoundSet swaps in while
// it isBaby, not the grown one, at the pitch a baby's voice rolls.
func TestABabyChickenMoansItsBabySound(t *testing.T) {
	t.Parallel()

	if _, ok := registry.SoundEventID("minecraft:entity.baby_chicken.ambient"); !ok {
		t.Fatal("the sound registry carries no minecraft:entity.baby_chicken.ambient")
	}

	srv, connect := servertest.Start(t, servertest.MobTestConfig())
	srv.StopTick()
	c := connect().Join("Chickkeeper")

	chickenID := srv.SpawnMob("minecraft:chicken", 2.5, 64, 1.5)
	c.ExpectMobSpawn(protocol.EntityTypeChicken)
	srv.SetMobBaby(chickenID, true)

	moaned := false
	for tick := 0; tick < servertest.AmbientTicks && !moaned; tick += 10 {
		srv.TickMobs(10)
		c.ReadTicks(srv, 0, func(id int32, data []byte) bool {
			if id != protocol.IDPlaySoundEffect {
				return false
			}
			name, category, _, _, _, volume, pitch := servertest.ReadAmbient(t, data)
			if name != "minecraft:entity.baby_chicken.ambient" {
				return false
			}
			moaned = true
			if category != protocol.SoundCategoryNeutral {
				t.Errorf("the chick's moan played in category %d, want the neutral bracket", category)
			}
			if volume != 1.0 {
				t.Errorf("the chick's moan played at volume %v, want the kind's 1.0", volume)
			}
			// The baby voice's spread, LivingEntity.getVoicePitch's baby
			// half: 1.5 ± 0.2.
			if pitch < 1.29 || pitch > 1.71 {
				t.Errorf("the chick's moan played at pitch %v, want the baby voice's spread around one and a half", pitch)
			}
			return false
		})
	}
	if !moaned {
		t.Errorf("the chick never moaned within %d ticks", servertest.AmbientTicks)
	}
}
