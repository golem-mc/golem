package entity

import (
	"testing"

	"github.com/golem-mc/golem/registry"
)

// The mob voice, as ordinary unit tests: the sound set a farm animal's sound
// variant speaks, the volume getSoundVolume gives, the die the idle sound
// rolls against, and the timer a wound restarts. The black-box tests in
// internal/server/*_test.go are the proof of the sounds on the wire.

// TestAFarmAnimalsSoundVariantSpeaksItsOwnEvents holds every non-classic
// sound variant to the events the vanilla variant class names, the classic
// variant to the kind's own set, and a baby to the baby set its kind keeps —
// or, for a cow, to its variant's set, the cow having no baby one.
// vanilla: CowSoundVariant, PigSoundVariant, ChickenSoundVariant
func TestAFarmAnimalsSoundVariantSpeaksItsOwnEvents(t *testing.T) {
	cases := []struct {
		kind                 MobType
		variant              string
		baby                 bool
		ambient, hurt, death string
	}{
		{TypeCow, "minecraft:moody", false,
			"minecraft:entity.cow_moody.ambient", "minecraft:entity.cow_moody.hurt", "minecraft:entity.cow_moody.death"},
		{TypePig, "minecraft:big", false,
			"minecraft:entity.pig_big.ambient", "minecraft:entity.pig_big.hurt", "minecraft:entity.pig_big.death"},
		{TypePig, "minecraft:mini", false,
			"minecraft:entity.pig_mini.ambient", "minecraft:entity.pig_mini.hurt", "minecraft:entity.pig_mini.death"},
		{TypeChicken, "minecraft:picky", false,
			"minecraft:entity.chicken_picky.ambient", "minecraft:entity.chicken_picky.hurt", "minecraft:entity.chicken_picky.death"},
		// A baby speaks the kind's baby set whatever its variant chose.
		{TypePig, "minecraft:big", true,
			"minecraft:entity.baby_pig.ambient", "minecraft:entity.baby_pig.hurt", "minecraft:entity.baby_pig.death"},
		{TypeChicken, "minecraft:picky", true,
			"minecraft:entity.baby_chicken.ambient", "minecraft:entity.baby_chicken.hurt", "minecraft:entity.baby_chicken.death"},
		// The cow keeps no baby set, so a baby moody cow still speaks the
		// variant's events.
		{TypeCow, "minecraft:moody", true,
			"minecraft:entity.cow_moody.ambient", "minecraft:entity.cow_moody.hurt", "minecraft:entity.cow_moody.death"},
	}
	for _, tc := range cases {
		m := NewMob(tc.kind, 1, 0.5, 64, 0.5, 3)
		m.SoundVariant = tc.variant
		m.SetBaby(tc.baby)
		s := m.Sounds()
		if s.Ambient.Name() != tc.ambient || s.Hurt.Name() != tc.hurt || s.Death.Name() != tc.death {
			t.Errorf("%s %s baby=%v speaks %s/%s/%s, want %s/%s/%s",
				tc.kind, tc.variant, tc.baby, s.Ambient.Name(), s.Hurt.Name(), s.Death.Name(), tc.ambient, tc.hurt, tc.death)
		}
		for _, event := range []string{tc.ambient, tc.hurt, tc.death} {
			if _, ok := registry.SoundEventID(event); !ok {
				t.Errorf("the sound registry carries no %s", event)
			}
		}
	}

	// The classic variant is the kind's own set, the ambient the base one.
	classic := NewMob(TypeCow, 1, 0.5, 64, 0.5, 3)
	classic.SoundVariant = "minecraft:classic"
	if s := classic.Sounds(); s.Ambient.Name() != "minecraft:entity.cow.ambient" {
		t.Errorf("a classic cow moans %s, want the kind's own", s.Ambient.Name())
	}
}
