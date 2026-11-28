package mobs

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/nbt"
)

// zombieVillagerHealth runs a zombie at night against a NoAI villager nine
// blocks away in a barrier pen for 200 ticks and returns the villager's health.
// vanilla: NearestAttackableTargetGoal, TargetingConditions.test
func zombieVillagerHealth(t *testing.T, wall, invisible bool) float64 {
	t.Helper()
	cfg := servertest.MobTestConfig()
	cfg.GameMode = "creative" // never a target
	srv, _ := servertest.Join(t, cfg, "Watcher")
	servertest.HoldTicks(t, srv)
	srv.SetDayTime(18000)
	srv.RunLine("tp Watcher 0.5 64 14.5")
	srv.RunLine("fill -9 64 -9 9 70 9 barrier")
	srv.RunLine("fill -8 64 -8 8 70 8 air")
	if wall {
		srv.RunLine("fill 0 64 -4 0 67 4 glass")
	}
	victim := nbt.Compound{"NoAI": nbt.Byte(1)}
	if invisible {
		victim["active_effects"] = nbt.NewList([]nbt.Tag{nbt.Compound{
			"id": nbt.String("minecraft:invisibility"), "duration": nbt.Int(100000),
		}})
	}
	v := srv.SpawnMobTagged("minecraft:villager", 4.5, 64, 0.5, victim)
	if v == 0 || srv.SpawnMob("minecraft:zombie", -4.5, 64, 0.5) == 0 {
		t.Fatal("could not spawn the pen's mobs")
	}
	servertest.RunTicks(t, srv, 200)
	hp, ok := srv.MobHealth(v)
	if !ok {
		return 0
	}
	return hp
}

func TestZombieDoesNotHuntVillagerBehindGlass(t *testing.T) {
	t.Parallel()
	if hp := zombieVillagerHealth(t, true, false); hp != 20 {
		t.Errorf("villager behind glass has %v health, want 20", hp)
	}
}

func TestZombieHuntsVillagerInTheOpen(t *testing.T) {
	t.Parallel()
	if hp := zombieVillagerHealth(t, false, false); hp >= 20 {
		t.Errorf("villager in the open has %v health, want it hurt", hp)
	}
}

func TestZombieDoesNotSpotInvisibleVillager(t *testing.T) {
	t.Parallel()
	if hp := zombieVillagerHealth(t, false, true); hp != 20 {
		t.Errorf("invisible villager has %v health, want 20", hp)
	}
}
