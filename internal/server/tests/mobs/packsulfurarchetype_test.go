package mobs

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

const packBouncyArchetype = `{
  "items": ["minecraft:diamond_block"],
  "attribute_modifiers": [
    {"attribute": "minecraft:bounciness", "id": "minecraft:bouncy_add_bounciness", "amount": 0.9, "operation": "add_value"}
  ],
  "buoyant": true,
  "knockback_modifiers": {"horizontal_power": 0.4125, "vertical_power": 0.105},
  "sound_settings": {
    "hit_sound": "minecraft:entity.sulfur_cube.bouncy.hit",
    "push_sound": "minecraft:entity.sulfur_cube.bouncy.push",
    "push_sound_cooldown": 0.7,
    "push_sound_impulse_threshold": 0.3
  }
}`

// TestAPackSulfurArchetypeChoosesWhichBlocksItAppliesTo: a pack's bouncy
// archetype with items [diamond_block] puts the bounciness modifier on a cube
// holding a diamond block and not on one holding planks, which the built-in
// archetype matched. The tables are process-wide, so the test is not parallel.
// vanilla: SulfurCube.matchingArchetypes, SulfurCubeArchetype.DIRECT_CODEC
func TestAPackSulfurArchetypeChoosesWhichBlocksItAppliesTo(t *testing.T) {
	t.Cleanup(func() { registry.ApplySulfurArchetypes(nil); registry.ApplyPackRegistries(nil) })
	cfg := servertest.HostileTestConfig()
	cfg.GameMode = "creative"
	cfg.LevelName = t.TempDir()
	pack := filepath.Join(cfg.LevelName, "datapacks", "p")
	for file, body := range map[string]string{
		"pack.mcmeta": `{"pack":{"description":"p"}}`,
		"data/minecraft/sulfur_cube_archetype/bouncy.json":      packBouncyArchetype,
		"data/minecraft/tags/item/sulfur_cube_swallowable.json": `{"values":["minecraft:diamond_block"]}`,
	} {
		path := filepath.Join(pack, file)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	srv, c := servertest.Join(t, cfg, "Keeper", servertest.OpsListing(t, "Keeper"))
	servertest.HoldTicks(t, srv)
	bounciness := registry.Attributes[registry.AttributeBounciness].ID
	for _, tc := range []struct {
		item string
		want bool
	}{{"diamond_block", true}, {"oak_planks", false}} {
		c.ChatCommand(`summon minecraft:sulfur_cube 4.5 64 7.5 {NoAI:1b,Age:0,equipment:{body:{id:"minecraft:` + tc.item + `",count:1}}}`)
		got, seen := false, false
		c.SettleAndCollect(func(id int32, data []byte) {
			if id != protocol.IDPlayUpdateAttributes {
				return
			}
			_, entries := servertest.ReadAttributes(t, data)
			_, ok := servertest.Modifier(entries[bounciness], "minecraft:bouncy_add_bounciness")
			got, seen = got || ok, true
		})
		if !seen {
			t.Fatalf("%s: no Update Attributes for the spawned cube", tc.item)
		}
		if got != tc.want {
			t.Errorf("%s: bounciness modifier on the wire = %v, want %v", tc.item, got, tc.want)
		}
	}
}
