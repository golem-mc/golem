package registry

import (
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
		if _, result, ok := imbueResult(cells); ok {
			t.Errorf("an empty %dx%d grid imbues %+v", width, width, result)
		}
		if result := specialCraftResult(cells, nil); !result.Empty() {
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
