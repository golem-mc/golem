package blocks

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"math"
	"slices"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/registry"
)

// The side effects a player break carries beyond the block itself: the fire's
// extinguish event, the decorated pot's crack, the unstable TNT's prime and
// the drop a click throws from the face it was made on.

// TestBreakingFireExtinguishesIt checks the fire's own playerWillDestroy: the
// break plays the extinguish event 1009 to everyone and, its
// spawnDestroyParticles being empty, no 2001.
func TestBreakingFireExtinguishesIt(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Digger(t, protocol.ItemStack{}, "minecraft:fire", nil, true)

	c.StartDigging(servertest.DigTarget[0], servertest.DigTarget[1], servertest.DigTarget[2])
	got := c.ReadBreak(servertest.DigTarget[0], servertest.DigTarget[1], servertest.DigTarget[2], func(b servertest.BreakWire) bool {
		return b.Air && b.Events[protocol.WorldEventFireExtinguish]+b.Events[protocol.WorldEventBlockBreak] > 0
	})
	if !got.Air {
		t.Fatal("the fire was never broken")
	}
	if got.Events[protocol.WorldEventFireExtinguish] == 0 {
		t.Error("the break never played the extinguish event 1009")
	}
	if got.Events[protocol.WorldEventBlockBreak] != 0 {
		t.Errorf("the fire's break played %d block-break events, want none: its spawnDestroyParticles is empty",
			got.Events[protocol.WorldEventBlockBreak])
	}
	servertest.ExpectAir(t, srv, servertest.DigTarget)
}

// TestAToolCracksADecoratedPot checks the pot's own playerWillDestroy: an item
// in #breaks_decorated_pots cracks the pot before the drop is rolled, so the
// loot table gives the sherds - four bricks for a pot placed from a plain
// item, its default pot_decorations - and the break event carries the cracked
// state.
// vanilla: Items.DECORATED_POT, DecoratedPotBlockEntity.applyImplicitComponents
func TestAToolCracksADecoratedPot(t *testing.T) {
	t.Parallel()
	srv, c, x, y, z := placedPot(t, servertest.ItemStack(t, "minecraft:decorated_pot", 1),
		servertest.ItemStack(t, "minecraft:wooden_pickaxe", 1))

	brick, ok := registry.ItemID("minecraft:brick")
	if !ok {
		t.Fatal("the item registry has no brick")
	}

	c.BreakBlock(x, y, z)
	got := c.ReadBreak(x, y, z, func(b servertest.BreakWire) bool {
		return b.Air && len(b.Drops) == 4
	})
	if !got.Air {
		t.Fatal("the pot was never broken")
	}
	if got.BlockEventSeen && (registry.BlockOf(got.BlockEventData) != registry.BlockDecoratedPot ||
		!registry.StateBool(got.BlockEventData, registry.PropCracked)) {
		t.Errorf("the break event named state %d, want a cracked pot", got.BlockEventData)
	}
	if len(got.Drops) != 4 {
		t.Fatalf("the cracked pot dropped %d stacks, want one brick per face", len(got.Drops))
	}
	for i, drop := range got.Drops {
		if drop.ItemID != brick || drop.Count != 1 {
			t.Errorf("sherd %d = %+v, want one brick", i, drop)
		}
	}
	if registry.BlockOf(srv.BlockAt(x, y, z)) != registry.BlockAir {
		t.Error("the pot is still standing")
	}
}

// sherdPotSides are a decorated pot's faces, indexed back, left, right,
// front, for the shattered-pot tests: three sherds and a brick.
func sherdPotSides(t *testing.T) [4]protocol.ItemStack {
	t.Helper()
	var sides [4]protocol.ItemStack
	for i, name := range []string{
		"minecraft:angler_pottery_sherd", "minecraft:arms_up_pottery_sherd",
		"minecraft:brick", "minecraft:blade_pottery_sherd",
	} {
		sides[i] = servertest.ItemStack(t, name, 1)
	}
	return sides
}

// expectPotSherds checks a shattered pot's drops: one of each face's item,
// in any order.
func expectPotSherds(t *testing.T, drops []string) {
	t.Helper()
	want := []string{"minecraft:angler_pottery_sherd", "minecraft:arms_up_pottery_sherd",
		"minecraft:blade_pottery_sherd", "minecraft:brick"}
	got := slices.Clone(drops)
	slices.Sort(got)
	if !slices.Equal(got, want) {
		t.Errorf("the shattered pot dropped %v, want one of each %v", got, want)
	}
}

// placedPot has a survival player place pot by hand at the dig target and
// then hold tool, empty for a bare hand.
func placedPot(t *testing.T, pot, tool protocol.ItemStack) (*server.Server, *servertest.Client, int32, int32, int32) {
	t.Helper()
	srv, c := servertest.Join(t, servertest.SurvivalConfig(), "Digger")
	c.MoveToGround(0.5, 64, 0.5, true)
	srv.GiveSlot("Digger", player.HotbarSlot, pot)
	c.Settle()
	x, y, z := servertest.DigTarget[0], servertest.DigTarget[1], servertest.DigTarget[2]

	c.PlaceBlock(x, y-1, z, protocol.FaceTop)
	c.Settle()
	if registry.BlockOf(srv.BlockAt(x, y, z)) != registry.BlockDecoratedPot {
		t.Fatal("the pot was never placed")
	}
	srv.GiveSlot("Digger", player.HotbarSlot, tool)
	c.Settle()
	return srv, c, x, y, z
}

// sherdPot is a decorated pot item whose sides are sherdPotSides.
func sherdPot(t *testing.T) protocol.ItemStack {
	t.Helper()
	pot := servertest.ItemStack(t, "minecraft:decorated_pot", 1)
	if !pot.SetPotDecorations(sherdPotSides(t)) {
		t.Fatal("the pot decorations were refused")
	}
	return pot
}

// TestAShatteredDecoratedPotDropsItsSherds places a pot crafted from three
// sherds and a brick and cracks it with a pickaxe: the loot table's sherds
// are the items on its faces, not four bricks.
// vanilla: DecoratedPotBlock.getDrops
func TestAShatteredDecoratedPotDropsItsSherds(t *testing.T) {
	t.Parallel()
	_, c, x, y, z := placedPot(t, sherdPot(t), servertest.ItemStack(t, "minecraft:wooden_pickaxe", 1))

	c.BreakBlock(x, y, z)
	got := c.ReadBreak(x, y, z, func(b servertest.BreakWire) bool {
		return b.Air && len(b.Drops) == 4
	})
	if !got.Air {
		t.Fatal("the pot was never broken")
	}
	if len(got.Drops) != 4 {
		t.Fatalf("the shattered pot dropped %d stacks, want one per face", len(got.Drops))
	}
	names := make([]string, 0, len(got.Drops))
	for _, drop := range got.Drops {
		if drop.Count != 1 {
			t.Errorf("drop %+v, want a single item", drop)
		}
		name, _ := registry.ItemName(drop.ItemID)
		names = append(names, name)
	}
	expectPotSherds(t, names)
}

// TestSilkTouchSparesADecoratedPot checks the enchantment on top of the tag:
// a tool in #breaks_decorated_pots that carries prevents_decorated_pot_shattering
// leaves the pot whole.
func TestSilkTouchSparesADecoratedPot(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Digger(t, servertest.EnchantedStack(t, "minecraft:wooden_pickaxe", "minecraft:silk_touch", 1),
		"minecraft:decorated_pot", nil, true)

	pot, ok := registry.ItemID("minecraft:decorated_pot")
	if !ok {
		t.Fatal("the item registry has no decorated pot")
	}

	c.BreakBlock(servertest.DigTarget[0], servertest.DigTarget[1], servertest.DigTarget[2])
	got := c.ReadBreak(servertest.DigTarget[0], servertest.DigTarget[1], servertest.DigTarget[2], func(b servertest.BreakWire) bool {
		return b.Air && len(b.Drops) == 1
	})
	if !got.Air {
		t.Fatal("the pot was never broken")
	}
	if len(got.Drops) != 1 || got.Drops[0].ItemID != pot {
		t.Fatalf("the silk-touched pot dropped %+v, want the pot item itself", got.Drops)
	}
	servertest.ExpectAir(t, srv, servertest.DigTarget)
}

// TestBreakingUnstableTNTPrimesIt checks TntBlock.playerWillDestroy: a
// survival break of an unstable TNT stands a primed body up where it stood
// instead of just leaving the hole.
func TestBreakingUnstableTNTPrimesIt(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Digger(t, protocol.ItemStack{}, "minecraft:tnt", nil, true)

	unstable, ok := registry.FindState("minecraft:tnt", map[string]string{"unstable": "true"})
	if !ok {
		t.Fatal("the registry has no unstable tnt")
	}
	if !srv.SetBlock(servertest.DigTarget[0], servertest.DigTarget[1], servertest.DigTarget[2], unstable) {
		t.Fatal("the world did not take the unstable tnt")
	}
	c.Settle()

	c.BreakBlock(servertest.DigTarget[0], servertest.DigTarget[1], servertest.DigTarget[2])
	got := c.ReadBreak(servertest.DigTarget[0], servertest.DigTarget[1], servertest.DigTarget[2], func(b servertest.BreakWire) bool {
		return b.Air && slices.Contains(b.Spawns, protocol.EntityTypeTnt)
	})
	if !got.Air {
		t.Fatal("the tnt was never broken")
	}
	if !slices.Contains(got.Spawns, protocol.EntityTypeTnt) {
		t.Error("the unstable tnt never stood a primed body up")
	}
	for _, drop := range got.Drops {
		if name, ok := registry.ItemName(drop.ItemID); ok && name == "minecraft:tnt" {
			t.Errorf("the priming tnt also dropped %+v, want no item", drop)
		}
	}
}

// TestBreakingStableTNTLeavesItsItem checks the other half of the rule: only
// the unstable state primes, and a stable TNT breaks to its item.
func TestBreakingStableTNTLeavesItsItem(t *testing.T) {
	t.Parallel()
	_, c := servertest.Digger(t, protocol.ItemStack{}, "minecraft:tnt", nil, true)

	tnt, ok := registry.ItemID("minecraft:tnt")
	if !ok {
		t.Fatal("the item registry has no tnt")
	}

	c.BreakBlock(servertest.DigTarget[0], servertest.DigTarget[1], servertest.DigTarget[2])
	got := c.ReadBreak(servertest.DigTarget[0], servertest.DigTarget[1], servertest.DigTarget[2], func(b servertest.BreakWire) bool {
		return b.Air && len(b.Drops) == 1
	})
	if !got.Air {
		t.Fatal("the tnt was never broken")
	}
	if slices.Contains(got.Spawns, protocol.EntityTypeTnt) {
		t.Error("the stable tnt stood a primed body up, want no prime")
	}
	if len(got.Drops) != 1 || got.Drops[0].ItemID != tnt {
		t.Fatalf("the stable tnt dropped %+v, want the tnt item", got.Drops)
	}
}

// TestAHoeDropsRootsTowardTheClickedFace checks HoeItem's face drop: the
// hanging roots a tilled rooted dirt gives are thrown from the face clicked,
// a quarter block plus the item's half width beyond the block, not from its
// centre.
func TestAHoeDropsRootsTowardTheClickedFace(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Camper", servertest.OpsListing(t, "Camper"))

	srv.StopTick()

	servertest.SetFarmBlock(t, c, srv, 6, 63, 0, "minecraft:rooted_dirt", nil)
	servertest.SetFarmBlock(t, c, srv, 6, 64, 0, "minecraft:stone", nil)
	servertest.GiveFarmItem(t, c, "minecraft:iron_hoe")
	c.PlaceBlock(6, 63, 0, protocol.FaceNorth)

	dirt := servertest.CropsStateOf(t, "minecraft:dirt", nil)
	servertest.AwaitBlockUpdate(t, c, 6, 63, 0, dirt)
	roots, _ := registry.ItemID("minecraft:hanging_roots")
	drop := c.ExpectDroppedItem()
	if drop.Stack.ItemID != roots || drop.Stack.Count != 1 {
		t.Fatalf("the till dropped %+v, want one hanging_roots (%d)", drop.Stack, roots)
	}
	// North is -Z: the drop's centre sits 0.5 + 0.125 past the block face,
	// and it is thrown out with a tenth of a block that way.
	if math.Abs(drop.Z-(0.5-(0.5+0.125))) > 1e-6 {
		t.Errorf("the roots spawned at z=%v, want the face drop's %v", drop.Z, 0.5-0.625)
	}
	if math.Abs(drop.Vz-(-0.1)) > 1e-3 {
		t.Errorf("the roots' z push = %v, want the face drop's -0.1", drop.Vz)
	}
	if drop.Vy < 0 || drop.Vy > 0.1 {
		t.Errorf("the roots' lift = %v, want the face drop's nothing to a tenth", drop.Vy)
	}
	if drop.X < 6.25 || drop.X > 6.75 {
		t.Errorf("the roots spawned at x=%v, want a quarter block of jitter about the centre", drop.X)
	}
	if got := srv.BlockAt(6, 63, 0); got != dirt {
		t.Errorf("the tilled rooted dirt = %d, want %d (dirt)", got, dirt)
	}
}

// popOffEvents breaks the stone at DigTarget with a block that cannot stay
// without it offset cells away on the far side (offset -1 stands the block
// above, 1 hangs it below), and returns the block-break states heard and the
// items dropped. The breaker does not hear its own break.
func popOffEvents(t *testing.T, block string, props map[string]string, offset int32) (states []uint16, drops int) {
	t.Helper()
	srv, c := servertest.Digger(t, protocol.ItemStack{}, "minecraft:stone", nil, true)
	x, y, z := servertest.DigTarget[0], servertest.DigTarget[1], servertest.DigTarget[2]
	state, ok := registry.FindState(block, props)
	if !ok {
		t.Fatalf("no %s state with %v", block, props)
	}
	if !srv.SetBlock(x, y-offset, z, state) {
		t.Fatalf("could not stand the %s", block)
	}
	c.Settle()
	c.BreakBlock(x, y, z)
	c.Pump(servertest.ExpectTimeout, nil, func(id int32, data []byte) bool {
		switch id {
		case protocol.IDPlayWorldEvent:
			r := protocol.NewReader(data)
			if r.ReadI32() == protocol.WorldEventBlockBreak {
				r.ReadPosition()
				states = append(states, uint16(r.ReadI32()))
			}
		case protocol.IDPlaySpawnEntity:
			if _, typ := servertest.SpawnEntityHead(data); typ == protocol.EntityTypeItem {
				drops++
			}
		}
		return len(states) > 0 && drops > 0
	})
	c.Settle()
	return states, drops
}

// TestPoppedOffBlocksPlayTheBreakEvent checks Level.destroyBlock's level event
// 2001 for a block a shape update removes: the lantern under or over a broken
// block and a torch on one play the break sound and particles once, and the
// item still drops.
func TestPoppedOffBlocksPlayTheBreakEvent(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		block  string
		props  map[string]string
		offset int32
	}{
		{"standing lantern", "minecraft:lantern", map[string]string{"hanging": "false"}, -1},
		{"hanging lantern", "minecraft:lantern", map[string]string{"hanging": "true"}, 1},
		{"torch", "minecraft:torch", nil, -1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			states, drops := popOffEvents(t, tc.block, tc.props, tc.offset)
			want, _ := registry.FindState(tc.block, tc.props)
			if len(states) != 1 || states[0] != want {
				t.Errorf("break events carried states %v, want [%d]", states, want)
			}
			if drops != 1 {
				t.Errorf("%d item drops, want 1", drops)
			}
		})
	}
}
