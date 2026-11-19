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

// TestAFollowOwnerGoalWaitsOutAnOrderToSit: an owner out of reach does not
// call a cat ordered to sit, and does call one that is not.
// vanilla: TamableAnimal.unableToMoveToOwner
func TestAFollowOwnerGoalWaitsOutAnOrderToSit(t *testing.T) {
	owner := uuid.New()
	cat := NewMob(TypeCat, 1, 0.5, 64, 0.5, 1)
	cat.Tamable.Tame, cat.Tamable.Owner = true, owner
	view := &tickView{players: []Player{{UUID: owner, X: 15.5, Y: 64, Z: 0.5}}}
	g := &followOwnerGoal{m: cat, stop: 5}
	for _, ordered := range []bool{true, false} {
		cat.Tamable.OrderedToSit = ordered
		if got := g.CanStart(view); got == ordered {
			t.Errorf("ordered to sit %v: follow starts %v", ordered, got)
		}
	}
}

// TestAWildCatIsNotTemptedOnceTame: the fish in a player's hand calls a wild
// cat and not a tame one.
// vanilla: Cat.CatTemptGoal.canUse
func TestAWildCatIsNotTemptedOnceTame(t *testing.T) {
	cat := NewMob(TypeCat, 1, 0.5, 64, 0.5, 1)
	g := &temptGoal{m: cat, untamed: true}
	view := &tickView{w: newTestWorld(), players: []Player{{X: 3.5, Y: 64, Z: 0.5, Huntable: true,
		HeldItem: stackOf(t, "minecraft:cod")}}}
	if !g.CanStart(view) {
		t.Fatal("a wild cat ignored a player holding a cod")
	}
	cat.Tamable.Tame = true
	if g.CanStart(view) {
		t.Error("a tame cat was tempted by a cod")
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

// TestACatSpeaksItsVariantsVoice: a wild cat speaks the stray ambient, a tame
// one the plain ambient, the royal variant its own set and a kitten the baby
// set.
// vanilla: Cat.getAmbientSound, Cat.getSoundSet
func TestACatSpeaksItsVariantsVoice(t *testing.T) {
	cat := NewMob(TypeCat, 1, 0, 64, 0, 1)
	if got := cat.Sounds().Ambient.Name(); got != "minecraft:entity.cat.stray_ambient" {
		t.Errorf("wild ambient %q", got)
	}
	cat.Tamable.Tame = true
	if got := cat.Sounds().Ambient.Name(); got != "minecraft:entity.cat.ambient" {
		t.Errorf("tame ambient %q", got)
	}
	cat.SoundVariant = "minecraft:royal"
	if got := cat.Sounds().Hurt.Name(); got != "minecraft:entity.cat_royal.hurt" {
		t.Errorf("royal hurt %q", got)
	}
	cat.SetBaby(true)
	if got := cat.Sounds().Death.Name(); got != "minecraft:entity.baby_cat.death" {
		t.Errorf("baby death %q", got)
	}
}

// TestOnlyAWildCatPastTwoMinutesDespawns: the despawn roll applies to a cat
// older than 2400 ticks that is not tame.
// vanilla: Cat.removeWhenFarAway
func TestOnlyAWildCatPastTwoMinutesDespawns(t *testing.T) {
	cat := NewMob(TypeCat, 1, 0, 64, 0, 1)
	cat.Age = 2400
	if cat.removeWhenFarAway() {
		t.Error("a cat of exactly 2400 ticks is despawnable")
	}
	cat.Age = 2401
	if !cat.removeWhenFarAway() {
		t.Error("a wild cat past 2400 ticks is not despawnable")
	}
	cat.Tamable.Tame = true
	if cat.removeWhenFarAway() {
		t.Error("a tame cat is despawnable")
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

// TestAllBlackJoinsTheDrawOnlyUnderABrightMoon: with the dice fixed by seed 4,
// which draws all_black from eleven variants and white from ten, the pick
// changes at a moon brightness of 0.9.
// vanilla: CatVariants.bootstrap, MoonBrightnessCheck
func TestAllBlackJoinsTheDrawOnlyUnderABrightMoon(t *testing.T) {
	for _, tc := range []struct {
		moon float32
		want string
	}{
		{0.75, "minecraft:white"},
		{0.89, "minecraft:white"},
		{0.9, "minecraft:all_black"},
		{1, "minecraft:all_black"},
	} {
		m := NewMob(TypeCat, 1, 0.5, 64, 0.5, 4)
		m.finalizeCat(GoDice{m.Rand}, tc.moon, false)
		if m.Variant != tc.want {
			t.Errorf("moon %v: variant %q, want %q", tc.moon, m.Variant, tc.want)
		}
	}
}

// TestAFleeSearchStopsThreeBlocksAboveAndBelow: a wild cat notices a player
// two blocks up and not ten, though both are inside the sixteen-block sphere.
// vanilla: AvoidEntityGoal.canUse
func TestAFleeSearchStopsThreeBlocksAboveAndBelow(t *testing.T) {
	cat := NewMob(TypeCat, 1, 0.5, 64, 0.5, 1)
	g := newFelineAvoidGoal(cat, catPriorityTempt, func() bool { return true })
	for _, tc := range []struct {
		dy   float64
		want bool
	}{{2, true}, {-2, true}, {10, false}, {-10, false}} {
		view := &tickView{w: newTestWorld(), players: []Player{{X: 0.5, Y: 64 + tc.dy, Z: 4.5, Huntable: true}}}
		if _, _, _, _, _, ok := g.nearestThreat(view); ok != tc.want {
			t.Errorf("player %v blocks up: threat found %v, want %v", tc.dy, ok, tc.want)
		}
	}
}

// TestACatCrouchesAtTheTemptPaceAndSprintsAtTheFleePace: the stance follows
// the pace of the walk, and a cat that is not walking stands.
// vanilla: Cat.customServerAiStep
func TestACatCrouchesAtTheTemptPaceAndSprintsAtTheFleePace(t *testing.T) {
	for _, tc := range []struct {
		pace           float64
		walking        bool
		crouch, sprint bool
	}{
		{felineTemptPace, true, true, false},
		{felineSprintPace, true, false, true},
		{felineWalkPace, true, false, false},
		{felineTemptPace, false, false, false},
		{felineSprintPace, false, false, false},
	} {
		for _, typ := range []MobType{TypeCat, TypeOcelot} {
			m := NewMob(typ, 1, 0.5, 64, 0.5, 1)
			m.nav.speedFactor = tc.pace
			if tc.walking {
				m.nav.path = []Waypoint{{X: 3, Y: 64, Z: 0}}
			}
			m.felineStance()
			if m.Crouching != tc.crouch || m.Sprinting != tc.sprint {
				t.Errorf("type %v pace %v walking %v: crouch %v sprint %v, want %v %v",
					typ, tc.pace, tc.walking, m.Crouching, m.Sprinting, tc.crouch, tc.sprint)
			}
		}
	}
}
