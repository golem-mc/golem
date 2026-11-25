package mobs

import (
	"os"
	"path/filepath"
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

const slowBoots = `{"anvil_cost":1,"description":{"text":"Slow"},"max_level":1,"weight":1,
	"min_cost":{"base":1,"per_level_above_first":0},"max_cost":{"base":50,"per_level_above_first":0},
	"slots":["feet"],"supported_items":"#minecraft:enchantable/foot_armor",
	"effects":{"minecraft:location_changed":[{"effect":{"type":"minecraft:attribute",
	"attribute":"minecraft:attack_speed","id":"pack:slow","amount":0.1,"operation":"add_value"}}]}}`

// TestLocationChangedLeavesKindlessAttributeOutOfSave: a location_changed
// attribute effect on boots adds no instance the mob's kind lacks.
// vanilla: EnchantmentAttributeEffect.onChangedBlock
func TestLocationChangedLeavesKindlessAttributeOutOfSave(t *testing.T) {
	// The registry tables are process-wide, so this test is not parallel.
	t.Cleanup(func() { registry.ApplyPackRegistries(nil); registry.ApplyPackEnchantments(nil) })
	dir := t.TempDir()
	files := map[string]string{
		"pack.mcmeta":                   `{"pack":{"description":"p"}}`,
		"data/pk/enchantment/slow.json": slowBoots,
	}
	for file, body := range files {
		path := filepath.Join(dir, "datapacks", "p", file)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cfg := servertest.MobTestConfig()
	cfg.LevelName = dir
	cfg.ViewDistance, cfg.SimulationDistance = 2, 2
	srv, c := servertest.Join(t, cfg, "Keeper", servertest.OpsListing(t, "Keeper"))
	servertest.HoldTicks(t, srv)
	c.ChatCommand(`summon minecraft:pig 1.5 100 0.5 {equipment:{feet:{id:"minecraft:iron_boots",count:1,components:{"minecraft:enchantments":{"pk:slow":1}}}}}`)
	c.ExpectMobSpawn(protocol.EntityTypePig)
	c.Settle()
	servertest.RunTicks(t, srv, 1)
	if got := savedField(t, srv, "pig", "attributes"); strings.Contains(got, "attack_speed") {
		t.Errorf("pig attributes list carries attack_speed: %s", got)
	}
}
