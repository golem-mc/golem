package entity

import (
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world/levelgen"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// regionDice is chunk (0, 0)'s WorldGenRegion random at level seed 0.
func regionDice() levelgen.RandomSource {
	return levelgen.NewXoroshiroNamedRandom(0, "minecraft:worldgen_region_random").At(0, 0, 0)
}

// TestRandomSpawnBonusSavesAndLoads holds the follow_range bonus through a
// save and a load, for an animal's list and beside a zombie's own bonus,
// without waking an attribute map, and checks it scales the follow range.
func TestRandomSpawnBonusSavesAndLoads(t *testing.T) {
	for _, kind := range []MobType{TypeCow, TypeZombie, TypeHorse} {
		m := NewMob(kind, 1, 0.5, 64, 0.5, 1)
		m.FinalizeSpawn(regionDice(), testBiome("minecraft:plains"), nil, rules.LocalDifficulty{})
		if m.followSpawnBonus == 0 {
			t.Fatalf("%v: finalize rolled no follow-range bonus", kind)
		}
		tag := nbt.Compound{}
		m.SaveTo(tag, nil)
		follow, ok := followRangeEntry(tag)
		if !ok {
			t.Fatalf("%v: saved attributes %v hold no follow_range entry", kind, tag["attributes"])
		}
		if got := modifierAmount(follow, modRandomSpawn); got != m.followSpawnBonus {
			t.Errorf("%v: saved %s = %v, want %v", kind, modRandomSpawn, got, m.followSpawnBonus)
		}
		back := NewMob(kind, 2, 0.5, 64, 0.5, 2)
		back.LoadFrom(tag, nil, nil)
		if back.followSpawnBonus != m.followSpawnBonus {
			t.Errorf("%v: loaded bonus %v, want %v", kind, back.followSpawnBonus, m.followSpawnBonus)
		}
		if back.Attributes() != nil {
			t.Errorf("%v: the saved bonus woke an attribute map", kind)
		}
		want := back.BaseFollowRange() * (1 + back.followSpawnBonus) * (1 + back.zombie.FollowBonus)
		if got := back.followRange(); math.Abs(got-want) > 1e-12 {
			t.Errorf("%v: follow range %v, want %v", kind, got, want)
		}
		if a := back.BuildAttributes(nbt.List{}); math.Abs(a.Value(registry.AttributeFollowRange)-want) > 1e-9 {
			t.Errorf("%v: map follow range %v, want %v", kind, a.Value(registry.AttributeFollowRange), want)
		}
	}
}

// checkMobStep checks Mob.finalizeSpawn's follow-range triangle and
// left-handed roll against want.
func checkMobStep(t *testing.T, want levelgen.RandomSource, m *Mob) {
	t.Helper()
	bonus := followSpawnSpread * (want.NextDouble() - want.NextDouble())
	if m.followSpawnBonus != bonus {
		t.Errorf("%v follow-range bonus = %v, want %v", m.Type, m.followSpawnBonus, bonus)
	}
	if left := want.NextFloat() < 0.05; m.LeftHanded != left {
		t.Errorf("%v left-handed = %v, want %v", m.Type, m.LeftHanded, left)
	}
}

// soundVariantOf draws a sound variant of the named registry from want.
// vanilla: VariantUtils.pickRandomSoundVariant
func soundVariantOf(want levelgen.RandomSource, name string) string {
	r, _ := registry.Get(name)
	return r.Entries[want.NextIntN(int32(len(r.Entries)))]
}
