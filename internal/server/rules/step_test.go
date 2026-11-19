package rules

import (
	"github.com/golem-mc/golem/registry"
	"testing"
)

func TestPlayerStepSoundsTakeTheBlockOnTop(t *testing.T) {
	t.Parallel()
	stone, carpet, petals := stateOf(t, "minecraft:stone"), stateOf(t, "minecraft:white_carpet"), stateOf(t, "minecraft:pink_petals")
	cases := []struct {
		name          string
		on, above     uint16
		inWater       bool
		step, muffled StepSound
	}{
		{"bare stone", stone, 0, false, StepSound{registry.SoundBlockStoneStep, 0.15, 1}, StepSound{}},
		{"carpet over stone", stone, carpet, false,
			StepSound{registry.SoundBlockWoolStep, 0.15, 1}, StepSound{registry.SoundBlockStoneStep, 0.05, 0.8}},
		{"petals over stone", stone, petals, false, StepSound{registry.SoundBlockPinkPetalsStep, 0.15, 1}, StepSound{}},
		{"stone under water", stone, 0, true, StepSound{}, StepSound{registry.SoundBlockStoneStep, 0.05, 0.8}},
	}
	for _, tc := range cases {
		step, muffled := PlayerStepSounds(tc.on, tc.above, tc.inWater)
		if step != tc.step || muffled != tc.muffled {
			t.Errorf("%s: %+v and %+v, want %+v and %+v", tc.name, step, muffled, tc.step, tc.muffled)
		}
	}
}

// vanilla: LivingEntity.getFallDamageSound, LivingEntity/Monster/Player.getFallSounds
func TestFallSoundDamageBoundary(t *testing.T) {
	for _, tc := range []struct {
		kind       FallKind
		small, big registry.Sound
	}{
		{FallGeneric, registry.SoundEntityGenericSmallFall, registry.SoundEntityGenericBigFall},
		{FallHostile, registry.SoundEntityHostileSmallFall, registry.SoundEntityHostileBigFall},
		{FallPlayer, registry.SoundEntityPlayerSmallFall, registry.SoundEntityPlayerBigFall},
		{FallKind(-1), registry.SoundEntityGenericSmallFall, registry.SoundEntityGenericBigFall},
	} {
		if got := FallSound(4, tc.kind); got != tc.small {
			t.Errorf("kind %d at 4: %s", tc.kind, got.Name())
		}
		if got := FallSound(5, tc.kind); got != tc.big {
			t.Errorf("kind %d at 5: %s", tc.kind, got.Name())
		}
	}
}
