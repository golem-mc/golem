package entity

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/registry"
)

// armorStandWorld grants a stone floor at y=63 over the cells about the
// origin, so a stand dropped above it lands rather than sinking.
func armorStandWorld(t *testing.T) testWorld {
	t.Helper()
	w := newTestWorld()
	for x := int32(-2); x <= 2; x++ {
		for z := int32(-2); z <= 2; z++ {
			w.set(x, 63, z, defaultState(t, "minecraft:stone"))
		}
	}
	return w
}

// TestAStandFallsToTheFloorUnderGravity drops one two blocks above the
// stone and steps it: it lands flush on the floor's top and stands there,
// the horizontal velocity it landed with burned off and the vertical left
// at the gravity tail a resting living body carries.
func TestAStandFallsToTheFloorUnderGravity(t *testing.T) {
	w := armorStandWorld(t)
	s := NewArmorStand(1, 0.5, 65, 0.5)
	moved := false
	for range 40 {
		s.Step(w)
		if s.Y != 65 {
			moved = true
		}
	}
	if !moved {
		t.Fatal("the stand never fell at all")
	}
	if !s.OnGround {
		t.Errorf("the landed stand is not on the ground")
	}
	if s.Y != 64 {
		t.Errorf("the stand rests at y %v, want the floor's top 64", s.Y)
	}
	if s.VX != 0 || s.VZ != 0 {
		t.Errorf("the resting stand keeps horizontal velocity, want it burned off")
	}
	if want := -ArmorStandGravity * ArmorStandDrag; s.VY != want {
		t.Errorf("the resting stand's VY = %v, want the gravity tail's %v", s.VY, want)
	}
}

// TestFireBreaksAStandWithinAFewTicks stands one in a fire cell and steps
// it: the cell re-lights it every tick, so its fire count stays at the full
// eight seconds and the burn's four-point bite lands every tick — the stand
// breaks within a handful of ticks. The rain puts a burning stand out and
// the damage with it.
func TestFireBreaksAStandWithinAFewTicks(t *testing.T) {
	w := armorStandWorld(t)
	w.set(0, 64, 0, defaultState(t, "minecraft:fire"))
	s := NewArmorStand(1, 0.5, 64, 0.5)
	var broke bool
	for tick := 0; tick < 10 && !broke; tick++ {
		for _, ev := range s.Step(w) {
			if ev.Kind == ArmorStandBroke {
				broke = true
			}
		}
	}
	if !broke {
		t.Fatalf("the stand in fire never burned out: health %v, fire %d", s.Health, s.FireTicks)
	}
	if !s.Dead {
		t.Error("the burned-out stand is not dead")
	}
	if s.FireTicks < fireTicksFire {
		t.Errorf("the broken stand's fire count = %d, want the cell's full %d", s.FireTicks, fireTicksFire)
	}

	// Out of the flame, the rain puts the burn and its bite out whole.
	w.set(0, 64, 0, defaultState(t, "minecraft:air"))
	s2 := NewArmorStand(2, 0.5, 64, 0.5)
	s2.FireTicks = 10
	w.rain = true
	s2.Step(w)
	if s2.FireTicks != 0 || s2.Health != ArmorStandHealth {
		t.Errorf("a rained-on burning stand kept fire %d and lost health %v, want the fire out whole",
			s2.FireTicks, ArmorStandHealth-s2.Health)
	}
}

// TestTheWaterDragsAStandWithItsOwnFraction stands one in still water with
// a sideways drift and steps it once: the water's own four-fifths drag
// shrinks the drift, not the air's 0.91.
func TestTheWaterDragsAStandWithItsOwnFraction(t *testing.T) {
	w := newTestWorld()
	w.set(0, 64, 0, defaultState(t, "minecraft:water"))
	s := NewArmorStand(1, 0.5, 64, 0.5)
	s.VX = 1
	s.Step(w)
	if s.VX != defaultWaterSlowDown {
		t.Errorf("the stand's drift after one tick in water = %v, want the water's drag %v", s.VX, defaultWaterSlowDown)
	}
}

// TestWaterCarriesAStandWithItsCurrent stands one in a water source
// pouring east into a thinner flow and steps it: the current's push
// carries it east, an entity fluid's current moving a body with no push
// of its own.
func TestWaterCarriesAStandWithItsCurrent(t *testing.T) {
	w := newTestWorld()
	for x := int32(-2); x <= 2; x++ {
		for z := int32(-2); z <= 2; z++ {
			w.set(x, 62, z, defaultState(t, "minecraft:stone"))
		}
	}
	// A source pouring east into its level-1 edge: the source's own flow
	// points at the thinner cell, which is the current that pushes.
	flowing, ok := registry.FindState("minecraft:water", map[string]string{"level": "1"})
	if !ok {
		t.Fatal("the registry carries no level-1 water")
	}
	w.set(0, 63, 0, defaultState(t, "minecraft:water"))
	w.set(1, 63, 0, flowing)

	s := NewArmorStand(1, 0.5, 63.5, 0.5)
	for range 20 {
		s.Step(w)
	}
	if s.X <= 0.5 {
		t.Fatalf("the stand still stands at x %v after a second in the current, want it carried east", s.X)
	}
	if s.VX == 0 && s.VZ == 0 && s.VY == 0 {
		t.Error("the current never pushed the stand's velocity")
	}
}

// hasHeightZero reports whether the box holds no height at all, the empty
// box a marker stands as.
func (b AABB) hasHeightZero() bool {
	return b.maxY == b.minY
}

// TestPunchesShakeThenBreakTheStand pins the punch window: a creative hand
// breaks at once, a survival first punch shakes and writes the window, the
// second within it breaks, and a punch after the window only shakes again.
// vanilla: ArmorStand.hurtServer's 5-tick window
func TestPunchesShakeThenBreakTheStand(t *testing.T) {
	s := NewArmorStand(1, 0, 0, 0)
	if out := s.Punch(100, true); out != PunchBroke {
		t.Errorf("a creative punch = %d, want the break", out)
	}

	s = NewArmorStand(1, 0, 0, 0)
	if out := s.Punch(100, false); out != PunchShaken {
		t.Errorf("a first punch = %d, want the shake", out)
	}
	if s.LastHit != 100 {
		t.Errorf("the shaken stand's window = %d, want the punch's tick 100", s.LastHit)
	}
	if out := s.Punch(104, false); out != PunchBroke {
		t.Errorf("the punch within the window = %d, want the break", out)
	}
	if !s.Dead {
		t.Error("the window's second punch left the stand alive")
	}

	s = NewArmorStand(1, 0, 0, 0)
	s.Punch(100, false)
	if out := s.Punch(106, false); out != PunchShaken {
		t.Errorf("the punch after the window = %d, want a fresh shake", out)
	}
	if s.Dead {
		t.Error("the punch after the window broke the stand anyway")
	}
}

// TestAnInvulnerableStandTurnsAsideEveryHurtButAKill checks the stand's
// invulnerability gate: a survival punch is refused and writes no window, a
// creative one reaches through, and only a bypassing type gets past the flag.
// vanilla: ArmorStand.hurtServer, LivingEntity.isInvulnerableTo
func TestAnInvulnerableStandTurnsAsideEveryHurtButAKill(t *testing.T) {
	s := NewArmorStand(1, 0, 0, 0)
	s.Invulnerable = true
	if !s.InvulnerableTo(rules.DamageTypePlayerAttack, false) {
		t.Error("a survival punch reaches an invulnerable stand")
	}
	if s.InvulnerableTo(rules.DamageTypePlayerAttack, true) {
		t.Error("a creative punch is turned aside by an invulnerable stand")
	}
	if s.InvulnerableTo(rules.DamageTypeGenericKill, false) {
		t.Error("generic_kill is turned aside by an invulnerable stand")
	}
	if out := s.Punch(100, false); out != PunchRefused {
		t.Errorf("a survival punch = %d, want the refusal", out)
	}
	if s.LastHit != 0 {
		t.Errorf("a refused punch wrote the window as %d, want it untouched", s.LastHit)
	}
	if s.Dead {
		t.Error("a refused punch broke the stand")
	}
}

// TestTheClickedSlotMapsTheStand reads the whole body through the slot
// picker: each band returns the part it reaches only where that slot holds
// an item, a stand bare of that part falling through to a hand, and the
// side of the body is not read at all.
func TestTheClickedSlotMapsTheStand(t *testing.T) {
	held := protocol.ItemStack{ItemID: 1, Count: 1}
	cases := []struct {
		name       string
		ox, oy, oz float64
		slots      []int
		want       int
	}{
		{"the head's crown", 0, 1.7, 0, []int{StandHead}, StandHead},
		{"the chest", 0, 1.2, 0, []int{StandChest}, StandChest},
		{"the chest's side", 0.3, 1.2, 0, []int{StandChest}, StandChest},
		{"the legs", 0, 0.6, 0, []int{StandLegs}, StandLegs},
		{"the feet", 0, 0.2, 0, []int{StandFeet}, StandFeet},
		{"a bare chest", 0, 1.2, 0, nil, StandMainHand},
		{"the main hand kept", 0, 1.2, 0, []int{StandMainHand}, StandMainHand},
		{"the off hand alone", 0, 1.2, 0, []int{StandOffHand}, StandOffHand},
	}
	for _, tc := range cases {
		s := NewArmorStand(1, 0, 0, 0)
		for _, slot := range tc.slots {
			s.SetStack(slot, held)
		}
		if got := s.ClickedSlot(tc.ox, tc.oy, tc.oz); got != tc.want {
			t.Errorf("%s (offset %v, %v, %v) picked slot %d, want %d",
				tc.name, tc.ox, tc.oy, tc.oz, got, tc.want)
		}
	}
	// A small stand's bands halve with it, read over the scale: its head
	// above 0.8, its chest up from 0.6, its legs up from 0.2, its feet
	// above 0.05.
	small := []struct {
		oy    float64
		slots []int
		want  int
	}{
		{0.9, []int{StandHead}, StandHead},
		{0.7, []int{StandChest}, StandChest},
		{0.3, []int{StandLegs}, StandLegs},
		{0.1, []int{StandFeet}, StandFeet},
	}
	for _, tc := range small {
		s := NewArmorStand(1, 0, 0, 0)
		s.Small = true
		for _, slot := range tc.slots {
			s.SetStack(slot, held)
		}
		if got := s.ClickedSlot(0, tc.oy, 0); got != tc.want {
			t.Errorf("a small stand clicked at y %v picked slot %d, want %d", tc.oy, got, tc.want)
		}
	}
}

// TestTheDisabledSlotsMaskLocksAPlayerOutOfASlot checks the mask the way a
// player's swap reads it: the +0 bit, on the EquipmentSlot ids the wire
// index does not match, refuses the interaction, the +16 bit refuses a
// take out and the +8 bit a put in. 1<<21 is the off hand's put bit.
func TestTheDisabledSlotsMaskLocksAPlayerOutOfASlot(t *testing.T) {
	for slot, bit := range map[int]uint{
		StandFeet: 1, StandLegs: 2, StandChest: 3, StandHead: 4,
	} {
		s := NewArmorStand(1, 0, 0, 0)
		s.DisabledSlots = int32(1) << bit
		if !s.SlotLocked(slot) {
			t.Errorf("the mask %#x left slot %d open, want its +0 bit locking it", s.DisabledSlots, slot)
		}
		s.DisabledSlots = int32(1) << (bit + 8)
		if s.SlotLocked(slot) {
			t.Errorf("the mask %#x locked slot %d, want the +0 bit alone doing it", s.DisabledSlots, slot)
		}
	}
	held := protocol.ItemStack{ItemID: 1, Count: 1}
	s := NewArmorStand(1, 0, 0, 0)
	s.DisabledSlots = 1 << 21
	if _, swapped := s.SwapItem(StandOffHand, held, false); swapped {
		t.Error("the 1<<21 mask let an item into the off hand, want the off-hand put bit locking it")
	}
	if _, swapped := s.SwapItem(StandHead, held, false); !swapped {
		t.Error("the 1<<21 mask locked the head, want the off hand alone")
	}
}

// TestASwapSplitsAStackAndCopiesForACreativeHand checks the two ways a
// stack of more than one is placed: a survival hand puts down a single item
// and keeps the rest, a creative hand copies one and keeps its whole stack.
func TestASwapSplitsAStackAndCopiesForACreativeHand(t *testing.T) {
	s := NewArmorStand(1, 0, 0, 0)
	held := protocol.ItemStack{ItemID: 1, Count: 3}
	hand, swapped := s.SwapItem(StandHead, held, false)
	if !swapped {
		t.Fatal("a survival stack of three was refused")
	}
	if hand.Count != 2 || s.StackOf(StandHead).Count != 1 {
		t.Errorf("the split left hand %d and stand %d, want 2 and 1",
			hand.Count, s.StackOf(StandHead).Count)
	}

	s = NewArmorStand(1, 0, 0, 0)
	hand, swapped = s.SwapItem(StandHead, protocol.ItemStack{ItemID: 1, Count: 3}, true)
	if !swapped {
		t.Fatal("a creative copy of three was refused")
	}
	if hand.Count != 3 || s.StackOf(StandHead).Count != 1 {
		t.Errorf("the copy left hand %d and stand %d, want 3 and 1",
			hand.Count, s.StackOf(StandHead).Count)
	}
}
