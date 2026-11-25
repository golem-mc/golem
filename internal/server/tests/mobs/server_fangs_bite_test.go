package mobs

import (
	"slices"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/nbt"
)

// TestFangsBiteHurtsMobsOnBothSidesOfAChunkBorderAndSparesAllies: fangs on a
// chunk border hurt the mobs filed on either side and a body whose feet are
// chunks away, and spare the owner standing in the bite and a vex whose summoner stands outside
// the search area.
// vanilla: EvokerFangs.dealDamageTo, Evoker.considersEntityAsAlly
func TestFangsBiteHurtsMobsOnBothSidesOfAChunkBorderAndSparesAllies(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.MobTestConfig(), "Watcher")
	servertest.HoldTicks(t, srv)

	owner := srv.SpawnMob("minecraft:evoker", 16, 64, 0.5)
	// East spawns first, so herd order differs from chunk order.
	east := srv.SpawnMob("minecraft:pig", 16.3, 64, 0.5)
	west := srv.SpawnMob("minecraft:pig", 15.7, 64, 0.5)
	// In a queried chunk but beyond the bite.
	near := srv.SpawnMob("minecraft:pig", 16, 64, 4.5)
	far := srv.SpawnMob("minecraft:pig", 0.5, 64, -40.5)
	distant := srv.SpawnMob("minecraft:evoker", -48.5, 64, 0.5)
	allyVex := srv.SpawnMob("minecraft:vex", 16, 64, 0.5)
	strayVex := srv.SpawnMob("minecraft:vex", 16, 64, 0.5)
	srv.SetMobVexOwner(allyVex, distant)
	// The ghast's feet are in chunk X=3, beyond the ordinary search padding.
	wide := srv.SpawnMobTagged("minecraft:ghast", 48.25, 64, 0.5, nbt.Compound{
		"attributes": nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{
			nbt.Compound{"id": nbt.String("minecraft:scale"), "base": nbt.Double(16)},
		}},
	})
	all := []int32{owner, west, east, near, far, distant, allyVex, strayVex, wide}
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
		{"west pig", west, true}, {"east pig", east, true}, {"far pig", far, false}, {"pig beside the bite", near, false},
		{"owner", owner, false}, {"vex of a distant illager", allyVex, false},
		{"stray vex", strayVex, true}, {"wide ghast", wide, true},
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
