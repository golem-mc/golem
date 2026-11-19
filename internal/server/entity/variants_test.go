package entity

import (
	"math/rand"
	"testing"

	"github.com/golem-mc/golem/internal/server/rules"
)

// diceFor scans the seeds in order for one under which a zombie's spawn
// check at the position passes, so a variant's own rule is what decides.
func diceFor(t *testing.T, w SpawnWorld, x, y, z int32) int64 {
	t.Helper()
	for seed := int64(1); seed < 10000; seed++ {
		if validSpawnPositionForType(w, rand.New(rand.NewSource(seed)), TypeZombie, x, y, z, 100, CategoryMonster) {
			return seed
		}
	}
	t.Fatal("no dice stand a zombie up here")
	return 0
}

// TestAZombieVillagersDataComesFromItsBiome checks ZombieVillager's
// VillagerData: a profession drawn from the whole registry at level one,
// the type left at plains until finalizeSpawn reads it off the biome
// (VillagerType.byBiome), and a type read from a tag never re-read.
func TestAZombieVillagersDataComesFromItsBiome(t *testing.T) {
	professions := map[string]bool{}
	for seed := int64(1); seed <= 2000; seed++ {
		m := NewMob(TypeZombieVillager, 1, 0, 64, 0, seed)
		if m.Villager.Type != "minecraft:plains" || m.Villager.Level != 1 || m.Villager.Finalized {
			t.Fatalf("a fresh zombie villager's data is %+v, want plains, level 1, not finalized", m.Villager)
		}
		professions[m.Villager.Profession] = true
	}
	if len(professions) != 15 {
		t.Errorf("%d professions drawn, want all 15 of the registry", len(professions))
	}
	for biome, want := range map[string]string{
		"minecraft:desert": "minecraft:desert", "minecraft:swamp": "minecraft:swamp",
		"minecraft:snowy_plains": "minecraft:snow", "minecraft:forest": "minecraft:plains",
	} {
		m := NewMob(TypeZombieVillager, 1, 0, 64, 0, 1)
		m.FinalizeSpawn(GoDice{m.Rand}, testBiome(biome), nil, rules.LocalDifficulty{})
		if m.Villager.Type != want || !m.Villager.Finalized {
			t.Errorf("finalized in %s: %+v, want type %s, finalized", biome, m.Villager, want)
		}
	}
	m := NewMob(TypeZombieVillager, 1, 0, 64, 0, 1)
	m.Villager = VillagerData{Type: "minecraft:taiga", Profession: "minecraft:mason", Level: 3, Finalized: true}
	m.FinalizeSpawn(GoDice{m.Rand}, testBiome("minecraft:desert"), nil, rules.LocalDifficulty{})
	if m.Villager.Type != "minecraft:taiga" {
		t.Errorf("a finalized type became %s, want taiga kept", m.Villager.Type)
	}
}

// TestAZombieUnderWaterConverts checks Zombie.tick's timing: the
// conversion starts on the 600th tick with the eyes in water and ends 300
// ticks later; eyes out of the water reset the count, and a drowned or a
// zombie villager never converts.
func TestAZombieUnderWaterConverts(t *testing.T) {
	m := NewMob(TypeZombie, 1, 0, 64, 0, 1)
	m.waterFluid.eyesIn = true
	for i := 1; i < 600; i++ {
		if into := m.waterConversionStep(); into != NoMobType || m.DrownedConverting {
			t.Fatalf("tick %d: converting %v into %q", i, m.DrownedConverting, into)
		}
	}
	m.waterConversionStep()
	if !m.DrownedConverting || m.DrownedConversionTime != 300 {
		t.Fatalf("after 600 ticks converting %v with %d left, want 300", m.DrownedConverting, m.DrownedConversionTime)
	}
	for i := range 300 {
		if into := m.waterConversionStep(); into != NoMobType {
			t.Fatalf("converted early, %d ticks in", i)
		}
	}
	if into := m.waterConversionStep(); into != TypeDrowned {
		t.Errorf("converted into %q, want a drowned", into)
	}
	m = NewMob(TypeZombie, 1, 0, 64, 0, 1)
	m.waterFluid.eyesIn = true
	for range 599 {
		m.waterConversionStep()
	}
	m.waterFluid.eyesIn = false
	m.waterConversionStep()
	if m.InWaterTime != -1 {
		t.Errorf("eyes out of the water left the count at %d, want -1", m.InWaterTime)
	}
	for _, kind := range []MobType{TypeDrowned, TypeZombieVillager} {
		m := NewMob(kind, 1, 0, 64, 0, 1)
		m.waterFluid.eyesIn = true
		for range 1000 {
			if m.waterConversionStep() != NoMobType || m.DrownedConverting {
				t.Fatalf("a %s converts under water", kind)
			}
		}
	}
}

// TestMooshroomBreedingFlipsASameVariantPair holds MushroomCow's
// getOffspringVariant: a same-variant pair turns the other variant out of a
// one-in-1024 roll, and a mixed pair takes either parent's by coin.
func TestMooshroomBreedingFlipsASameVariantPair(t *testing.T) {
	brown := func() *Mob {
		m := NewMob(TypeMooshroom, 1, 0, 64, 0, 1)
		m.Mushroom = "brown"
		return m
	}
	// A seed whose first draw lands under one in 1024 turns the brown pair
	// red; the old coin alone never could.
	flip := int64(0)
	for rand.New(rand.NewSource(flip)).Intn(1024) != 0 {
		flip++
	}
	mother, mate := brown(), brown()
	mother.Rand = rand.New(rand.NewSource(flip))
	calf := NewMob(TypeMooshroom, 2, 0, 64, 0, 1)
	mother.BreedInto(calf, mate)
	if calf.Mushroom != "red" {
		t.Errorf("a brown pair's calf at the flip = %q, want the other variant red", calf.Mushroom)
	}

	// A mixed pair gives each variant by coin, never a third.
	mate.Mushroom = "red"
	seen := map[string]bool{}
	for seed := range int64(40) {
		mother.Rand = rand.New(rand.NewSource(seed))
		calf = NewMob(TypeMooshroom, 2, 0, 64, 0, 1)
		mother.BreedInto(calf, mate)
		if calf.Mushroom != "brown" && calf.Mushroom != "red" {
			t.Fatalf("a mixed pair's calf = %q, want one of the parents", calf.Mushroom)
		}
		seen[calf.Mushroom] = true
	}
	if !seen["brown"] || !seen["red"] {
		t.Errorf("a mixed pair gave variants %v, want both parents' by coin", seen)
	}
}
