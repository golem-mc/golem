package registry

import (
	"slices"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
)

func TestEmptyGridCraftsNothing(t *testing.T) {
	for _, width := range []int32{2, 3} {
		cells := make([]protocol.ItemStack, width*width)
		if id, result := CraftMatch(cells, width); id != "" || !result.Empty() {
			t.Errorf("an empty %dx%d grid crafts %q %+v", width, width, id, result)
		}
		if _, result, ok := stewResult(cells); ok {
			t.Errorf("an empty %dx%d grid makes stew %+v", width, width, result)
		}
		if result := specialCraftResult(cells); !result.Empty() {
			t.Errorf("an empty %dx%d grid makes special %+v", width, width, result)
		}
	}
}

func TestCraftMatchPickaxeAndSticks(t *testing.T) {
	grids := craftBenchGrids()
	for _, c := range []struct {
		name string
		want Item
	}{{"pickaxe", ItemWoodenPickaxe}, {"sticks", ItemStick}, {"nomatch9", 0}} {
		_, result := CraftMatch(grids[c.name], 3)
		if Item(result.ItemID) != c.want || result.Empty() != (c.want == 0) {
			t.Errorf("%s crafts %+v, want item %d", c.name, result, c.want)
		}
	}
}

// craftBenchGrids returns the 3x3 grids BenchmarkCraftMatch times.
func craftBenchGrids() map[string][]protocol.ItemStack {
	plank := protocol.ItemStack{ItemID: int32(ItemOakPlanks), Count: 1}
	stick := protocol.ItemStack{ItemID: int32(ItemStick), Count: 1}
	none := protocol.ItemStack{}
	nomatch := make([]protocol.ItemStack, 9)
	for i := range nomatch {
		nomatch[i] = plank
		if i%2 == 1 {
			nomatch[i] = stick
		}
	}
	return map[string][]protocol.ItemStack{
		"empty3x3": make([]protocol.ItemStack, 9),
		"pickaxe": {
			plank, plank, plank,
			none, stick, none,
			none, stick, none,
		},
		"sticks": {
			none, plank, none,
			none, plank, none,
			none, none, none,
		},
		"nomatch9": nomatch,
	}
}

func BenchmarkCraftMatch(b *testing.B) {
	grids := craftBenchGrids()
	for _, name := range []string{"empty3x3", "pickaxe", "sticks", "nomatch9"} {
		cells := grids[name]
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				CraftMatch(cells, 3)
			}
		})
	}
}

// vanilla: TransmuteRecipe, DyeRecipe, ImbueRecipe
func TestBuiltInRewritingRecipes(t *testing.T) {
	stack := func(name string, n int32) protocol.ItemStack {
		id, ok := ItemID(name)
		if !ok {
			t.Fatalf("no item %s", name)
		}
		return protocol.ItemStack{ItemID: id, Count: n}
	}
	for _, c := range []struct {
		name  string
		cells []protocol.ItemStack
		id    string
		want  protocol.ItemStack
	}{
		{"bundle dye", []protocol.ItemStack{stack("minecraft:bundle", 1), stack("minecraft:blue_dye", 1)}, "minecraft:blue_bundle", stack("minecraft:blue_bundle", 1)},
		{"same colour", []protocol.ItemStack{stack("minecraft:blue_bundle", 1), stack("minecraft:blue_dye", 1)}, "", protocol.ItemStack{}},
		{"map clone", []protocol.ItemStack{stack("minecraft:filled_map", 1), stack("minecraft:map", 1), stack("minecraft:map", 1)}, "minecraft:map_cloning", stack("minecraft:filled_map", 3)},
		{"eight maps", append([]protocol.ItemStack{stack("minecraft:filled_map", 1)}, slices.Repeat([]protocol.ItemStack{stack("minecraft:map", 1)}, 8)...), "minecraft:map_cloning", stack("minecraft:filled_map", 9)},
		{"tipped arrow", append(slices.Repeat([]protocol.ItemStack{stack("minecraft:arrow", 1)}, 4), append([]protocol.ItemStack{stack("minecraft:lingering_potion", 1)}, slices.Repeat([]protocol.ItemStack{stack("minecraft:arrow", 1)}, 4)...)...), "minecraft:tipped_arrow", stack("minecraft:tipped_arrow", 8)},
		{"wolf armour", []protocol.ItemStack{stack("minecraft:wolf_armor", 1), stack("minecraft:red_dye", 1)}, "minecraft:wolf_armor_dyed", func() protocol.ItemStack {
			s := stack("minecraft:wolf_armor", 1)
			s.SetDyedColor(0xB02E26)
			return s
		}()},
	} {
		width := int32(3)
		cells := append(slices.Clone(c.cells), make([]protocol.ItemStack, 9-len(c.cells))...)
		id, got := CraftMatch(cells, width)
		if id != c.id || !got.Equal(c.want) {
			t.Errorf("%s: crafted %q %+v, want %q %+v", c.name, id, got, c.id, c.want)
		}
	}
}
