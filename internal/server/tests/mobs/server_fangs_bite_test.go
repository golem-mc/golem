package mobs

import (
	"testing"

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
	srv, _ := servertest.Join(t, servertest.MobTestConfig(), "Watcher")
	servertest.HoldTicks(t, srv)

	owner := srv.SpawnMob("minecraft:evoker", 16, 64, 0.5)
	west := srv.SpawnMob("minecraft:pig", 15.7, 64, 0.5)
	east := srv.SpawnMob("minecraft:pig", 16.3, 64, 0.5)
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
	all := []int32{owner, west, east, far, distant, allyVex, strayVex, wide}
	for _, id := range all {
		srv.SetMobNoAI(id)
		srv.SetMobHealth(id, 100)
	}

	srv.SpawnFangs(16, 64, 0.5, owner)
	servertest.RunTicks(t, srv, 8)
	for _, tc := range []struct {
		name string
		id   int32
		hurt bool
	}{
		{"west pig", west, true}, {"east pig", east, true}, {"far pig", far, false},
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
