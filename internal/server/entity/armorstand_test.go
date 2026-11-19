package entity

import (
	"math"
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

// TestAStandsEyeSitsAtItsDimensionHeight reads the eye an explosion's
// knockback runs from: 1.7775 above a full stand's feet, 0.9875 on the small.
func TestAStandsEyeSitsAtItsDimensionHeight(t *testing.T) {
	for _, tc := range []struct {
		small bool
		want  float64
	}{
		{false, 1.7775},
		{true, 0.9875},
	} {
		s := NewArmorStand(1, 0.5, 64, 0.5)
		s.Small = tc.small
		_, y, _ := s.EyePosition()
		if got := y - s.Y; math.Abs(got-tc.want) > 1e-9 {
			t.Errorf("small=%v: eye height = %v, want %v", tc.small, got, tc.want)
		}
	}
}

// TestFireLeavesAnInvulnerableStandStanding stands an invulnerable one in a
// fire cell and steps it: the cell still lights it, but neither the source's
// bite nor the burn's lands, so it keeps its full health.
func TestFireLeavesAnInvulnerableStandStanding(t *testing.T) {
	w := armorStandWorld(t)
	w.set(0, 64, 0, defaultState(t, "minecraft:fire"))
	s := NewArmorStand(1, 0.5, 64, 0.5)
	s.Invulnerable = true
	full := s.Health
	for tick := range 100 {
		for _, ev := range s.Step(w) {
			if ev.Kind == ArmorStandBroke {
				t.Fatalf("tick %d broke the invulnerable stand", tick)
			}
		}
	}
	if s.Health != full {
		t.Errorf("the invulnerable stand's health = %v, want its full %v", s.Health, full)
	}
	if s.FireTicks == 0 {
		t.Error("the fire never caught the invulnerable stand, want it alight")
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

// TestARoofedBurningStandKeepsItsBurnInTheRain stands one in fire under a
// roof while it rains and steps it: the rain never lands on the stand, so
// the fire cell keeps re-lighting it and the burn's four points land.
// vanilla: Entity.applyEffectsFromBlocks, Entity.isInRain
func TestARoofedBurningStandKeepsItsBurnInTheRain(t *testing.T) {
	w := armorStandWorld(t)
	w.set(0, 64, 0, defaultState(t, "minecraft:fire"))
	w.set(0, 66, 0, defaultState(t, "minecraft:stone")) // the roof over the stand
	w.rain = true
	s := NewArmorStand(1, 0.5, 64, 0.5)
	var broke bool
	for tick := 0; tick < 40 && !broke; tick++ {
		for _, ev := range s.Step(w) {
			if ev.Kind == ArmorStandBroke {
				broke = true
			}
		}
	}
	if !broke {
		t.Fatalf("a roofed stand in fire never burned out in the rain: health %v, fire %d", s.Health, s.FireTicks)
	}
}

// TestLavaLightsAStandWithoutBurningIt stands one in a lava cell and steps
// it: lava lights the stand for its fifteen seconds but, in neither of the
// armor-stand damage tags, lands no damage of its own, and the burn is
// skipped while it stands there.
func TestLavaLightsAStandWithoutBurningIt(t *testing.T) {
	w := armorStandWorld(t)
	w.set(0, 64, 0, defaultState(t, "minecraft:lava"))
	s := NewArmorStand(1, 0.5, 64, 0.5)
	for tick := range 30 {
		if evs := s.Step(w); len(evs) > 0 {
			t.Fatalf("lava broke the stand at tick %d", tick)
		}
	}
	if s.Health != ArmorStandHealth {
		t.Errorf("a stand in lava lost %v health, want none", ArmorStandHealth-s.Health)
	}
	if s.FireTicks < fireTicksLava {
		t.Errorf("the stand's fire count = %d, want the lava's %d", s.FireTicks, fireTicksLava)
	}
}

// TestAnInvisibleStandDoesNotBurn stands an invisible one in fire and
// steps it: the stand lights, but ArmorStand.hurtServer's invisible early
// return leaves both the fire source's bite and the burn off it.
func TestAnInvisibleStandDoesNotBurn(t *testing.T) {
	w := armorStandWorld(t)
	w.set(0, 64, 0, defaultState(t, "minecraft:fire"))
	s := NewArmorStand(1, 0.5, 64, 0.5)
	s.Invisible = true
	var broke bool
	for tick := 0; tick < 40 && !broke; tick++ {
		for _, ev := range s.Step(w) {
			if ev.Kind == ArmorStandBroke {
				broke = true
			}
		}
	}
	if broke || s.Health != ArmorStandHealth {
		t.Errorf("an invisible stand in fire lost %v health, want none", ArmorStandHealth-s.Health)
	}
	if s.FireTicks == 0 {
		t.Error("the invisible stand never lit, want the fire still on it")
	}
}

// TestANoGravityStandIsOutOfTheCellsFire stands a no-gravity one in a fire
// cell and steps it: Entity.isAffectedByBlocks is false for a stand whose
// hasPhysics the NoGravity flag turns off, so the cell neither lights the
// stand nor lands its 0.15, and a lava cell lights it not either. A burn it
// already carried still bites, the countdown running outside the block
// effects.
// vanilla: Entity.applyEffectsFromBlocks, ArmorStand.hasPhysics
func TestANoGravityStandIsOutOfTheCellsFire(t *testing.T) {
	w := armorStandWorld(t)
	w.set(0, 64, 0, defaultState(t, "minecraft:fire"))
	s := NewArmorStand(1, 0.5, 64, 0.5)
	s.NoGravity = true
	for range 40 {
		s.Step(w)
	}
	if s.FireTicks != 0 {
		t.Errorf("a no-gravity stand in fire holds fire ticks %d, want none — no cell lights it", s.FireTicks)
	}
	if s.Health != ArmorStandHealth {
		t.Errorf("a no-gravity stand in fire lost %v health, want none", ArmorStandHealth-s.Health)
	}

	// The burn already on it still bites: the countdown is not gated.
	alight := NewArmorStand(2, 0.5, 64, 0.5)
	alight.NoGravity = true
	alight.FireTicks = 20
	alight.Step(w)
	if want := ArmorStandHealth - burnDamage; alight.Health != want {
		t.Errorf("a no-gravity stand alight kept health %v, want the burn's bite cut to %v", alight.Health, want)
	}

	// Lava is out of it the same way.
	lava := armorStandWorld(t)
	lava.set(0, 64, 0, defaultState(t, "minecraft:lava"))
	s2 := NewArmorStand(3, 0.5, 64, 0.5)
	s2.NoGravity = true
	for range 10 {
		s2.Step(lava)
	}
	if s2.FireTicks != 0 {
		t.Errorf("a no-gravity stand in lava holds fire ticks %d, want none", s2.FireTicks)
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

// TestTheLavaDragsAStandWithItsOwnFraction stands one in deep lava with a
// sideways drift and steps it once: deep lava drags everything by half,
// and its own quarter-gravity pull adds after.
func TestTheLavaDragsAStandWithItsOwnFraction(t *testing.T) {
	w := newTestWorld()
	w.set(0, 64, 0, defaultState(t, "minecraft:lava"))
	s := NewArmorStand(1, 0.5, 64, 0.5)
	s.VX = 1
	s.Step(w)
	if s.VX != 0.5 {
		t.Errorf("the stand's drift after one tick in deep lava = %v, want the deep lava's half", s.VX)
	}
	if want := -ArmorStandGravity / 4; s.VY != want {
		t.Errorf("the lava's own pull left VY %v, want %v", s.VY, want)
	}
}

// TestWaterOverLavaPullsAStandDownOnce stands one with water in the top of
// its box over lava in the bottom and steps it: isInWater wins the fluid
// branch, so the water's own pull lands alone where the lava's
// quarter-gravity would otherwise pull the stand down a second time.
// vanilla: LivingEntity.travelInFluid
func TestWaterOverLavaPullsAStandDownOnce(t *testing.T) {
	w := newTestWorld()
	w.set(0, 64, 0, defaultState(t, "minecraft:lava"))
	w.set(0, 65, 0, defaultState(t, "minecraft:water"))
	s := NewArmorStand(1, 0.5, 64, 0.5)
	s.Step(w)
	if want := -ArmorStandGravity / 16; s.VY != want {
		t.Errorf("the stand in water over lava left VY %v, want the water's pull alone %v", s.VY, want)
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

// TestASmallStandsEyeClearsAOneDeepSource stands a small one in a single
// source cell at its feet and steps it: the small stand's eye sits at the
// whole of its box, above the source's eight ninths, so the eye is not
// marked in the water as the default 0.85 eye height would mark it.
// vanilla: EntityFluidInteraction.update
func TestASmallStandsEyeClearsAOneDeepSource(t *testing.T) {
	w := newTestWorld()
	w.set(0, 64, 0, defaultState(t, "minecraft:water"))
	s := NewArmorStand(1, 0.5, 64, 0.5)
	s.Small = true
	s.Step(w)
	if s.water.eyesIn {
		t.Errorf("the small stand's eye at %v was marked in a source topping %v", 64+s.eyeHeight(), 64+sourceHeight)
	}
}

// TestAMarkerStandStaysPut stands a marker in fire and in a current and
// steps it: a marker holds no box, never burns and never moves.
func TestAMarkerStandStaysPut(t *testing.T) {
	w := armorStandWorld(t)
	w.set(0, 64, 0, defaultState(t, "minecraft:fire"))
	s := NewArmorStand(1, 0.5, 64, 0.5)
	s.Marker = true
	for range 100 {
		s.Step(w)
	}
	if s.FireTicks != 0 {
		t.Errorf("a marker in fire holds fire ticks %d, want none — no box to light", s.FireTicks)
	}
	if s.X != 0.5 || s.Y != 64 || s.Z != 0.5 {
		t.Errorf("the marker moved to (%v, %v, %v), want it never moving", s.X, s.Y, s.Z)
	}
	if !s.Box().hasHeightZero() {
		t.Error("the marker's box is not the empty one")
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

// TestAPunchInTheWorldsFirstTicksBreaks pins the open window vanilla's
// lastHit starting at nought leaves: a punch in the world's first five
// ticks reads as one within the window and breaks the stand.
func TestAPunchInTheWorldsFirstTicksBreaks(t *testing.T) {
	s := NewArmorStand(1, 0, 0, 0)
	if out := s.Punch(3, false); out != PunchBroke {
		t.Errorf("a punch at tick 3 = %d, want the break the open window gives", out)
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

// TestAPlacedStandFacesItsPlacerSnappedToAnEighth checks the placement yaw:
// the placer's yaw turned about and snapped to 45 degrees.
func TestAPlacedStandFacesItsPlacerSnappedToAnEighth(t *testing.T) {
	for _, tc := range []struct{ placer, want float32 }{
		{0, -180}, {90, -90}, {-90, 90}, {30, -135}, {200, 0},
	} {
		if got := ArmorStandPlacementYaw(tc.placer); got != tc.want {
			t.Errorf("a placer at yaw %v stood the stand at %v, want %v", tc.placer, got, tc.want)
		}
	}
}

// TestTheUseSlotPicksTheHeldItemsSlotOrTheClickedOne checks the use's
// choice: a name tag and a marker fall through, a held helmet goes to the
// head, a locked slot refuses it, and an empty hand on an empty stand does
// nothing.
func TestTheUseSlotPicksTheHeldItemsSlotOrTheClickedOne(t *testing.T) {
	helmetID, ok := registry.ItemID("minecraft:iron_helmet")
	if !ok {
		t.Fatal("no iron_helmet in the registry")
	}
	tagID, _ := registry.ItemID("minecraft:name_tag")
	helmet := protocol.ItemStack{ItemID: helmetID, Count: 1}

	s := NewArmorStand(1, 0, 0, 0)
	if _, use := s.UseSlot(protocol.ItemStack{ItemID: tagID, Count: 1}, 0, 1, 0); use != StandUsePass {
		t.Errorf("a name tag came to %d, want a pass", use)
	}
	if _, use := s.UseSlot(protocol.ItemStack{}, 0, 1, 0); use != StandUsePass {
		t.Errorf("an empty hand on an empty stand came to %d, want a pass", use)
	}
	if slot, use := s.UseSlot(helmet, 0, 0.2, 0); use != StandUseSwap || slot != StandHead {
		t.Errorf("a helmet came to slot %d use %d, want the head swapped", slot, use)
	}
	s.DisabledSlots = 1 << 4
	if _, use := s.UseSlot(helmet, 0, 0.2, 0); use != StandUseRefused {
		t.Errorf("a helmet on a locked head came to %d, want a refusal", use)
	}
	s = NewArmorStand(1, 0, 0, 0)
	s.Marker = true
	if _, use := s.UseSlot(helmet, 0, 0.2, 0); use != StandUsePass {
		t.Errorf("a marker came to %d, want a pass", use)
	}
}

// TestABrokenStandDropsWhatItHeldInWireOrder checks the equipment drops
// leave the empty slots out and keep the wire order.
func TestABrokenStandDropsWhatItHeldInWireOrder(t *testing.T) {
	s := NewArmorStand(1, 0, 0, 0)
	s.SetStack(StandHead, protocol.ItemStack{ItemID: 3, Count: 1})
	s.SetStack(StandMainHand, protocol.ItemStack{ItemID: 2, Count: 1})
	drops := s.EquipmentDrops()
	if len(drops) != 2 || drops[0].ItemID != 2 || drops[1].ItemID != 3 {
		t.Errorf("the drops were %+v, want the main hand then the head", drops)
	}
}

// TestALavaCauldronLightsAStand checks that a stand in a lava cauldron is
// lit for the lava's fifteen seconds.
// vanilla: LavaCauldronBlock.entityInside
func TestALavaCauldronLightsAStand(t *testing.T) {
	w := armorStandWorld(t)
	w.set(0, 64, 0, defaultState(t, "minecraft:lava_cauldron"))
	s := NewArmorStand(1, 0.5, 64, 0.5)
	s.Step(w)
	if s.FireTicks < fireTicksLava-1 {
		t.Errorf("the stand's fire count = %d, want about the lava's %d", s.FireTicks, fireTicksLava)
	}
}
