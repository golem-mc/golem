package rules

import (
	"fmt"
	"math/rand"
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/registry"
)

func TestFillLootLaysTheTableIntoFreeSlots(t *testing.T) {
	heart, _ := registry.ItemID("minecraft:heart_of_the_sea")
	slots := make([]protocol.ItemStack, 27)
	slots[5] = protocol.ItemStack{ItemID: heart + 1, Count: 1}
	if !FillLoot("minecraft:chests/buried_treasure", &LootContext{}, rand.New(rand.NewSource(1)), slots) {
		t.Fatal("buried_treasure is unknown")
	}
	if slots[5].ItemID != heart+1 || slots[5].Count != 1 {
		t.Errorf("the occupied slot was overwritten: %+v", slots[5])
	}
	hearts := 0
	for _, s := range slots {
		if s.ItemID == heart {
			hearts++
		}
	}
	if hearts != 1 {
		t.Errorf("%d hearts of the sea, want 1", hearts)
	}
}

func TestFillLootEnchantsTheStacksItsTablesEnchant(t *testing.T) {
	enchanted, books, damaged := 0, 0, 0
	for seed := range int64(200) {
		slots := make([]protocol.ItemStack, 27)
		FillLoot("minecraft:chests/simple_dungeon", &LootContext{}, rand.New(rand.NewSource(seed)), slots)
		for _, s := range slots {
			if len(s.Enchantments) > 0 {
				enchanted++
			}
			if len(s.StoredEnchantments) > 0 {
				books++
			}
		}
		FillLoot("minecraft:chests/ancient_city", &LootContext{}, rand.New(rand.NewSource(seed)), slots)
		for _, s := range slots {
			if s.Damage > 0 {
				damaged++
			}
		}
	}
	if enchanted == 0 && books == 0 {
		t.Error("simple_dungeon never rolled an enchanted stack")
	}
	if books == 0 {
		t.Error("simple_dungeon never turned a book into an enchanted book")
	}
	if damaged == 0 {
		t.Error("ancient_city never rolled a damaged stack")
	}
}

// Seed 42 of simple_dungeon on an empty 27-slot chest, as the vanilla 26.3
// server generated it (LootTable.fill with a java.util.Random).
func TestFillLootSeededMatchesVanillaSimpleDungeon(t *testing.T) {
	slots := make([]protocol.ItemStack, 27)
	if !FillLoot("minecraft:chests/simple_dungeon", &LootContext{}, NewJavaRandom(42), slots) {
		t.Fatal("simple_dungeon is unknown")
	}
	want := map[int]struct {
		item  string
		count int32
	}{
		2: {"minecraft:leather", 1}, 3: {"minecraft:gunpowder", 1}, 6: {"minecraft:string", 1},
		7: {"minecraft:leather", 1}, 8: {"minecraft:leather", 1}, 9: {"minecraft:gunpowder", 1},
		10: {"minecraft:rotten_flesh", 2}, 11: {"minecraft:coal", 2}, 12: {"minecraft:gunpowder", 1},
		13: {"minecraft:coal", 2}, 14: {"minecraft:leather", 1}, 15: {"minecraft:wheat", 1},
		16: {"minecraft:leather", 2}, 19: {"minecraft:string", 3}, 22: {"minecraft:name_tag", 1},
		24: {"minecraft:wheat", 1}, 25: {"minecraft:wheat", 1}, 26: {"minecraft:rotten_flesh", 1},
	}
	for i, s := range slots {
		w, ok := want[i]
		if !ok {
			if !s.Empty() {
				name, _ := registry.ItemName(s.ItemID)
				t.Errorf("slot %d holds %d %s, want empty", i, s.Count, name)
			}
			continue
		}
		name, _ := registry.ItemName(s.ItemID)
		if s.Empty() || name != w.item || s.Count != w.count {
			t.Errorf("slot %d = %d %s, want %d %s", i, s.Count, name, w.count, w.item)
		}
	}
}

// Slot layouts from vanilla 26.3's LootTable.fill on an empty 27-slot
// container (a harness over the server jar's own classes).
func TestFillLootSeededMatchesVanillaOtherTables(t *testing.T) {
	cases := []struct {
		table string
		seed  int64
		want  string
	}{
		{"minecraft:chests/desert_pyramid", 7, "0 enchanted_book 1,1 gunpowder 1,2 string 1,5 gunpowder 1,6 sand 1,7 iron_ingot 1,9 string 1,10 sand 1,11 golden_apple 1,12 gunpowder 1,14 sand 1,15 gunpowder 1,16 gunpowder 1,18 sand 2,19 gunpowder 2,20 gunpowder 1,23 string 1,26 sand 1"},
		{"minecraft:chests/abandoned_mineshaft", 123456789, "0 bread 1,1 torch 1,5 bread 1,6 name_tag 1,9 activator_rail 1,10 torch 1,14 torch 3,18 torch 1,19 iron_ingot 1,20 iron_ingot 4,21 activator_rail 1,22 torch 1,23 torch 2,24 torch 1,25 torch 4,26 torch 1"},
	}
	for _, c := range cases {
		slots := make([]protocol.ItemStack, 27)
		if !FillLoot(c.table, &LootContext{}, NewJavaRandom(c.seed), slots) {
			t.Fatalf("%s is unknown", c.table)
		}
		var got []string
		for i, s := range slots {
			if !s.Empty() {
				name, _ := registry.ItemName(s.ItemID)
				got = append(got, fmt.Sprintf("%d %s %d", i, strings.TrimPrefix(name, "minecraft:"), s.Count))
			}
		}
		if g := strings.Join(got, ","); g != c.want {
			t.Errorf("%s seed %d:\n got %s\nwant %s", c.table, c.seed, g, c.want)
		}
	}
}
