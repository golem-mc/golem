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
