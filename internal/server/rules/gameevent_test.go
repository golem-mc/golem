package rules

import (
	"testing"

	"github.com/golem-mc/golem/registry"
)

// TestVibrationTablesMatchTags pins every sneaking entry to its tag.
func TestVibrationTablesMatchTags(t *testing.T) {
	var want [registry.GameEventCount]bool
	for _, r := range registry.BuiltinTags {
		if r.Registry != "minecraft:game_event" {
			continue
		}
		for _, tag := range r.Tags {
			if tag.Name == "minecraft:ignore_vibrations_sneaking" {
				for _, id := range tag.IDs {
					want[id+1] = true
				}
			}
		}
	}
	if ignoredWhenSneaking != want {
		t.Errorf("sneaking table %v, tag %v", ignoredWhenSneaking, want)
	}
}

func TestVibrationOf(t *testing.T) {
	wool := registry.MustState("minecraft:white_wool", nil)
	stone := registry.MustState("minecraft:stone", nil)
	cases := []struct {
		name     string
		ev       GameEvent
		ctx      GameEventContext
		wantFreq int
	}{
		{"block place", GameEventBlockPlace, GameEventContext{Affected: stone}, 13},
		{"block destroy", GameEventBlockDestroy, GameEventContext{Affected: stone}, 12},
		{"hit ground", GameEventHitGround, GameEventContext{Affected: stone}, 2},
		{"spectator", GameEventBlockPlace, GameEventContext{Spectator: true, Affected: stone}, 0},
		{"sneaking landing", GameEventHitGround, GameEventContext{Sneaking: true, Affected: stone}, 0},
		{"sneaking place", GameEventBlockPlace, GameEventContext{Sneaking: true, Affected: stone}, 13},
		{"wool placed", GameEventBlockPlace, GameEventContext{Affected: wool}, 0},
		{"not a vibration", registry.GameEventShriek, GameEventContext{}, 0},
	}
	for _, c := range cases {
		v, ok := VibrationOf(c.ev, 0, 0, 0, c.ctx)
		if ok != (c.wantFreq > 0) || v.Frequency != c.wantFreq {
			t.Errorf("%s: got %d, %v; want %d", c.name, v.Frequency, ok, c.wantFreq)
		}
		if ok && v.PlaceOrDestroy != (c.ev == GameEventBlockPlace || c.ev == GameEventBlockDestroy) {
			t.Errorf("%s: PlaceOrDestroy %v", c.name, v.PlaceOrDestroy)
		}
	}
}

func TestVibrationOfFollowsPackBlockTags(t *testing.T) {
	t.Cleanup(func() { registry.ApplyPackTags(nil) })
	stone := registry.MustState("minecraft:stone", nil)
	ctx := GameEventContext{Affected: stone}
	if _, ok := VibrationOf(GameEventBlockPlace, 0, 0, 0, ctx); !ok {
		t.Fatal("stone dampens vibrations before any pack")
	}
	registry.ApplyPackTags(registry.PackTags{"minecraft:block": {
		"minecraft:dampens_vibrations": {Entries: []registry.PackTagEntry{{ID: "minecraft:stone", Required: true}}},
	}})
	if _, ok := VibrationOf(GameEventBlockPlace, 0, 0, 0, ctx); ok {
		t.Error("stone still vibrates after a pack added it to dampens_vibrations")
	}
}

func TestShapeAndFallGroupsFollowPackBlockTags(t *testing.T) {
	t.Cleanup(func() { registry.ApplyPackTags(nil) })
	stone := registry.MustState("minecraft:stone", nil)
	sb := registry.BlockOf(stone)
	if IsWallBlock(sb) || IsStairsBlock(sb) || IsFenceBlock(sb) || writesFallOff(stone, false, false) {
		t.Fatal("stone is in a shape or fall group before any pack")
	}
	entry := registry.PackTag{Entries: []registry.PackTagEntry{{ID: "minecraft:stone", Required: true}}}
	registry.ApplyPackTags(registry.PackTags{"minecraft:block": {
		"minecraft:walls": entry, "minecraft:stairs": entry, "minecraft:fences": entry,
		"minecraft:fall_damage_resetting": entry,
	}})
	if !IsWallBlock(sb) || !IsStairsBlock(sb) || !IsFenceBlock(sb) || !writesFallOff(stone, false, false) {
		t.Error("a shape or fall group ignores the pack's tag")
	}
}

func TestPostBlocksFollowPackBlockTags(t *testing.T) {
	t.Cleanup(func() { registry.ApplyPackTags(nil) })
	stone := registry.MustState("minecraft:stone", nil)
	if isPostBlock(stone) {
		t.Fatal("stone is a post before any pack")
	}
	registry.ApplyPackTags(registry.PackTags{"minecraft:block": {
		"minecraft:walls": {Entries: []registry.PackTagEntry{{ID: "minecraft:stone", Required: true}}},
	}})
	if !isPostBlock(stone) {
		t.Error("stone is no post after a pack added it to walls")
	}
}

func TestFenceConnectionExceptionsFollowPackBlockTags(t *testing.T) {
	t.Cleanup(func() { registry.ApplyPackTags(nil) })
	stone := registry.MustState("minecraft:stone", nil)
	if !registry.ConnectsToBars(stone, "north") {
		t.Fatal("bars do not connect to a stone face before any pack")
	}
	registry.ApplyPackTags(registry.PackTags{"minecraft:block": {
		"minecraft:leaves": {Entries: []registry.PackTagEntry{{ID: "minecraft:stone", Required: true}}},
	}})
	if registry.ConnectsToBars(stone, "north") {
		t.Error("bars still connect to stone after a pack added it to leaves")
	}
}

func TestHiddenBySneakingNeedsASneakingNonSpectatorAndAnIgnoredEvent(t *testing.T) {
	sneak := GameEventContext{Sneaking: true}
	if !HiddenBySneaking(GameEventStep, sneak) {
		t.Error("a sneaking step was not hidden")
	}
	if HiddenBySneaking(GameEventStep, GameEventContext{}) {
		t.Error("a standing step was hidden")
	}
	if HiddenBySneaking(GameEventBlockPlace, sneak) {
		t.Error("block_place is not ignored while sneaking")
	}
	if HiddenBySneaking(GameEventStep, GameEventContext{Sneaking: true, Spectator: true}) {
		t.Error("a spectator was counted")
	}
}
