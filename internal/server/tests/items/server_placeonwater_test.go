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
