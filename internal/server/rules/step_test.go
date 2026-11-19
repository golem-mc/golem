package rules

import (
	"github.com/golem-mc/golem/registry"
	"testing"
)

func TestStepTrackerInWaterMovesTheNextStepOn(t *testing.T) {
	t.Parallel()
	var tr StepTracker
	// A long swim, then the same stride on land: the swim spent its distance.
	if stepped, swam := tr.Advance(Stride{DX: 5, InWater: true}); stepped || !swam {
		t.Fatalf("a swimmer: stepped %v, swam %v", stepped, swam)
	}
	if stepped, _ := tr.Advance(Stride{DX: 0.1, OnGround: true}); stepped {
		t.Error("the first move on land after a swim steps at once")
	}
}

func TestStepTrackerClimbingCountsVerticalMovement(t *testing.T) {
	t.Parallel()
	var tr StepTracker
	if stepped, _ := tr.Advance(Stride{DY: 2, Climbing: true}); !stepped {
		t.Error("a two-block climb makes no step")
	}
}

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

func TestCrystalStepChimesAtMostEveryTwentyTicks(t *testing.T) {
	t.Parallel()
	amethyst := stateOf(t, "minecraft:amethyst_block")
	var c CrystalStep
	rolls := 0
	roll := func() float32 {
		rolls++
		return 0.5
	}
	if _, ok := c.Chime(stateOf(t, "minecraft:stone"), 100, roll); ok {
		t.Error("stone chimes")
	}
	first, ok := c.Chime(amethyst, 100, roll)
	if !ok || first.Event != registry.SoundBlockAmethystBlockChime {
		t.Fatalf("the first step on amethyst: %+v, %v", first, ok)
	}
	if _, ok := c.Chime(amethyst, 119, roll); ok {
		t.Error("a chime 19 ticks after the last")
	}
	if rolls != 1 {
		t.Fatalf("%d random draws before the second chime, want 1", rolls)
	}
	second, ok := c.Chime(amethyst, 120, roll)
	if !ok || second.Volume <= first.Volume {
		t.Errorf("a chime 20 ticks on: %+v, %v; want one louder than %v", second, ok, first.Volume)
	}
	if rolls != 2 {
		t.Errorf("%d random draws after the second chime, want 2", rolls)
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
