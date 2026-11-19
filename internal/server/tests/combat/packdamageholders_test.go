package combat

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// vanilla: TagPredicate.matches, Enchantment.getDamageProtection
func TestPackEnchantmentDamageHolderSets(t *testing.T) {
	t.Cleanup(func() { registry.ApplyPackRegistries(nil); registry.ApplyPackEnchantments(nil) })
	cases := []struct {
		name, id string
		expected bool
		members  []string
	}{
		{"single", `"minecraft:lava"`, true, []string{"lava"}},
		{"short", `"lava"`, true, []string{"lava"}},
		{"list", `["minecraft:lava","minecraft:fall"]`, true, []string{"lava", "fall"}},
		{"empty", `[]`, true, nil},
		{"tag", `"#minecraft:is_fire"`, true, []string{"lava"}},
		{"not_single", `"minecraft:lava"`, false, []string{"lava"}},
		{"not_list", `["minecraft:lava","minecraft:fall"]`, false, []string{"lava", "fall"}},
		{"not_empty", `[]`, false, nil},
		{"not_tag", `"#minecraft:is_fire"`, false, []string{"lava"}},
	}
	cfg := servertest.SurvivalTestConfig()
	cfg.LevelName = t.TempDir()
	pack := filepath.Join(cfg.LevelName, "datapacks", "holders")
	files := map[string]string{"pack.mcmeta": `{"pack":{"description":"holders"}}`}
	for _, tc := range cases {
		files["data/pk/enchantment/"+tc.name+".json"] = fmt.Sprintf(`{
   "supported_items":"minecraft:elytra","max_level":1,"slots":["chest"],
   "effects":{"minecraft:damage_protection":[{"effect":{"type":"minecraft:add","value":10},
   "requirements":{"type":"minecraft:damage_source_properties","predicate":{"tags":[{"id":%s,"expected":%t}]}}}]}}`, tc.id, tc.expected)
	}
	for file, body := range files {
		path := filepath.Join(pack, file)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
	}
	const name = "Protected"
	srv, c, _ := servertest.JoinAs(t, cfg, name, servertest.OpsListing(t, name))
	servertest.HoldTicks(t, srv)
	for _, tc := range cases {
		srv.GiveSlot(name, 6, servertest.EnchantedStack(t, "minecraft:elytra", "pk:"+tc.name, 1))
		for _, damage := range []string{"lava", "fall", "generic"} {
			srv.SetHealth(name, 20)
			c.ChatCommand("damage @s 10 minecraft:" + damage)
			c.Settle()
			servertest.UntilTicks(t, srv, 10, "the damage lands", func() bool { return srv.PlayerHealth(name) < 20 })
			member := slices.Contains(tc.members, damage)
			want := 10.0
			if member == tc.expected {
				want = 14
			}
			if got := srv.PlayerHealth(name); math.Abs(got-want) > 0.001 {
				t.Errorf("%s/%s: health %v, want %v", tc.name, damage, got, want)
			}
		}
	}
}
