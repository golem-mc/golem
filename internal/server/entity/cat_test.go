package entity

import (
	"math/rand"
	"testing"

	"github.com/google/uuid"
)

// TestCatsCourtOnlyWhenBothAreTame: a loving cat finds a loving cat as its
// partner only when both are tame.
// vanilla: Cat.canMate
func TestCatsCourtOnlyWhenBothAreTame(t *testing.T) {
	for _, tc := range []struct {
		name         string
		aTame, bTame bool
		want         bool
	}{
		{"both tame", true, true, true},
		{"wild partner", true, false, false},
		{"wild self", false, true, false},
	} {
		a := NewMob(TypeCat, 1, 0.5, 64, 0.5, 1)
		b := NewMob(TypeCat, 2, 1.5, 64, 0.5, 2)
		a.Tamable.Tame, b.Tamable.Tame = tc.aTame, tc.bTame
		a.InLove, b.InLove = 600, 600
		g := &breedGoal{m: a}
		got := g.freePartner(&tickView{others: []*Mob{a, b}}) == b
		if got != tc.want {
			t.Errorf("%s: partner found %v, want %v", tc.name, got, tc.want)
		}
	}
}

// TestKittensTakeAParentsVariantAndATameMothersOwner: a bred cat wears the
// variant of one of its parents, and belongs to a tame mother's owner.
// vanilla: Cat.getBreedOffspring
func TestKittensTakeAParentsVariantAndATameMothersOwner(t *testing.T) {
	owner := uuid.New()
	for _, tame := range []bool{true, false} {
		sawMother, sawFather := false, false
		for seed := range int64(40) {
			mother := NewMob(TypeCat, 1, 0, 64, 0, seed)
			father := NewMob(TypeCat, 2, 1, 64, 0, seed+100)
			baby := NewMob(TypeCat, 3, 0, 64, 0, seed+200)
			mother.Variant, father.Variant = "minecraft:red", "minecraft:white"
			mother.Tamable.Tame, mother.Tamable.Owner = tame, owner
			mother.BreedCat(baby, father)
			switch baby.Variant {
			case "minecraft:red":
				sawMother = true
			case "minecraft:white":
				sawFather = true
			default:
				t.Fatalf("kitten wears %q, want a parent's variant", baby.Variant)
			}
			if baby.Tamable.Tame != tame || (tame && baby.Tamable.Owner != owner) {
				t.Fatalf("mother tame %v gave a kitten tame %v owned by %v", tame, baby.Tamable.Tame, baby.Tamable.Owner)
			}
		}
		if !sawMother || !sawFather {
			t.Errorf("in 40 litters the mother's variant came %v and the father's %v, want both", sawMother, sawFather)
		}
	}
}

// TestAnOcelotSpawnsOverGrassOrLeavesAtSeaLevelTwoTimesInThree: the ocelot's
// own rule decides in either spawn category, ignoring the light the animals
// need.
// vanilla: Ocelot.checkOcelotSpawnRules, Ocelot.checkSpawnObstruction
func TestAnOcelotSpawnsOverGrassOrLeavesAtSeaLevelTwoTimesInThree(t *testing.T) {
	for _, tc := range []struct {
		name  string
		floor string
		y     float64
		any   bool
	}{
		{"grass", "minecraft:grass_block", 64, true},
		{"leaves", "minecraft:oak_leaves", 64, true},
		{"stone", "minecraft:stone", 64, false},
		{"grass below sea level", "minecraft:grass_block", 62, false},
	} {
		for _, category := range []SpawnCategory{CategoryCreature, CategoryMonster} {
			w := newSpawnTestWorld()
			w.sky = 0
			floor(w.testWorld, -4, -4, int32(tc.y)-1, 4, int32(tc.y)-1, defaultState(t, tc.floor))
			rng := rand.New(rand.NewSource(7))
			spawned := 0
			for range 300 {
				if validSpawnPositionAt(w, GoDice{rng}, TypeOcelot, 0.5, tc.y, 0.5, 0, category) {
					spawned++
				}
			}
			if tc.any && (spawned < 170 || spawned > 230) {
				t.Errorf("%s, category %d: %d of 300 spawned, want about two in three", tc.name, category, spawned)
			}
			if !tc.any && spawned != 0 {
				t.Errorf("%s, category %d: %d of 300 spawned, want none", tc.name, category, spawned)
			}
		}
	}
}
