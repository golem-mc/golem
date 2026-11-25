package mobs

import (
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestEquipmentLeavesKindlessAttributesOutOfSave: an equipment modifier for an
// attribute the mob's kind lacks creates no instance (AttributeMap.getInstance
// is null), so the saved list omits it.
func TestEquipmentLeavesKindlessAttributesOutOfSave(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.MobTestConfig(), "Keeper", servertest.OpsListing(t, "Keeper"))
	servertest.HoldTicks(t, srv)
	sword := `equipment:{mainhand:{id:"minecraft:iron_sword",count:1}}`
	c.ChatCommand("summon minecraft:pig 1.5 100 0.5 {" + sword + "}")
	c.ExpectMobSpawn(protocol.EntityTypePig)
	c.ChatCommand("summon minecraft:zombie 4.5 100 0.5 {" + sword + "}")
	c.ExpectMobSpawn(protocol.EntityTypeZombie)
	c.Settle()
	servertest.RunTicks(t, srv, 5)
	pig := savedField(t, srv, "pig", "attributes")
	for _, id := range []string{"attack_damage", "attack_speed"} {
		if strings.Contains(pig, id) {
			t.Errorf("pig attributes list %s: %s", id, pig)
		}
	}
	zombie := savedField(t, srv, "zombie", "attributes")
	if strings.Contains(zombie, "attack_speed") || !strings.Contains(zombie, "attack_damage") {
		t.Errorf("zombie attributes: %s", zombie)
	}
	got := srv.RunLine("attribute @e[type=zombie,limit=1] attack_damage get")
	if len(got) != 1 || !strings.Contains(got[0].Plain()+got[0].With[len(got[0].With)-1].Plain(), "8.0") {
		t.Errorf("zombie attack_damage: %+v", got)
	}
}
