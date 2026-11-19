package scoreboard

import (
	"testing"
)

func TestLookupStatisticCriteria(t *testing.T) {
	for _, name := range []string{
		"minecraft.mined:minecraft.stone", "minecraft.custom:minecraft.jump",
		"minecraft.crafted:minecraft.stick", "minecraft.killed:minecraft.zombie",
		"minecraft.killed_by:minecraft.creeper", "minecraft.picked_up:minecraft.dirt",
	} {
		c, ok := LookupCriteria(name)
		if !ok || c.Name != name || c.ReadOnly || c.StatType == "" {
			t.Errorf("LookupCriteria(%q) = %+v, %t", name, c, ok)
		}
	}
	c, _ := LookupCriteria("minecraft.mined:minecraft.stone")
	if c.StatType != "minecraft:mined" || c.StatKey != "minecraft:stone" {
		t.Errorf("stone criteria counts %q %q", c.StatType, c.StatKey)
	}
	if c, ok := LookupCriteria("minecraft.mined:.stone"); !ok || c.StatKey != "minecraft:stone" {
		t.Errorf("empty namespace = %+v, %t", c, ok)
	}
	for _, name := range []string{
		"minecraft.mined:minecraft.zombie", "minecraft.killed:minecraft.stone",
		"minecraft.custom:minecraft.stone", "minecraft.bogus:minecraft.stone", "stone", ":",
	} {
		if _, ok := LookupCriteria(name); ok {
			t.Errorf("LookupCriteria(%q) accepted", name)
		}
	}
}

func TestUpdateStatTouchesOnlyMatchingObjectives(t *testing.T) {
	b := New(nil)
	stone, _ := LookupCriteria("minecraft.mined:minecraft.stone")
	dirt, _ := LookupCriteria("minecraft.mined:minecraft.dirt")
	b.AddObjective("s", stone, nil)
	b.AddObjective("d", dirt, nil)
	if !b.CountsStat("minecraft:mined", "minecraft:stone") || b.CountsStat("minecraft:mined", "minecraft:gravel") {
		t.Fatal("CountsStat wrong")
	}
	b.UpdateStat("minecraft:mined", "minecraft:stone", Holder{Name: "A"}, func(v int32) int32 { return v + 2 })
	if s, _ := b.Score("A", "s"); s.Value != 2 {
		t.Errorf("s = %d, want 2", s.Value)
	}
	if _, ok := b.Score("A", "d"); ok {
		t.Error("dirt objective was written")
	}
	b.UpdateStat("minecraft:mined", "minecraft:stone", Holder{Name: "A"}, func(int32) int32 { return 0 })
	if s, ok := b.Score("A", "s"); !ok || s.Value != 0 {
		t.Errorf("score after reset = %d, %v; want 0, true", s.Value, ok)
	}
	b.RemoveObjective("s")
	b.RemoveObjective("d")
	if b.CountsStat("minecraft:mined", "minecraft:dirt") {
		t.Error("removed objective still counted")
	}
}
