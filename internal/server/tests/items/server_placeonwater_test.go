package items

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/registry"
)

// waterLily joins a survival player holding count of the item at the first
// hotbar slot. A test moves it above the pad's cell: the pad has a collision
// shape, so a body in that cell refuses it.
func waterLily(t *testing.T, item string, count int32) (*server.Server, *servertest.Client) {
	t.Helper()
	return waterLilyIn(t, "survival", item, count)
}

// waterLilyIn is waterLily for a player in the game mode named.
func waterLilyIn(t *testing.T, mode, item string, count int32) (*server.Server, *servertest.Client) {
	t.Helper()
	cfg := servertest.SurvivalTestConfig()
	cfg.GameMode = mode
	srv, c := servertest.Join(t, cfg, "Lily")
	srv.GiveSlot("Lily", player.HotbarSlot, protocol.ItemStack{ItemID: servertest.ItemIDOrDie(t, item), Count: count})
	return srv, c
}

func stateOrDie(t *testing.T, name string, props map[string]string) uint16 {
	t.Helper()
	s, ok := registry.FindState(name, props)
	if !ok {
		t.Fatalf("the block registry has no %s", name)
	}
	return s
}

// TestALilyPadLaysOnAWaterSurface: Use Item aimed down at a water source puts
// the pad in the cell above it and spends one of the stack.
// vanilla: PlaceOnWaterBlockItem.use
func TestALilyPadLaysOnAWaterSurface(t *testing.T) {
	t.Parallel()
	for _, item := range []string{"minecraft:lily_pad", "minecraft:frogspawn"} {
		t.Run(item, func(t *testing.T) {
			t.Parallel()
			srv, c := waterLily(t, item, 3)
			srv.SetBlock(4, 63, 4, stateOrDie(t, "minecraft:water", nil))
			c.Settle()
			c.MoveStep(4.5, 65, 4.5, true)
			servertest.UseHeld(c, 0, 90)

			want := stateOrDie(t, item, nil)
			servertest.WaitFor(t, srv, 100, func() bool { return srv.BlockAt(4, 64, 4) == want })
			c.Settle() // the stack is spent after the pad is laid
			if got := srv.InventorySlot("Lily", player.HotbarSlot).Count; got != 2 {
				t.Errorf("the stack after the click holds %d, want 2", got)
			}
		})
	}
}

// TestALilyPadLaysNowhereElse: land, flowing water and a clicked block hold
// no pad, and the stack stays whole.
func TestALilyPadLaysNowhereElse(t *testing.T) {
	t.Parallel()
	srv, c := waterLily(t, "minecraft:lily_pad", 3)
	pad := stateOrDie(t, "minecraft:lily_pad", nil)
	c.MoveStep(4.5, 65, 4.5, true)

	// Stone below: the stone is no support.
	srv.SetBlock(4, 63, 4, stateOrDie(t, "minecraft:stone", nil))
	c.Settle()
	servertest.UseHeld(c, 0, 90)
	// Flowing water below.
	srv.SetBlock(4, 63, 4, stateOrDie(t, "minecraft:water", map[string]string{"level": "1"}))
	c.Settle()
	servertest.UseHeld(c, 0, 90)
	// Use Item On against the face of a block is the item's own useOn: a pass.
	srv.SetBlock(4, 63, 4, stateOrDie(t, "minecraft:stone", nil))
	c.Settle()
	c.PlaceBlock(4, 63, 4, protocol.FaceTop)
	c.Settle()

	if got := srv.BlockAt(4, 64, 4); got == pad {
		t.Errorf("a pad lay where none can stand")
	}
	if got := srv.InventorySlot("Lily", player.HotbarSlot).Count; got != 3 {
		t.Errorf("the stack holds %d, want 3", got)
	}
}

// TestALilyPadLaysOnIce: ice is in supports_lily_pad.
func TestALilyPadLaysOnIce(t *testing.T) {
	t.Parallel()
	srv, c := waterLily(t, "minecraft:lily_pad", 1)
	srv.SetBlock(4, 63, 4, stateOrDie(t, "minecraft:ice", nil))
	c.Settle()
	c.MoveStep(4.5, 65, 4.5, true)
	servertest.UseHeld(c, 0, 90)
	want := stateOrDie(t, "minecraft:lily_pad", nil)
	servertest.WaitFor(t, srv, 100, func() bool { return srv.BlockAt(4, 64, 4) == want })
}

// TestAPlaceOnWaterItemKeepsItsStackInCreative: creative lays the pad and
// keeps the item.
func TestAPlaceOnWaterItemKeepsItsStackInCreative(t *testing.T) {
	t.Parallel()
	srv, c := waterLilyIn(t, "creative", "minecraft:lily_pad", 3)
	srv.SetBlock(4, 63, 4, stateOrDie(t, "minecraft:water", nil))
	c.Settle()
	c.MoveStep(4.5, 65, 4.5, true)
	servertest.UseHeld(c, 0, 90)
	want := stateOrDie(t, "minecraft:lily_pad", nil)
	servertest.WaitFor(t, srv, 100, func() bool { return srv.BlockAt(4, 64, 4) == want })
	c.Settle() // a stack spent would be spent after the pad is laid
	if got := srv.InventorySlot("Lily", player.HotbarSlot).Count; got != 3 {
		t.Errorf("the creative stack holds %d, want 3", got)
	}
}

// TestAPlaceOnWaterItemLaysInAdventure: PlaceOnWaterBlockItem.use never asks
// whether the hand may build, so an adventure hand without can_place_on lays
// the pad.
// vanilla: PlaceOnWaterBlockItem.use
func TestAPlaceOnWaterItemLaysInAdventure(t *testing.T) {
	t.Parallel()
	srv, c := waterLilyIn(t, "adventure", "minecraft:lily_pad", 2)
	srv.SetBlock(4, 63, 4, stateOrDie(t, "minecraft:water", nil))
	c.Settle()
	c.MoveStep(4.5, 65, 4.5, true)
	servertest.UseHeld(c, 0, 90)
	want := stateOrDie(t, "minecraft:lily_pad", nil)
	servertest.WaitFor(t, srv, 100, func() bool { return srv.BlockAt(4, 64, 4) == want })
	c.Settle() // the stack is spent after the pad is laid
	if got := srv.InventorySlot("Lily", player.HotbarSlot).Count; got != 1 {
		t.Errorf("the adventure stack holds %d, want 1", got)
	}
}

// TestFrogspawnLaysNotOnIce: frogspawn's support tag is empty, so ice that
// holds a lily pad refuses it.
func TestFrogspawnLaysNotOnIce(t *testing.T) {
	t.Parallel()
	srv, c := waterLily(t, "minecraft:frogspawn", 2)
	srv.SetBlock(4, 63, 4, stateOrDie(t, "minecraft:ice", nil))
	c.Settle()
	c.MoveStep(4.5, 65, 4.5, true)
	servertest.UseHeld(c, 0, 90)
	c.Settle()
	if got := srv.BlockAt(4, 64, 4); got == stateOrDie(t, "minecraft:frogspawn", nil) {
		t.Errorf("frogspawn lay on ice")
	}
	if got := srv.InventorySlot("Lily", player.HotbarSlot).Count; got != 2 {
		t.Errorf("the stack holds %d, want 2", got)
	}
}

// TestALilyPadLaysOnTheTopWater: with a source above a source the ray stops
// at the upper one and the pad lies over it.
func TestALilyPadLaysOnTheTopWater(t *testing.T) {
	t.Parallel()
	srv, c := waterLily(t, "minecraft:lily_pad", 2)
	water := stateOrDie(t, "minecraft:water", nil)
	srv.SetBlock(4, 63, 4, water)
	srv.SetBlock(4, 64, 4, water)
	c.Settle()
	c.MoveStep(4.5, 66, 4.5, true)
	servertest.UseHeld(c, 0, 90)
	want := stateOrDie(t, "minecraft:lily_pad", nil)
	servertest.WaitFor(t, srv, 100, func() bool { return srv.BlockAt(4, 65, 4) == want })
	if got := srv.BlockAt(4, 64, 4); got != water {
		t.Errorf("the upper water was replaced")
	}
}

// TestALilyPadPopsWhenItsWaterGoes: stone placed into the water source under
// a lily pad pops the pad with its one item; frogspawn goes with nothing,
// its loot table being empty. A block set on the pad leaves it standing.
// vanilla: VegetationBlock.updateShape, FrogspawnBlock.updateShape
func TestALilyPadPopsWhenItsWaterGoes(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		item  string
		drops bool
	}{{"minecraft:lily_pad", true}, {"minecraft:frogspawn", false}} {
		t.Run(tc.item, func(t *testing.T) {
			t.Parallel()
			srv, c := waterLily(t, tc.item, 1)
			stone := stateOrDie(t, "minecraft:stone", nil)
			srv.SetBlock(4, 63, 4, stateOrDie(t, "minecraft:water", nil))
			srv.SetBlock(5, 63, 4, stone)
			srv.SetBlock(5, 64, 4, stone)
			c.Settle()
			c.MoveStep(4.5, 65, 4.5, true)
			servertest.UseHeld(c, 0, 90)
			c.Settle()
			want := stateOrDie(t, tc.item, nil)
			servertest.WaitFor(t, srv, 100, func() bool { return srv.BlockAt(4, 64, 4) == want })

			srv.GiveSlot("Lily", player.HotbarSlot, protocol.ItemStack{ItemID: servertest.ItemIDOrDie(t, "minecraft:stone"), Count: 2})
			c.MoveStep(5.5, 65, 4.5, true)
			c.PlaceBlock(4, 64, 4, protocol.FaceTop)
			c.Settle()
			servertest.WaitFor(t, srv, 100, func() bool { return srv.BlockAt(4, 65, 4) == stone })
			c.Settle()
			if got := srv.BlockAt(4, 64, 4); got != want {
				t.Fatalf("a block set on the %s left %d there, want it standing", tc.item, got)
			}

			c.PlaceBlock(5, 63, 4, protocol.FaceWest)
			c.Settle()
			servertest.WaitFor(t, srv, 100, func() bool { return srv.BlockAt(4, 63, 4) == stone })
			servertest.WaitFor(t, srv, 100, func() bool { return srv.BlockAt(4, 64, 4) != want })
			c.Settle()
			servertest.ExpectAir(t, srv, [3]int32{4, 64, 4})
			if tc.drops {
				if it := servertest.ExpectOneDroppedItem(t, srv, tc.item); it.Count != 1 {
					t.Fatalf("the pad dropped %d items, want 1", it.Count)
				}
			} else {
				servertest.ExpectNoDroppedItems(t, srv)
			}
		})
	}
}

// TestALilyPadIsRefusedIntoABodysCell: a pad has a collision shape, so one
// aimed into the cell the player's feet are in is not laid, and the stack
// stays whole; a plant with no shape (frogspawn) is laid there.
// vanilla: BlockItem.canPlace, Level.isUnobstructed
func TestALilyPadIsRefusedIntoABodysCell(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		item string
		laid bool
	}{{"minecraft:lily_pad", false}, {"minecraft:frogspawn", true}} {
		t.Run(tc.item, func(t *testing.T) {
			t.Parallel()
			srv, c := waterLily(t, tc.item, 2)
			srv.SetBlock(4, 63, 4, stateOrDie(t, "minecraft:water", nil))
			c.Settle()
			c.MoveStep(4.5, 64, 4.5, true)
			servertest.UseHeld(c, 0, 90)
			c.Settle()
			want, count := stateOrDie(t, tc.item, nil), int32(2)
			if tc.laid {
				count = 1
			}
			if got := srv.BlockAt(4, 64, 4) == want; got != tc.laid {
				t.Errorf("%s laid into the player's cell = %v, want %v", tc.item, got, tc.laid)
			}
			if got := srv.InventorySlot("Lily", player.HotbarSlot).Count; got != count {
				t.Errorf("the stack holds %d, want %d", got, count)
			}
		})
	}
}
