package entity

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/effect"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/registry"
)

// The squid's own halves, driven against the stub world: the flee's scaling,
// the despawn the categories share, the water cells the spawn rules accept,
// and the ink and dark ticks a hurt leaves behind.

// TestSquidFleeScalesTheRawOffset checks Squid$SquidFleeGoal's thrown-away
// normalize: the raw offset from the attacker is scaled by the push, not
// the unit vector, so a strike from four blocks swims six tenths a tick away
// and one from eight the tapered 0.96, where a normalized vector would give
// a fifth and an eighth of that.
// vanilla: Squid$SquidFleeGoal.tick
func TestSquidFleeScalesTheRawOffset(t *testing.T) {
	w := newTestWorld()
	water := defaultState(t, "minecraft:water")
	for x := int32(-20); x <= 20; x++ {
		for z := int32(-20); z <= 20; z++ {
			for y := int32(58); y <= 68; y++ {
				w.set(x, y, z, water)
			}
		}
	}
	view := &tickView{w: w}
	for _, tc := range []struct {
		name      string
		attackerX float64
		wantMoveX float64
	}{
		{"three blocks out", 3.5, -0.45},
		{"four blocks out", 4.5, -0.6},
		{"eight blocks out", 8.5, -0.96},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := NewMob(TypeSquid, 1, 0.5, 63, 0.5, 1)
			m.squidFleeFrom(view, &Player{X: tc.attackerX, Y: 63, Z: 0.5, EntityID: 2})
			if !closeEnough(m.squid.moveX, tc.wantMoveX) {
				t.Errorf("the flee's swim vector = %v, want %v", m.squid.moveX, tc.wantMoveX)
			}
		})
	}
}

// TestSquidDespawnsBeyondTheCapDistance checks the despawn the two water
// categories share with the monsters: a squid two hundred blocks from the
// nearest player is removed by the next tick, no corpse and no sound, while
// the peaceful discard stays the monsters' alone — a squid with a player
// beside it on a peaceful world is not removed.
// vanilla: Mob.checkDespawn, Monster.shouldDespawnInPeaceful
func TestSquidDespawnsBeyondTheCapDistance(t *testing.T) {
	w := newSpawnTestWorld()
	m := NewMob(TypeSquid, 7, 0.5, 64, 0.5, 1)
	far := []Player{{X: 0.5, Y: 64, Z: 200.5, EntityID: 2}}
	m.Tick(w, far)
	if m.Alive() {
		t.Fatal("a squid two hundred blocks from the nearest player stayed in the world")
	}

	w.diff = uint8(rules.DifficultyPeaceful)
	w.diffUp = true
	calm := NewMob(TypeSquid, 8, 0.5, 64, 0.5, 1)
	near := []Player{{X: 0.5, Y: 64, Z: 5.5, EntityID: 2}}
	calm.Tick(w, near)
	if !calm.Alive() {
		t.Fatal("a squid with a player beside it despawned on a peaceful world")
	}
}

// TestSquidSurfaceSpawnTakesAFluidCell checks the surface rule's water: the
// IN_WATER placement reads the cell's fluid, so a kelp cell is taken where the
// water block alone would not be, and the block above must be the water block
// itself, so a seagrass above is refused.
// vanilla: AgeableWaterCreature.checkSurfaceAgeableWaterCreatureSpawnRules,
// SpawnPlacementTypes.IN_WATER
func TestSquidSurfaceSpawnTakesAFluidCell(t *testing.T) {
	w := newSpawnTestWorld()
	x, z := atChunk(0, 0)
	const y = 60
	water := defaultState(t, "minecraft:water")
	kelp := defaultState(t, "minecraft:kelp")
	seagrass := defaultState(t, "minecraft:seagrass")
	w.set(x, y-1, z, water)
	w.set(x, y, z, water)
	w.set(x, y+1, z, water)
	if !squidSpawnPositionOk(w, CategoryWaterCreature, x, y, z) {
		t.Fatal("a column of water was refused a surface squid")
	}
	w.set(x, y, z, kelp)
	if !squidSpawnPositionOk(w, CategoryWaterCreature, x, y, z) {
		t.Error("a kelp cell was refused the surface squid's water")
	}
	w.set(x, y, z, water)
	w.set(x, y+1, z, seagrass)
	if squidSpawnPositionOk(w, CategoryWaterCreature, x, y, z) {
		t.Error("a seagrass above was taken as the surface squid's water")
	}
}

// TestGlowSquidSpawnAsksTheWaterBlockAndNoConductor checks the underground
// rule's water cell and the placement's blocked-above gate: the cell must be
// the water block itself, and the block above may not be a redstone
// conductor — so glass above is left through where a full-block test would
// refuse it, and stone above is not.
// vanilla: GlowSquid.checkGlowSquidSpawnRules, SpawnPlacementTypes.IN_WATER
func TestGlowSquidSpawnAsksTheWaterBlockAndNoConductor(t *testing.T) {
	w := newSpawnTestWorld()
	x, z := atChunk(0, 0)
	const y = 20 // sea level 63, the glow squid's band under 30
	water := defaultState(t, "minecraft:water")
	glass := defaultState(t, "minecraft:glass")
	stone := defaultState(t, "minecraft:stone")
	kelp := defaultState(t, "minecraft:kelp")
	w.set(x, y, z, water)
	w.set(x, y+1, z, glass)
	if !squidSpawnPositionOk(w, CategoryUndergroundWater, x, y, z) {
		t.Fatal("a glass block over the glow squid's water refused it")
	}
	w.set(x, y+1, z, stone)
	if squidSpawnPositionOk(w, CategoryUndergroundWater, x, y, z) {
		t.Error("a stone block over the glow squid's water was taken for the open cell")
	}
	w.set(x, y, z, kelp)
	w.set(x, y+1, z, glass)
	if squidSpawnPositionOk(w, CategoryUndergroundWater, x, y, z) {
		t.Error("a kelp cell was taken as the glow squid's water")
	}
}

// TestGlowSquidDimsOnAHurtWithNoAttacker checks the dark ticks the glow
// squid's own hurt sets: any landed hurt dims it, a fall with nobody behind
// it as much as a blow.
// vanilla: GlowSquid.hurtServer
func TestGlowSquidDimsOnAHurtWithNoAttacker(t *testing.T) {
	m := NewMob(TypeGlowSquid, 9, 0.5, 64, 0.5, 1)
	if m.DarkTicks != 0 {
		t.Fatalf("a fresh glow squid stands %d dark ticks in, want none", m.DarkTicks)
	}
	if landed, _ := m.plainHurt(1, rules.DamageTypeFall, false); !landed {
		t.Fatal("the fall did not land")
	}
	if m.DarkTicks != 100 {
		t.Errorf("a hurt with no attacker left %d dark ticks, want the hundred", m.DarkTicks)
	}
}

// TestSquidInkNeedsAnAttackerBehindTheBlow checks the getLastHurtByMob gate
// Squid.hurtServer squirts through: a hurt with nobody remembered leaves the
// squid dry, and one with a remembered striker squeezes the ink out however
// the hurt itself came.
// vanilla: Squid.hurtServer
func TestSquidInkNeedsAnAttackerBehindTheBlow(t *testing.T) {
	lonely := NewMob(TypeSquid, 10, 0.5, 64, 0.5, 1)
	if landed, _ := lonely.plainHurt(1, rules.DamageTypeFall, false); !landed {
		t.Fatal("the fall did not land")
	}
	if lonely.Squirt {
		t.Error("a squid with nobody behind the blow squirted ink")
	}

	remembering := NewMob(TypeSquid, 11, 0.5, 64, 0.5, 1)
	remembering.retaliate = 5
	if landed, _ := remembering.plainHurt(1, rules.DamageTypeFall, false); !landed {
		t.Fatal("the fall did not land")
	}
	if !remembering.Squirt {
		t.Error("a squid a striker still stands behind did not squirt on a later hurt")
	}
}

// TestABeachedSquidIgnoresSlowFallingAndRisesOnLevitation pins the squid's
// own out-of-water vertical: plain gravity whatever Slow Falling says, and
// Levitation setting the velocity outright.
// vanilla: Squid.aiStep
func TestABeachedSquidIgnoresSlowFallingAndRisesOnLevitation(t *testing.T) {
	w := newTestWorld()
	view := &tickView{w: w}

	slow := NewMob(TypeSquid, 1, 0.5, 120, 0.5, 1)
	slow.AddEffect(effect.New(registry.MustMobEffect("minecraft:slow_falling"), 100000, 0, false, true, true))
	slow.squidTravel(view)
	if want := -mobGravity * verticalDrag; !closeEnough(slow.VY, want) {
		t.Errorf("vy of a slow-falling beached squid = %v, want %v", slow.VY, want)
	}

	lev := NewMob(TypeSquid, 2, 0.5, 120, 0.5, 1)
	lev.AddEffect(effect.New(registry.MustMobEffect("minecraft:levitation"), 100000, 1, false, true, true))
	lev.squidTravel(view)
	if want := 0.1 * verticalDrag; !closeEnough(lev.VY, want) {
		t.Errorf("vy of a levitating beached squid = %v, want %v", lev.VY, want)
	}
}
