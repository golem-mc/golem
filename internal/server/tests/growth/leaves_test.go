package growth

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"strconv"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/registry"
)

// The leaves, black-box: the leaf a player places standing persistent at
// the distance the logs around it make, and the leaf the random ticks rot,
// its loot's to roll. The decay stops the main loop and drives the passes
// by hand against a pinned seed, the way the fire's tests do, and the
// loot's dice are pinned beside the engine's so the roll is one the test
// knows.

// leafSeed is the seed the leaf tests pin the dice with.
const leafSeed = int64(9)

// leafStateProps reads the properties of a leaf state for the test to
// judge, failing it when the state is not a leaf's.
func leafStateProps(t *testing.T, state uint16) map[string]string {
	t.Helper()
	_, props, ok := registry.BlockStateName(state)
	if !ok || !registry.BlockOf(state).IsLeaves() {
		t.Fatalf("block state %d is not a leaf", state)
	}
	return registry.ParseStateProps(props)
}

// setLeafBlock lays a leaf block with /setblock and the properties given,
// the way an operator would set the state exactly, and waits for the
// update that says it landed.
func setLeafBlock(t *testing.T, c *servertest.Client, x, y, z int32, overrides map[string]string) uint16 {
	t.Helper()
	state, ok := registry.FindState("minecraft:oak_leaves", overrides)
	if !ok {
		t.Fatalf("the registry has no oak leaves with %v", overrides)
	}
	word := "oak_leaves"
	if len(overrides) > 0 {
		word += "["
		first := true
		for k, v := range overrides {
			if !first {
				word += ","
			}
			word += k + "=" + v
			first = false
		}
		word += "]"
	}
	c.ChatCommand("setblock " + strconv.FormatInt(int64(x), 10) + " " +
		strconv.FormatInt(int64(y), 10) + " " + strconv.FormatInt(int64(z), 10) + " " + word)
	servertest.AwaitBlockUpdate(t, c, x, y, z, state)
	return state
}

// giveLeaves puts oak leaves in the player's hand.
func giveLeaves(t *testing.T, c *servertest.Client) {
	t.Helper()
	leafID, ok := registry.ItemID("minecraft:oak_leaves")
	if !ok {
		t.Fatal("the item registry has no oak leaves")
	}
	c.SetCreativeSlot(player.HotbarSlot, protocol.ItemStack{ItemID: leafID, Count: 1})
}

// TestPlacingLeavesMarksThemPersistent checks the placement: the leaf a
// player puts down is persistent, and stands at the distance the logs and
// leaves around the position make — one beside a log, two beside a leaf
// that a log holds at one, seven in the open — the state the placement's
// own walk gives.
func TestPlacingLeavesMarksThemPersistent(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Camper", servertest.OpsListing(t, "Camper"))

	// A log to hold leaves at one, laid the way an operator lays one.
	servertest.Setblock(t, c, 7, 64, 0, "oak_log")

	giveLeaves(t, c)

	// The first leaf goes against the stone top beside the log, and lands
	// persistent at the distance one the log beside it makes.
	c.PlaceBlock(6, 63, 0, protocol.FaceTop)
	c.ExpectBlockAck()
	props := leafStateProps(t, srv.BlockAt(6, 64, 0))
	if props["persistent"] != "true" {
		t.Errorf("the leaf beside the log is persistent=%s, want true", props["persistent"])
	}
	if props["distance"] != "1" {
		t.Errorf("the leaf beside the log stands at distance=%s, want 1", props["distance"])
	}

	// The second goes a step further out, where the first leaf holds it at
	// two: the distance chains through the leaves, the way the walk out
	// from a log counts them.
	c.PlaceBlock(5, 63, 0, protocol.FaceTop)
	c.ExpectBlockAck()
	props = leafStateProps(t, srv.BlockAt(5, 64, 0))
	if props["persistent"] != "true" {
		t.Errorf("the leaf beside the leaf is persistent=%s, want true", props["persistent"])
	}
	if props["distance"] != "2" {
		t.Errorf("the leaf beside the leaf stands at distance=%s, want 2", props["distance"])
	}

	// The third goes in the open, where nothing holds it: seven, the
	// distance the walk cannot better.
	c.PlaceBlock(3, 63, 0, protocol.FaceTop)
	c.ExpectBlockAck()
	props = leafStateProps(t, srv.BlockAt(3, 64, 0))
	if props["persistent"] != "true" {
		t.Errorf("the leaf in the open is persistent=%s, want true", props["persistent"])
	}
	if props["distance"] != "7" {
		t.Errorf("the leaf in the open stands at distance=%s, want 7", props["distance"])
	}
}

// TestDecayingLeafDropsItsLoot checks the decay end to end: a lone
// non-persistent leaf at the distance seven nothing holds it back from is
// what a random tick pass takes, the world left air where it stood, the
// client told of the block it became, and what its loot table rolled stood
// up beside it on the wire. The dice are pinned, so the pass's finding of
// the leaf and the table's roll are both ones the test knows.
func TestDecayingLeafDropsItsLoot(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Camper", servertest.OpsListing(t, "Camper"))

	// The main loop stopped, the engine's dice pinned to the test's seed.
	srv.StopTick()
	servertest.SeedPhysics(t, srv, leafSeed)

	// A lone leaf, set the way an operator sets a state exactly:
	// non-persistent, at the default seven, nothing around to hold it.
	leaf := setLeafBlock(t, c, 5, 64, 0, map[string]string{"persistent": "false"})
	if props := leafStateProps(t, leaf); props["distance"] != "7" {
		t.Fatalf("the registry's non-persistent oak leaf stands at distance=%s, want 7", props["distance"])
	}

	// The loot's dice pinned before the passes run, so the roll the decay
	// makes is the seed's.
	srv.LootSeed(leafSeed)

	// The passes, driven by hand: the leaf rots the first pass that lands
	// on it, and the passes after that find air.
	srv.TickRandomTicks(20000)

	// The world says air, and the client was told.
	if got := srv.BlockAt(5, 64, 0); got != 0 {
		t.Fatalf("the leaf after the passes = %d, want 0 (air)", got)
	}
	got := c.ExpectBlockUpdate()
	if got.X != 5 || got.Y != 64 || got.Z != 0 {
		t.Errorf("block update at (%d, %d, %d), want (5, 64, 0)", got.X, got.Y, got.Z)
	}
	if got.State != 0 {
		t.Errorf("block state after the decay = %d, want 0 (air)", got.State)
	}

	// What the loot table rolled for the leaf, read off the wire: with the
	// dice pinned, exactly what the seed gives, which for this one is the
	// sapling the table's low chance turned up.
	var drops []protocol.ItemStack
	servertest.CollectDropsTicks(c, srv, 0, func(stack protocol.ItemStack) bool {
		drops = append(drops, stack)
		return false
	})
	if len(drops) != 1 {
		t.Fatalf("the decay laid down %d drops, want 1", len(drops))
	}
	name, _ := registry.ItemName(drops[0].ItemID)
	if name != "minecraft:oak_sapling" {
		t.Errorf("the decay dropped %s, want minecraft:oak_sapling", name)
	}
	if drops[0].Count != 1 {
		t.Errorf("the decay dropped %d saplings, want 1", drops[0].Count)
	}
}

// TestWaterloggedLeafUpdatesItsDistanceAndDecays checks that a waterlogged
// leaf is not spent on its water visit: with the log beside it gone, its
// distance tick recomputes seven the way a dry leaf's does, and the random
// ticks then rot it. A drain that takes the visit for the fluid's alone
// leaves the leaf at its old distance forever.
func TestWaterloggedLeafUpdatesItsDistanceAndDecays(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Camper", servertest.OpsListing(t, "Camper"))

	srv.StopTick()
	srv.SetPhysicsSeed(leafSeed)
	srv.LootSeed(leafSeed)

	// A log with a waterlogged non-persistent leaf beside it, set the way
	// an operator sets a state exactly.
	servertest.Setblock(t, c, 6, 64, 0, "oak_log")
	leaf := setLeafBlock(t, c, 5, 64, 0, map[string]string{
		"distance": "1", "persistent": "false", "waterlogged": "true",
	})
	if props := leafStateProps(t, leaf); props["waterlogged"] != "true" {
		t.Fatalf("the leaf is waterlogged=%s, want true", props["waterlogged"])
	}

	// The log goes, and the leaf's distance tick recomputes it.
	servertest.Setblock(t, c, 6, 64, 0, "air")
	srv.TickPhysics(6)
	props := leafStateProps(t, srv.BlockAt(5, 64, 0))
	if props["distance"] != "7" {
		t.Fatalf("the waterlogged leaf stands at distance=%s after the log went, want 7", props["distance"])
	}
	if props["waterlogged"] != "true" {
		t.Errorf("the leaf is waterlogged=%s, want true", props["waterlogged"])
	}

	// At seven the random ticks rot it, as they rot a dry leaf — and the
	// water it held stays behind, the fluid vanilla's removeBlock writes.
	srv.TickRandomTicks(20000)
	name, _, ok := registry.BlockStateName(srv.BlockAt(5, 64, 0))
	if !ok || name != "minecraft:water" {
		t.Fatalf("the decayed waterlogged leaf became %q, want minecraft:water", name)
	}
}
