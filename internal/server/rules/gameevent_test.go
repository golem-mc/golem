package rules

import (
	"testing"

	"github.com/golem-mc/golem/registry"
)

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
