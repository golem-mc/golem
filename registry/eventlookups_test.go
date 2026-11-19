package registry

import (
	"slices"
	"testing"
)

func TestIndexedFuelAndParticles(t *testing.T) {
	for _, tc := range []struct {
		item        int32
		burn, brews int32
	}{
		{int32(ItemOakPlanks), 300, 0}, {int32(ItemWoodenPickaxe), 200, 0},
		{int32(ItemLavaBucket), 20000, 0}, {int32(ItemBlazePowder), 0, 20},
		{int32(ItemGlassBottle), 0, 0}, {-1, 0, 0}, {ItemCount, 0, 0}, {1<<31 - 1, 0, 0},
	} {
		if burn, brews := FuelBurnTicks(tc.item), BrewingFuelUses(tc.item); burn != tc.burn || brews != tc.brews {
			t.Errorf("fuel %d = (%d, %d), want (%d, %d)", tc.item, burn, brews, tc.burn, tc.brews)
		}
	}
	for want, name := range ParticleTypes {
		id, data, ok := ParticleID(name)
		if !ok || id != int32(want) || data != particlesWithData[name] {
			t.Errorf("particle %s = %d, %v, %v", name, id, data, ok)
		}
	}
	if id, data, ok := ParticleID("test:missing"); id != 0 || data || ok {
		t.Fatalf("unknown particle = %d, %v, %v", id, data, ok)
	}
}

func TestOrderedBlockTagMembersFollowReload(t *testing.T) {
	for tag, names := range BlockTags {
		got := BlockTagOf(tag).Blocks()
		if len(got) != len(names) {
			t.Fatalf("%s member count = %d, want %d", tag, len(got), len(names))
		}
		for i, name := range names {
			if got[i].Name() != name {
				t.Fatalf("%s member %d = %s, want %s", tag, i, got[i].Name(), name)
			}
		}
	}
	tag := TagIDWallCorals
	old := tag.Blocks()
	wantOld := slices.Clone(old)
	applyForTest(t, PackTags{"minecraft:block": {"minecraft:wall_corals": {Replace: true, Entries: []PackTagEntry{req("minecraft:brain_coral_wall_fan"), req("minecraft:tube_coral_wall_fan")}}}})
	if got := tag.Blocks(); !slices.Equal(got, []Block{BlockBrainCoralWallFan, BlockTubeCoralWallFan}) {
		t.Fatalf("reloaded members = %v", got)
	}
	if !slices.Equal(old, wantOld) {
		t.Fatal("reload mutated the old member slice")
	}
	if got := noBlockTag.Blocks(); len(got) != 0 {
		t.Fatalf("unknown tag = %v", got)
	}
}
