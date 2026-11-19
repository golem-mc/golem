package entity

import (
	"testing"

	"github.com/google/uuid"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/registry"
)

func saddleStack() protocol.ItemStack {
	return protocol.ItemStack{ItemID: int32(registry.ItemSaddle), Count: 1}
}

// TestHorseFinalizeRollsVanillasRanges: the rolled health, pace and jump stay
// inside the bounds the generators allow, and the colour and markings inside
// their enums.
func TestHorseFinalizeRollsVanillasRanges(t *testing.T) {
	const speedLow, speedHigh = 0.45 * 0.25, 1.35 * 0.25
	for seed := range int64(300) {
		m := NewMob(TypeHorse, 1, 0, 64, 0, seed)
		m.FinalizeSpawn(GoDice{m.Rand}, testBiome("minecraft:plains"), nil, rules.LocalDifficulty{})
		if m.MaxHealth < 15 || m.MaxHealth > 15+7+8 || m.Health != m.MaxHealth {
			t.Fatalf("seed %d: health %v/%v, want 15 to 30 and full", seed, m.Health, m.MaxHealth)
		}
		if s := m.Horse.Speed; s < speedLow-1e-6 || s > speedHigh+1e-6 {
			t.Fatalf("seed %d: speed %v outside [%v, %v]", seed, s, speedLow, speedHigh)
		}
		if j := m.Horse.Jump; j < 0.4-1e-6 || j > 1.0+1e-6 {
			t.Fatalf("seed %d: jump %v outside [0.4, 1.0]", seed, j)
		}
		if colour, marking := m.Horse.Variant&0xFF, m.Horse.Variant>>8; colour >= horseColours || marking >= horseMarkings {
			t.Fatalf("seed %d: variant %#x has colour %d, markings %d", seed, m.Horse.Variant, colour, marking)
		}
	}
}

// riddenWildHorse is a wild horse with a player on it and temper given.
func riddenWildHorse(seed int64, temper int32) (*Mob, uuid.UUID) {
	m := NewMob(TypeHorse, 1, 0.5, 64, 0.5, seed)
	rider := uuid.New()
	m.Board(rider)
	m.Horse.RiderID = 7
	m.Horse.Temper = temper
	return m, rider
}

// runTicks ticks the goal until an event comes, up to a bound.
func runTicks(g *runAroundGoal) (MobEvent, bool) {
	for range 20000 {
		view := &tickView{w: newTestWorld()}
		g.Tick(view)
		if len(view.events) > 0 {
			return view.events[0], true
		}
	}
	return MobEvent{}, false
}

// TestWildHorseThrowsAnUnluckyRider: with no temper built up a rider is
// always thrown, the temper grows by five and the horse rears.
func TestWildHorseThrowsAnUnluckyRider(t *testing.T) {
	m, _ := riddenWildHorse(3, 0)
	ev, ok := runTicks(&runAroundGoal{m: m})
	if !ok || ev.Kind != MobHorseThrewRider {
		t.Fatalf("event %v (found %v), want the rider thrown", ev.Kind, ok)
	}
	if m.Horse.Rider != uuid.Nil || m.Horse.Temper != horseTemperStep || m.Horse.Tame || m.Horse.Standing == 0 {
		t.Errorf("after the throw: rider %v temper %d tame %v standing %d, want no rider, temper 5, wild and rearing",
			m.Horse.Rider, m.Horse.Temper, m.Horse.Tame, m.Horse.Standing)
	}
}

func TestRiddenHorseTakesFallDamageOnLanding(t *testing.T) {
	t.Parallel()
	w := newTestWorld()
	m := NewMob(TypeHorse, 1, 0, 54, 0, 1)
	m.Health, m.MaxHealth = 20, 20
	// The horse's safe distance is 6 and its multiplier 0.5: a 10-block
	// fall costs floor((10-6)*0.5), where the generic numbers give 7.
	if _, _, hurt, _, _ := m.RideLanding(w, 0, 64, 0, false); hurt {
		t.Fatal("a fall in the air hurt")
	}
	landed, damage, hurt, _, _ := m.RideLanding(w, 0, 54, 0, true)
	if !hurt || damage != 2 || landed != 10 || m.Health != 18 {
		t.Errorf("landing = %v fall, %v damage, hurt %v, health %v, want 10, 2, true, 18", landed, damage, hurt, m.Health)
	}
	m.Y = 52
	if _, _, hurt, _, _ := m.RideLanding(w, 0, 54, 0, true); hurt {
		t.Error("a 2-block drop hurt")
	}
	m.Y = 46
	if _, damage, _, _, _ := m.RideLanding(w, 0, 52, 0, true); damage != 0 {
		t.Errorf("a 6-block drop dealt %v, want it inside the safe distance", damage)
	}
}

// openBoxes is a world with nothing to collide with.
type openBoxes struct{}

func (openBoxes) BoxCollides(_, _, _, _, _, _ float64) bool { return false }

func (openBoxes) CollidesAnew(_, _ [6]float64) bool { return false }

func chestStack() protocol.ItemStack {
	id, _ := registry.ItemID("minecraft:chest")
	return protocol.ItemStack{ItemID: id, Count: 1}
}

func TestChestOnATameDonkeyDropsOnDeath(t *testing.T) {
	m := NewMob(TypeDonkey, 1, 0, 64, 0, 1)
	m.Horse.Tame = true
	click, _ := m.UseItem(chestStack(), false)
	if click != HorseClickChest || !m.Horse.Chest {
		t.Fatalf("click %v chest %v, want the chest fitted", click, m.Horse.Chest)
	}
	taken := m.TakeChest()
	if len(taken) != 1 || taken[0].ItemID != chestStack().ItemID || m.Horse.Chest {
		t.Errorf("took %v chest %v, want the chest taken once", taken, m.Horse.Chest)
	}
	if again := m.TakeChest(); len(again) != 0 {
		t.Errorf("a second take gave %v", again)
	}
}

func horseItem(t *testing.T, name string) int32 {
	t.Helper()
	id, ok := registry.ItemID(name)
	if !ok {
		t.Fatalf("no item %s", name)
	}
	return id
}

// TestHorseFoodHealsGrowsAndTempers: wheat heals a hurt horse two points and
// adds three temper; a full tame adult it does nothing for keeps the wheat,
// while a wild one still eats it for temper; a foal grows twenty seconds.
// vanilla: AbstractHorse.handleEating
func TestHorseFoodHealsGrowsAndTempers(t *testing.T) {
	w := newTestWorld()
	wheat := horseItem(t, "minecraft:wheat")
	m := NewMob(TypeHorse, 1, 0, 64, 0, 1)
	m.Health = m.MaxHealth - 5
	if ate, loved := m.FeedHorse(w, wheat); !ate || loved {
		t.Fatalf("a hurt wild horse: ate %v loved %v, want eaten without love", ate, loved)
	}
	if m.Health != m.MaxHealth-3 || m.Horse.Temper != 3 {
		t.Errorf("health %v temper %d, want %v and 3", m.Health, m.Horse.Temper, m.MaxHealth-3)
	}
	m.Health = m.MaxHealth
	if ate, _ := m.FeedHorse(w, wheat); !ate || m.Horse.Temper != 6 {
		t.Errorf("a full wild horse: ate %v temper %d, want eaten for temper 6", ate, m.Horse.Temper)
	}
	m.Horse.Tame = true
	if ate, _ := m.FeedHorse(w, wheat); ate || m.Horse.Temper != 6 {
		t.Errorf("a full tame adult: ate %v temper %d, want the wheat kept", ate, m.Horse.Temper)
	}
	m.Horse.Temper = HorseMaxTemper
	m.Horse.Tame = false
	if ate, _ := m.FeedHorse(w, wheat); ate {
		t.Error("a full wild horse at the temper cap ate the wheat, want it kept")
	}

	foal := NewMob(TypeHorse, 2, 0, 64, 0, 2)
	foal.SetBaby(true)
	before := foal.BabyAge
	foal.FeedHorse(w, wheat)
	if foal.BabyAge != before+400 {
		t.Errorf("the foal's age went %d to %d, want 400 ticks on", before, foal.BabyAge)
	}
	foal.BabyAge = -100
	foal.FeedHorse(w, horseItem(t, "minecraft:hay_block"))
	if foal.BabyAge != 0 {
		t.Errorf("hay left the foal at age %d, want grown", foal.BabyAge)
	}
}

// TestHorsesMateAcrossKindsIntoAMule: a horse courts a donkey, a mule courts
// nothing, and a wild or hurt one is no partner.
// vanilla: Horse.canMate, Donkey.canMate, AbstractHorse.canParent
func TestHorsesMateAcrossKindsIntoAMule(t *testing.T) {
	tame := func(id int32, x float64, kind MobType) *Mob {
		m := lover(id, x, kind)
		m.Horse.Tame = true
		return m
	}
	horse, donkey := tame(1, 0.5, TypeHorse), tame(2, 2.5, TypeDonkey)
	if got := (&breedGoal{m: horse}).freePartner(breedTickView(horse, donkey)); got != donkey {
		t.Fatal("a horse found no partner in a loving tame donkey")
	}
	if horse.OffspringType(donkey) != TypeMule || donkey.OffspringType(horse) != TypeMule {
		t.Error("a horse and a donkey bear no mule")
	}
	if donkey.OffspringType(tame(3, 0, TypeDonkey)) != TypeDonkey {
		t.Error("two donkeys bear no donkey")
	}
	mule := tame(4, 1.5, TypeMule)
	if got := (&breedGoal{m: horse}).freePartner(breedTickView(horse, mule)); got != nil {
		t.Error("a horse courted a mule")
	}
	donkey.Health = donkey.MaxHealth - 1
	if got := (&breedGoal{m: horse}).freePartner(breedTickView(horse, donkey)); got != nil {
		t.Error("a horse courted a hurt donkey")
	}
	donkey.Health, donkey.Horse.Tame = donkey.MaxHealth, false
	if got := (&breedGoal{m: horse}).freePartner(breedTickView(horse, donkey)); got != nil {
		t.Error("a horse courted a wild donkey")
	}
}

// TestAFoalInheritsWithinTheRanges: a bred foal's health, jump and speed stay
// inside the generators' ranges, and a horse foal of two like parents mostly
// wears their colour.
// vanilla: AbstractHorse.createOffspringAttribute, Horse.getBreedOffspring
func TestAFoalInheritsWithinTheRanges(t *testing.T) {
	same := 0
	for seed := range int64(300) {
		mother := NewMob(TypeHorse, 1, 0, 64, 0, seed)
		father := NewMob(TypeHorse, 2, 0, 64, 0, seed+1000)
		mother.finalizeHorse(GoDice{mother.Rand}, nil)
		father.finalizeHorse(GoDice{father.Rand}, nil)
		father.Horse.Variant = mother.Horse.Variant
		foal := NewMob(TypeHorse, 3, 0, 64, 0, seed)
		mother.BreedInto(foal, father)
		if foal.MaxHealth < horseHealthMin || foal.MaxHealth > horseHealthMax || foal.Health != foal.MaxHealth {
			t.Fatalf("seed %d: health %v of %v", seed, foal.Health, foal.MaxHealth)
		}
		if foal.Horse.Jump < horseJumpMin || foal.Horse.Jump > horseJumpMax {
			t.Fatalf("seed %d: jump %v", seed, foal.Horse.Jump)
		}
		if foal.Horse.Speed < horseSpeedMin || foal.Horse.Speed > horseSpeedMax {
			t.Fatalf("seed %d: speed %v", seed, foal.Horse.Speed)
		}
		if foal.Horse.Variant == mother.Horse.Variant {
			same++
		}
	}
	// 8 in 9 colours and 4 in 5 markings come from the parents, about 72%.
	if same < 180 {
		t.Errorf("%d of 300 foals wore their parents' look, want about 216", same)
	}
}

// TestWearsFollowsTheKindsTagsAndTheSaddlesConditions pins the mount screen's
// slot rules: the saddle tag, the horse-armour tag, and a saddle only on a
// living tame adult.
func TestWearsFollowsTheKindsTagsAndTheSaddlesConditions(t *testing.T) {
	saddle := stackOf(t, "minecraft:saddle")
	armour := stackOf(t, "minecraft:iron_horse_armor")
	tame := func(typ MobType) *Mob {
		m := NewMob(typ, 1, 0, 64, 0, 1)
		m.Horse.Tame = true
		return m
	}
	for _, c := range []struct {
		typ    MobType
		saddle bool
		armour bool
	}{
		{TypeHorse, true, true},
		{TypeZombieHorse, true, true},
		{TypeSkeletonHorse, true, false},
		{TypeDonkey, true, false},
		{TypeMule, true, false},
	} {
		m := tame(c.typ)
		if got := m.Wears("saddle", saddle); got != c.saddle {
			t.Errorf("%s: saddle fits = %v, want %v", c.typ, got, c.saddle)
		}
		if got := m.Wears("body", armour); got != c.armour {
			t.Errorf("%s: horse armour fits = %v, want %v", c.typ, got, c.armour)
		}
		if m.Wears("saddle", armour) || m.Wears("body", saddle) {
			t.Errorf("%s: a piece fits the wrong slot", c.typ)
		}
	}

	wild := NewMob(TypeHorse, 2, 0, 64, 0, 1)
	if wild.Wears("saddle", saddle) {
		t.Error("an untame horse takes a saddle")
	}
	if !wild.Wears("body", armour) {
		t.Error("an untame horse refuses armour")
	}
	foal := tame(TypeHorse)
	foal.SetBaby(true)
	if foal.Wears("saddle", saddle) {
		t.Error("a foal takes a saddle")
	}
	dead := tame(TypeHorse)
	dead.Health = 0
	if dead.Wears("saddle", saddle) {
		t.Error("a dead horse takes a saddle")
	}
}
