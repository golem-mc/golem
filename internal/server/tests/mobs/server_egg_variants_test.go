package mobs

import (
	"slices"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// holderEgg is a spawn egg carrying a registry-holder variant component.
func holderEgg(t *testing.T, egg string, component int32, registryName, entry string, builtin []string) protocol.ItemStack {
	t.Helper()
	entries := builtin
	if r, ok := registry.Get(registryName); ok {
		entries = r.Entries
	}
	id := slices.Index(entries, entry)
	if id < 0 {
		t.Fatalf("no %s in %s", entry, registryName)
	}
	stack := protocol.ItemStack{ItemID: servertest.MustItemID(t, egg), Count: 1}
	stack.SetEnum(component, int32(id))
	return stack
}

func enumEgg(t *testing.T, egg string, component, id int32) protocol.ItemStack {
	t.Helper()
	stack := protocol.ItemStack{ItemID: servertest.MustItemID(t, egg), Count: 1}
	stack.SetEnum(component, id)
	return stack
}

func savedField(t *testing.T, srv *server.Server, kind, path string) string {
	t.Helper()
	for _, text := range srv.RunLine("data get entity @e[type=" + kind + ",limit=1] " + path) {
		if text.Translate == "commands.data.entity.query" && len(text.With) == 2 {
			return text.With[1].Plain()
		}
	}
	t.Fatalf("no %s on %s: %+v", path, kind, srv.RunLine("data get entity @e[type="+kind+",limit=1] "+path))
	return ""
}

// TestSpawnEggsTakeTheirMobVariantComponents: a spawn egg's variant
// component decides the hatched mob (Entity.applyComponentsFromItemStack),
// read back from the saved tag.
func TestSpawnEggsTakeTheirMobVariantComponents(t *testing.T) {
	t.Parallel()
	srv, c := servertest.EggPlayer(t, "creative", "Breeder", "minecraft:wolf_spawn_egg", 1)
	for i, tc := range []struct {
		kind  string
		egg   protocol.ItemStack
		extra map[int32]int32 // further components on the egg, each a wire id
		want  [][2]string     // saved-tag path and value
	}{
		// the collar rides on the variant egg; a variant on one kind leaves the other alone
		{"minecraft:wolf", holderEgg(t, "minecraft:wolf_spawn_egg", protocol.DataComponentWolfVariant, "minecraft:wolf_variant", "minecraft:black", nil),
			map[int32]int32{protocol.DataComponentWolfCollar: 11}, [][2]string{{"variant", `"minecraft:black"`}, {"CollarColor", "11b"}}},
		{"minecraft:villager", holderEgg(t, "minecraft:villager_spawn_egg", protocol.DataComponentVillagerVariant, "minecraft:villager_type", "minecraft:snow",
			[]string{"minecraft:desert", "minecraft:jungle", "minecraft:plains", "minecraft:savanna", "minecraft:snow", "minecraft:swamp", "minecraft:taiga"}),
			nil, [][2]string{{"VillagerData.type", `"minecraft:snow"`}}},
		// a wolf collar on a sheep egg is not the sheep's
		{"minecraft:sheep", enumEgg(t, "minecraft:sheep_spawn_egg", protocol.DataComponentSheepColor, 14),
			map[int32]int32{protocol.DataComponentWolfCollar: 11}, [][2]string{{"Color", "14b"}}},
		{"minecraft:fox", enumEgg(t, "minecraft:fox_spawn_egg", protocol.DataComponentFoxVariant, 1),
			nil, [][2]string{{"Type", `"snow"`}}},
		{"minecraft:cat", holderEgg(t, "minecraft:cat_spawn_egg", protocol.DataComponentCatVariant, "minecraft:cat_variant", "minecraft:black", nil),
			map[int32]int32{protocol.DataComponentCatCollar: 1}, [][2]string{{"variant", `"minecraft:black"`}, {"CollarColor", "1b"}}},
		{"minecraft:pig", holderEgg(t, "minecraft:pig_spawn_egg", protocol.DataComponentPigVariant, "minecraft:pig_variant", "minecraft:cold", nil),
			nil, [][2]string{{"variant", `"minecraft:cold"`}}},
		{"minecraft:mooshroom", enumEgg(t, "minecraft:mooshroom_spawn_egg", protocol.DataComponentMooshroomVariant, 1),
			nil, [][2]string{{"Type", `"brown"`}}},
	} {
		for typ, id := range tc.extra {
			tc.egg.SetEnum(typ, id)
		}
		srv.GiveSlot("Breeder", player.HotbarSlot, tc.egg)
		c.PlaceBlock([...]int32{2, -2, 2, -2, 0, 0, 4}[i], 63, [...]int32{3, 3, -3, -3, 3, -3, 0}[i], protocol.FaceTop)
		servertest.AwaitTicks(t, srv, 3)
		for _, w := range tc.want {
			if got := savedField(t, srv, tc.kind, w[0]); got != w[1] {
				t.Errorf("%s %s = %s, want %s", tc.kind, w[0], got, w[1])
			}
		}
	}
}
