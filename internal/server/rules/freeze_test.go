package rules

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/registry"
)

// The freeze's own arithmetic, where the wire cannot reach: the frozen
// counter's way up in the powder snow, its way down out of it, the hurts of
// the fully frozen body, and the wearables and the rule that hold it all
// off.

// freezeWearableOf is the piece of equipment named, as a worn stack.
func freezeWearableOf(t *testing.T, name string) protocol.ItemStack {
	t.Helper()
	id, ok := registry.ItemID(name)
	if !ok {
		t.Fatalf("the item registry has no %s", name)
	}
	return protocol.ItemStack{ItemID: id, Count: 1}
}

// TestPowderSnowCountsAndThaws runs the counter's two ways: a tick up a
// tick while the body stands in the snow, stopping at the full freeze, two
// back down a tick out of it, and never below nought.
func TestPowderSnowCountsAndThaws(t *testing.T) {
	ticks := 0
	for i := 1; i <= FullyFrozenTicks; i++ {
		ticks = FreezeTicksStep(ticks, true, true)
		if ticks != i {
			t.Fatalf("the counter's tick %d in the snow = %d, want %d", i, ticks, i)
		}
	}
	// The FREEZE effect's min stops the counter at the full freeze; a body
	// that stands on past it never counts higher.
	if got := FreezeTicksStep(ticks, true, true); got != FullyFrozenTicks {
		t.Errorf("a tick past the full freeze left the counter at %d, want the %d cap", got, FullyFrozenTicks)
	}

	// Out of the snow: two come off a tick, and the floor keeps the
	// counter from falling through nought.
	for i := 1; i <= FullyFrozenTicks/FreezeThawTicks; i++ {
		ticks = FreezeTicksStep(ticks, false, true)
		if want := FullyFrozenTicks - i*FreezeThawTicks; ticks != want {
			t.Fatalf("the thaw's tick %d left the counter at %d, want %d", i, ticks, want)
		}
	}
	if got := FreezeTicksStep(ticks, false, true); got != 0 {
		t.Errorf("a tick past the thaw left the counter at %d, want it floored at nought", got)
	}

	// A body the snow has no hold on never starts its counter.
	if got := FreezeTicksStep(0, true, false); got != 0 {
		t.Errorf("a body that cannot freeze counted %d frozen ticks, want the snow held off", got)
	}
}

// TestTheFreezeHurtsEveryFortiethTickOfTheAge checks when the hurts of a
// fully frozen body fall due: none of the ticks standing in the snow up to
// the full of the freeze, then the bite on every fortieth tick of the body's
// own age — the first at the hundred and sixtieth here, twenty past the
// freeze itself.
func TestTheFreezeHurtsEveryFortiethTickOfTheAge(t *testing.T) {
	v := FreshVitals(protocol.GameModeSurvival)
	bites := 0
	for age := 1; age <= FullyFrozenTicks+2*FreezeHurtTicks; age++ {
		v.AgeTicks = age
		hurts := TickFreeze(&v, true, true)
		want := age%FreezeHurtTicks == 0 && age >= FullyFrozenTicks
		if got := len(hurts) == 1; got != want {
			t.Fatalf("the body's age %d came to %v, want the bite due %v", age, hurts, want)
		}
		for _, h := range hurts {
			if h.DamageType != DamageTypeFreeze {
				t.Errorf("a tick in the snow bit with %s, want the freeze alone", h.DamageType)
			}
		}
		bites += len(hurts)
	}
	if bites != 2 {
		t.Fatalf("a hundred and eighty ticks of age past the freeze landed %d bites, want the two of every fortieth", bites)
	}
}

// TestFreezeImmuneWearablesHoldTheCounter checks the wearables the freeze
// stands aside for: a body wearing one of the tag's pieces in the snow
// never counts a frozen tick and never takes the hurt.
func TestFreezeImmuneWearablesHoldTheCounter(t *testing.T) {
	v := FreshVitals(protocol.GameModeSurvival)
	hurts := 0
	for age := 1; age <= FullyFrozenTicks+2*FreezeHurtTicks; age++ {
		v.AgeTicks = age
		hurts += len(TickFreeze(&v, true, false))
	}
	if v.TicksFrozen != 0 {
		t.Errorf("a leathered body counted %d frozen ticks, want the counter held at nought", v.TicksFrozen)
	}
	if hurts != 0 {
		t.Errorf("a leathered body took %d hurts in the snow, want none", hurts)
	}
}
