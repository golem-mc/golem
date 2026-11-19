package mobs

import (
	"github.com/golem-mc/golem/internal/server/servertest"

	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/rules"
)

// burningZombieBlow stands a zombie holding held, on fire, beside a player
// on a hard world whose effective difficulty is high enough to make the
// ignite roll certain, and returns the player's fire ticks after the first
// blow.
func burningZombieBlow(t *testing.T, kind string, held protocol.ItemStack, burning bool) int {
	t.Helper()
	srv, connect := servertest.Start(t, servertest.HostileTestConfig())
	srv.SetDifficulty(rules.DifficultyHard)
	// Midnight of day 72, a full moon long past the clock term's cap: 3 * 1.25.
	srv.SetDayTime(72*24000 + 18000)
	c := connect().Join("Target")
	// The blow's health report goes out before its ignite is applied, so
	// on a free loop the fire ticks read after the report can still be
	// the ones from before the blow; a held tick has ended by then.
	servertest.HoldTicks(t, srv)

	id := srv.SpawnMob(kind, 4.5, 64, 4.5)
	srv.SetMobHand(id, held)
	if burning {
		srv.SetMobFire(id, 1000)
	}
	hit := false
	pumpTicks(c, srv, 400, servertest.HostileSkip, func(pid int32, data []byte) bool {
		if pid == protocol.IDPlaySetHealth {
			if h := servertest.ExpectHealthReport(c, data); h.Health < 20 {
				hit = true
				return true
			}
		}
		return false
	})
	if !hit {
		t.Fatal("the mob never landed a blow")
	}
	return srv.PlayerFire("Target")
}

// TestABurningZombieSetsItsTargetAlight: an empty-handed zombie or husk on
// fire ignites what it hits for 2 * (int)effective seconds; a held item, or
// no fire on the body, leaves the target unburnt.
// vanilla: Zombie.doHurtTarget
func TestABurningZombieSetsItsTargetAlight(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"minecraft:zombie", "minecraft:husk"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			if fire := burningZombieBlow(t, kind, protocol.ItemStack{}, true); fire <= 0 {
				t.Error("a burning empty-handed blow left the player not burning")
			}
		})
	}
	t.Run("armed", func(t *testing.T) {
		t.Parallel()
		sword := protocol.ItemStack{ItemID: servertest.MustItem("minecraft:iron_sword"), Count: 1}
		if fire := burningZombieBlow(t, "minecraft:zombie", sword, true); fire > 0 {
			t.Errorf("an armed zombie's blow burnt the player for %d ticks", fire)
		}
	})
	t.Run("cold", func(t *testing.T) {
		t.Parallel()
		if fire := burningZombieBlow(t, "minecraft:zombie", protocol.ItemStack{}, false); fire > 0 {
			t.Errorf("a zombie not on fire burnt the player for %d ticks", fire)
		}
	})
}
