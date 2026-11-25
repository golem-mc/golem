package mobs

import (
	"slices"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestFangsBiteHurtsMobsOnBothSidesOfAChunkBorderAndSparesAllies: fangs on a
// chunk border hurt the mobs filed on either side in herd order, and spare a
// pig beside the bite, the owner standing in it and a vex whose summoner
// stands outside the sections the bite searches.
// vanilla: EvokerFangs.dealDamageTo, Evoker.considersEntityAsAlly
func TestFangsBiteHurtsMobsOnBothSidesOfAChunkBorderAndSparesAllies(t *testing.T) {
	t.Parallel()
	cfg := servertest.MobTestConfig()
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	srv, c := servertest.Join(t, cfg, "Watcher")
	servertest.HoldTicks(t, srv)

	owner := srv.SpawnMob("minecraft:evoker", 16, 64, 0.5)
	// East spawns first, so herd order differs from chunk order.
	east := srv.SpawnMob("minecraft:pig", 16.3, 64, 0.5)
	west := srv.SpawnMob("minecraft:pig", 15.7, 64, 0.5)
	// In a queried chunk but beyond the bite.
	near := srv.SpawnMob("minecraft:pig", 16, 64, 4.5)
	distant := srv.SpawnMob("minecraft:evoker", -16.5, 64, 0.5)
	allyVex := srv.SpawnMob("minecraft:vex", 16, 64, 0.5)
	strayVex := srv.SpawnMob("minecraft:vex", 16, 64, 0.5)
	srv.SetMobVexOwner(allyVex, distant)
	all := []int32{owner, west, east, near, distant, allyVex, strayVex}
	for _, id := range all {
		srv.SetMobNoAI(id)
		srv.SetMobHealth(id, 100)
	}

	srv.SpawnFangs(16, 64, 0.5, owner)
	c.Settle()
	servertest.RunTicks(t, srv, 8)
	var hurt []int32
	c.SettleAndCollect(func(pid int32, data []byte) {
		if pid == protocol.IDPlayDamageEvent {
			hurt = append(hurt, protocol.NewReader(data).ReadVarInt())
		}
	})
	if e, w := slices.Index(hurt, east), slices.Index(hurt, west); e < 0 || w < 0 || e > w {
		t.Errorf("damage order %v: want east pig %d before west pig %d", hurt, east, west)
	}
	for _, tc := range []struct {
		name string
		id   int32
		hurt bool
	}{
		{"west pig", west, true}, {"east pig", east, true}, {"pig beside the bite", near, false},
		{"owner", owner, false}, {"vex of a distant illager", allyVex, false},
		{"stray vex", strayVex, true},
	} {
		hp, ok := srv.MobHealth(tc.id)
		if !ok {
			t.Fatalf("%s is gone", tc.name)
		}
		if got := hp < 100; got != tc.hurt {
			t.Errorf("%s hurt = %v (health %v), want %v", tc.name, got, hp, tc.hurt)
		}
	}
}
