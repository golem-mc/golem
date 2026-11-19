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

// TestHorsePackSharesOneColour: every member of a pack takes the colour the
// first rolled.
func TestHorsePackSharesOneColour(t *testing.T) {
	group := NewSpawnGroup(GoDice{NewMob(TypeCow, 9, 0, 64, 0, 9).Rand})
	first := NewMob(TypeHorse, 1, 0, 64, 0, 1)
	first.FinalizeSpawn(GoDice{first.Rand}, testBiome("minecraft:plains"), group, rules.LocalDifficulty{})
	for seed := int64(2); seed < 40; seed++ {
		m := NewMob(TypeHorse, int32(seed), 0, 64, 0, seed)
		m.FinalizeSpawn(GoDice{m.Rand}, testBiome("minecraft:plains"), group, rules.LocalDifficulty{})
		if m.Horse.Variant&0xFF != first.Horse.Variant&0xFF {
			t.Fatalf("pack member %d wears colour %d, want the first's %d", seed, m.Horse.Variant&0xFF, first.Horse.Variant&0xFF)
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

// TestWildHorseYieldsToAFullTemper: at the maximum temper the roll always
// lands, the horse is tamed to the rider and the rider stays on.
func TestWildHorseYieldsToAFullTemper(t *testing.T) {
	m, rider := riddenWildHorse(4, HorseMaxTemper)
	ev, ok := runTicks(&runAroundGoal{m: m})
	if !ok || ev.Kind != MobHorseTamed {
		t.Fatalf("event %v (found %v), want the horse tamed", ev.Kind, ok)
	}
	if !m.Horse.Tame || m.Horse.Owner != rider || m.Horse.Rider != rider {
		t.Errorf("tame %v owner %v rider %v, want tamed to and still ridden by %v", m.Horse.Tame, m.Horse.Owner, m.Horse.Rider, rider)
	}
}

// TestTameHorseIsNotBuckedOrRunAround: the goal only runs on a wild horse.
func TestTameHorseIsNotBuckedOrRunAround(t *testing.T) {
	m, _ := riddenWildHorse(5, 0)
	m.Horse.Tame = true
	g := &runAroundGoal{m: m}
	if g.CanStart(&tickView{w: newTestWorld()}) {
		t.Error("a tame horse started to run around")
	}
	if _, ok := runTicks(g); ok {
		t.Error("a tame horse threw or was tamed again")
	}
}

// TestSaddleNeedsATameGrownHorse pins the saddle slot's conditions.
func TestSaddleNeedsATameGrownHorse(t *testing.T) {
	m := NewMob(TypeHorse, 1, 0, 64, 0, 1)
	if m.CanTakeSaddle() {
		t.Error("a wild horse takes a saddle")
	}
	m.Horse.Tame = true
	if !m.CanTakeSaddle() {
		t.Error("a tame horse refuses a saddle")
	}
	m.Horse.Saddle = saddleStack()
	if m.CanTakeSaddle() {
		t.Error("a saddled horse takes a second saddle")
	}
	m.Horse.Saddle = protocol.ItemStack{}
	m.SetBaby(true)
	if m.CanTakeSaddle() {
		t.Error("a foal takes a saddle")
	}
}

// TestOnlyASaddledRiddenHorseIsSteered: the client moves the horse only with
// a saddle and a rider.
func TestOnlyASaddledRiddenHorseIsSteered(t *testing.T) {
	m := NewMob(TypeHorse, 1, 0, 64, 0, 1)
	m.Board(uuid.New())
	if m.Horse.Controlled() {
		t.Error("an unsaddled horse is steered")
	}
	m.Horse.Saddle = saddleStack()
	if !m.Horse.Controlled() {
		t.Error("a saddled, ridden horse is not steered")
	}
	m.Unboard()
	if m.Horse.Controlled() {
		t.Error("a horse nobody rides is steered")
	}
}

// TestRidingJumpNeedsASaddleAndPower: the jump rears the horse only when it
// is saddled and the power is above nought.
func TestRidingJumpNeedsASaddleAndPower(t *testing.T) {
	m := NewMob(TypeHorse, 1, 0, 64, 0, 1)
	if m.RidingJump(50) {
		t.Error("an unsaddled horse jumped")
	}
	m.Horse.Saddle = saddleStack()
	if m.RidingJump(0) {
		t.Error("a jump of no power counted")
	}
	if !m.RidingJump(50) || m.Horse.Standing != horseStandTicks {
		t.Errorf("saddled jump: standing %d, want %d", m.Horse.Standing, horseStandTicks)
	}
}

// TestRiderIsListedAmongThePassengers: the Set Passengers list names the
// player riding.
func TestRiderIsListedAmongThePassengers(t *testing.T) {
	m := NewMob(TypeHorse, 1, 0, 64, 0, 1)
	m.Board(uuid.New())
	m.Horse.RiderID = 42
	if ids := m.PassengerIDs(); len(ids) != 1 || ids[0] != 42 {
		t.Errorf("passengers %v, want [42]", ids)
	}
	m.Unboard()
	if ids := m.PassengerIDs(); len(ids) != 0 {
		t.Errorf("passengers %v after the dismount, want none", ids)
	}
}

func TestSaddledHorseAlwaysDropsItsSaddle(t *testing.T) {
	t.Parallel()
	saddle, ok := registry.ItemID("minecraft:saddle")
	if !ok {
		t.Fatal("no saddle item")
	}
	m := NewMob(TypeHorse, 1, 0, 64, 0, 1)
	m.Horse.Saddle = protocol.ItemStack{ItemID: saddle, Count: 1}
	m.Dead = true
	for range 20 {
		found := false
		for _, d := range m.RollDrops(rules.LootKiller{}, nil) {
			found = found || d.ItemID == saddle
		}
		if !found {
			t.Fatal("a saddled horse's death dropped no saddle")
		}
	}
}

func TestVehicleMoveIsMeasuredFromTheTickStart(t *testing.T) {
	t.Parallel()
	m := NewMob(TypeHorse, 1, 0, 64, 0, 1)
	w := openBoxes{}
	if m.RejectsMove(w, 0, 64, 0, 9, 64, 0) {
		t.Error("a 9-block move was refused")
	}
	if !m.RejectsMove(w, 0, 64, 0, 18, 64, 0) {
		t.Error("an 18-block move from the tick's start was accepted")
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

func TestRiddenHorseFallEndsInWater(t *testing.T) {
	t.Parallel()
	w := newTestWorld()
	m := NewMob(TypeHorse, 1, 0.5, 64, 0.5, 1)
	m.Health, m.MaxHealth = 20, 20
	m.Y = 44
	m.RideLanding(w, 0.5, 64, 0.5, false)
	w.set(0, 43, 0, defaultState(t, "minecraft:water"))
	m.Y = 43.5
	m.RideLanding(w, 0.5, 44, 0.5, false)
	if m.FallDistance != 0 {
		t.Errorf("fall = %v after the feet entered water, want it written off", m.FallDistance)
	}
	if _, _, hurt, _, _ := m.RideLanding(w, 0.5, 43.5, 0.5, true); hurt {
		t.Error("a fall that went through water hurt on landing")
	}
}

// openBoxes is a world with nothing to collide with.
type openBoxes struct{}

func (openBoxes) BoxCollides(_, _, _, _, _, _ float64) bool { return false }

func (openBoxes) CollidesAnew(_, _ [6]float64) bool { return false }

func TestRearingHorseTakesNoWalkingInput(t *testing.T) {
	t.Parallel()
	w := newTestWorld()
	for x := int32(-8); x < 24; x++ {
		for z := int32(-8); z < 24; z++ {
			w.set(x, 63, z, 1)
		}
	}
	walk := func(rear bool) float64 {
		m := NewMob(TypeHorse, 1, 0.5, 64, 0.5, 1)
		m.NoAI = false
		m.seekTo(w, 12.5, 64, 0.5, 1, navReach)
		if rear {
			m.MakeMad()
		}
		for range 10 {
			m.Tick(w, nil)
		}
		return m.X - 0.5
	}
	if moved := walk(false); moved < 0.1 {
		t.Fatalf("control horse moved %v, want it walking", moved)
	}
	if moved := walk(true); moved > 0.01 {
		t.Errorf("a rearing horse walked %v blocks", moved)
	}
}

func TestDonkeyAndMuleRollOnlyTheirHealth(t *testing.T) {
	for _, kind := range []MobType{TypeDonkey, TypeMule} {
		for seed := int64(1); seed < 50; seed++ {
			m := NewMob(kind, 1, 0, 64, 0, seed)
			m.FinalizeSpawn(GoDice{m.Rand}, testBiome("minecraft:plains"), nil, rules.LocalDifficulty{})
			if m.MaxHealth < 15 || m.MaxHealth > 15+7+8 || m.Health != m.MaxHealth {
				t.Fatalf("%s health %v/%v, want 15 to 30 and full", kind, m.Health, m.MaxHealth)
			}
			if m.Horse.Speed != float64(float32(0.175)) || m.Horse.Jump != 0.5 {
				t.Fatalf("%s speed %v jump %v, want 0.175 and 0.5", kind, m.Horse.Speed, m.Horse.Jump)
			}
		}
	}
}

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

// TestATameFoalTakesAChestAndDropsItWithNoLoot: a tame baby donkey is fitted
// like a grown one, and a baby rolls no loot yet its chest is still taken.
// vanilla: AbstractChestedHorse.mobInteract, AbstractChestedHorse.dropEquipment
func TestATameFoalTakesAChestAndDropsItWithNoLoot(t *testing.T) {
	m := NewMob(TypeDonkey, 1, 0, 64, 0, 1)
	m.SetBaby(true)
	if click, _ := m.UseItem(chestStack(), false); click != HorseClickRear {
		t.Fatalf("a wild foal's click was %v, want it to rear", click)
	}
	m.Horse.Tame = true
	if click, _ := m.UseItem(chestStack(), false); click != HorseClickChest {
		t.Fatalf("a tame foal's click was %v, want the chest", click)
	}
	if drops := m.RollDrops(rules.LootKiller{}, nil); len(drops) != 0 {
		t.Errorf("a baby rolled %v", drops)
	}
	if len(m.TakeChest()) != 1 {
		t.Error("the foal's chest was not taken")
	}
}

// TestDonkeyAndMuleDropLeather: the generated loot tables give leather.
func TestDonkeyAndMuleDropLeather(t *testing.T) {
	leather, _ := registry.ItemID("minecraft:leather")
	for _, kind := range []MobType{TypeHorse, TypeDonkey, TypeMule} {
		got := false
		for seed := int64(1); seed < 60 && !got; seed++ {
			m := NewMob(kind, 1, 0, 64, 0, seed)
			for _, d := range m.RollDrops(rules.LootKiller{}, nil) {
				got = got || d.ItemID == leather
			}
		}
		if !got {
			t.Errorf("%s never dropped leather", kind)
		}
	}
}

// TestEquinePackBabiesAreOneInFive: later members of a donkey pack are babies
// 20% of the time, the first never.
// vanilla: AbstractHorse.finalizeSpawn
func TestEquinePackBabiesAreOneInFive(t *testing.T) {
	babies := 0
	const packs, size = 400, 10
	for p := range int64(packs) {
		group := NewSpawnGroup(GoDice{NewMob(TypeCow, 9, 0, 64, 0, p).Rand})
		for i := range size {
			m := NewMob(TypeDonkey, 1, 0, 64, 0, p*size+int64(i))
			m.FinalizeSpawn(GoDice{m.Rand}, testBiome("minecraft:plains"), group, rules.LocalDifficulty{})
			if m.IsBaby() {
				if i == 0 {
					t.Fatal("the first of a pack was a baby")
				}
				babies++
			}
		}
	}
	got := float64(babies) / (packs * (size - 1))
	if got < 0.17 || got > 0.23 {
		t.Errorf("baby share %.3f, want about 0.2", got)
	}
}

// TestLlamaPackBabiesAreOneInTwenty: later members of a llama pack are babies
// 5% of the time, not the horse's 20%.
// vanilla: Llama.finalizeSpawn, LlamaGroupData
func TestLlamaPackBabiesAreOneInTwenty(t *testing.T) {
	babies := 0
	const packs, size = 600, 6
	for p := range int64(packs) {
		group := NewSpawnGroup(GoDice{NewMob(TypeCow, 9, 0, 64, 0, p).Rand})
		for i := range size {
			m := NewMob(TypeLlama, 1, 0, 64, 0, p*size+int64(i))
			m.FinalizeSpawn(GoDice{m.Rand}, testBiome("minecraft:plains"), group, rules.LocalDifficulty{})
			if m.IsBaby() {
				babies++
			}
		}
	}
	got := float64(babies) / (packs * (size - 1))
	if got < 0.03 || got > 0.08 {
		t.Errorf("baby share %.3f, want about 0.05", got)
	}
}

// TestShearsTakeTheSaddleOnlyOffAnUnriddenHorse pins ShearEquipment's conditions.
func TestShearsTakeTheSaddleOnlyOffAnUnriddenHorse(t *testing.T) {
	m := NewMob(TypeHorse, 1, 0, 64, 0, 1)
	if _, ok := m.ShearEquipment(uuid.Nil, false); ok {
		t.Error("sheared a saddle off a bare horse")
	}
	m.Horse.Saddle = saddleStack()
	m.Horse.Rider = uuid.New()
	if _, ok := m.ShearEquipment(uuid.Nil, false); ok {
		t.Error("sheared the saddle off a ridden horse")
	}
	m.Horse.Rider = uuid.Nil
	got, ok := m.ShearEquipment(uuid.Nil, false)
	if !ok || got.Stack.ItemID != saddleStack().ItemID || !m.Horse.Saddle.Empty() {
		t.Errorf("shear = %v, %v; saddle left %v", got, ok, m.Horse.Saddle)
	}
}

// TestHorseHealsAPointNowAndThen: a hurt horse regains health slowly, up to
// its maximum, and a dead one does not.
func TestHorseHealsAPointNowAndThen(t *testing.T) {
	m := NewMob(TypeHorse, 1, 0, 64, 0, 1)
	m.FinalizeSpawn(GoDice{m.Rand}, testBiome("minecraft:plains"), nil, rules.LocalDifficulty{})
	m.Health = m.MaxHealth - 3
	w := newTestWorld()
	for range 20 * horseHealOdds {
		m.horseStep(w)
	}
	if m.Health != m.MaxHealth {
		t.Errorf("health %v, want the full %v after 20 expected heals", m.Health, m.MaxHealth)
	}
	m.Health = 0
	for range 20 * horseHealOdds {
		m.horseStep(w)
	}
	if m.Health != 0 {
		t.Errorf("a dead horse healed to %v", m.Health)
	}
}

func TestHurtHorseRearsOnOneHitInThree(t *testing.T) {
	rears := 0
	for i := range 300 {
		m := NewMob(TypeHorse, 1, 0.5, 64, 0.5, int64(i))
		m.noteHorseHurt()
		if m.Horse.Standing > 0 {
			rears++
		}
	}
	if rears < 60 || rears > 140 {
		t.Errorf("%d of 300 hurt horses reared, want about 100", rears)
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

// TestAFoalFedToAdulthoodMovesClearOfAWall: hay that grows a foal up
// refreshes its box, so the grown horse steps off the wall beside it.
// vanilla: AgeableMob.ageUp, AgeableMob.onSyncedDataUpdated
func TestAFoalFedToAdulthoodMovesClearOfAWall(t *testing.T) {
	w := newTestWorld()
	stone := defaultState(t, "minecraft:stone")
	w.set(1, 64, 0, stone)
	w.set(1, 65, 0, stone)
	foal := NewMob(TypeHorse, 1, 0.5, 64, 0.5, 1)
	foal.SetBaby(true)
	foal.BabyAge = -100
	foal.FeedHorse(w, horseItem(t, "minecraft:hay_block"))
	if foal.IsBaby() {
		t.Fatal("hay left the foal a baby")
	}
	if east := foal.X + foal.width()/2; east > 1 {
		t.Errorf("the grown horse reaches x=%v, want it clear of the wall at x=1", east)
	}
}

// TestAFoalIgnoresAGoldenDandelion: a golden dandelion sends a foal to the
// age lock, never to rearing.
// vanilla: Horse.mobInteract, AbstractChestedHorse.mobInteract
func TestAFoalIgnoresAGoldenDandelion(t *testing.T) {
	dandelion := protocol.ItemStack{ItemID: horseItem(t, "minecraft:golden_dandelion"), Count: 1}
	for _, kind := range []MobType{TypeHorse, TypeDonkey} {
		foal := NewMob(kind, 1, 0, 64, 0, 1)
		foal.SetBaby(true)
		if click, mad := foal.UseItem(dandelion, false); click != HorseClickNone || mad {
			t.Errorf("kind %v: click %v mad %v, want nothing", kind, click, mad)
		}
	}
}

// TestGoldenFoodSendsOnlyATameAdultIntoLove: a golden carrot loves a tame
// adult with no cooldown, and only once.
// vanilla: AbstractHorse.handleEating
func TestGoldenFoodSendsOnlyATameAdultIntoLove(t *testing.T) {
	w := newTestWorld()
	carrot := horseItem(t, "minecraft:golden_carrot")
	m := NewMob(TypeDonkey, 1, 0, 64, 0, 1)
	m.Horse.Temper = HorseMaxTemper
	if _, loved := m.FeedHorse(w, carrot); loved {
		t.Error("a wild donkey fell in love")
	}
	m.Horse.Tame = true
	m.BabyAge = breedCooldownTicks
	if _, loved := m.FeedHorse(w, carrot); loved {
		t.Error("a donkey in its breeding cooldown fell in love")
	}
	m.BabyAge = 0
	if ate, loved := m.FeedHorse(w, carrot); !ate || !loved || m.InLove != loveTicks {
		t.Errorf("a tame adult: ate %v loved %v love %d, want love begun", ate, loved, m.InLove)
	}
	if ate, loved := m.FeedHorse(w, carrot); ate || loved {
		t.Errorf("a donkey already in love: ate %v loved %v, want the carrot kept", ate, loved)
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

// TestOffspringAttributeReflectsAtTheEnds: two parents at the top of the
// range draw around it, and a draw past the end comes back inside.
func TestOffspringAttributeReflectsAtTheEnds(t *testing.T) {
	m := NewMob(TypeHorse, 1, 0, 64, 0, 7)
	for range 500 {
		if v := offspringAttribute(40, 40, horseHealthMin, horseHealthMax, m); v > horseHealthMax || v < horseHealthMax-0.15*15 {
			t.Fatalf("health %v from two capped parents, want within 2.25 below 30", v)
		}
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

// TestShearsLeaveBindingCursedEquipmentUnlessCreative pins the curse check.
// vanilla: Mob.attemptToShearEquipment
func TestShearsLeaveBindingCursedEquipmentUnlessCreative(t *testing.T) {
	curse, ok := registry.EnchantmentID("minecraft:binding_curse")
	if !ok {
		t.Fatal("no binding_curse enchantment")
	}
	m := NewMob(TypeHorse, 1, 0, 64, 0, 1)
	m.Horse.Saddle = saddleStack()
	m.Horse.Saddle.Enchantments = map[int32]int32{curse: 1}
	if _, ok := m.ShearEquipment(uuid.Nil, false); ok || m.Horse.Saddle.Empty() {
		t.Error("survival shears took a binding-cursed saddle")
	}
	if _, ok := m.ShearEquipment(uuid.Nil, true); !ok || !m.Horse.Saddle.Empty() {
		t.Error("creative shears left a binding-cursed saddle")
	}
}

// TestAnIdleHorseRearsWithinItsAmbientInterval: the counter starts at minus
// the ambient interval, so the first stand comes after it and then only 1 in
// 10 rolls rears, with the ambient call.
func TestAnIdleHorseRearsWithinItsAmbientInterval(t *testing.T) {
	m := NewMob(TypeHorse, 1, 0.5, 64, 0.5, 7)
	g := newRandomStandGoal(m)
	if g.nextStand != -400 {
		t.Fatalf("counter starts at %d, want -400", g.nextStand)
	}
	for tick := 1; tick <= 20000; tick++ {
		view := &tickView{w: newTestWorld()}
		if !g.CanStart(view) {
			continue
		}
		if tick <= 400 {
			t.Fatalf("rolled at tick %d, inside the interval", tick)
		}
		g.Start(view)
		if m.Horse.Standing == 0 || len(view.events) != 1 || view.events[0].Kind != MobSounded {
			t.Fatalf("standing %d events %v, want rearing with a call", m.Horse.Standing, view.events)
		}
		return
	}
	t.Fatal("an idle horse never reared")
}

// TestAHorseGoalListHasRandomStandExceptLlamas: llamas cannot rear.
func TestAHorseGoalListHasRandomStandExceptLlamas(t *testing.T) {
	for typ, want := range map[MobType]bool{TypeHorse: true, TypeDonkey: true, TypeMule: true,
		TypeSkeletonHorse: true, TypeZombieHorse: true, TypeLlama: false} {
		got := false
		for _, g := range horseGoals(NewMob(typ, 1, 0, 64, 0, 1)) {
			_, ok := g.(*randomStandGoal)
			got = got || ok
		}
		if got != want {
			t.Errorf("%v: random stand %v, want %v", typ, got, want)
		}
	}
}

// TestAWanderingTraderFleesAnIllusioner: the avoid list reaches 12 blocks.
func TestAWanderingTraderFleesAnIllusioner(t *testing.T) {
	trader := NewMob(TypeWanderingTrader, 1, 0.5, 64, 0.5, 1)
	ill := NewMob(TypeIllusioner, 2, 6.5, 64, 0.5, 2)
	for _, goal := range traderGoals(trader) {
		if g, ok := goal.(*avoidGoal); ok && g.maxDist == 12 && g.threatens(ill) {
			return
		}
	}
	t.Fatal("no 12-block avoid goal threatens an illusioner")
}
