package entity

import (
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/registry"
)

// The trident simulation as ordinary unit tests, driving the step over the
// world that is nothing but the blocks each case sets, the way the
// arrow's own are. The black-box tests in internal/server/server_trident_test.go
// remain the proof of the whole life of a trident — the charge, the throw,
// the hit, the pickup, the return — and these are the cheapest possible
// checks of the flight's own differences from an arrow's.

// newTestTrident lays a plain trident flying east at chest height, the
// shape every case but the return's starts from, carrying the stack of the
// plain item.
func newTestTrident(ownerID int32) *Arrow {
	return NewTrident(1, 0, 64, 0, 3, 0, 0, PickupAllowed, ownerID,
		protocol.ItemStack{ItemID: int32(registry.ItemTrident), Count: 1}, 3)
}

// TestATridentHitDealsItsEight checks the hit's damage against the
// arithmetic ThrownTrident.onHitEntity runs: the fixed eight without a
// velocity's scale, the enchantments of the thrown stack added over it —
// impaling's 2.5 a level against the aquatic, none against a body the
// impaling tag does not name.
func TestATridentHitDealsItsEight(t *testing.T) {
	a := newTestTrident(0)
	if got := a.hitDamage(&ArrowTarget{}); got != 8 {
		t.Errorf("a plain trident's hit = %v, want the eight of the thrown body", got)
	}
	// A trident read back from a file carries the arrow's own 2.0 in
	// baseDamage — ThrownTrident never sets it — and still fights with the
	// fixed eight.
	saved := newTestTrident(0)
	saved.BaseDamage = ArrowBaseDamage
	if got := saved.hitDamage(&ArrowTarget{}); got != 8 {
		t.Errorf("a trident loaded with the file's 2.0 hit for %v, want the fixed eight", got)
	}

	impalingID, ok := registry.EnchantmentID("minecraft:impaling")
	if !ok {
		t.Fatal("the enchantment registry has no impaling")
	}
	enchanted := NewTrident(2, 0, 64, 0, 3, 0, 0, PickupAllowed, 0,
		protocol.ItemStack{
			ItemID:       int32(registry.ItemTrident),
			Count:        1,
			Enchantments: map[int32]int32{impalingID: 3},
		}, 3)
	squid, ok := registry.EntityTypeID("minecraft:squid")
	if !ok {
		t.Fatal("the entity type registry has no squid")
	}
	player, ok := registry.EntityTypeID("minecraft:player")
	if !ok {
		t.Fatal("the entity type registry has no player")
	}
	if got := enchanted.hitDamage(&ArrowTarget{Type: squid}); got != 8+7.5 {
		t.Errorf("an impaling III trident hit a squid for %v, want the eight and the level's 2.5 each", got)
	}
	if got := enchanted.hitDamage(&ArrowTarget{Type: player}); got != 8 {
		t.Errorf("an impaling III trident hit a player for %v, want the eight alone", got)
	}
}

// TestATridentFliesAtFullSpeedInWater checks the flight's own difference
// from an arrow's in water: the 0.99 inertia the arrow's 0.6 gives way to.
func TestATridentFliesAtFullSpeedInWater(t *testing.T) {
	w := newTestWorld()
	w.set(0, 64, 0, defaultState(t, "minecraft:water"))
	w.set(1, 64, 0, defaultState(t, "minecraft:water"))
	w.set(2, 64, 0, defaultState(t, "minecraft:water"))
	w.set(3, 64, 0, defaultState(t, "minecraft:water"))

	a := newTestTrident(0)
	a.Step(w, nil, nil)
	if !closeEnough(a.VX, 3*tridentWaterInertia) {
		t.Errorf("a trident in water flies on at %v, want the 0.99 share of the three it arrived with", a.VX)
	}
	if !closeEnough(a.VY, -0.05) {
		t.Errorf("the water's gravity pulled the trident to %v, want the same fall an arrow's", a.VY)
	}
}

// TestATridentBouncesOffAndNeverHitsAgain plays the server's part in a
// hit — the deflection the hurt settles — and checks what the flight is
// left with: the almost-dead bounce back, the dealt mark that empties the
// sweep of every body, the trident alive to fall and stick where it
// lands.
func TestATridentBouncesOffAndNeverHitsAgain(t *testing.T) {
	w := newTestWorld()
	body := PlayerTarget(7, 1.5, 63, 0, false)
	a := newTestTrident(0)

	var events []ArrowEvent
	events = append(events, a.Step(w, nil, []ArrowTarget{body})...)
	if len(events) != 1 || events[0].Kind != ArrowHitTarget || events[0].Target.EntityID != 7 {
		t.Fatalf("a trident flying at a body reported %v, want the one hit of it", events)
	}
	if events[0].Damage != 8 {
		t.Errorf("the trident's hit carried damage %v, want the eight", events[0].Damage)
	}

	a.TridentDeflect(w)
	if !closeEnough(a.VX, 3*-0.5*0.02*arrowAirDrag) {
		t.Errorf("the deflected trident flies on at %v, want the reverse's share of almost nothing and the tick's drag", a.VX)
	}
	if !closeEnough(a.VY, -arrowGravity) {
		t.Errorf("the deflected trident's fall = %v, want the tick's gravity on the hit", a.VY)
	}
	if a.Yaw <= 255 || a.Yaw >= 285 {
		t.Errorf("the deflected trident points at yaw %v, want the 170-to-190 turn about on the 90 it flew at", a.Yaw)
	}
	if a.Dead {
		t.Error("a hit trident is dead, want it bouncing off alive")
	}

	// The body keeps standing, the trident falls: the dealt mark leaves
	// the sweep empty of every body, so the flight on past it lands
	// nothing.
	for range 80 {
		for _, ev := range a.Step(w, nil, []ArrowTarget{body}) {
			if ev.Kind == ArrowHitTarget {
				t.Fatalf("a spent trident was swept up by the body it hit again, at (%v, %v, %v)", a.X, a.Y, a.Z)
			}
		}
	}
	if a.Dead {
		t.Fatal("the fallen trident died without a block or a body to spend it")
	}
}

// TestALoyalTridentReturnsToItsOwner sticks a loyalty trident into a wall
// and ticks it with its owner standing behind: the fifth tick stuck marks
// the blow dealt, the sixth turns the flight for the owner's eyes, the
// return sounding once, and the position closes on the owner until the
// world could hand it to them.
func TestALoyalTridentReturnsToItsOwner(t *testing.T) {
	w := newTestWorld()
	stone := defaultState(t, "minecraft:stone")
	w.set(3, 64, 0, stone)
	w.set(3, 65, 0, stone)

	loyal := registryEnchantmentID(t, "minecraft:loyalty")
	a := NewTrident(1, 0, 64, 0, 3, 0, 0, PickupAllowed, 7,
		protocol.ItemStack{
			ItemID:       int32(registry.ItemTrident),
			Count:        1,
			Enchantments: map[int32]int32{loyal: 1},
		}, 3)
	if a.Loyalty != 1 {
		t.Fatalf("a stack of loyalty one threw a trident of loyalty %d", a.Loyalty)
	}
	a.Step(w, nil, nil)
	if !a.InGround {
		t.Fatal("the trident shot into the wall did not stick")
	}

	// Five ticks stuck: the blow is not dealt yet, and so far the return
	// has not begun.
	for range 5 {
		a.Step(w, nil, nil)
	}
	if a.DealtDamage() {
		t.Fatal("a trident stuck five ticks has its blow marked dealt already")
	}

	owner := PlayerTarget(7, -4, 63, 0, false)
	ownerX := (owner.MinX + owner.MaxX) / 2
	returns := 0
	start := math.Abs(a.X - ownerX)
	for range 30 {
		for _, ev := range a.Step(w, &owner, []ArrowTarget{owner}) {
			if ev.Kind == ArrowTridentReturn {
				returns++
			}
		}
	}
	if !a.DealtDamage() {
		t.Fatal("a trident stuck five ticks never had its blow marked dealt")
	}
	if !a.NoPhysics {
		t.Fatal("a returning trident never turned noclip")
	}
	if returns != 1 {
		t.Errorf("the return sounded %d times, want the once of its first tick", returns)
	}
	if closed := math.Abs(a.X - ownerX); closed >= start {
		t.Errorf("the returning trident closed the distance to its owner from %v to %v, want nearer", start, closed)
	}
}

// TestAnUnloyalTridentNeverReturns sticks a plain trident into the same
// wall with its owner behind it: without a loyalty, the body lies stuck —
// the deflection's dealt mark and the empty sweep its only marks — until
// the despawn takes it.
func TestAnUnloyalTridentNeverReturns(t *testing.T) {
	w := newTestWorld()
	stone := defaultState(t, "minecraft:stone")
	w.set(3, 64, 0, stone)
	w.set(3, 65, 0, stone)

	a := newTestTrident(7)
	a.Step(w, nil, nil)
	if !a.InGround {
		t.Fatal("the trident shot into the wall did not stick")
	}

	owner := PlayerTarget(7, -4, 63, 0, false)
	for range 20 {
		for _, ev := range a.Step(w, &owner, []ArrowTarget{owner}) {
			if ev.Kind == ArrowTridentReturn {
				t.Fatal("a trident without loyalty sounded a return")
			}
		}
	}
	if !a.InGround || a.NoPhysics {
		t.Fatalf("a trident without loyalty got up out of the ground: inGround=%v noclip=%v", a.InGround, a.NoPhysics)
	}

	// The despawn an arrow answers for: the 1200 takes the stuck plain
	// trident, a loyal one the pickup is allowed being its owner's to
	// wait for instead.
	a.Life = arrowLifeLimit - 1
	events := a.Step(w, &owner, nil)
	if len(events) != 1 || events[0].Kind != ArrowGone || !a.Dead {
		t.Fatalf("a stuck trident without loyalty at 1200 ticks reported %v dead=%v, want the despawn arrow's own", events, a.Dead)
	}

	// The loyal one never counts out: the same 1200 leaves it stuck
	// waiting on its owner, the despawn its own override denies.
	stone2 := defaultState(t, "minecraft:stone")
	w.set(0, 64, 2, stone2)
	loyal := registryEnchantmentID(t, "minecraft:loyalty")
	waiting := NewTrident(2, 0.5, 64.7, 2.5, 0, 0, 0, PickupAllowed, 7,
		protocol.ItemStack{
			ItemID:       int32(registry.ItemTrident),
			Count:        1,
			Enchantments: map[int32]int32{loyal: 1},
		}, 3)
	waiting.InGround = true
	waiting.LastState = stone2
	waiting.Life = arrowLifeLimit - 1
	if events := waiting.Step(w, nil, nil); len(events) != 0 || waiting.Dead || !waiting.InGround {
		t.Fatalf("a loyal trident the pickup allows counted out at 1200: %v dead=%v inGround=%v",
			events, waiting.Dead, waiting.InGround)
	}
}

// registryEnchantmentID is the numeric ID of a named enchantment, loud
// where the registry has none.
func registryEnchantmentID(t *testing.T, name string) int32 {
	t.Helper()
	id, ok := registry.EnchantmentID(name)
	if !ok {
		t.Fatalf("the enchantment registry has no %s", name)
	}
	return id
}

// TestAnEffectlessTippedArrowTurnsPlainInTheGround checks that a stuck arrow
// whose potion contents give no effect still loses them after 600 ticks.
// vanilla: Arrow.tick
func TestAnEffectlessTippedArrowTurnsPlainInTheGround(t *testing.T) {
	w := newTestWorld()
	w.set(3, 64, 0, defaultState(t, "minecraft:stone"))
	w.set(3, 65, 0, defaultState(t, "minecraft:stone"))
	a := NewArrow(1, 0, 64, 0, 3, 0, 0, false, 7, PickupAllowed, 0, 1)
	a.Effect = ArrowEffect{Tipped: true}
	a.PickupStack = protocol.ItemStack{ItemID: 1, Count: 1}
	a.Step(w, nil, nil)
	if !a.InGround {
		t.Fatal("the arrow did not stick")
	}
	a.Life = arrowPotionDecay - 1
	a.Step(w, nil, nil)
	if !a.Effect.IsPlain() || !a.PickupStack.Empty() {
		t.Errorf("effect %+v pickup %v, want a plain arrow", a.Effect, a.PickupStack)
	}
}
