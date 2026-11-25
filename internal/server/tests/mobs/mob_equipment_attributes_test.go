package mobs

import (
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// TestEquipmentLeavesKindlessAttributesOutOfSave: an equipment modifier for an
// attribute the mob's kind lacks creates no instance (AttributeMap.getInstance
// is null), so neither the pairing nor the saved list carries it. The fresh
// pig takes the build path; the zombie gets a live map from /attribute before
// it is equipped, which takes the tick sync path.
func TestEquipmentLeavesKindlessAttributesOutOfSave(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.MobTestConfig(), "Keeper", servertest.OpsListing(t, "Keeper"))
	servertest.HoldTicks(t, srv)
	sword := `minecraft:iron_sword`
	c.ChatCommand(`summon minecraft:pig 1.5 100 0.5 {equipment:{mainhand:{id:"` + sword + `",count:1}}}`)
	pig := c.ExpectMobSpawn(protocol.EntityTypePig)
	if _, ok := pig.Attributes[int32(registry.AttributeAttackSpeed)]; ok {
		t.Errorf("pig pairing carries attack_speed: %v", pig.Attributes)
	}
	c.ChatCommand("summon minecraft:zombie 4.5 100 0.5")
	c.ExpectMobSpawn(protocol.EntityTypeZombie)
	c.Settle()
	srv.RunLine("attribute @e[type=zombie,limit=1] minecraft:movement_speed get")
	srv.RunLine("item replace entity @e[type=zombie,limit=1] weapon.mainhand with " + sword)
	servertest.RunTicks(t, srv, 5)
	pigAttrs := savedField(t, srv, "pig", "attributes")
	for _, id := range []string{"attack_damage", "attack_speed"} {
		if strings.Contains(pigAttrs, id) {
			t.Errorf("pig attributes list %s: %s", id, pigAttrs)
		}
	}
	zombie := savedField(t, srv, "zombie", "attributes")
	if strings.Contains(zombie, "attack_speed") || !strings.Contains(zombie, "attack_damage") {
		t.Errorf("zombie attributes: %s", zombie)
	}
	got := srv.RunLine("attribute @e[type=zombie,limit=1] minecraft:attack_damage get")
	if len(got) != 1 || got[0].With[len(got[0].With)-1].Plain() != "8.0" {
		t.Errorf("zombie attack_damage: %+v", got)
	}
}

// TestSavedAttributesDropKindlessEntries: a saved list entry for an attribute
// the kind lacks is skipped on load and absent from the next save, while an
// unknown ID passes through.
// vanilla: AttributeMap.load, AttributeSupplier.hasAttribute
func TestSavedAttributesDropKindlessEntries(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.MobTestConfig(), "Keeper", servertest.OpsListing(t, "Keeper"))
	servertest.HoldTicks(t, srv)
	c.ChatCommand(`summon minecraft:pig 1.5 100 0.5 {attributes:[{id:"minecraft:attack_damage",base:2d},{id:"minecraft:attack_speed",base:4d},{id:"golem:unknown",base:1d}]}`)
	c.ExpectMobSpawn(protocol.EntityTypePig)
	c.Settle()
	servertest.RunTicks(t, srv, 2)
	got := savedField(t, srv, "pig", "attributes")
	for _, id := range []string{"attack_damage", "attack_speed"} {
		if strings.Contains(got, id) {
			t.Errorf("pig attributes list %s: %s", id, got)
		}
	}
	if !strings.Contains(got, "golem:unknown") {
		t.Errorf("pig attributes list lost the unknown entry: %s", got)
	}
}
