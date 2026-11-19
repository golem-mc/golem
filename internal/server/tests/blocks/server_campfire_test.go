package blocks

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/registry"
)

// campfireState is the campfire block state with the given lit property.
func campfireState(t *testing.T, name string, lit bool) uint16 {
	t.Helper()
	want := "false"
	if lit {
		want = "true"
	}
	state, ok := registry.FindState(name, map[string]string{"lit": want})
	if !ok {
		t.Fatalf("no %s state with lit=%s", name, want)
	}
	return state
}

// countItems counts the item entities of the overworld holding the item.
func countItems(srv *server.Server, name string) int {
	n := 0
	for _, it := range srv.ItemsInDimension("minecraft:overworld") {
		if it.Item == name {
			n++
		}
	}
	return n
}

// TestACampfireCooksFoodAndDropsTheResult puts raw beef on a lit campfire: one
// leaves the hand, nothing drops until the recipe's 600 ticks are done, then
// a steak lies on the block and the slot is empty.
func TestACampfireCooksFoodAndDropsTheResult(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowed(t, servertest.SurvivalTestConfig())
	srv.StopTick()

	srv.SetBlock(3, 64, 0, campfireState(t, "minecraft:campfire", true))
	beef := servertest.ItemStack(t, "minecraft:beef", 2)
	srv.GiveSlot("Testificate", player.HotbarSlot, beef)
	w.Settle()

	w.UseBlock(3, 64, 0)
	w.Settle()
	if got := srv.InventorySlot("Testificate", player.HotbarSlot); got.Count != 1 {
		t.Errorf("the hand after placing = %+v, want one beef left", got)
	}
	slots := srv.InventoryAt(3, 64, 0)
	if len(slots) != 4 || slots[0].Count != 1 || slots[0].ItemID != beef.ItemID {
		t.Fatalf("the campfire holds %+v, want one beef in the first slot", slots)
	}

	servertest.TickBlockEntitiesByHand(srv, 599)
	if got := countItems(srv, "minecraft:cooked_beef"); got != 0 {
		t.Fatalf("%d steaks dropped before the cook time", got)
	}
	servertest.TickBlockEntitiesByHand(srv, 1)
	if got := countItems(srv, "minecraft:cooked_beef"); got != 1 {
		t.Errorf("%d steaks dropped at the cook time, want 1", got)
	}
	if slots := srv.InventoryAt(3, 64, 0); !slots[0].Empty() {
		t.Errorf("the slot after the cook = %+v, want it empty", slots[0])
	}
}

// TestACampfireTakesFourItemsAndCools fills a campfire, then lets it go out:
// a fifth item is refused, and an unlit campfire cooks nothing while its
// progress falls two a tick.
func TestACampfireTakesFourItemsAndCools(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowed(t, servertest.SurvivalTestConfig())
	srv.StopTick()

	srv.SetBlock(3, 64, 0, campfireState(t, "minecraft:campfire", true))
	srv.GiveSlot("Testificate", player.HotbarSlot, servertest.ItemStack(t, "minecraft:beef", 5))
	w.Settle()
	for range 5 {
		w.UseBlock(3, 64, 0)
		w.Settle()
	}
	if got := srv.InventorySlot("Testificate", player.HotbarSlot); got.Count != 1 {
		t.Errorf("the hand after five clicks = %+v, want one beef left", got)
	}

	servertest.TickBlockEntitiesByHand(srv, 100)
	srv.SetBlock(3, 64, 0, campfireState(t, "minecraft:campfire", false))
	servertest.TickBlockEntitiesByHand(srv, 50)
	progress := srv.CampfireProgress(3, 64, 0)
	if progress[0] != 0 {
		t.Errorf("progress after 50 unlit ticks from 100 = %d, want 0", progress[0])
	}
	servertest.TickBlockEntitiesByHand(srv, 700)
	if got := countItems(srv, "minecraft:cooked_beef"); got != 0 {
		t.Errorf("an unlit campfire dropped %d steaks", got)
	}
}

// TestACampfireBurnsAStandingPlayer stands a survival player in a lit
// campfire and a soul campfire: one and two half-hearts, never in an unlit
// one.
func TestACampfireBurnsAStandingPlayer(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		lit  bool
		want float32
	}{
		{"minecraft:campfire", true, 19},
		{"minecraft:soul_campfire", true, 18},
		{"minecraft:campfire", false, 20},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, connect := servertest.Start(t, servertest.EnvironmentConfig())
			tick := servertest.StopTicking(t, srv)
			c := connect()
			entityID := c.JoinAs("Burned")
			srv.SetFood("Burned", 17)
			if !srv.SetBlock(10, 64, 50, campfireState(t, tc.name, tc.lit)) {
				t.Fatal("the campfire's chunk was not loaded")
			}
			c.MoveToGround(10.5, 64, 50.7, true)
			c.Settle()

			tick(1)
			if !tc.lit {
				if n := servertest.DrainDamage(c); n != 0 {
					t.Errorf("an unlit campfire hurt the body %d times", n)
				}
				return
			}
			if got := c.ExpectHealth(); got.Health != tc.want {
				t.Errorf("health after one tick in the fire = %v, want %v", got.Health, tc.want)
			}
			c.ExpectDamageEvent(entityID, "minecraft:campfire", 0)
		})
	}
}

// TestACampfireBurnsAMob holds a cow in a lit campfire: it takes the
// campfire's damage type and is not set alight.
func TestACampfireBurnsAMob(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.MobTestConfig(), "Cook", servertest.OpsListing(t, "Cook"))
	c.Settle()
	srv.SetDayTime(servertest.TestMidnight)

	cow := srv.SpawnMob("minecraft:cow", 4.5, 64, 0.5)
	c.ExpectMobSpawn(protocol.EntityTypeCow)
	srv.StopTick()
	c.Settle()
	if !srv.SetMobNoAI(cow) {
		t.Fatal("the cow could not be held in the campfire")
	}
	if !srv.SetBlock(4, 64, 0, campfireState(t, "minecraft:campfire", true)) {
		t.Fatal("the campfire's chunk was not loaded")
	}

	kindID := registry.MustID("minecraft:damage_type", "minecraft:campfire")
	bites := 0
	srv.TickMobs(1)
	c.SettleAndCollect(func(id int32, data []byte) {
		if id != protocol.IDPlayDamageEvent {
			return
		}
		r := protocol.NewReader(data)
		if victim, kind := r.ReadVarInt(), r.ReadVarInt(); victim == cow && kind == kindID {
			bites++
		}
	})
	if bites != 1 {
		t.Errorf("the campfire bit the cow %d times in a tick, want 1", bites)
	}
	if ticks := srv.MobFireTicks(cow); ticks != 0 {
		t.Errorf("the cow burns for %d ticks after the campfire, want 0", ticks)
	}
}

// TestAHopperIgnoresACampfire puts a hopper under and another over a lit
// campfire holding a beef: the one below takes nothing, and the one above
// pushes nothing into the campfire.
// vanilla: CampfireBlockEntity is no Container
func TestAHopperIgnoresACampfire(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowed(t, servertest.SurvivalTestConfig())
	srv.StopTick()

	srv.SetBlock(3, 64, 0, campfireState(t, "minecraft:campfire", true))
	srv.GiveSlot("Testificate", player.HotbarSlot, servertest.ItemStack(t, "minecraft:beef", 1))
	w.Settle()
	w.UseBlock(3, 64, 0)
	w.Settle()
	if !srv.PlaceContainer(3, 63, 0, "minecraft:hopper") ||
		!srv.PlaceContainer(3, 65, 0, "minecraft:hopper") {
		t.Fatal("could not place the hoppers")
	}
	stick := servertest.ItemStack(t, "minecraft:stick", 1)
	if !srv.SetInventoryAt(3, 65, 0, 0, stick) {
		t.Fatal("could not fill the upper hopper")
	}

	servertest.TickBlockEntitiesByHand(srv, 20)
	if hopper := srv.InventoryAt(3, 63, 0); !servertest.NothingIn(hopper) {
		t.Errorf("the lower hopper holds %+v, want nothing taken from the campfire", hopper)
	}
	slots := srv.InventoryAt(3, 64, 0)
	if slots[0].Count != 1 {
		t.Errorf("the campfire's first slot = %+v, want its beef still there", slots[0])
	}
	if !slots[1].Empty() {
		t.Errorf("the campfire's second slot = %+v, want nothing pushed in", slots[1])
	}
	if hopper := srv.InventoryAt(3, 65, 0); !hopper[0].Equal(stick) {
		t.Errorf("the upper hopper holds %+v, want its stick kept", hopper)
	}
}

// TestBreakingACampfireDropsItsFood destroys a campfire holding a beef: the
// beef lies on the ground afterwards.
// vanilla: CampfireBlock.affectNeighborsAfterRemoval
func TestBreakingACampfireDropsItsFood(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowedOps(t, servertest.SurvivalTestConfig())
	srv.StopTick()

	srv.SetBlock(3, 64, 0, campfireState(t, "minecraft:campfire", true))
	srv.GiveSlot("Testificate", player.HotbarSlot, servertest.ItemStack(t, "minecraft:beef", 1))
	w.Settle()
	w.UseBlock(3, 64, 0)
	w.Settle()
	w.ChatCommand("setblock 3 64 0 minecraft:air destroy")
	w.Settle()
	if got := countItems(srv, "minecraft:beef"); got != 1 {
		t.Errorf("%d beef lie after the campfire broke, want 1", got)
	}
}

// TestACampfireOverAHayBaleIsASignalFire checks signal_fire at placement and
// when the block below changes.
func TestACampfireOverAHayBaleIsASignalFire(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)
	signal := func(x, y, z int32) string {
		_, props := servertest.StateProps(srv.BlockAt(x, y, z))
		return props["signal_fire"]
	}

	servertest.Setblock(t, c, 4, 64, 6, "hay_block")
	servertest.PlaceFrom(c, "minecraft:campfire", 2, 6, 0, 4, 64, 6, protocol.FaceTop)
	if got := signal(4, 65, 6); got != "true" {
		t.Errorf("placed on hay: signal_fire = %q, want true", got)
	}

	servertest.Setblock(t, c, 8, 64, 6, "stone")
	servertest.Setblock(t, c, 8, 65, 6, "soul_campfire")
	_, before := servertest.StateProps(srv.BlockAt(8, 65, 6))
	servertest.Setblock(t, c, 8, 64, 6, "hay_block")
	if got := signal(8, 65, 6); got != "true" {
		t.Errorf("hay put under: signal_fire = %q, want true", got)
	}
	servertest.Setblock(t, c, 8, 64, 6, "stone")
	_, after := servertest.StateProps(srv.BlockAt(8, 65, 6))
	if got := signal(8, 65, 6); got != "false" {
		t.Errorf("hay replaced: signal_fire = %q, want false", got)
	}
	for _, k := range []string{"facing", "lit", "waterlogged"} {
		if before[k] != after[k] {
			t.Errorf("%s changed from %q to %q", k, before[k], after[k])
		}
	}
}
