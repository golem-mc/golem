package gamemode

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"testing"
	"time"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/registry"
)

// TestCreativeSlotIgnoredOutsideCreative checks that a survival client cannot
// conjure items with the creative inventory packet, which the vanilla server
// only honours from a creative player.
func TestCreativeSlotIgnoredOutsideCreative(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.SurvivalConfig(), "Cheater")

	diamond, _ := registry.ItemID("minecraft:diamond")
	c.SetCreativeSlot(player.HotbarSlot, protocol.ItemStack{ItemID: diamond, Count: 64})
	c.Settle()

	if got := srv.InventorySlot("Cheater", player.HotbarSlot); !got.Empty() {
		t.Errorf("survival player's hotbar after a creative slot = %+v, want empty", got)
	}
}

// TestCreativeSlotRefusesUnknownItems checks that an item ID the registry
// does not name never reaches the inventory, where it would be saved and shown
// to every other player as equipment their clients cannot decode.
func TestCreativeSlotRefusesUnknownItems(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Builder")

	c.SetCreativeSlot(player.HotbarSlot, protocol.ItemStack{ItemID: int32(len(registry.Items)) + 1000, Count: 1})
	c.Settle()

	if got := srv.InventorySlot("Builder", player.HotbarSlot); !got.Empty() {
		t.Errorf("hotbar after an unknown item = %+v, want empty", got)
	}
}

// TestCreativeSlotIgnoresAnOversizedStack checks that a stack larger than a
// full one is refused whole rather than trimmed: the slot keeps what it held,
// and the resend is what puts the client's shown stack back.
func TestCreativeSlotIgnoresAnOversizedStack(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Builder")

	dirt, _ := registry.ItemID("minecraft:dirt")
	stone, _ := registry.ItemID("minecraft:stone")
	srv.GiveSlot("Builder", player.HotbarSlot, protocol.ItemStack{ItemID: dirt, Count: 5})

	c.SetCreativeSlot(player.HotbarSlot, protocol.ItemStack{ItemID: stone, Count: 100000})
	c.Settle()

	got := srv.InventorySlot("Builder", player.HotbarSlot)
	if got.ItemID != dirt || got.Count != 5 {
		t.Errorf("hotbar after an oversized stack = %+v, want five dirt", got)
	}
}

// TestCreativeSlotIgnoresSlotZero checks that the crafting output, slot 0 of
// the player's window, is left alone: vanilla accepts only the player's own
// slots 1 to 45.
func TestCreativeSlotIgnoresSlotZero(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Builder")

	stone, _ := registry.ItemID("minecraft:stone")
	c.SetCreativeSlot(0, protocol.ItemStack{ItemID: stone, Count: 1})
	c.Settle()

	if got := srv.InventorySlot("Builder", 0); !got.Empty() {
		t.Errorf("crafting output after a creative slot 0 = %+v, want empty", got)
	}
}

// TestCreativeDropThrottle checks that a burst of creative drops is cut off.
// The drops are counted on the step clock and the tick loop is never
// stepped, so no two thrown stacks merge and every spawn is its own drop:
// vanilla's TickThrottler(20, 1480) lets 1480/20 drops through.
func TestCreativeDropThrottle(t *testing.T) {
	t.Parallel()
	clock := servertest.NewStepClock()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Builder", func(s *server.Server) {
		s.SetTickClock(clock, nil)
		s.SetKeepAlive(time.Hour, time.Hour)
	})

	dirt, _ := registry.ItemID("minecraft:dirt")
	for range 120 {
		c.SetCreativeSlot(-1, protocol.ItemStack{ItemID: dirt, Count: 1})
	}
	c.Settle()

	if got := srv.ItemCount(); got != 74 {
		t.Errorf("a burst of 120 creative drops spawned %d items, want 74", got)
	}
}
