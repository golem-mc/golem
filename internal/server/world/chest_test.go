package world

import "testing"

// TestTrappedChestSignalsItsOpenersWeaklyAroundAndStronglyDown checks the
// trapped chest's two signals: its openers count, capped at 15, through
// every face, and strongly through its bottom face alone.
func TestTrappedChestSignalsItsOpenersWeaklyAroundAndStronglyDown(t *testing.T) {
	w, p := newPhysicsWorld(t)
	setBlock(t, w, p, 10, 63, 9, rsState(t, "minecraft:trapped_chest", nil))
	be := w.EnsureBlockEntity(10, 63, 9, KindTrappedChest)
	for _, c := range []struct{ openers, want int }{{0, 0}, {3, 3}, {20, 15}} {
		be.Use(func(b *BlockEntity) { b.Openers = c.openers })
		for d := range dirCount {
			if got := p.signalThrough(10, 63, 9, d); got != c.want {
				t.Errorf("%d openers: weak signal through face %d = %d, want %d", c.openers, d, got, c.want)
			}
			want := 0
			if d == dirDown {
				want = c.want
			}
			if got := p.directThrough(10, 63, 9, d); got != want {
				t.Errorf("%d openers: strong signal through face %d = %d, want %d", c.openers, d, got, want)
			}
		}
	}
}

// TestChestHalvesPairAndSplit checks the shape update between two chest
// halves: a right half set beside a single north-facing chest turns it into
// the left half, the pair reads right half first, and the right half's
// removal leaves the left a single again.
func TestChestHalvesPairAndSplit(t *testing.T) {
	w, p := newPhysicsWorld(t)
	setBlock(t, w, p, 10, 63, 9, rsState(t, "minecraft:chest", map[string]string{"facing": "north", "type": "single"}))
	setBlock(t, w, p, 11, 63, 9, rsState(t, "minecraft:chest", map[string]string{"facing": "north", "type": "right"}))
	if _, props := rsNameAt(t, w, 10, 63, 9); props["type"] != "left" {
		t.Fatalf("the single beside a right half stands %v, want left", props)
	}
	first, second, ok := w.ChestPair(10, 63, 9)
	if !ok || first != [3]int32{11, 63, 9} || second != [3]int32{10, 63, 9} {
		t.Errorf("the pair reads %v, %v, %v; want the right half (11, 63, 9) first", first, second, ok)
	}
	setBlock(t, w, p, 11, 63, 9, airState)
	if _, props := rsNameAt(t, w, 10, 63, 9); props["type"] != "single" {
		t.Errorf("the left half stands %v after its partner went, want single", props)
	}
	if _, _, ok := w.ChestPair(10, 63, 9); ok {
		t.Error("a lone chest still reads as a pair")
	}
}

// TestAShulkerBoxConductsOnlyWhileClosed checks the lid phases: a box under a
// torch passes strong power to the wire beside it while CLOSED, and the wire
// hears of each change when the lid starts and finishes moving.
// vanilla: ShulkerBoxBlockEntity.updateAnimation, isClosed
func TestAShulkerBoxConductsOnlyWhileClosed(t *testing.T) {
	w, p := newPhysicsWorld(t)
	setBlock(t, w, p, 10, 63, 10, rsState(t, "minecraft:redstone_torch", map[string]string{"lit": "true"}))
	setBlock(t, w, p, 10, 64, 10, rsState(t, "minecraft:red_shulker_box", map[string]string{"facing": "up"}))
	setBlock(t, w, p, 11, 64, 10, rsState(t, "minecraft:redstone_wire", nil))
	clock := tickFor(p, 0, 3)
	power := func() string {
		_, props := rsNameAt(t, w, 11, 64, 10)
		return props["power"]
	}
	if power() != "15" || !w.ConductorAt(10, 64, 10) {
		t.Fatalf("a closed box beside the wire: power %s, conductor %v, want 15, true", power(), w.ConductorAt(10, 64, 10))
	}

	p.ShulkerViewersChanged(10, 64, 10, 1)
	if !w.ConductorAt(10, 64, 10) {
		t.Error("a shulker box with zero lid progress is not a conductor")
	}
	if power() != "15" {
		t.Errorf("the wire heard of the lid before the first animation tick: power %s", power())
	}
	clock = tickFor(p, clock, 1)
	if got := power(); got != "0" {
		t.Errorf("the wire beside an opening box has power %s, want 0", got)
	}

	clock = tickFor(p, clock, shulkerLidTicks)
	p.ShulkerViewersChanged(10, 64, 10, 0)
	clock = tickFor(p, clock, shulkerLidTicks-1)
	if w.ConductorAt(10, 64, 10) {
		t.Error("a closing shulker box is a conductor")
	}
	if got := power(); got != "0" {
		t.Errorf("the wire beside a closing box has power %s, want 0", got)
	}
	tickFor(p, clock, 1)
	if !w.ConductorAt(10, 64, 10) {
		t.Error("a closed shulker box is not a conductor")
	}
	if got := power(); got != "15" {
		t.Errorf("the wire beside the closed box has power %s, want 15", got)
	}
}
