package mobs

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// TestAZombieNautilusAndABabyZombieVillagerMoanInTheirOwnVoices drives a
// zombie nautilus in the pond and a baby zombie villager on the stone by
// hand and waits for each one's idle sound: the nautilus's own event, and the
// baby villager's moan at its kind's baby pitch, 2.0 ± 0.2.
// vanilla: Mob.baseTick, ZombieNautilus.getAmbientSound, ZombieVillager.getVoicePitch
func TestAZombieNautilusAndABabyZombieVillagerMoanInTheirOwnVoices(t *testing.T) {
	t.Parallel()
	const nautilusSound = "minecraft:entity.zombie_nautilus.ambient"
	const villagerSound = "minecraft:entity.zombie_villager.ambient"
	for _, event := range []string{nautilusSound, villagerSound} {
		if _, ok := registry.SoundEventID(event); !ok {
			t.Fatalf("the sound registry carries no %s", event)
		}
	}

	srv, c := squidTests(t)
	// Midnight: the villager would burn at noon, and a landed hurt restarts
	// the idle timer.
	srv.SetDayTime(18000)
	srv.SpawnMob("minecraft:zombie_nautilus", pondCentreX, pondY, -8.5)
	c.AwaitSpawn(protocol.EntityTypeZombieNautilus)
	villagerID := srv.SpawnMob("minecraft:zombie_villager", 2.5, 64, -8.5)
	c.AwaitSpawn(protocol.EntityTypeZombieVillager)
	srv.SetMobBaby(villagerID, true)

	var nautilusHeard, villagerHeard bool
	for tick := 0; tick < servertest.AmbientTicks && !(nautilusHeard && villagerHeard); tick += 10 {
		srv.TickMobs(10)
		c.ReadTicks(srv, 0, func(id int32, data []byte) bool {
			if id != protocol.IDPlaySoundEffect {
				return false
			}
			name, _, _, _, _, _, pitch := servertest.ReadAmbient(t, data)
			switch name {
			case nautilusSound:
				nautilusHeard = true
			case villagerSound:
				villagerHeard = true
				// ZombieVillager.getVoicePitch's baby voice, a hair
				// either side left for the rounding.
				if pitch < 1.79 || pitch > 2.21 {
					t.Errorf("the baby zombie villager moaned at pitch %v, want 2.0 ± 0.2", pitch)
				}
			}
			return false
		})
	}
	if !nautilusHeard {
		t.Errorf("the zombie nautilus never made its %s within %d ticks", nautilusSound, servertest.AmbientTicks)
	}
	if !villagerHeard {
		t.Errorf("the baby zombie villager never moaned within %d ticks", servertest.AmbientTicks)
	}
}
