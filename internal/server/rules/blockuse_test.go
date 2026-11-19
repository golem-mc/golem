package rules

import (
	_ "github.com/golem-mc/golem/internal/server/datapack"
	"math/rand"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/registry"
)

func TestCakeBite(t *testing.T) {
	cake := func(b string) uint16 { return registry.MustState("minecraft:cake", map[string]string{"bites": b}) }
	if got, ok := CakeBite(cake("0")); !ok || got != cake("1") {
		t.Errorf("a fresh cake bitten = %d, %v", got, ok)
	}
	if got, ok := CakeBite(cake("6")); !ok || got != airState {
		t.Errorf("the last slice bitten = %d, %v; want air", got, ok)
	}
	if got, ok := CakeBite(registry.MustState("minecraft:blue_candle_cake", map[string]string{"lit": "true"})); !ok || got != cake("1") {
		t.Errorf("a candle cake bitten = %d, %v; want a cake with one bite", got, ok)
	}
	if _, ok := CandleOnCake(cake("1"), registry.ItemCandle); ok {
		t.Error("a candle went on a cut cake")
	}
	if got, ok := CandleOnCake(cake("0"), registry.ItemCandle); !ok || got != registry.MustState("minecraft:candle_cake", map[string]string{"lit": "false"}) {
		t.Errorf("a candle on a cake = %d, %v", got, ok)
	}
}

func TestCandleOut(t *testing.T) {
	cluster := registry.MustState("minecraft:red_candle", map[string]string{"candles": "4", "lit": "true", "waterlogged": "true"})
	want := registry.MustState("minecraft:red_candle", map[string]string{"candles": "4", "lit": "false", "waterlogged": "true"})
	if got, ok := CandleOut(cluster, 0.3); !ok || got != want {
		t.Errorf("dousing changed the candle count or waterlogging: %d, %v", got, ok)
	}
	lit := registry.MustState("minecraft:candle_cake", map[string]string{"lit": "true"})
	if _, ok := CandleOut(lit, 0.3); ok {
		t.Error("a candle cake put out by a click on the cake")
	}
	if got, ok := CandleOut(lit, 0.8); !ok || got != registry.MustState("minecraft:candle_cake", map[string]string{"lit": "false"}) {
		t.Errorf("a candle cake put out by a click on the candle = %d, %v", got, ok)
	}
}

func TestFlowerPotUse(t *testing.T) {
	empty := registry.BlockStates["minecraft:flower_pot"]
	fern := registry.MustState("minecraft:potted_fern", nil)
	if after, taken, _, used := FlowerPotUse(empty, registry.ItemFern, false); !used || !taken || after != fern {
		t.Errorf("a fern into an empty pot = %d, %v, %v", after, taken, used)
	}
	if after, taken, _, used := FlowerPotUse(fern, registry.ItemPoppy, true); !used || taken || after != fern {
		t.Errorf("a poppy against a full pot = %d, %v, %v; want the click spent", after, taken, used)
	}
	if after, _, back, used := FlowerPotUse(fern, registry.ItemStick, true); !used || after != empty || back != registry.ItemFern {
		t.Errorf("a full pot emptied = %d, %v, %v", after, back, used)
	}
	if _, _, _, used := FlowerPotUse(fern, registry.ItemStick, false); used {
		t.Error("an off-hand stick emptied a pot")
	}
}

func TestComposterInsert(t *testing.T) {
	level := func(n int) uint16 {
		s, _ := registry.WithInt(registry.BlockComposter.DefaultState(), registry.PropLevel, n)
		return s
	}
	for _, tc := range []struct {
		start  int
		layers int32
		want   int
	}{
		{0, 0, 0}, {3, -1, 3}, {3, 2, 5}, {3, 100, 7}, {3, 2147483647, 0},
	} {
		stack := protocol.ItemStack{ItemID: int32(registry.ItemStick), Count: 1}
		stack.SetCompostable(protocol.CompostableComponent{Layers: tc.layers})
		after, ins, used := ComposterInsert(level(tc.start), stack, &LootContext{}, rand.New(rand.NewSource(1)))
		if !ins || !used || after != level(tc.want) {
			t.Errorf("level %d + %d = %d, inserted=%v used=%v; want level %d", tc.start, tc.layers, after, ins, used, tc.want)
		}
	}
	stack := protocol.ItemStack{ItemID: int32(registry.ItemCake), Count: 1}
	if _, ins, used := ComposterInsert(level(7), stack, &LootContext{}, nil); !used || ins {
		t.Error("level 7 did not spend only the click")
	}
	if _, _, used := ComposterInsert(level(8), stack, &LootContext{}, nil); used {
		t.Error("level 8 answered an item")
	}
	seeds := protocol.ItemStack{ItemID: int32(registry.ItemWheatSeeds), Count: 1}
	if after, ins, used := ComposterInsert(level(0), seeds, &LootContext{}, rand.New(rand.NewSource(1))); !used || !ins || after != level(1) {
		t.Error("default seed provider did not guarantee the first layer")
	}
}

func TestCauldronBucketUse(t *testing.T) {
	empty := registry.BlockStates["minecraft:cauldron"]
	if use, _, ok := CauldronBucketUse(empty, registry.ItemLavaBucket, false); !ok || use.State != registry.MustState("minecraft:lava_cauldron", nil) || use.Bucket != registry.ItemBucket {
		t.Errorf("lava poured = %+v, %v", use, ok)
	}
	if _, refused, ok := CauldronBucketUse(empty, registry.ItemPowderSnowBucket, true); !ok || !refused {
		t.Error("powder snow poured under water")
	}
	two := registry.MustState("minecraft:water_cauldron", map[string]string{"level": "2"})
	if _, _, ok := CauldronBucketUse(two, registry.ItemBucket, false); ok {
		t.Error("a bucket filled from a cauldron two-thirds full")
	}
	if use, _, ok := CauldronBucketUse(two, registry.ItemWaterBucket, false); !ok || use.State != registry.MustState("minecraft:water_cauldron", map[string]string{"level": "3"}) {
		t.Errorf("water poured into a part-full cauldron = %+v, %v", use, ok)
	}
}

func TestShearsTrim(t *testing.T) {
	use, ok := ShearsTrim(registry.MustState("minecraft:weeping_vines", map[string]string{"age": "3"}))
	if !ok || use.State != registry.MustState("minecraft:weeping_vines", map[string]string{"age": "25"}) || use.Sound.Name() != "minecraft:block.growing_plant.crop" {
		t.Errorf("trimmed vine = %+v, %v", use, ok)
	}
	if _, ok := ShearsTrim(registry.MustState("minecraft:kelp", map[string]string{"age": "25"})); ok {
		t.Error("a stopped kelp trimmed again")
	}
	if _, ok := ShearsTrim(registry.MustState("minecraft:weeping_vines_plant", nil)); ok {
		t.Error("a vine's body trimmed")
	}
}

// TestBookshelfHitSlot checks the chiseled bookshelf's hit grid: the front
// face alone answers, and the cell the cursor names follows vanilla's
// section arithmetic on the front's cross-axis and the height.
// vanilla: SelectableSlotContainer.getHitSlot
func TestBookshelfHitSlot(t *testing.T) {
	for _, tc := range []struct {
		facing           string
		face             int
		cursorX, cursorY float32
		cursorZ          float32
		want             int
	}{
		// The north-facing front mirrors the west-to-east cursor.
		{"north", dirNorth, 0.1, 0.75, 0, 2},
		{"north", dirNorth, 0.75, 0.25, 0, 3},
		{"north", dirNorth, 0.5, 0.5, 0, 4},
		// Block mid-height already names the lower row.
		{"south", dirSouth, 0.25, 0.75, 0, 0},
		{"south", dirSouth, 0.25, 0.25, 0, 3},
		// The east-west facings read the north-to-south cursor, the east
		// front mirroring it.
		{"west", dirWest, 0, 0.75, 0.2, 0},
		{"west", dirWest, 0, 0.25, 0.6, 4},
		{"east", dirEast, 0, 0.75, 0.75, 0},
		{"east", dirEast, 0, 0.25, 0.25, 5},
	} {
		ctx := PlacementContext{Face: int32(tc.face), CursorX: tc.cursorX, CursorY: tc.cursorY, CursorZ: tc.cursorZ}
		if got, ok := ContainerHitSlot(tc.facing, 2, 3, ctx); !ok || got != tc.want {
			t.Errorf("the %s front at (x %.2f, y %.2f, z %.2f) = slot %d, %v, want %d",
				tc.facing, tc.cursorX, tc.cursorY, tc.cursorZ, got, ok, tc.want)
		}
	}
	// A cursor outside the block stays on the front: the grid clamps.
	ctx := PlacementContext{Face: int32(dirNorth), CursorX: -0.5, CursorY: 0.75}
	if got, ok := ContainerHitSlot("north", 2, 3, ctx); !ok || got != 2 {
		t.Errorf("the north front past its edge = slot %d, %v, want the clamp on cell 2", got, ok)
	}
	// Every face but the front passes, whatever the cursor names.
	for _, face := range []int{dirDown, dirUp, dirSouth, dirWest, dirEast} {
		if _, ok := ContainerHitSlot("north", 2, 3, PlacementContext{Face: int32(face), CursorX: 0.1, CursorY: 0.75}); ok {
			t.Errorf("a north-facing shelf answered face %d", face)
		}
	}
}

func TestFlowerPotContents(t *testing.T) {
	empty := registry.BlockFlowerPot.DefaultState()
	for name, potted := range registry.FlowerPotted {
		item, _ := registry.ItemByName(name)
		state := registry.MustState(potted, nil)
		if after, taken, back, used := FlowerPotUse(empty, item, false); after != state || !taken || back != registry.ItemAir || !used {
			t.Errorf("potting %s = %d, %v, %v, %v", name, after, taken, back, used)
		}
		if after, taken, back, used := FlowerPotUse(state, registry.ItemAir, true); after != empty || taken || back != item || !used {
			t.Errorf("unpotting %s = %d, %v, %v, %v", name, after, taken, back, used)
		}
	}
	for _, item := range []registry.Item{registry.ItemAir, -1, registry.ItemCount} {
		if after, taken, back, used := FlowerPotUse(empty, item, true); after != empty || taken || back != registry.ItemAir || !used {
			t.Errorf("empty pot with item %d = %d, %v, %v, %v", item, after, taken, back, used)
		}
	}
	if _, _, _, used := FlowerPotUse(registry.BlockStone.DefaultState(), registry.ItemFern, true); used {
		t.Error("stone answered potting")
	}
}

func TestCauldronBottleLevels(t *testing.T) {
	water := protocol.ItemStack{ItemID: int32(registry.ItemPotion), Count: 1}
	water.SetPotion(waterPotion)
	glass := protocol.ItemStack{ItemID: int32(registry.ItemGlassBottle), Count: 1}
	for level := 0; level <= 3; level++ {
		state := registry.BlockCauldron.DefaultState()
		if level > 0 {
			state, _ = registry.WithInt(registry.BlockWaterCauldron.DefaultState(), registry.PropLevel, level)
		}
		use, ok := CauldronItemUse(state, water)
		if ok != (level < 3) {
			t.Fatalf("fill level %d: ok = %v", level, ok)
		}
		if ok {
			want, _ := registry.WithInt(registry.BlockWaterCauldron.DefaultState(), registry.PropLevel, level+1)
			if use.State != want || !use.Result.Equal(glass) {
				t.Errorf("fill level %d = %+v", level, use)
			}
		}
		use, ok = CauldronItemUse(state, glass)
		if ok != (level > 0) {
			t.Fatalf("drain level %d: ok = %v", level, ok)
		}
		if ok {
			want := registry.BlockCauldron.DefaultState()
			if level > 1 {
				want, _ = registry.WithInt(registry.BlockWaterCauldron.DefaultState(), registry.PropLevel, level-1)
			}
			if use.State != want || !use.Result.Equal(water) {
				t.Errorf("drain level %d = %+v", level, use)
			}
		}
	}
}
