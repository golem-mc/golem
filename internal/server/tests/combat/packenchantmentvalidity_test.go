package combat

import (
	"slices"
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// A projectile_spawned entry whose effect has no type is rejected by vanilla's
// codec, so the load fails naming the enchantment and nothing is served.
// vanilla: Enchantment.DIRECT_CODEC, EnchantmentEntityEffect.CODEC
func TestPackInvalidEnchantmentFailsLoad(t *testing.T) {
	cfg := servertest.MobTestConfig()
	cfg.LevelName = servertest.PackEnchantments(t, map[string]string{
		"data/pk/enchantment/ammo.json": spawnEnchantment(`{"effect":{"run":"pk:x"}}`),
	})
	_, err := server.NewSeeded(cfg, 1)
	if err == nil || !strings.Contains(err.Error(), "pk:ammo") {
		t.Fatalf("load error = %v, want one naming pk:ammo", err)
	}
	if ench, ok := registry.Get("minecraft:enchantment"); ok && slices.Contains(ench.Entries, "pk:ammo") {
		t.Error("the rejected enchantment is in the registry sent to clients")
	}
}

// A valid effect the tree cannot represent (an impulse not along the look)
// still loads, warns, and reaches the client unchanged.
// vanilla: ApplyImpulse.CODEC
func TestPackUnimplementedEnchantmentEffectStillSent(t *testing.T) {
	cfg := servertest.MobTestConfig()
	cfg.LevelName = servertest.PackEnchantments(t, map[string]string{
		"data/pk/enchantment/ammo.json": spawnEnchantment(`{"effect":{"type":"minecraft:apply_impulse","direction":[0,1,0],"coordinate_scale":[1,1,1],"magnitude":1}}`),
	})
	servertest.JoinAs(t, cfg, "Joiner")
	ench, _ := registry.Get("minecraft:enchantment")
	if !slices.Contains(ench.Entries, "pk:ammo") {
		t.Error("pk:ammo is not in the registry sent to the client")
	}
}

// A requirement the loot codec cannot represent (an environment attribute that
// is not a float) is valid in vanilla, so the enchantment loads and is sent.
// vanilla: EnvironmentAttributeCheck.CODEC
func TestPackUnsupportedEnchantmentRequirementStillSent(t *testing.T) {
	cfg := servertest.MobTestConfig()
	cfg.LevelName = servertest.PackEnchantments(t, map[string]string{
		"data/pk/enchantment/ammo.json": spawnEnchantment(`{"effect":{"type":"minecraft:apply_impulse","direction":[0,1,0],"coordinate_scale":[1,1,1],"magnitude":1},"requirements":{"type":"minecraft:environment_attribute_check","attribute":"minecraft:gameplay/can_start_raid","value":true}}`),
	})
	servertest.JoinAs(t, cfg, "Joiner")
	ench, _ := registry.Get("minecraft:enchantment")
	if !slices.Contains(ench.Entries, "pk:ammo") {
		t.Error("pk:ammo is not in the registry sent to the client")
	}
}
