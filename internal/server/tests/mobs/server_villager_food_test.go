package mobs

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestTwoFedVillagersWithAFreeBedHaveAChild: two adults with twelve food
// points each court, and the child is born a baby once a bed is left over
// after both parents took theirs.
// vanilla: InteractWith (BREED_TARGET), VillagerMakeLove
func TestTwoFedVillagersWithAFreeBedHaveAChild(t *testing.T) {
	t.Parallel()
	const fed = ` {Inventory:[{id:"minecraft:bread",count:3}]}`
	villagersCourt(t, fed, fed, true)
}

// villagersCourt summons two villagers with the given NBT beside free beds
// and reports whether a child is born within the courtship window.
func villagersCourt(t *testing.T, first, second string, wantChild bool) {
	t.Helper()
	const name = "Matchmaker"
	srv, c, _ := servertest.JoinAs(t, servertest.MobTestConfig(), name, servertest.OpsListing(t, name))
	c.MoveToGround(0.5, 64, 0.5, true)
	c.Settle()
	servertest.HoldTicks(t, srv)
	srv.SetDayTime(100)
	for _, z := range []int32{4, 6, 8} {
		srv.SetBlock(3, 64, z, servertest.BedStateName("red", "head", "east"))
		srv.SetBlock(2, 64, z, servertest.BedStateName("red", "foot", "east"))
	}
	c.ChatCommand("summon minecraft:villager 0.5 64 0.5" + first)
	c.ChatCommand("summon minecraft:villager 1.5 64 0.5" + second)
	c.Settle()
	parents := srv.MobIDsOfKind("minecraft:villager")
	if len(parents) != 2 {
		t.Fatalf("villagers %v, want the two parents", parents)
	}
	for range 150 {
		c.ReadTicks(srv, 10, func(int32, []byte) bool { return false })
		if ids := srv.MobIDsOfKind("minecraft:villager"); len(ids) == 3 {
			if !wantChild {
				t.Fatal("a child was born without twelve food points")
			}
			if child := ids[2]; !srv.MobIsBaby(child) {
				t.Errorf("the child %d is not a baby", child)
			}
			if srv.MobIsBaby(parents[0]) || srv.MobIsBaby(parents[1]) {
				t.Error("a parent became a baby")
			}
			if inv := srv.MobInventory(parents[1]); len(inv) != 0 {
				t.Errorf("the bread-fed parent kept %v, want the bread eaten", inv)
			}
			return
		}
	}
	if wantChild {
		t.Errorf("villagers after 1500 ticks %v, want a child", srv.MobIDsOfKind("minecraft:villager"))
	}
}
