package rules

import (
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/effect"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// tickOne runs one plain tick of the vitals, the body standing in the open
// air: the habitat most of these tests mean, and no dice for the fire that
// never reaches them. The rules stand where a fresh world's stand, natural
// regeneration among them.
func tickOne(v *player.Vitals, difficulty uint8, belowVoid bool) (VitalsEvent, []Hurt, Douse) {
	event, hurts, douse, _ := TickVitals(v, nil, nil, difficulty, belowVoid, Habitat{}, nil, 0, true)
	return event, hurts, douse
}

// trackFall runs TrackMovement for its damage alone, the way the falls'
// tests read it; the fall a landing itself carried is the trample's
// business, not theirs. The steps these take start out of the climb, the
// climbing step its own helper below, with no effects and the attribute's
// own default safe distance.
func trackFall(v *player.Vitals, was, now player.Position, flying, sprinting bool, water Water) float64 {
	damage, _ := TrackMovement(v, nil, was, now, flying, sprinting, water, false, false, false, 3.0, 1, Landing{})
	return damage
}

// trackClimbingFall runs TrackMovement for a step taken with the feet
// already in what the body climbs, the way the ladder's tests read it.
func trackClimbingFall(v *player.Vitals, was, now player.Position) float64 {
	damage, _ := TrackMovement(v, nil, was, now, false, false, Dry, false, true, false, 3.0, 1, Landing{})
	return damage
}

// trackFallEffects runs TrackMovement for a faller carrying the given
// effects, the attribute's own default safe distance.
func trackFallEffects(v *player.Vitals, effects []effect.Instance, was, now player.Position, flying, sprinting bool, water Water) float64 {
	damage, _ := TrackMovement(v, effects, was, now, flying, sprinting, water, false, false, false, 3.0, 1, Landing{})
	return damage
}

// TestFallDamageByHeight checks what a landing takes out of a fall, by how
// far the fall went: the first three blocks are free, everything past them
// rounds up to the half-heart it costs, and a fall tracked one step at a
// time lands the same as one whole.
func TestFallDamageByHeight(t *testing.T) {
	landFrom := func(v *player.Vitals, steps int, fall float64) float64 {
		// Walk a fall of the given height in `steps` downward steps, all
		// but the last through the air, so the distance is accumulated
		// the way the connection reports it.
		perStep := fall / float64(steps)
		start := 100.0
		for i := 0; i < steps-1; i++ {
			was := player.Position{Y: start}
			start -= perStep
			now := player.Position{Y: start}
			if damage := trackFall(v, was, now, false, false, Dry); damage != 0 {
				t.Fatalf("a step through the air hurt for %v", damage)
			}
		}
		return trackFall(v, player.Position{Y: start},
			player.Position{Y: start - perStep, OnGround: true}, false, false, Dry)
	}

	cases := []struct {
		fall, want float64
	}{
		{1, 0},     // closer than the threshold
		{3, 0},     // the safe fall distance itself
		{3.25, 0},  // a sprint jump off a two-block edge, whose apex adds a quarter past it
		{3.5, 0},   // half past the threshold is still free
		{4, 1},     // one past it
		{4.5, 1},   // a half past one costs the one, not two
		{10, 7},    // a plain tower's worth
		{25.5, 22}, // a fractional fall keeps its fraction rather than rounding up
	}
	for _, c := range cases {
		v := FreshVitals(protocol.GameModeSurvival)
		if got := landFrom(&v, 1, c.fall); got != c.want {
			t.Errorf("a %v-block fall hurt for %v, want %v", c.fall, got, c.want)
		}
		// The same fall told apart as several steps lands identically.
		v = FreshVitals(protocol.GameModeSurvival)
		if got := landFrom(&v, 4, c.fall); got != c.want {
			t.Errorf("a %v-block fall in four steps hurt for %v, want %v", c.fall, got, c.want)
		}
	}

	// Rising ends a fall, and a landing after it starts over from nothing.
	v := FreshVitals(protocol.GameModeSurvival)
	if damage := trackFall(&v, player.Position{Y: 100}, player.Position{Y: 97}, false, false, Dry); damage != 0 {
		t.Fatalf("the fall's step through the air hurt for %v", damage)
	}
	if damage := trackFall(&v, player.Position{Y: 97}, player.Position{Y: 98, OnGround: true}, false, false, Dry); damage != 0 {
		t.Errorf("a rise after three fallen blocks hurt for %v", damage)
	}
	if v.FallDistance != 0 {
		t.Errorf("after rising the fall distance is %v, want it reset", v.FallDistance)
	}
	if damage := trackFall(&v, player.Position{Y: 98, OnGround: true}, player.Position{Y: 98, OnGround: true}, false, false, Dry); damage != 0 {
		t.Errorf("landing after a rise hurt for %v", damage)
	}

	// A flying player never falls, and what a fall had come to before the
	// flight is dropped rather than waited out.
	v = FreshVitals(protocol.GameModeSurvival)
	trackFall(&v, player.Position{Y: 100}, player.Position{Y: 90}, false, false, Dry)
	if damage := trackFall(&v, player.Position{Y: 90}, player.Position{Y: 90, OnGround: true}, true, false, Dry); damage != 0 {
		t.Errorf("a flying landing hurt for %v", damage)
	}
	if v.FallDistance != 0 {
		t.Errorf("a flying landing left %v of fall distance", v.FallDistance)
	}
}

func TestCreativeFallTrackingAndImmunity(t *testing.T) {
	v := FreshVitals(protocol.GameModeCreative)
	damage, landed := TrackMovement(&v, nil, player.Position{Y: 72}, player.Position{Y: 66},
		false, false, Dry, false, false, false, 3, 1, Landing{})
	if damage != 0 || landed != 0 || v.FallDistance != 6 {
		t.Fatalf("airborne creative fall: damage=%v landed=%v distance=%v, want 0, 0, 6", damage, landed, v.FallDistance)
	}
	damage, landed = TrackMovement(&v, nil, player.Position{Y: 66}, player.Position{Y: 64, OnGround: true},
		false, false, Dry, false, false, false, 3, 1, Landing{})
	if damage != 0 || landed != 8 || v.FallDistance != 0 {
		t.Errorf("creative landing: damage=%v landed=%v distance=%v, want 0, 8, 0", damage, landed, v.FallDistance)
	}
	trackFall(&v, player.Position{Y: 64, OnGround: true}, player.Position{Y: 65}, false, true, Dry)
	trackFall(&v, player.Position{X: 0, Y: 64}, player.Position{X: 5, Y: 64, OnGround: true}, false, true, Dry)
	trackFall(&v, player.Position{X: 0, Y: 64}, player.Position{X: 5, Y: 64}, false, false, Submerged)
	if v.Exhaustion != 0 {
		t.Errorf("creative movement exhaustion = %v, want 0", v.Exhaustion)
	}

	for _, mode := range []int32{protocol.GameModeCreative, protocol.GameModeSpectator} {
		v = FreshVitals(mode)
		v.FallDistance = 6
		trackFall(&v, player.Position{Y: 72}, player.Position{Y: 66}, mode == protocol.GameModeCreative, false, Dry)
		if v.FallDistance != 0 {
			t.Errorf("flying/spectator mode %v kept fall distance %v, want 0", mode, v.FallDistance)
		}
	}
}

// TestFallIntoWaterLandsUnhurt checks the water's mercy: the fall's
// distance is written off the moment the body is in the water, so however
// far a body fell to reach it, the landing costs nothing.
func TestFallIntoWaterLandsUnhurt(t *testing.T) {
	v := FreshVitals(protocol.GameModeSurvival)
	if damage := trackFall(&v, player.Position{Y: 100}, player.Position{Y: 96}, false, false, Dry); damage != 0 {
		t.Fatalf("the step through the air hurt for %v", damage)
	}
	if damage := trackFall(&v, player.Position{Y: 96}, player.Position{Y: 94, OnGround: true}, false, false, Wading); damage != 0 {
		t.Errorf("landing in water hurt for %v, want the water to have taken the fall back", damage)
	}
	if v.FallDistance != 0 {
		t.Errorf("the water left %v of fall distance, want it written off", v.FallDistance)
	}

	// A body already in the water banks nothing of a step's descent either.
	trackFall(&v, player.Position{Y: 94, OnGround: true}, player.Position{Y: 93, OnGround: true}, false, false, Wading)
	if v.FallDistance != 0 {
		t.Errorf("wading left %v of fall distance banked", v.FallDistance)
	}
}

// TestFallDamageReadsTheSafeDistance pins the safe_fall_distance attribute's
// word on the fall: everything past the safe distance costs, floored, and a
// fall no further than it costs nothing.
func TestFallDamageReadsTheSafeDistance(t *testing.T) {
	for _, tc := range []struct{ fall, safe, want float64 }{
		{10, 0, 10}, // nothing safe: the whole fall costs
		{5, 0, 5},
		{10, 3, 7}, // the default safe distance
		{10, 5, 5}, // jump boost II's safe five
		{5, 5, 0},  // no further than the safe distance
	} {
		if got := FallDamage(tc.fall, tc.safe, 1); got != tc.want {
			t.Errorf("a %v-block fall with a safe distance of %v hurt for %v, want %v", tc.fall, tc.safe, got, tc.want)
		}
	}
}

// TestResetsFallNamesTheEffectsThatWriteTheFallOff pins which effects the
// fall reset reads: slow falling and levitation, and nothing else.
func TestResetsFallNamesTheEffectsThatWriteTheFallOff(t *testing.T) {
	inst := func(id int32) []effect.Instance {
		return []effect.Instance{effect.New(id, 100, 0, false, true, true)}
	}
	for _, id := range []int32{effectSlowFalling, effectLevitation} {
		if !ResetsFall(inst(id)) {
			t.Errorf("effect %d should write the fall off", id)
		}
	}
	if ResetsFall(nil) {
		t.Error("a body with no effects should keep its fall")
	}
	if ResetsFall(inst(effectRegeneration)) {
		t.Error("regeneration should keep the fall")
	}
}

// TestAnEffectTakesTheFallBack walks the effects branch of TrackMovement:
// with slow falling or levitation active the body banks nothing of a step's
// descent, so the landing after it costs nothing.
func TestAnEffectTakesTheFallBack(t *testing.T) {
	for _, id := range []int32{effectSlowFalling, effectLevitation} {
		effects := []effect.Instance{effect.New(id, 100, 0, false, true, true)}
		v := FreshVitals(protocol.GameModeSurvival)
		if damage := trackFallEffects(&v, effects, player.Position{Y: 100}, player.Position{Y: 96}, false, false, Dry); damage != 0 {
			t.Fatalf("the step through the air under effect %d hurt for %v", id, damage)
		}
		if v.FallDistance != 0 {
			t.Fatalf("effect %d left %v of fall distance banked", id, v.FallDistance)
		}
		if damage := trackFallEffects(&v, effects, player.Position{Y: 96}, player.Position{Y: 94, OnGround: true}, false, false, Dry); damage != 0 {
			t.Errorf("landing under effect %d hurt for %v", id, damage)
		}
	}
}

// TestAClimbableCatchesAFallUnhurt checks the climb's mercy, the one the
// ladders owe a falling body: the fall the body carried into the catch is
// written off the moment its feet start a step in what it climbs, so the
// landing the catch itself makes costs nothing, however far the fall to it
// was.
func TestAClimbableCatchesAFallUnhurt(t *testing.T) {
	v := FreshVitals(protocol.GameModeSurvival)
	if damage := trackFall(&v, player.Position{Y: 100}, player.Position{Y: 96}, false, false, Dry); damage != 0 {
		t.Fatalf("the fall's step through the air hurt for %v", damage)
	}
	if v.FallDistance != 4 {
		t.Fatalf("the fall banked %v, want the four blocks it carried", v.FallDistance)
	}

	// The catch: the feet start the step on the climb, the fall the body
	// carried is written off, and the step's own descent — the slide the
	// catch settles into — is all that is left banked.
	if damage := trackClimbingFall(&v, player.Position{Y: 96}, player.Position{Y: 95.85}); damage != 0 {
		t.Errorf("the catch itself hurt for %v, want the climb to have taken the fall back", damage)
	}
	if math.Abs(v.FallDistance-0.15) > 1e-9 {
		t.Errorf("the catch left %v of fall distance, want the step's own 0.15", v.FallDistance)
	}
	if damage := trackClimbingFall(&v, player.Position{Y: 95.85}, player.Position{Y: 95.85, OnGround: true}); damage != 0 {
		t.Errorf("the catch's landing hurt for %v", damage)
	}
}

// TestAStepOffAClimbableCountsOnlyTheNewDrop checks the catch's other half:
// the climb forgives only what it caught, the drop that follows the step off
// being a fall of its own, banked from nothing.
func TestAStepOffAClimbableCountsOnlyTheNewDrop(t *testing.T) {
	v := FreshVitals(protocol.GameModeSurvival)
	if damage := trackFall(&v, player.Position{Y: 100}, player.Position{Y: 96}, false, false, Dry); damage != 0 {
		t.Fatalf("the fall's step through the air hurt for %v", damage)
	}

	// The hold: the body hangs on the climb, the fall it carried stays
	// written off, and the drop the step off takes is the only one the
	// landing reads.
	if damage := trackClimbingFall(&v, player.Position{Y: 96}, player.Position{Y: 96}); damage != 0 {
		t.Fatalf("holding on the climb hurt for %v", damage)
	}
	if damage := trackClimbingFall(&v, player.Position{Y: 96}, player.Position{Y: 95.85}); damage != 0 {
		t.Fatalf("the slide the hold's step off began hurt for %v", damage)
	}
	if damage := trackFall(&v, player.Position{Y: 95.85}, player.Position{Y: 89.85, OnGround: true}, false, false, Dry); damage != 3 {
		t.Errorf("landing six blocks below the catch hurt for %v, want the new drop's 3", damage)
	}
}

// TestAClimbSlideBanksNothingButItsSteps checks the climb's own descent: a
// body sliding down what it climbs banks each step's descent as it goes,
// but the climb writes the carry-off at the step's start, so however far
// the slide ran, the banked fall it lands with is the last step's own.
func TestAClimbSlideBanksNothingButItsSteps(t *testing.T) {
	v := FreshVitals(protocol.GameModeSurvival)
	was := player.Position{Y: 100}
	for range 40 {
		now := player.Position{Y: was.Y - 0.15}
		if damage := trackClimbingFall(&v, was, now); damage != 0 {
			t.Fatalf("a step of the slide hurt for %v", damage)
		}
		was = now
	}
	if math.Abs(v.FallDistance-0.15) > 1e-9 {
		t.Errorf("the slide left %v banked, want its last step's 0.15", v.FallDistance)
	}
	if damage := trackClimbingFall(&v, was, player.Position{Y: was.Y, OnGround: true}); damage != 0 {
		t.Errorf("landing off the slide hurt for %v", damage)
	}
}

// TestFallChargesExhaustionForItsOwn is the other side of a step: what a
// jump costs the food bar - a bite of exhaustion for the plain one, four
// bites for the sprint - and what sprinting a stretch of ground costs.
func TestFallChargesExhaustionForItsOwn(t *testing.T) {
	// A jump from standing: the exhaustion a plain jump carries.
	v := FreshVitals(protocol.GameModeSurvival)
	trackFall(&v, player.Position{Y: 100, OnGround: true}, player.Position{Y: 101}, false, false, Dry)
	if v.Exhaustion != 0.05 {
		t.Errorf("a jump left %v exhaustion, want 0.05", v.Exhaustion)
	}

	// A sprint jump: the 0.2 vanilla charges for it.
	v = FreshVitals(protocol.GameModeSurvival)
	trackFall(&v, player.Position{Y: 100, OnGround: true}, player.Position{Y: 101}, false, true, Dry)
	if v.Exhaustion != 0.2 {
		t.Errorf("a sprint jump left %v exhaustion, want 0.2", v.Exhaustion)
	}

	// Sprinting flat ground: 0.1 per block covered.
	v = FreshVitals(protocol.GameModeSurvival)
	trackFall(&v, player.Position{X: 0, Y: 100, OnGround: true}, player.Position{X: 5, Y: 100, OnGround: true}, false, true, Dry)
	if v.Exhaustion != 0.5 {
		t.Errorf("sprinting five blocks left %v exhaustion, want 0.5", v.Exhaustion)
	}

	// Walking costs nothing.
	v = FreshVitals(protocol.GameModeSurvival)
	trackFall(&v, player.Position{X: 0, Y: 100, OnGround: true}, player.Position{X: 100, Y: 100, OnGround: true}, false, false, Dry)
	if v.Exhaustion != 0 {
		t.Errorf("walking left %v exhaustion, want none", v.Exhaustion)
	}
}

// TestWaterAndAirChargeTheirOwn follows checkMovementStatistics: a swim or
// a walk under water costs a hundredth per block of the whole distance, a
// wade a hundredth per block of the horizontal one, and a sprint through
// the air nothing beyond its jump.
func TestWaterAndAirChargeTheirOwn(t *testing.T) {
	near := func(got, want float64) bool { return math.Abs(got-want) < 1e-9 }

	v := FreshVitals(protocol.GameModeSurvival)
	trackFall(&v, player.Position{X: 0, Y: 50}, player.Position{X: 3, Y: 46}, false, true, Submerged)
	if !near(v.Exhaustion, 0.05) {
		t.Errorf("swimming five blocks left %v exhaustion, want 0.05", v.Exhaustion)
	}

	v = FreshVitals(protocol.GameModeSurvival)
	trackFall(&v, player.Position{X: 0, Y: 62, OnGround: true}, player.Position{X: 4, Y: 61, OnGround: true}, false, false, Wading)
	if !near(v.Exhaustion, 0.04) {
		t.Errorf("wading four blocks left %v exhaustion, want 0.04", v.Exhaustion)
	}

	v = FreshVitals(protocol.GameModeSurvival)
	trackFall(&v, player.Position{X: 0, Y: 101}, player.Position{X: 2, Y: 100.5}, false, true, Dry)
	if v.Exhaustion != 0 {
		t.Errorf("sprinting through the air left %v exhaustion, want none", v.Exhaustion)
	}
}

// TestSwimmingChargesTheWholeDistanceAboveWater checks that the swimming
// pose is charged the 3D distance before the eye position is looked at, and
// a wader not swimming only the horizontal one.
func TestSwimmingChargesTheWholeDistanceAboveWater(t *testing.T) {
	was, now := player.Position{X: 0, Y: 62}, player.Position{X: 3, Y: 58}
	v := FreshVitals(protocol.GameModeSurvival)
	TrackMovement(&v, nil, was, now, false, false, Wading, true, false, false, 3.0, 1, Landing{})
	if math.Abs(v.Exhaustion-0.05) > 1e-9 {
		t.Errorf("swimming with the eyes above water left %v exhaustion, want 0.05", v.Exhaustion)
	}
	v = FreshVitals(protocol.GameModeSurvival)
	TrackMovement(&v, nil, was, now, false, false, Wading, false, false, false, 3.0, 1, Landing{})
	if math.Abs(v.Exhaustion-0.03) > 1e-9 {
		t.Errorf("wading left %v exhaustion, want 0.03", v.Exhaustion)
	}
}

// TestActionsChargeExhaustion pins the costs that are not movement: the
// damage type's own exhaustion, and nothing at all charged to the modes
// that cannot be hurt.
func TestActionsChargeExhaustion(t *testing.T) {
	for damageType, want := range map[registry.DamageType]float64{
		DamageTypeMobAttack: 0.1, DamageTypeArrow: 0.1, DamageTypeExplosion: 0.1,
		DamageTypeFall: 0, DamageTypeStarve: 0, DamageTypeOutOfWorld: 0, DamageTypeOnFire: 0,
		DamageTypeDrown: 0, DamageTypeInWall: 0, DamageTypeEnderPearl: 0,
		DamageTypeLava: 0.1, DamageTypeInFire: 0.1, DamageTypeCactus: 0.1,
		DamageTypeHotFloor: 0.1, DamageTypeSweetBerryBush: 0.1,
	} {
		if got := DamageExhaustion(damageType); got != want {
			t.Errorf("%s costs %v exhaustion, want %v", damageType, got, want)
		}
	}

	v := FreshVitals(protocol.GameModeCreative)
	Exhaust(&v, AttackExhaustion)
	if v.Exhaustion != 0 {
		t.Errorf("a creative player was charged %v exhaustion", v.Exhaustion)
	}
	v = FreshVitals(protocol.GameModeSurvival)
	Exhaust(&v, MineExhaustion)
	if v.Exhaustion != MineExhaustion {
		t.Errorf("a survival break left %v exhaustion, want %v", v.Exhaustion, MineExhaustion)
	}
}

// TestDamageExhaustionMatchesTheVanillaTable checks every registered damage
// type against the exhaustion its file in the vanilla data pack carries: the
// types on the nought list cost nothing, every other one a tenth.
// vanilla: data/minecraft/damage_type/*.json
func TestDamageExhaustionMatchesTheVanillaTable(t *testing.T) {
	// The nineteen types whose files carry "exhaustion": 0.
	zero := []registry.DamageType{
		registry.DamageTypeCramming, registry.DamageTypeDragonBreath, DamageTypeDrown,
		DamageTypeEnderPearl, DamageTypeFall, registry.DamageTypeFlyIntoWall,
		DamageTypeFreeze, registry.DamageTypeGeneric, DamageTypeGenericKill,
		DamageTypeInWall, registry.DamageTypeIndirectMagic, DamageTypeMagic,
		DamageTypeOnFire, DamageTypeOutOfWorld, registry.DamageTypeOutsideBorder,
		registry.DamageTypeSonicBoom, registry.DamageTypeStalagmite, DamageTypeStarve,
		DamageTypeWither,
	}
	// Every other type of the registry charges the tenth of a blow.
	tenth := []registry.DamageType{
		registry.DamageTypeArrow, registry.DamageTypeBadRespawnPoint, registry.DamageTypeCactus,
		registry.DamageTypeCampfire, registry.DamageTypeDryOut, registry.DamageTypeExplosion,
		registry.DamageTypeFallingAnvil, registry.DamageTypeFallingBlock,
		registry.DamageTypeFallingStalactite, registry.DamageTypeFireball,
		registry.DamageTypeFireworks, registry.DamageTypeHotFloor, registry.DamageTypeInFire,
		registry.DamageTypeLava, registry.DamageTypeLightningBolt, registry.DamageTypeMaceSmash,
		registry.DamageTypeMobAttack, registry.DamageTypeMobAttackNoAggro,
		registry.DamageTypeMobProjectile, registry.DamageTypePlayerAttack,
		registry.DamageTypePlayerExplosion, registry.DamageTypeSpear, registry.DamageTypeSpit,
		registry.DamageTypeSting, registry.DamageTypeSulfurCubeHot,
		registry.DamageTypeSweetBerryBush, registry.DamageTypeThorns, registry.DamageTypeThrown,
		registry.DamageTypeTrident, registry.DamageTypeUnattributedFireball,
		registry.DamageTypeWindCharge, registry.DamageTypeWitherSkull,
	}
	for _, damageType := range zero {
		if got := DamageExhaustion(damageType); got != 0 {
			t.Errorf("%s costs %v exhaustion, want 0", damageType, got)
		}
	}
	for _, damageType := range tenth {
		if got := DamageExhaustion(damageType); got != 0.1 {
			t.Errorf("%s costs %v exhaustion, want 0.1", damageType, got)
		}
	}
}

// TestStarvationFloorPerDifficulty is where each difficulty lets an empty
// stomach go: easy stops at ten, normal at one, hard nowhere, and peaceful
// would rather feed than bite.
func TestStarvationFloorPerDifficulty(t *testing.T) {
	// Whether an empty stomach takes its bite at the eighty-tick step,
	// with the player standing safely above the void at the given health.
	starves := func(difficulty uint8, health float64) bool {
		v := FreshVitals(protocol.GameModeSurvival)
		v.Health = health
		v.Food = 0
		var event VitalsEvent
		for range vitalsStepTicks {
			event, _, _ = tickOne(&v, difficulty, false)
		}
		return event == VitalsStarved
	}

	// Peaceful never starves anyone; its step is the free heal and feed
	// instead.
	for _, health := range []float64{20, 10, 0.5} {
		if starves(world.DifficultyPeaceful, health) {
			t.Errorf("peaceful starved a player at %v health", health)
		}
	}
	// Easy spares everything at ten and below; normal everything at one.
	if starves(world.DifficultyEasy, 10) {
		t.Error("easy starved a player at its floor of ten")
	}
	if !starves(world.DifficultyEasy, 10.5) {
		t.Error("easy did not starve a player above its floor")
	}
	if starves(world.DifficultyNormal, 1) {
		t.Error("normal starved a player at its floor of one")
	}
	if !starves(world.DifficultyNormal, 1.5) {
		t.Error("normal did not starve a player above its floor")
	}
	// Hard spares nothing at all.
	if !starves(world.DifficultyHard, 0.5) {
		t.Error("hard did not starve a player at half a half-heart")
	}
	// And the bite leaves the health for the hurt pipeline to take.
	v := FreshVitals(protocol.GameModeSurvival)
	v.Health = 15
	v.Food = 0
	for range vitalsStepTicks {
		tickOne(&v, world.DifficultyHard, false)
	}
	if v.Health != 15 {
		t.Errorf("the starving tick changed the health to %v, want it left for hurt", v.Health)
	}
}

// TestRegenerationNeedsAFedPlayer pins when the natural rhythm heals: at a
// food level of eighteen and above it pays a half-heart back and charges
// the food bar for it; below eighteen nothing happens at all.
func TestRegenerationNeedsAFedPlayer(t *testing.T) {
	regen := func(food int32) (event VitalsEvent, v player.Vitals) {
		v = FreshVitals(protocol.GameModeSurvival)
		v.Health = 10
		v.Food = food
		for range vitalsStepTicks {
			event, _, _ = tickOne(&v, world.DifficultyNormal, false)
		}
		return event, v
	}

	if event, after := regen(regenFood); event != VitalsHealed {
		t.Errorf("a fed player's tick came to %v, want a heal", event)
	} else {
		if after.Health != 11 {
			t.Errorf("the heal took the health to %v, want eleven", after.Health)
		}
		if after.Food != regenFood {
			t.Errorf("the heal changed the food to %v, want it untouched", after.Food)
		}
		// The heal's six exhaustion is charged now and drained on the
		// next tick, FoodData.tick's head.
		if after.Exhaustion != healExhaustion {
			t.Errorf("the heal left %v exhaustion, want the %v it cost", after.Exhaustion, healExhaustion)
		}
		tickOne(&after, world.DifficultyNormal, false)
		if after.Saturation != 4 {
			t.Errorf("the next tick left %v saturation, want the one the heal cost", after.Saturation)
		}
	}

	// One below the threshold: no heal, no drain.
	if event, after := regen(regenFood - 1); event != VitalsSteady {
		t.Errorf("a player below the threshold's tick came to %v, want steady", event)
	} else if after.Health != 10 || after.Saturation != 5 {
		t.Errorf("no heal should have changed nothing: health %v saturation %v", after.Health, after.Saturation)
	}

	// A full bar holds no heal, the food staying in reserve.
	v := FreshVitals(protocol.GameModeSurvival)
	for range vitalsStepTicks {
		if event, _, _ := tickOne(&v, world.DifficultyNormal, false); event != VitalsSteady {
			t.Fatalf("a full player's tick came to %v, want steady", event)
		}
	}
	if v.Health != MaxHealth || v.Saturation != 5 {
		t.Errorf("a full player changed: health %v saturation %v", v.Health, v.Saturation)
	}
}

// TestVoidHitsBelowTheWorld checks the void hurts on every tick below the
// world, the first included, and on none above it.
func TestVoidHitsBelowTheWorld(t *testing.T) {
	v := FreshVitals(protocol.GameModeSurvival)
	for i := 1; i <= 3; i++ {
		if _, hurts, _ := tickOne(&v, world.DifficultyNormal, true); len(hurts) != 1 ||
			hurts[0].DamageType != DamageTypeOutOfWorld || hurts[0].Amount != VoidDamage {
			t.Fatalf("void tick %d came to %v, want one void blow", i, hurts)
		}
	}
	if _, hurts, _ := tickOne(&v, world.DifficultyNormal, false); len(hurts) != 0 {
		t.Errorf("a tick above the world came to %v, want no blow", hurts)
	}
}

// TestInvulnerabilityCountsDown ticks off the half-second window a hit
// grants, which runs even for a body that cannot be hurt by anything else.
func TestInvulnerabilityCountsDown(t *testing.T) {
	v := FreshVitals(protocol.GameModeSurvival)
	v.InvulnerableTicks = HurtInvulnerableTicks
	for i := HurtInvulnerableTicks; i > 0; i-- {
		if event, _, _ := tickOne(&v, world.DifficultyNormal, false); event != VitalsSteady {
			t.Fatalf("the tick under invulnerability came to %v, want steady", event)
		}
		if v.InvulnerableTicks != i-1 {
			t.Fatalf("the window counted down to %v, want %v", v.InvulnerableTicks, i-1)
		}
	}

	// The window ticks away for a creative player too.
	v = FreshVitals(protocol.GameModeCreative)
	v.InvulnerableTicks = 1
	tickOne(&v, world.DifficultyNormal, false)
	if v.InvulnerableTicks != 0 {
		t.Errorf("the window did not count down for a creative player: %v", v.InvulnerableTicks)
	}
}

// TestModeCannotBeHurt pins which game modes keep a player out of harm's
// way, as the hurt pipeline asks it of the rules.
func TestModeCannotBeHurt(t *testing.T) {
	for _, mode := range []int32{protocol.GameModeCreative, protocol.GameModeSpectator} {
		if !ModeInvulnerable(mode) {
			t.Errorf("ModeInvulnerable said no to mode %v", mode)
		}
	}
	for _, mode := range []int32{protocol.GameModeSurvival, protocol.GameModeAdventure} {
		if ModeInvulnerable(mode) {
			t.Errorf("ModeInvulnerable said yes to mode %v", mode)
		}
	}
}

// TestFreshVitalsStartsFull is the state a join and a respawn grant.
func TestFreshVitalsStartsFull(t *testing.T) {
	v := FreshVitals(protocol.GameModeSurvival)
	if v.GameMode != protocol.GameModeSurvival || v.Health != MaxHealth || v.Food != MaxFood || v.Saturation != 5 {
		t.Errorf("fresh vitals are %+v, want a full fed survivor", v)
	}
	if dead, deadWeight := v.Dead, v.InvulnerableTicks != 0; dead || deadWeight {
		t.Errorf("fresh vitals carry death or a hurt window: %+v", v)
	}
	if math.IsNaN(v.Saturation) {
		t.Error("fresh saturation is not a number")
	}
}

// TestASaturatedFullBarHealsFast pins FoodData.tick's fast regeneration: a
// hurt player on a full bar with saturation behind it heals every tenth
// tick, a sixth of the saturation spent (six at most) coming back as
// health and going on the exhaustion — the heal that keeps a well-fed
// player standing through a fight.
func TestASaturatedFullBarHealsFast(t *testing.T) {
	v := FreshVitals(protocol.GameModeSurvival)
	v.Health = 10
	for i := 1; i < saturatedRegenTicks; i++ {
		if event, _, _ := tickOne(&v, world.DifficultyNormal, false); event != VitalsSteady {
			t.Fatalf("tick %d came to %v, want steady until the tenth", i, event)
		}
	}
	if event, _, _ := tickOne(&v, world.DifficultyNormal, false); event != VitalsHealed {
		t.Fatalf("the tenth tick came to %v, want a heal", event)
	}
	// Five saturation: five sixths of a half-heart back, five exhaustion
	// on — four of it turned into a point of saturation on the next tick,
	// one left over.
	if want := 10 + 5.0/6; v.Health != want {
		t.Errorf("the fast heal took the health to %v, want %v", v.Health, want)
	}
	tickOne(&v, world.DifficultyNormal, false)
	if v.Saturation != 4 || v.Exhaustion != 1 {
		t.Errorf("the fast heal left saturation %v exhaustion %v, want 4 and 1", v.Saturation, v.Exhaustion)
	}
	if v.Food != MaxFood {
		t.Errorf("the fast heal took food to %v, want the bar untouched", v.Food)
	}
}

// TestPeacefulHealsAndFeedsOnItsOwnClocks pins ServerPlayer.
// tickRegeneration: a half-heart and a point of saturation every twenty
// ticks, a shank every ten.
func TestPeacefulHealsAndFeedsOnItsOwnClocks(t *testing.T) {
	v := FreshVitals(protocol.GameModeSurvival)
	v.Health = 10
	v.Food = 10
	v.Saturation = 0
	for range peacefulHealTicks {
		tickOne(&v, world.DifficultyPeaceful, false)
	}
	if v.Food != 12 {
		t.Errorf("twenty peaceful ticks fed the bar to %v, want 12, a shank every ten", v.Food)
	}
	if v.Health != 11 {
		t.Errorf("twenty peaceful ticks healed to %v, want 11", v.Health)
	}
	if v.Saturation != 1 {
		t.Errorf("twenty peaceful ticks left saturation %v, want 1", v.Saturation)
	}
}

// TestPeacefulHealReachesTheBoostedBar heals on the peaceful world's own
// clock up to the bar health boost raises, past the base twenty.
func TestPeacefulHealReachesTheBoostedBar(t *testing.T) {
	// Age 19 so the tick that counts up to 20 is the peaceful heal's due one.
	v := player.Vitals{Health: 21, Food: 20, AirSupply: MaxAirSupply, AgeTicks: 19}
	var s effect.Set
	AddEffect(&v, &s, effect.New(effectHealthBoost, 100, 1, false, true, true))
	TickVitals(&v, &s, nil, world.DifficultyPeaceful, false, Habitat{}, nil, 0, true)
	if v.Health != 22 {
		t.Errorf("a peaceful heal under Health Boost II reached %v, want 22", v.Health)
	}
}

// TestLandingScalesTheFall pins the landing block's word on a fall,
// vanilla's Block.fallOn: a ten-block fall past the safe three is seven
// half-hearts by default, a hay bale a fifth of that floored to one, a bed
// its halved distance floored to two, a slime block nothing at all. A web or
// a berry bush anywhere in the body's cells, or powder snow at the feet,
// banks nothing; a powder snow in a head cell alone does not.
func TestLandingScalesTheFall(t *testing.T) {
	slime := registry.MustState("minecraft:slime_block", nil)
	hay := registry.MustState("minecraft:hay_block", nil)
	bed := registry.MustState("minecraft:red_bed", nil)
	web := registry.MustState("minecraft:cobweb", nil)
	snow := registry.MustState("minecraft:powder_snow", nil)
	stone := registry.MustState("minecraft:stone", nil)
	land := func(landing Landing) float64 {
		v := FreshVitals(protocol.GameModeSurvival)
		damage, _ := TrackMovement(&v, nil, player.Position{Y: 100}, player.Position{Y: 90, OnGround: true},
			false, false, Dry, false, false, false, 3.0, 1, landing)
		return damage
	}
	for _, tc := range []struct {
		name    string
		landing Landing
		want    float64
	}{
		{"default", Landing{Ground: stone}, 7},
		{"hay", Landing{Ground: hay}, 1},
		{"bed", Landing{Ground: bed}, 2},
		{"slime", Landing{Ground: slime}, 0},
		{"web at the feet", Landing{Ground: stone, Feet: web, Overlapped: []uint16{web}}, 0},
		{"web at a head cell", Landing{Ground: stone, Overlapped: []uint16{stone, web}}, 0},
		{"powder snow at the feet", Landing{Ground: stone, Feet: snow}, 0},
		{"powder snow in a head cell", Landing{Ground: stone, Overlapped: []uint16{stone, snow}}, 7},
	} {
		if got := land(tc.landing); math.Abs(got-tc.want) > 1e-9 {
			t.Errorf("a ten-block fall onto %s hurt for %v, want %v", tc.name, got, tc.want)
		}
	}
}

// TestAShortDropOntoASoftBlockIsFree pins the floor after the landing
// block's scaling: a fall of four to six blocks has three or fewer past the
// safe three, so a hay bale's fifth and a bed's half both floor to nothing.
func TestAShortDropOntoASoftBlockIsFree(t *testing.T) {
	for _, ground := range []struct {
		name  string
		state uint16
	}{
		{"hay", registry.MustState("minecraft:hay_block", nil)},
		{"bed", registry.MustState("minecraft:red_bed", nil)},
	} {
		for _, drop := range []float64{4, 5, 6} {
			v := FreshVitals(protocol.GameModeSurvival)
			damage, _ := TrackMovement(&v, nil, player.Position{Y: 64 + drop}, player.Position{Y: 64, OnGround: true},
				false, false, Dry, false, false, false, 3.0, 1, Landing{Ground: ground.state})
			if damage != 0 {
				t.Errorf("a %v-block fall onto %s hurt for %v, want none", drop, ground.name, damage)
			}
		}
	}
}

// TestFallResetSweepReadsTheFallDamageResettingTag checks the sweep's
// predicate and its gates: a step of a block or longer through a web, a berry
// bush, a vine, a ladder or water writes the fall off, a step through stone
// does not, a step of a block or less is not swept however much fall it
// carries, and neither is a step by a body carrying none. A player's sweep
// also writes the fall off on an end portal or gateway and on a nether portal
// while the default delay rule makes it immediate; a mob's does not.
// vanilla: Entity.move, ClipContext.Block.FALLDAMAGE_RESETTING,
// ClipContext.Fluid.WATER
func TestFallResetSweepReadsTheFallDamageResettingTag(t *testing.T) {
	for _, tc := range []struct {
		name     string
		cell     [3]int32
		block    string
		carry    float64
		fromY    float64
		toY      float64
		isPlayer bool
		nether   bool
		want     bool
	}{
		{"web", [3]int32{0, 70, 0}, "minecraft:cobweb", 4, 76, 64, false, false, true},
		{"berry bush", [3]int32{0, 70, 0}, "minecraft:sweet_berry_bush", 4, 76, 64, false, false, true},
		{"vine", [3]int32{0, 70, 0}, "minecraft:vine", 4, 76, 64, false, false, true},
		{"ladder", [3]int32{0, 70, 0}, "minecraft:ladder", 4, 76, 64, false, false, true},
		{"water", [3]int32{0, 70, 0}, "minecraft:water", 4, 76, 64, false, false, true},
		{"stone", [3]int32{0, 70, 0}, "minecraft:stone", 4, 76, 64, false, false, false},
		{"a step under a block", [3]int32{0, 70, 0}, "minecraft:cobweb", 4, 71.5, 70.7, false, false, false},
		{"no fall carried", [3]int32{0, 70, 0}, "minecraft:cobweb", 0, 76, 64, false, false, false},
		{"a layer above the path", [3]int32{0, 80, 0}, "minecraft:cobweb", 4, 76, 64, false, false, false},
		{"a layer past the eight-block reach", [3]int32{0, 66, 0}, "minecraft:cobweb", 4, 76, 64, false, false, false},
		{"a player's end portal", [3]int32{0, 70, 0}, "minecraft:end_portal", 4, 76, 64, true, false, true},
		{"a player's end gateway", [3]int32{0, 70, 0}, "minecraft:end_gateway", 4, 76, 64, true, false, true},
		{"a mob's end portal", [3]int32{0, 70, 0}, "minecraft:end_portal", 4, 76, 64, false, false, false},
		{"a player's nether portal made immediate", [3]int32{0, 70, 0}, "minecraft:nether_portal", 4, 76, 64, true, true, true},
		{"a player's nether portal still delayed", [3]int32{0, 70, 0}, "minecraft:nether_portal", 4, 76, 64, true, false, false},
	} {
		w := stubBlocks{tc.cell: stateOf(t, tc.block)}
		was := player.Position{X: 0.5, Y: tc.fromY, Z: 0.5}
		now := player.Position{X: 0.5, Y: tc.toY, Z: 0.5}
		if got := FallResetSweep(w, tc.carry, was, now, tc.isPlayer, tc.nether); got != tc.want {
			t.Errorf("FallResetSweep through %s = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// TestLandingOnAnUpwardDripstoneTipIsStalagmiteDamage pins
// PointedDripstoneBlock.fallOn: a landing on an upward tip counts the fall
// 2.5 blocks longer and doubles the damage, as minecraft:stalagmite damage;
// a downward tip, a thicker piece and a landing that fell nothing act as stone.
func TestLandingOnAnUpwardDripstoneTipIsStalagmiteDamage(t *testing.T) {
	piece := func(direction, thickness string) uint16 {
		return registry.MustState("minecraft:pointed_dripstone", map[string]string{
			"vertical_direction": direction, "thickness": thickness, "waterlogged": "false"})
	}
	land := func(ground uint16, drop float64) float64 {
		v := FreshVitals(protocol.GameModeSurvival)
		damage, _ := TrackMovement(&v, nil, player.Position{Y: 64 + drop}, player.Position{Y: 64, OnGround: true},
			false, false, Dry, false, false, false, 3.0, 1, Landing{Ground: ground})
		return damage
	}
	tip := piece("up", "tip")
	// floor((1 + 2.5 - 3) * 2)
	if got := land(tip, 1); got != 1 {
		t.Errorf("a one-block drop onto an upward tip hurt for %v, want 1", got)
	}
	if got := land(tip, 0); got != 0 {
		t.Errorf("standing on an upward tip hurt for %v, want none", got)
	}
	if got := FallOn(tip).DamageType; got != DamageTypeStalagmite {
		t.Errorf("the upward tip's damage type = %q, want %q", got, DamageTypeStalagmite)
	}
	for _, tc := range []struct{ direction, thickness string }{
		{"down", "tip"}, {"up", "frustum"}, {"up", "middle"}, {"up", "base"}, {"up", "tip_merge"},
	} {
		ground := piece(tc.direction, tc.thickness)
		if got := land(ground, 1); got != 0 {
			t.Errorf("a one-block drop onto %s/%s hurt for %v, want none like stone", tc.direction, tc.thickness, got)
		}
		if got := FallOn(ground).DamageType; got != DamageTypeFall {
			t.Errorf("%s/%s damage type = %q, want fall", tc.direction, tc.thickness, got)
		}
	}
	if got := DeathMessage(DamageTypeStalagmite, protocol.Text{Text: "Alex"}, nil); got.Translate != "death.attack.stalagmite" {
		t.Errorf("the stalagmite death message = %q, want death.attack.stalagmite", got.Translate)
	}
}
