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

// TestAStepSoundIsTheBlocksSoundTypeScaledByItsVolume walks a creeper on
// blocks of different sound types: the step event of the type, a sixth of
// the volume it is placed at, at the type's pitch.
// vanilla: Entity.playStepSound
func TestAStepSoundIsTheBlocksSoundTypeScaledByItsVolume(t *testing.T) {
	for _, tc := range []struct {
		block         string
		event         string
		volume, pitch float32
	}{
		{"minecraft:stone", "minecraft:block.stone.step", 0.15, 1},
		{"minecraft:grass_block", "minecraft:block.grass.step", 0.15, 1},
		{"minecraft:sand", "minecraft:block.sand.step", 0.15, 1},
		{"minecraft:gold_block", "minecraft:block.metal.step", 0.15, 1.5},
		{"minecraft:anvil", "minecraft:block.anvil.step", float32(0.3) * 0.15, 1},
	} {
		m, w := stepper(t, TypeCreeper, tc.block)
		events := walk(m, w, 4, 0.5, 0, 0)
		if len(events) != 1 {
			t.Fatalf("%s: %d step events, want 1", tc.block, len(events))
		}
		ev := events[0]
		if ev.Kind != MobSounded || ev.SoundID.Name() != tc.event || ev.Volume != tc.volume || ev.Pitch != tc.pitch {
			t.Errorf("%s: step %s at %v, %v, want %s at %v, %v", tc.block, ev.SoundID.Name(), ev.Volume, ev.Pitch, tc.event, tc.volume, tc.pitch)
		}
	}
}

// TestAKindWithItsOwnStepSoundIgnoresTheBlock steps kinds that have one:
// the zombie's and a grown pig's whatever the floor, the baby's own, the
// moody cow's, an iron golem's at full volume.
// vanilla: Zombie.playStepSound, Pig.playStepSound, IronGolem.playStepSound
func TestAKindWithItsOwnStepSoundIgnoresTheBlock(t *testing.T) {
	for _, tc := range []struct {
		name   string
		kind   MobType
		baby   bool
		sound  string
		event  string
		volume float32
	}{
		{"zombie", TypeZombie, false, "", "minecraft:entity.zombie.step", 0.15},
		{"pig", TypePig, false, "", "minecraft:entity.pig.step", 0.15},
		{"baby pig", TypePig, true, "", "minecraft:entity.baby_pig.step", 0.15},
		{"moody cow", TypeCow, false, "minecraft:moody", "minecraft:entity.cow_moody.step", 0.15},
		{"big pig", TypePig, false, "minecraft:big", "minecraft:entity.pig.step", 0.15},
		{"iron golem", TypeIronGolem, false, "", "minecraft:entity.iron_golem.step", 1},
	} {
		m, w := stepper(t, tc.kind, "minecraft:sand")
		m.SoundVariant = tc.sound
		m.SetBaby(tc.baby)
		events := walk(m, w, 4, 0.5, 0, 0)
		if len(events) != 1 || events[0].SoundID.Name() != tc.event || events[0].Volume != tc.volume {
			t.Errorf("%s: step events %+v, want %s at %v", tc.name, events, tc.event, tc.volume)
		}
	}
}

// TestAStridersNextStepIsSixTenthsPastTheLast walks a strider and a creeper
// to their first step: the creeper's next step is the next whole number, the
// strider's 0.6 past where it stood.
// vanilla: Entity.nextStep, Strider.nextStep
func TestAStridersNextStepIsSixTenthsPastTheLast(t *testing.T) {
	for _, tc := range []struct {
		kind MobType
		gap  float32
	}{{TypeCreeper, 0.8}, {TypeStrider, 0.6}} {
		m, w := stepper(t, tc.kind, "minecraft:netherrack")
		if len(walk(m, w, 4, 0.5, 0, 0)) != 1 {
			t.Fatalf("%s: no step in 2 blocks", tc.kind)
		}
		gap := m.stride.nextStep - m.stride.moveDist
		if gap < tc.gap-1e-4 || gap > tc.gap+1e-4 {
			t.Errorf("%s: next step %v past the way walked, want %v", tc.kind, gap, tc.gap)
		}
	}
}

// TestAStriderInLavaStepsTheLavaSound steps a strider with its box in lava
// on netherrack: the effect and supporting block are both the netherrack,
// so the one step plays the lava step sound and posts the STEP game event
// with the netherrack.
// vanilla: Strider.playStepSound, Entity.applyMovementEmissionAndPlaySound
func TestAStriderInLavaStepsTheLavaSound(t *testing.T) {
	m, w := stepper(t, TypeStrider, "minecraft:netherrack")
	w.set(0, 64, 0, defaultState(t, "minecraft:lava"))
	m.updateMobFluids(w)
	var events []MobEvent
	for range 4 {
		events = m.stepSoundStep(w, 0.5, 0, 0, events)
	}
	netherrack := defaultState(t, "minecraft:netherrack")
	if len(events) != 2 ||
		events[0].Kind != MobSounded || events[0].SoundID.Name() != "minecraft:entity.strider.step_lava" || events[0].Volume != 1 ||
		events[1].Kind != MobStepped || events[1].BlockState != netherrack {
		t.Errorf("strider in lava stepped %+v, want strider.step_lava at 1, then a step on netherrack", events)
	}
}

// TestAMobThatCannotStepMakesNoStepSound walks a body that is swimming,
// airborne and crouching on the floor's edge of nothing: only the walker on
// the ground, or the crouching one that neither rises nor falls, steps.
// vanilla: Entity.vibrationAndSoundEffectsFromBlock
func TestAMobThatCannotStepMakesNoStepSound(t *testing.T) {
	m, w := stepper(t, TypeCreeper, "minecraft:stone")
	m.Swimming = true
	if got := walk(m, w, 8, 0.5, 0, 0); len(got) != 0 {
		t.Errorf("a swimming body stepped: %+v", got)
	}

	m, w = stepper(t, TypeCreeper, "minecraft:stone")
	m.OnGround = false
	if got := walk(m, w, 8, 0.5, 0, 0); len(got) != 0 {
		t.Errorf("an airborne body stepped: %+v", got)
	}

	// A body on a climbable block steps with no ground under it.
	m, w = stepper(t, TypeCreeper, "minecraft:scaffolding")
	m.OnGround = false
	if got := walk(m, w, 4, 0.5, 0, 0); len(got) != 1 {
		t.Errorf("a body on scaffolding made %d steps, want 1", len(got))
	}

	m, w = stepper(t, TypeCat, "minecraft:stone")
	m.OnGround, m.Crouching = false, true
	if got := walk(m, w, 4, 0.5, 0, 0); len(got) != 1 {
		t.Errorf("a crouching body on a level made %d steps, want 1", len(got))
	}
	m, w = stepper(t, TypeCat, "minecraft:stone")
	m.OnGround, m.Crouching = false, true
	if got := walk(m, w, 4, 0.5, -0.1, 0); len(got) != 0 {
		t.Errorf("a crouching body that was falling stepped: %+v", got)
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

// TestAClimbingBodyCountsItsClimb moves a body straight up a ladder: the way
// climbed counts, where a body in the air moving up counts nothing.
// vanilla: Entity.applyMovementEmissionAndPlaySound
func TestAClimbingBodyCountsItsClimb(t *testing.T) {
	m, w := stepper(t, TypeCreeper, "minecraft:stone")
	ladder := defaultState(t, "minecraft:ladder")
	for y := int32(64); y < 70; y++ {
		w.set(0, y, 0, ladder)
	}
	m.Y = 66.5
	m.OnGround = false
	walk(m, w, 2, 0, 0.5, 0)
	if m.stride.moveDist < 0.59 {
		t.Errorf("a body climbing 1 block counted %v of way, want 0.6", m.stride.moveDist)
	}
	m, w = stepper(t, TypeCreeper, "minecraft:stone")
	m.Y = 66.5
	m.OnGround = false
	walk(m, w, 2, 0, 0.5, 0)
	if m.stride.moveDist != 0 {
		t.Errorf("a body rising through air counted %v of way, want none", m.stride.moveDist)
	}
}

// TestAHorseStepsHoovesNotBlocks walks a horse on wood, stone and stone under
// snow, and in water: the wood's own sound on wood, the plain step else, in
// the snow's volume under snow, none in a liquid.
// vanilla: AbstractHorse.playStepSound
func TestAHorseStepsHoovesNotBlocks(t *testing.T) {
	for _, tc := range []struct {
		kind                MobType
		block, above, event string
		baby                bool
	}{
		{TypeDonkey, "minecraft:stone", "", "minecraft:entity.horse.step", false},
		{TypeMule, "minecraft:oak_planks", "", "minecraft:entity.horse.step_wood", false},
		{TypeHorse, "minecraft:oak_planks", "", "minecraft:entity.horse.step_wood", false},
		{TypeHorse, "minecraft:stone", "", "minecraft:entity.horse.step", false},
		{TypeHorse, "minecraft:stone", "", "minecraft:entity.baby_horse.step", true},
		{TypeHorse, "minecraft:oak_planks", "minecraft:snow", "minecraft:entity.horse.step", false},
		{TypeHorse, "minecraft:water", "", "", false},
	} {
		m, w := stepper(t, tc.kind, tc.block)
		m.SetBaby(tc.baby)
		if tc.above != "" {
			w.set(0, 64, 0, defaultState(t, tc.above))
		}
		events := walk(m, w, 4, 0.5, 0, 0)
		switch {
		case tc.event == "" && len(events) != 0:
			t.Errorf("%s: a horse in a liquid stepped %+v", tc.block, events)
		case tc.event != "" && (len(events) != 1 || events[0].SoundID.Name() != tc.event):
			t.Errorf("%s over %q baby=%v: %+v, want %s", tc.block, tc.above, tc.baby, events, tc.event)
		}
	}
}
