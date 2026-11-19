package entity

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/rules"
)

// TestAVillagerIsAnAgeableWalkerOfTheFinalizedBiome: a villager starts as a
// plains jobless level one, takes its type from the biome at finalize, and
// its baby box is the smaller one.
func TestAVillagerIsAnAgeableWalkerOfTheFinalizedBiome(t *testing.T) {
	m := NewMob(TypeVillager, 1, 0.5, 64, 0.5, 1)
	if m.Villager != (VillagerData{Type: "minecraft:plains", Profession: "minecraft:none", Level: 1}) {
		t.Fatalf("fresh villager data %+v", m.Villager)
	}
	if m.MaxHealth != 20 || m.eyeHeight() != 1.62 {
		t.Errorf("health %v eye %v", m.MaxHealth, m.eyeHeight())
	}
	m.FinalizeSpawn(GoDice{m.Rand}, testBiome("minecraft:desert"), nil, rules.LocalDifficulty{})
	if m.Villager.Type != "minecraft:desert" || !m.Villager.Finalized {
		t.Errorf("finalized villager %+v", m.Villager)
	}
	m.BabyAge = -24000
	if !m.IsBaby() {
		t.Error("a villager with negative age is not a baby")
	}
}

// TestAVillagerWandersOnItsOwn: with open ground and no player about, a
// villager's stroll carries it off its spot. One seed may idle for all 600
// ticks (the stroll is a per-tick chance), so any of several walking suffices.
func TestAVillagerWandersOnItsOwn(t *testing.T) {
	stone := defaultState(t, "minecraft:stone")
	w := newTestWorld()
	groundTo(w, -40, -40, 40, 40, 63, stone)
	walked := 0
	for seed := int64(1); seed <= 12; seed++ {
		m := NewMob(TypeVillager, 1, 0.5, 64, 0.5, seed)
		for range 600 {
			m.Tick(w, nil)
		}
		if dx, dz := m.X-0.5, m.Z-0.5; dx*dx+dz*dz >= 1 {
			walked++
		}
	}
	if walked < 3 {
		t.Errorf("only %d of 12 villagers left their spot in 600 ticks", walked)
	}
}

// TestAVillagerIsNoLeadOrCreatureCapKind: vanilla refuses a lead on a villager
// and files it under MISC, so it fills no creature cap.
func TestAVillagerIsNoLeadOrCreatureCapKind(t *testing.T) {
	if Leashable(TypeVillager) {
		t.Error("a villager can be leashed")
	}
	if CreatureKind(TypeVillager) {
		t.Error("a villager counts as a creature-category kind")
	}
	if !CreatureKind(TypeCow) || !Leashable(TypeCow) {
		t.Error("a cow lost its category or lead")
	}
}

// TestAVillagerRegistersItsStrollAndPanicGoals pins the villager's idle
// walking speed and its panic goal.
func TestAVillagerRegistersItsStrollAndPanicGoals(t *testing.T) {
	m := NewMob(TypeVillager, 1, 0.5, 64, 0.5, 1)
	var stroll *randomStroll
	var panic *villagerPanicGoal
	for _, g := range villagerGoals(m) {
		switch g := g.(type) {
		case *randomStroll:
			stroll = g
		case *villagerPanicGoal:
			panic = g
		}
	}
	if stroll == nil || stroll.pace != 0.5 || panic == nil {
		t.Errorf("stroll %+v panic %+v", stroll, panic)
	}
}

// TestAVillagerIsNoAnimalForTheAnimalOnlyRules: no baby roll in a spawn
// group and no feeding, while the baby age still counts down.
func TestAVillagerIsNoAnimalForTheAnimalOnlyRules(t *testing.T) {
	if ageableAnimal(TypeVillager) {
		t.Error("villager is an ageable animal")
	}
	m := NewMob(TypeVillager, 1, 0.5, 64, 0.5, 1)
	if loved, grew := m.Feed(); loved || grew {
		t.Error("a villager took food")
	}
	stone := defaultState(t, "minecraft:stone")
	w := newTestWorld()
	floor(w, -10, -10, 10, 10, 63, stone)
	m.BabyAge = -2
	m.Tick(w, nil)
	m.Tick(w, nil)
	if m.BabyAge != 0 || m.IsBaby() {
		t.Errorf("baby age %d after two ticks", m.BabyAge)
	}
}
