package entity

import (
	"math/rand"
	"testing"

	"github.com/google/uuid"

	"github.com/golem-mc/golem/internal/server/rules"
)

// TestAWolfShakesFor42DryTicksAndSoundsOnce steps a wet wolf in dry weather:
// the shake sound plays on its first dry tick only, and the shake ends on
// the 42nd, after which the wolf is dry.
// vanilla: Wolf.tick
func TestAWolfShakesFor42DryTicksAndSoundsOnce(t *testing.T) {
	w := newTestWorld()
	m := NewMob(TypeWolf, 1, 0.5, 64, 0.5, 1)
	m.OnGround = true
	m.wolf.wet = true
	sounds, ticks := 0, 0
	for ; m.wolf.wet && ticks < 100; ticks++ {
		for _, ev := range m.wolfWetStep(w, nil) {
			if ev.Kind == MobWolfShakeSound {
				sounds++
			}
		}
	}
	if sounds != 1 {
		t.Errorf("shake sounds = %d, want 1", sounds)
	}
	if ticks != 42 {
		t.Errorf("the wolf dried after %d ticks, want 42", ticks)
	}
}

// TestAStruckWolfTurnsOnItsAttackerAndRalliesThePack: a wild wolf hit by a
// player targets them, and a neighbouring wolf is sent after them too.
// vanilla: Wolf.registerGoals' HurtByTargetGoal.setAlertOthers
func TestAStruckWolfTurnsOnItsAttackerAndRalliesThePack(t *testing.T) {
	w := newTestWorld()
	floor64(w, t)
	wolf := NewMob(TypeWolf, 1, 0.5, 64, 0.5, 3)
	packmate := NewMob(TypeWolf, 2, 3.5, 64, 3.5, 4)
	attacker := survivor(7, 5.5)
	wolf.TakeDamage(1, attacker)
	for range 5 {
		wolf.tick(w, []Player{attacker}, []*Mob{packmate})
	}
	if wolf.TargetID != 7 {
		t.Errorf("a struck wolf targeted %d, want 7", wolf.TargetID)
	}
	if packmate.TargetID != 7 {
		t.Errorf("the packmate targeted %d, want 7", packmate.TargetID)
	}
}

// TestAWolfBitesForFour: the hunt's bite carries the wolf's attack damage.
// vanilla: Wolf.createAttributes
func TestAWolfBitesForFour(t *testing.T) {
	w := newTestWorld()
	floor64(w, t)
	wolf := NewMob(TypeWolf, 1, 0.5, 64, 0.5, 3)
	attacker := survivor(7, 1.0)
	wolf.TakeDamage(1, attacker)
	bitten := false
	for i := 0; i < 60 && !bitten; i++ {
		for _, ev := range wolf.Tick(w, []Player{attacker}) {
			if ev.Kind == MobAttackedPlayer {
				bitten = true
				if ev.Damage != 4 {
					t.Errorf("bite = %v, want 4", ev.Damage)
				}
			}
		}
	}
	if !bitten {
		t.Error("the struck wolf never bit")
	}
}

var (
	ownerA = uuid.MustParse("00000000-0000-0000-0000-00000000000a")
	ownerB = uuid.MustParse("00000000-0000-0000-0000-00000000000b")
)

// tameWolf is a tame wolf of the owner at the place given, standing.
func tameWolf(id int32, x, z float64, owner uuid.UUID) *Mob {
	m := NewMob(TypeWolf, id, x, 64, z, int64(id))
	m.OnGround = true
	m.Tamable.Tame, m.Tamable.Owner = true, owner
	return m
}

// TestATameWolfSitsWithoutItsOwnerAndStandsWithThem: a tame wolf whose owner
// is away sits; with the owner about it stands unless ordered down.
// vanilla: SitWhenOrderedToGoal.canUse
func TestATameWolfSitsWithoutItsOwnerAndStandsWithThem(t *testing.T) {
	w := newTestWorld()
	floor64(w, t)
	away := tameWolf(1, 0.5, 0.5, ownerA)
	near := tameWolf(2, 0.5, 0.5, ownerA)
	owner := Player{X: 3.5, Y: 64, Z: 0.5, EntityID: 7, Huntable: true, UUID: ownerA}
	for range 6 {
		away.tick(w, nil, nil)
		near.tick(w, []Player{owner}, nil)
	}
	if !away.Tamable.SittingPose {
		t.Error("a tame wolf without its owner stands")
	}
	if near.Tamable.SittingPose {
		t.Error("a tame wolf beside its owner sits unordered")
	}
	near.ToggleSit()
	for range 6 {
		near.tick(w, []Player{owner}, nil)
	}
	if !near.Tamable.SittingPose {
		t.Error("an ordered wolf stands")
	}
	near.TakeDamage(1, survivor(9, 3))
	for range 6 {
		near.tick(w, []Player{owner}, nil)
	}
	if near.Tamable.OrderedToSit || near.Tamable.SittingPose {
		t.Error("a hurt wolf is still sitting")
	}
}

// TestAWolfTakesItsVariantFromTheBiomeAndItsPackKeepsIt: the first wolf of a
// pack picks the variant of the biome, the rest repeat it, whatever biome
// they stand in; a biome without a variant gives the pale wolf.
// vanilla: Wolf.finalizeSpawn, WolfVariants.bootstrap
func TestAWolfTakesItsVariantFromTheBiomeAndItsPackKeepsIt(t *testing.T) {
	for biome, want := range map[string]string{
		"minecraft:forest":                  "minecraft:woods",
		"minecraft:savanna":                 "minecraft:spotted",
		"minecraft:jungle":                  "minecraft:rusty",
		"minecraft:badlands":                "minecraft:striped",
		"minecraft:grove":                   "minecraft:snowy",
		"minecraft:old_growth_pine_taiga":   "minecraft:black",
		"minecraft:snowy_taiga":             "minecraft:ashen",
		"minecraft:old_growth_spruce_taiga": "minecraft:chestnut",
		"minecraft:plains":                  "minecraft:pale",
	} {
		first := NewMob(TypeWolf, 1, 0, 64, 0, 1)
		group := NewSpawnGroup(GoDice{rand.New(rand.NewSource(1))})
		first.FinalizeSpawn(GoDice{first.Rand}, testBiome(biome), group, rules.LocalDifficulty{})
		if first.Variant != want {
			t.Errorf("%s: variant %q, want %q", biome, first.Variant, want)
		}
		next := NewMob(TypeWolf, 2, 0, 64, 0, 2)
		next.FinalizeSpawn(GoDice{next.Rand}, testBiome("minecraft:desert"), group, rules.LocalDifficulty{})
		if next.Variant != want {
			t.Errorf("%s: the pack's next wolf is %q, want %q", biome, next.Variant, want)
		}
		if first.SoundVariant == "" {
			t.Errorf("%s: no sound variant", biome)
		}
	}
}

// spawnGroundCases checks validSpawnPositionAt for creature t, in full sky
// light, on each ground block.
func spawnGroundCases(t *testing.T, typ MobType, cases map[string]bool) {
	t.Helper()
	rng := rand.New(rand.NewSource(1))
	for ground, want := range cases {
		w := newSpawnTestWorld()
		w.sky = 15
		floor(w.testWorld, -4, -4, 4, 4, 63, defaultState(t, ground))
		if got := validSpawnPositionAt(w, GoDice{rng}, typ, 0.5, 64, 0.5, 0, CategoryCreature); got != want {
			t.Errorf("%v on %s = %v, want %v", typ, ground, got, want)
		}
	}
}
