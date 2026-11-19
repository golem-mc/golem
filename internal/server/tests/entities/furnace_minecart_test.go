package entities

import (
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
)

const furnaceCart = "minecraft:furnace_minecart"

// furnaceCartAt summons a furnace cart at the test rails' x=4.5 with the tick
// loop held.
func furnaceCartAt(t *testing.T, srv *server.Server, nbtText string) int32 {
	t.Helper()
	srv.StopTick()
	servertest.CartRails(t, srv, 0, 30)
	// The dispatcher refuses input left behind the last argument, so
	// trim the space an empty tag leaves.
	srv.RunLine(strings.TrimSpace("summon " + furnaceCart + " 4.5 64.0625 1.5 " + nbtText))
	for _, cart := range srv.MinecartsInDimension(world.DimensionName) {
		return cart.ID
	}
	t.Fatal("the summon stood no cart")
	return 0
}

func savedFuel(t *testing.T, srv *server.Server, id int32) int16 {
	t.Helper()
	fuel, ok := nbt.Get[nbt.Short](cartSavedTag(t, srv, id), "Fuel")
	if !ok {
		t.Fatal("the furnace cart saved no Fuel")
	}
	return int16(fuel)
}

// TestCoalFuelsAFurnaceCartUpToTheCap: each right-click spends a coal for
// 3600 ticks and lights the cart on its next tick; the ninth would pass 32000
// and keeps its coal. Charcoal is fuel, a stick is not.
// vanilla: MinecartFurnace.interact, addFuel
func TestCoalFuelsAFurnaceCartUpToTheCap(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.GameMode = "survival"
	srv, c := servertest.OpenWindowed(t, cfg)
	c.MoveToGround(2.5, 64, 1.5, true)
	c.Settle()
	id := furnaceCartAt(t, srv, "")

	srv.GiveSlot("Testificate", player.HotbarSlot, itemOf(t, "minecraft:stick", 1))
	c.InteractAt(id, protocol.HandMain, 0.5, 0.5, 0.5)
	c.Settle()
	if got := savedFuel(t, srv, id); got != 0 {
		t.Fatalf("a stick fuelled the cart: %d", got)
	}

	srv.GiveSlot("Testificate", player.HotbarSlot, itemOf(t, "minecraft:coal", 9))
	c.InteractAt(id, protocol.HandMain, 0.5, 0.5, 0.5)
	c.Settle()
	if got := savedFuel(t, srv, id); got != 3600 {
		t.Fatalf("fuel after one coal = %d, want 3600", got)
	}
	tag := cartSavedTag(t, srv, id)
	if px, _ := nbt.Get[nbt.Double](tag, "PushX"); px < 1.9 || px > 2.1 {
		t.Errorf("PushX = %v, want about 2 (cart at 4.5, player at 2.5)", px)
	}
	if lit, _ := srv.MinecartLit(id); lit {
		t.Error("the cart is lit before its first tick")
	}
	srv.TickMinecarts(1)
	if lit, _ := srv.MinecartLit(id); !lit {
		t.Error("the cart is not lit after a tick with fuel")
	}
	for range 7 {
		c.InteractAt(id, protocol.HandMain, 0.5, 0.5, 0.5)
		c.Settle()
	}
	if got := savedFuel(t, srv, id); got != 3599+7*3600 {
		t.Fatalf("fuel after eight coal = %d, want %d", got, 3599+7*3600)
	}
	// 3599 + 8*3600 = 32399 passes the cap.
	c.InteractAt(id, protocol.HandMain, 0.5, 0.5, 0.5)
	c.Settle()
	if got := savedFuel(t, srv, id); got != 3599+7*3600 {
		t.Errorf("the ninth coal changed fuel to %d", got)
	}
	if held := srv.InventorySlot("Testificate", player.HotbarSlot); held.Count != 1 {
		t.Errorf("coal left = %d, want 1 (eight spent)", held.Count)
	}

	srv.GiveSlot("Testificate", player.HotbarSlot, itemOf(t, "minecraft:charcoal", 1))
	srv.RunLine("data merge entity @e[type=" + furnaceCart + ",limit=1] {Fuel:0s}")
	c.InteractAt(id, protocol.HandMain, 0.5, 0.5, 0.5)
	c.Settle()
	if got := savedFuel(t, srv, id); got != 3600 {
		t.Errorf("fuel after charcoal = %d, want 3600", got)
	}
}

// TestALitFurnaceCartOutrunsAPlainCart: a pushed furnace cart keeps moving
// where an unpushed one has rolled to a stop, and stays under its halved top
// speed of 0.2 a tick.
// vanilla: MinecartFurnace.applyNaturalSlowdown, getMaxSpeed
func TestALitFurnaceCartOutrunsAPlainCart(t *testing.T) {
	t.Parallel()
	srv, _ := servertest.OpenWindowed(t, servertest.BlockTestConfig())
	furn := furnaceCartAt(t, srv, "{Fuel:3600s,PushX:0.5d}")
	// The plain cart runs its own line, clear of the furnace cart's push.
	for x := int32(0); x <= 30; x++ {
		if !srv.SetBlock(x, 64, 4, servertest.MustState(t, "minecraft:rail", map[string]string{"shape": "east_west"})) {
			t.Fatalf("the rail at (%d, 64, 4) did not stand", x)
		}
	}
	plain := srv.SpawnMinecartOfKind("minecraft:minecart", 4.5, 64.0625, 4.5)
	srv.SetMinecartMotion(plain, 0.2, 0, 0)

	start, _, _, _, _ := srv.MinecartState(furn)
	prev := start
	for i := range 20 {
		srv.TickMinecarts(1)
		x, _, _, _, _ := srv.MinecartState(furn)
		if step := x - prev; step > 0.2+1e-9 {
			t.Fatalf("tick %d moved the furnace cart %v, past the 0.2 cap", i, step)
		}
		prev = x
	}
	furnX, _, _, _, _ := srv.MinecartState(furn)
	plainX, _, _, _, _ := srv.MinecartState(plain)
	if furnX-start < 3 {
		t.Errorf("the pushed cart went %v in 20 ticks, want about 4 at its 0.2 cap", furnX-start)
	}
	if furnX <= plainX {
		t.Errorf("furnace cart at %v, plain cart at %v: the pushed one should be ahead", furnX, plainX)
	}
}

// TestAFurnaceCartKeepsItsFuelAcrossASave: Fuel and the push save and load,
// and the load clamps nothing.
// vanilla: MinecartFurnace.addAdditionalSaveData, readAdditionalSaveData
func TestAFurnaceCartKeepsItsFuelAcrossASave(t *testing.T) {
	t.Parallel()
	srv, _ := servertest.OpenWindowed(t, servertest.BlockTestConfig())
	id := furnaceCartAt(t, srv, "{Fuel:1234s,PushX:0.25d,PushZ:-0.5d}")
	tag := cartSavedTag(t, srv, id)
	if got := savedFuel(t, srv, id); got != 1234 {
		t.Errorf("Fuel = %d, want 1234", got)
	}
	px, _ := nbt.Get[nbt.Double](tag, "PushX")
	pz, _ := nbt.Get[nbt.Double](tag, "PushZ")
	if px != 0.25 || pz != -0.5 {
		t.Errorf("push = (%v, %v), want (0.25, -0.5)", px, pz)
	}
	if got := srv.MinecartKind(id); got != furnaceCart {
		t.Errorf("kind = %q", got)
	}
}
