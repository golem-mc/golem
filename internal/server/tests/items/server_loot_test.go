package items

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/registry"
)

// The loot tables at the wire: what a break or a death lays down, driven
// the way every black-box test here is — a real connection, real packets —
// with the tables' own dice seeded through the server's LootSeed so a
// failing chance is reproducible. The engine's formulas have their unit
// tests in internal/server/rules; these pin the wiring: the tool in the breaking
// hand reaching the table, the enchantments on it reaching its conditions
// and functions, and the killer's weapon reaching a mob's.

// enchantStack is a stack of the named item carrying one enchantment at a
// level, the way /give with a component list would leave one.
func enchantStack(t *testing.T, item, enchantment string, level int32) protocol.ItemStack {
	t.Helper()
	id, ok := registry.ItemID(item)
	if !ok {
		t.Fatalf("the item registry has no %s", item)
	}
	ench, ok := registry.EnchantmentID(enchantment)
	if !ok {
		t.Fatalf("the enchantment registry has no %s", enchantment)
	}
	return protocol.ItemStack{ItemID: id, Count: 1, Enchantments: map[int32]int32{ench: level}}
}

// plainStack is a plain stack of one of the named item.
func plainStack(t *testing.T, item string) protocol.ItemStack {
	t.Helper()
	id, ok := registry.ItemID(item)
	if !ok {
		t.Fatalf("the item registry has no %s", item)
	}
	return protocol.ItemStack{ItemID: id, Count: 1}
}

// dropTicks is how many ticks a kill's drops get to arrive.
const dropTicks = 40

// leafTicks covers a hand's five ticks to a leaf and the break's drops.
const leafTicks = 10

// collectDrop takes the first stack of the item named off the wire, the orbs
// an ore's experience stepping past unread. What comes back says whether one
// came within servertest.ExpectTimeout.
func collectDrop(c *servertest.Client, srv *server.Server, want string) (protocol.ItemStack, bool) {
	var stack protocol.ItemStack
	found := false
	servertest.CollectDropsTicks(c, srv, 1200, func(got protocol.ItemStack) bool {
		if name, _ := registry.ItemName(got.ItemID); name == want {
			stack, found = got, true
		}
		return found
	})
	return stack, found
}

// breakAndCollect breaks the block at the position from where the player
// stands, moves the player beside it first, and collects the drop.
func breakAndCollect(c *servertest.Client, srv *server.Server, x, y, z int32, want string) (protocol.ItemStack, bool) {
	c.MoveStep(float64(x)+0.5, float64(y)+1, float64(z)+3.5, true)
	c.BreakBlock(x, y, z)
	return collectDrop(c, srv, want)
}

// TestAFortunePickaxeWidensTheDiamonds breaks a run of diamond ores with a
// fortune III pickaxe and watches what each lays down: one to four
// diamonds, never nothing and never past four, with more than the one the
// plain ore gives having come out of the run — the ore_drops formula the
// table carries, at the wire.
func TestAFortunePickaxeWidensTheDiamonds(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Dial(t, servertest.SurvivalConfig())
	c.Login("Prospector")
	c.Configure()
	c.EnterWorld("Prospector")

	ore := registry.BlockStates["minecraft:diamond_ore"]
	if ore == 0 {
		t.Fatal("the block registry has no diamond ore")
	}
	srv.GiveSlot("Prospector", player.HotbarSlot, enchantStack(t, "minecraft:diamond_pickaxe", "minecraft:fortune", 3))
	servertest.HoldTicks(t, srv)

	var sizes []int32
	for i := range 14 {
		x, y, z := int32(2), int32(63), int32(3+i)
		if !srv.SetBlock(x, y, z, ore) {
			t.Fatal("the world did not take the diamond ore")
		}
		drop, ok := breakAndCollect(c, srv, x, y, z, "minecraft:diamond")
		if !ok {
			t.Fatalf("a fortune III pickaxe broke diamond ore at (%d, %d, %d) and laid no diamonds down", x, y, z)
		}
		if drop.Count < 1 || drop.Count > 4 {
			t.Fatalf("a fortune III pickaxe rolled %d diamonds, want the vanilla bound of one to four", drop.Count)
		}
		sizes = append(sizes, drop.Count)
	}
	widened := false
	for _, size := range sizes {
		if size > 1 {
			widened = true
		}
	}
	if !widened {
		t.Errorf("fortune III rolled %v, and never past the plain ore's one diamond", sizes)
	}
}

// TestOakLeavesDropTheirChances breaks a run of oak leaves three ways:
// with shears, where the table's match_tool keeps the leaves themselves;
// bare-handed, where the chances are the only thing that falls and nothing
// else ever does; and with a fortune III hoe, whose table bonus lifts the
// sapling's chance to its top entry. The dice are seeded through the
// server's LootSeed, so the chances that come up come up the same every
// run.
func TestOakLeavesDropTheirChances(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Dial(t, servertest.SurvivalConfig(), func(s *server.Server) { s.SetAllowFlight(true) })
	c.Login("Arborist")
	c.Configure()
	c.EnterWorld("Arborist")

	// Persistent leaves, so no decay rolls the shared loot dice between
	// the breaks and the seed's chances land the same every run.
	leaves, ok := registry.FindState("minecraft:oak_leaves", map[string]string{"persistent": "true"})
	if !ok {
		t.Fatal("the block registry has no persistent oak leaves")
	}
	leafID, _ := registry.ItemID("minecraft:oak_leaves")
	srv.LootSeed(7602)
	servertest.HoldTicks(t, srv)

	// The shears: every leaf comes out a leaf, and nothing beside it.
	srv.GiveSlot("Arborist", player.HotbarSlot, plainStack(t, "minecraft:shears"))
	for i := range 20 {
		x, y, z := int32(2), int32(70), int32(3+i)
		if !srv.SetBlock(x, y, z, leaves) {
			t.Fatal("the world did not take the oak leaves")
		}
		c.MoveStep(float64(x)+0.5, float64(y)+1, float64(z)+3.5, true)
		c.BreakBlock(x, y, z)
		drop, ok := collectDrop(c, srv, "minecraft:oak_leaves")
		if !ok || drop.Count != 1 || drop.ItemID != leafID {
			t.Fatalf("shears broke oak leaves at (%d, %d, %d) into %+v, want the leaves themselves", x, y, z, drop)
		}
	}

	// Bare hands: the saplings, sticks and apples of the chances, and
	// nothing else, with the sapling's five per hundred having come up
	// inside the run.
	saplings := 0
	for i := range 40 {
		x, y, z := int32(2), int32(70), int32(30+i)
		if !srv.SetBlock(x, y, z, leaves) {
			t.Fatal("the world did not take the oak leaves back")
		}
		srv.GiveSlot("Arborist", player.HotbarSlot, protocol.ItemStack{})
		c.MoveStep(float64(x)+0.5, float64(y)+1, float64(z)+3.5, true)
		c.BreakBlock(x, y, z)
		// A hand takes five ticks to the leaf, so the break lands
		// behind the click; waiting for it keeps the next click's dig off
		// the one still waiting to be finished.
		servertest.CollectDropsTicks(c, srv, leafTicks, func(got protocol.ItemStack) bool {
			name, _ := registry.ItemName(got.ItemID)
			switch name {
			case "minecraft:oak_sapling":
				saplings++
			case "minecraft:stick", "minecraft:apple":
			default:
				t.Errorf("a hand-broken oak leaf dropped %s, which its table does not name", name)
			}
			return false
		})
	}
	// The saplings are counted where they lie, not off the client's short
	// windows, which a slow machine's late drops would slip past.
	saplings = saplingsLying(t, srv, 30, 69)
	if saplings == 0 {
		t.Errorf("forty hand-broken leaves never dropped a sapling at the table's chance")
	}

	// Fortune III: the sapling's chance is the table's top entry now, and
	// the run is long enough to tell it from the plain one's five.
	srv.GiveSlot("Arborist", player.HotbarSlot, enchantStack(t, "minecraft:iron_hoe", "minecraft:fortune", 3))
	fortuned := 0
	for i := range 40 {
		x, y, z := int32(2), int32(70), int32(90+i)
		if !srv.SetBlock(x, y, z, leaves) {
			t.Fatal("the world did not take the oak leaves again")
		}
		c.MoveStep(float64(x)+0.5, float64(y)+1, float64(z)+3.5, true)
		c.BreakBlock(x, y, z)
		// The hoe's share of a leaf takes a few ticks, so the break lands
		// behind the click; waiting for it keeps the next click's dig off
		// the one still waiting to be finished.
		servertest.CollectDropsTicks(c, srv, leafTicks, func(protocol.ItemStack) bool { return false })
	}
	fortuned = saplingsLying(t, srv, 90, 129)
	if fortuned <= saplings {
		t.Errorf("fortune III rolled %d saplings of forty leaves, want more than bare hands' %d", fortuned, saplings)
	}
}

// saplingsLying counts the oak saplings lying on the ground over the leaf
// row from z0 to z1, once the count has held still for ten ticks: the
// drops of the last breaks may still be on their way in.
func saplingsLying(t *testing.T, srv *server.Server, z0, z1 float64) int {
	t.Helper()
	count := func() int {
		n := 0
		for _, it := range srv.ItemsInDimension("minecraft:overworld") {
			if it.Item == "minecraft:oak_sapling" && it.Z >= z0-2 && it.Z <= z1+2 {
				n += int(it.Count)
			}
		}
		return n
	}
	last, since := count(), srv.TicksRun()
	servertest.WaitFor(t, srv, 300, func() bool {
		if n := count(); n != last {
			last, since = n, srv.TicksRun()
		}
		return srv.TicksRun()-since >= 10
	})
	return last
}

// TestLootingSwordGrowsTheBeef kills a run of cows with a looting III
// sword: the beef the deaths leave grows past the plain table's one to
// three, the looting roll the killer's weapon carries, and never past the
// six the vanilla bound gives.
func TestLootingSwordGrowsTheBeef(t *testing.T) {
	t.Parallel()
	cfg := servertest.HostileTestConfig()
	cfg.GameMode = "creative"

	srv, connect := servertest.Start(t, cfg)
	c := connect()
	c.Login("Butcher")
	c.Configure()
	c.EnterWorld("Butcher")

	srv.GiveSlot("Butcher", player.HotbarSlot, enchantStack(t, "minecraft:diamond_sword", "minecraft:looting", 3))

	// Eight cows, strung along the X axis clear of each other's reach.
	cowIDs := make([]int32, 8)
	for i := range cowIDs {
		cowIDs[i] = srv.SpawnMob("minecraft:cow", float64(2+i*3)+0.5, 64, 20.5)
	}
	for range 8 {
		c.ExpectMobSpawn(protocol.EntityTypeCow)
	}
	servertest.HoldTicks(t, srv)
	var beef []int32
	for i, id := range cowIDs {
		// The killing hand stands within the entity interaction's reach
		// of the cow where it has wandered to: vanilla refuses a blow past
		// the reach, and the row of cows outruns any one standing. Each
		// blow waits out the sword's whole recovery by the ticker, so a
		// slow machine still lands two whole blows; the second waits for
		// the first to have landed.
		x, _, z, ok := srv.MobPos(id)
		if !ok {
			t.Fatalf("cow %d of the row is gone before its blow", i)
		}
		c.MoveToGround(x, 64, z-1.5, true)
		servertest.AwaitFullSwing(t, srv, "Butcher")
		c.AttackLanded(srv, "Butcher", id)
		servertest.AwaitFullSwing(t, srv, "Butcher")
		c.Attack(id)
		servertest.CollectDropsTicks(c, srv, dropTicks, func(got protocol.ItemStack) bool {
			if name, _ := registry.ItemName(got.ItemID); name == "minecraft:beef" {
				beef = append(beef, got.Count)
			}
			return false
		})
	}
	if len(beef) < 4 {
		t.Fatalf("eight dead cows laid %d beef down, want the table's one-a-death at least", len(beef))
	}
	looted := false
	for _, count := range beef {
		if count < 1 || count > 6 {
			t.Fatalf("a looting III sword rolled %d beef, want the vanilla bound of one to six", count)
		}
		if count > 3 {
			looted = true
		}
	}
	if !looted {
		t.Errorf("a looting III sword rolled %v, and never past the plain table's three", beef)
	}
}
