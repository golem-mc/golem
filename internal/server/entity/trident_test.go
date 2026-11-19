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
