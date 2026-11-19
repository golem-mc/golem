package entity

import (
	"math/rand"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// TestLlamaFinalizeRollsStrengthAndColour: strength is 1 to 3 (1 to 5 for one
// llama in 25), the colour one of four, and a pack shares its colour.
// vanilla: Llama.setRandomStrength, Llama.finalizeSpawn
func TestLlamaFinalizeRollsStrengthAndColour(t *testing.T) {
	strong := false
	for seed := int64(1); seed < 400; seed++ {
		m := NewMob(TypeLlama, 1, 0, 64, 0, seed)
		m.FinalizeSpawn(GoDice{m.Rand}, testBiome("minecraft:savanna"), nil, rules.LocalDifficulty{})
		if s := m.Horse.Strength; s < 1 || s > llamaMaxStrength {
			t.Fatalf("strength %d out of range", s)
		} else if s > 3 {
			strong = true
		}
		if v := m.Horse.Variant; v < 0 || v >= llamaVariants {
			t.Fatalf("variant %d out of range", v)
		}
		if m.MaxHealth < 15 || m.MaxHealth > 30 {
			t.Fatalf("health %v, want 15 to 30", m.MaxHealth)
		}
	}
	if !strong {
		t.Error("no llama rolled the strong range in 400")
	}
	group := NewSpawnGroup(GoDice{rand.New(rand.NewSource(1))})
	first := NewMob(TypeLlama, 1, 0, 64, 0, 5)
	first.FinalizeSpawn(GoDice{first.Rand}, testBiome("minecraft:savanna"), group, rules.LocalDifficulty{})
	for seed := int64(6); seed < 20; seed++ {
		m := NewMob(TypeLlama, 2, 0, 64, 0, seed)
		m.FinalizeSpawn(GoDice{m.Rand}, testBiome("minecraft:savanna"), group, rules.LocalDifficulty{})
		if m.Horse.Variant != first.Horse.Variant {
			t.Fatalf("pack variant %d, want %d", m.Horse.Variant, first.Horse.Variant)
		}
	}
}

// TestLlamaChestIsAsWideAsItsStrength: a tame llama takes a chest of its
// strength's columns and no saddle; a carpet goes in its body slot.
// vanilla: Llama.getInventoryColumns, Llama.canUseSlot
func TestLlamaChestIsAsWideAsItsStrength(t *testing.T) {
	m := NewMob(TypeLlama, 1, 0, 64, 0, 1)
	m.Horse.Strength = 4
	m.Horse.Tame = true
	if click, _ := m.UseItem(chestStack(), false); click != HorseClickChest {
		t.Fatalf("chest click %v", click)
	}
	if m.ChestColumns() != 4 || len(m.Horse.Items) != 12 {
		t.Errorf("columns %d slots %d, want 4 and 12", m.ChestColumns(), len(m.Horse.Items))
	}
	if m.CanTakeSaddle() {
		t.Error("a llama can take a saddle")
	}
	carpet := protocol.ItemStack{ItemID: horseItem(t, "minecraft:red_carpet"), Count: 1}
	if click, _ := m.UseItem(carpet, false); click != HorseClickArmor || m.Body.Empty() {
		t.Errorf("carpet click %v body %v", click, m.Body)
	}
	trader := NewMob(TypeTraderLlama, 2, 0, 64, 0, 1)
	trader.Horse.Tame = true
	if click, _ := trader.UseItem(carpet, false); click != HorseClickArmor {
		t.Errorf("trader llama carpet click %v", click)
	}
}

// TestLlamaIsTamedByFoodAndNeverRears: hay adds six temper, wheat three, up
// to 30, and a wild llama only sounds off at an item.
// vanilla: Llama.handleEating, Llama.canPerformRearing, Llama.getMaxTemper
func TestLlamaIsTamedByFoodAndNeverRears(t *testing.T) {
	w := newTestWorld()
	m := NewMob(TypeLlama, 1, 0, 64, 0, 1)
	hay, wheat := horseItem(t, "minecraft:hay_block"), horseItem(t, "minecraft:wheat")
	if ate, _ := m.FeedHorse(w, hay); !ate || m.Horse.Temper != 6 {
		t.Fatalf("hay: ate %v temper %d, want 6", ate, m.Horse.Temper)
	}
	if ate, _ := m.FeedHorse(w, wheat); !ate || m.Horse.Temper != 9 {
		t.Fatalf("wheat: ate %v temper %d, want 9", ate, m.Horse.Temper)
	}
	for range 20 {
		m.FeedHorse(w, hay)
	}
	if m.Horse.Temper != llamaMaxTemper {
		t.Errorf("temper %d, want the cap %d", m.Horse.Temper, llamaMaxTemper)
	}
	if click, mad := m.UseItem(protocol.ItemStack{ItemID: horseItem(t, "minecraft:stick"), Count: 1}, false); click != HorseClickRear || !mad || m.Horse.Standing != 0 {
		t.Errorf("click %v mad %v standing %d, want a sound and no rearing", click, mad, m.Horse.Standing)
	}
	if click, _ := m.UseItem(protocol.ItemStack{ItemID: horseItem(t, "minecraft:golden_carrot"), Count: 1}, false); click != HorseClickRear {
		t.Errorf("golden carrot click %v, want it not to be food", click)
	}
}

// TestLlamasBreedWithEitherKind: a llama and a trader llama mate, the foal
// is the mother's kind with a strength below the parents' best plus one.
// vanilla: Llama.canMate, Llama.getBreedOffspring
func TestLlamasBreedWithEitherKind(t *testing.T) {
	tame := func(id int32, x float64, kind MobType, strength int32) *Mob {
		m := lover(id, x, kind)
		m.Horse.Tame, m.Horse.Strength = true, strength
		return m
	}
	llama, trader := tame(1, 0.5, TypeLlama, 2), tame(2, 2.5, TypeTraderLlama, 3)
	if got := (&breedGoal{m: llama}).freePartner(breedTickView(llama, trader)); got != trader {
		t.Fatal("a llama found no partner in a trader llama")
	}
	if trader.OffspringType(llama) != TypeTraderLlama || llama.OffspringType(trader) != TypeLlama {
		t.Error("a foal is not its mother's kind")
	}
	if got := (&breedGoal{m: llama}).freePartner(breedTickView(llama, tame(3, 1.5, TypeMule, 0))); got != nil {
		t.Error("a llama courted a mule")
	}
	for seed := int64(1); seed < 60; seed++ {
		baby := NewMob(TypeLlama, 9, 0, 64, 0, seed)
		llama.Rand.Seed(seed)
		llama.breedHorse(baby, trader)
		if s := baby.Horse.Strength; s < 1 || s > 4 {
			t.Fatalf("foal strength %d, want 1 to 4", s)
		}
	}
}

// TestALlamaKeepsItsFieldsThroughASave: strength, colour and chest are
// saved, and a bad strength or colour is clamped on load.
// vanilla: Llama.addAdditionalSaveData, Llama.readAdditionalSaveData
func TestALlamaKeepsItsFieldsThroughASave(t *testing.T) {
	now := func() int64 { return 500 }
	m := NewMob(TypeTraderLlama, 1, 0.5, 64, 0.5, 1)
	m.Horse.Tame, m.Horse.Strength, m.Horse.Variant = true, 3, 2
	m.fitChest()
	m.Horse.Items[4] = protocol.ItemStack{ItemID: horseItem(t, "minecraft:stone"), Count: 5}
	tag := nbt.Compound{}
	m.SaveTo(tag, now)
	back := NewMob(TypeTraderLlama, 2, 0.5, 64, 0.5, 2)
	back.LoadFrom(tag, nil, now)
	if back.Horse.Strength != 3 || back.Horse.Variant != 2 || !back.Horse.Chest || back.Horse.Items[4].Count != 5 || len(back.Horse.Items) != 9 {
		t.Errorf("came back strength %d variant %d chest %v items %v", back.Horse.Strength, back.Horse.Variant, back.Horse.Chest, back.Horse.Items)
	}
	tag["Strength"], tag["Variant"] = nbt.Int(9), nbt.Int(-4)
	back.LoadFrom(tag, nil, now)
	if back.Horse.Strength != 5 || back.Horse.Variant != 0 {
		t.Errorf("clamped to strength %d variant %d, want 5 and 0", back.Horse.Strength, back.Horse.Variant)
	}
	tag["Strength"] = nbt.Int(0)
	back.LoadFrom(tag, nil, now)
	if back.Horse.Strength != 1 {
		t.Errorf("zero strength loaded as %d, want 1", back.Horse.Strength)
	}
}

// TestLlamasDropLeather: the generated loot tables give leather.
func TestLlamasDropLeather(t *testing.T) {
	leather, _ := registry.ItemID("minecraft:leather")
	for _, kind := range []MobType{TypeLlama, TypeTraderLlama} {
		got := false
		for seed := int64(1); seed < 40 && !got; seed++ {
			m := NewMob(kind, 1, 0, 64, 0, seed)
			for _, d := range m.RollDrops(rules.LootKiller{}, nil) {
				got = got || d.ItemID == leather
			}
		}
		if !got {
			t.Errorf("%s never dropped leather", kind)
		}
	}
}
