package entity

import (
	"testing"
)

// The step sound as ordinary unit tests: the way walked that makes a step,
// the sound a kind and a block give it, and the states that keep a body
// from stepping. The black-box test in internal/server/tests/mobs is the proof on
// the wire.

// stepper is a mob of the kind standing at (0.5, 64, 0.5) on a floor of the
// named block, with the whole floor beneath it.
func stepper(t *testing.T, kind MobType, block string) (*Mob, testWorld) {
	t.Helper()
	w := newTestWorld()
	floor(w, -4, -4, 4, 4, 63, defaultState(t, block))
	m := NewMob(kind, 1, 0.5, 64, 0.5, 1)
	m.OnGround = true
	return m, w
}

// walk moves the body n times by (dx, dy, dz) and returns the sounds it
// made; the step and swim game events beside them are left out.
func walk(m *Mob, w World, n int, dx, dy, dz float64) []MobEvent {
	var events []MobEvent
	for range n {
		events = m.stepSoundStep(w, dx, dy, dz, events)
	}
	sounds := events[:0]
	for _, ev := range events {
		if ev.Kind == MobSounded {
			sounds = append(sounds, ev)
		}
	}
	return sounds
}

// TestAStepSoundPlaysOncePerWholeStepWalked walks in half blocks: the way
// counts 0.6 of each, so the first sound is at the fourth move (1.2 passes
// 1) and the next at the seventh (2.1 passes 2).
// vanilla: Entity.applyMovementEmissionAndPlaySound
func TestAStepSoundPlaysOncePerWholeStepWalked(t *testing.T) {
	m, w := stepper(t, TypeCreeper, "minecraft:stone")
	var at []int
	for i := 1; i <= 8; i++ {
		if len(walk(m, w, 1, 0.5, 0, 0)) > 0 {
			at = append(at, i)
		}
	}
	if len(at) != 2 || at[0] != 4 || at[1] != 7 {
		t.Errorf("steps at moves %v, want [4 7]", at)
	}
}

// TestABodyWalkingInWaterSwimsInsteadOfStepping moves a zombie and a
// drowned through water over a floor: at the step the swim sound of the
// kind plays, at the speed's volume, and no block's step.
// vanilla: Entity.waterSwimSound, Monster.getSwimSound, Drowned.getSwimSound,
// Hoglin.getSwimSound
func TestABodyWalkingInWaterSwimsInsteadOfStepping(t *testing.T) {
	for _, tc := range []struct {
		kind  MobType
		event string
	}{
		{TypeZombie, "minecraft:entity.hostile.swim"},
		{TypeDrowned, "minecraft:entity.drowned.swim"},
		{TypeCow, "minecraft:entity.generic.swim"},
		{TypePhantom, "minecraft:entity.generic.swim"},
		{TypeGhast, "minecraft:entity.generic.swim"},
		{TypeShulker, "minecraft:entity.generic.swim"},
		{TypeHoglin, "minecraft:entity.hostile.swim"},
	} {
		m, w := stepper(t, tc.kind, "minecraft:stone")
		w.set(0, 64, 0, defaultState(t, "minecraft:water"))
		m.OnGround = false
		m.updateMobFluids(w)
		m.VX = 0.1
		events := walk(m, w, 4, 0.5, 0, 0)
		if len(events) != 1 || events[0].SoundID.Name() != tc.event {
			t.Errorf("%s: %+v, want one %s", tc.kind, events, tc.event)
			continue
		}
		if v := events[0].Volume; v <= 0 || v >= 0.1 {
			t.Errorf("%s: swim volume %v, want the 0.1 speed's 0.35 share", tc.kind, v)
		}
	}
}
