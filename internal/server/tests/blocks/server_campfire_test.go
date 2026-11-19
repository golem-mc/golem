package blocks

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"testing"

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
