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
