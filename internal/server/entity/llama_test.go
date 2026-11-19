package entity

import (
	"math/rand"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/nbt"
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
