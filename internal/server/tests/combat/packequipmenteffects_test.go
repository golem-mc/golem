package combat

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// packEnchantments writes the enchantment files (by path under data/) into a
// data pack of a fresh level directory and returns it. The registry tables are
// process-wide, so the tests using it are not parallel and put them back.
func packEnchantments(t *testing.T, files map[string]string) string {
	t.Helper()
	t.Cleanup(func() { registry.ApplyPackRegistries(nil); registry.ApplyPackEnchantments(nil) })
	dir := t.TempDir()
	pack := filepath.Join(dir, "datapacks", "p")
	files["pack.mcmeta"] = `{"pack":{"description":"p"}}`
	for file, body := range files {
		path := filepath.Join(pack, file)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

const helmetEnchantment = `{"anvil_cost":1,"description":{"text":"E"},"max_level":1,"weight":1,
	"min_cost":{"base":1,"per_level_above_first":0},"max_cost":{"base":50,"per_level_above_first":0},
	"slots":["head"],"supported_items":"#minecraft:enchantable/head_armor","effects":%s}`

// takeOffHelmet clicks the helmet in the head slot with an empty cursor and
// reports whether it stayed,.
func takeOffHelmet(t *testing.T, name, enchantment string, files map[string]string) (stayed bool) {
	t.Helper()
	cfg := servertest.BowTestConfig()
	cfg.LevelName = packEnchantments(t, files)
	srv, c := servertest.Dial(t, cfg)
	w := servertest.WindowedOf(t, name, c)
	servertest.HoldTicks(t, srv)
	srv.GiveSlot(name, player.ArmorSlot, servertest.EnchantedStack(t, "minecraft:iron_helmet", enchantment, 1))
	w.SettleTrackingState()
	w.Click(player.ArmorSlot, 0, protocol.ClickModePickup)
	// The click is the connection's own handling: the settle waits for it.
	w.SettleTrackingState()
	servertest.RunTicks(t, srv, 2)
	return !srv.InventorySlot(name, player.ArmorSlot).Empty()
}

// A pack enchantment with prevent_armor_change binds like Curse of Binding.
// vanilla: EnchantmentHelper.has(stack, PREVENT_ARMOR_CHANGE), ArmorSlot.mayPickup
func TestPackPreventArmorChangeBindsTheHelmet(t *testing.T) {
	files := map[string]string{
		"data/pk/enchantment/sticky.json": `{"anvil_cost":1,"description":{"text":"Sticky"},"max_level":1,"weight":1,
			"min_cost":{"base":1,"per_level_above_first":0},"max_cost":{"base":50,"per_level_above_first":0},
			"slots":["head"],"supported_items":"#minecraft:enchantable/head_armor",
			"effects":{"minecraft:prevent_armor_change":{}}}`,
	}
	if !takeOffHelmet(t, "Sticky", "pk:sticky", files) {
		t.Error("the helmet with pk:sticky came off, want it bound")
	}
}

// A pack enchantment's damage_immunity for #is_fire spares its wearer lava.
// vanilla: EnchantmentHelper.isImmuneToDamage, LivingEntity.isInvulnerableTo
func TestPackDamageImmunityKeepsTheWearerOutOfLavaHarm(t *testing.T) {
	const name = "Walker"
	files := map[string]string{
		"data/pk/enchantment/fireproof.json": `{"anvil_cost":1,"description":{"text":"Fireproof"},"max_level":1,"weight":1,
			"min_cost":{"base":1,"per_level_above_first":0},"max_cost":{"base":50,"per_level_above_first":0},
			"slots":["feet"],"supported_items":"#minecraft:enchantable/foot_armor",
			"effects":{"minecraft:damage_immunity":[{"effect":{},"requirements":{"condition":"minecraft:damage_source_properties",
			"predicate":{"tags":[{"id":"#minecraft:is_fire","expected":true}]}}}]}}`,
	}
	for _, tc := range []struct {
		boots string
		want  func(health float64) bool
		label string
	}{
		{"pk:fireproof", func(h float64) bool { return h == 20 }, "keeps full health"},
		{"minecraft:fire_protection", func(h float64) bool { return h < 20 }, "burns without the immunity"},
	} {
		t.Run(tc.boots, func(t *testing.T) {
			cfg := servertest.BowTestConfig()
			cfg.LevelName = packEnchantments(t, map[string]string{"data/pk/enchantment/fireproof.json": files["data/pk/enchantment/fireproof.json"]})
			srv, c, _ := servertest.JoinAs(t, cfg, name)
			servertest.HoldTicks(t, srv)
			srv.SetBlock(0, 64, 0, servertest.MustState(t, "minecraft:lava", nil))
			srv.GiveSlot(name, player.ArmorSlot+3, servertest.EnchantedStack(t, "minecraft:iron_boots", tc.boots, 1))
			c.MoveTo(0.5, 64, 0.5, 0, 0)
			servertest.RunTicks(t, srv, 20)
			if got := srv.PlayerHealth(name); !tc.want(got) {
				t.Errorf("health in lava with %s is %v, want it to %s", tc.boots, got, tc.label)
			}
		})
	}
}
