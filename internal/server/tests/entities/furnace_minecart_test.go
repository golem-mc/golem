package entities

import (
	"math"
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
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

// TestCreativeKeepsTheCoalItFeeds: the cart takes the fuel, the creative
// player keeps the item.
// vanilla: MinecartFurnace.interact
func TestCreativeKeepsTheCoalItFeeds(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.GameMode = "creative"
	srv, c := servertest.OpenWindowed(t, cfg)
	c.MoveToGround(2.5, 64, 1.5, true)
	c.Settle()
	id := furnaceCartAt(t, srv, "")
	srv.GiveSlot("Testificate", player.HotbarSlot, itemOf(t, "minecraft:coal", 3))
	c.InteractAt(id, protocol.HandMain, 0.5, 0.5, 0.5)
	c.Settle()
	if got := savedFuel(t, srv, id); got != 3600 {
		t.Fatalf("fuel = %d, want 3600", got)
	}
	if held := srv.InventorySlot("Testificate", player.HotbarSlot); held.Count != 3 {
		t.Errorf("creative coal count = %d, want 3", held.Count)
	}
}

// TestASpectatorCannotFuelAFurnaceCart: the click does nothing.
func TestASpectatorCannotFuelAFurnaceCart(t *testing.T) {
	t.Parallel()
	srv, c := servertest.OpenWindowed(t, servertest.BlockTestConfig())
	c.MoveToGround(2.5, 64, 1.5, true)
	c.Settle()
	id := furnaceCartAt(t, srv, "")
	srv.GiveSlot("Testificate", player.HotbarSlot, itemOf(t, "minecraft:coal", 3))
	srv.RunLine("gamemode spectator Testificate")
	if srv.PlayerGameMode("Testificate") != protocol.GameModeSpectator {
		t.Fatal("the switch to spectator applies")
	}
	c.Settle()
	c.InteractAt(id, protocol.HandMain, 0.5, 0.5, 0.5)
	c.Settle()
	if got := savedFuel(t, srv, id); got != 0 {
		t.Errorf("a spectator fuelled the cart: %d", got)
	}
	if held := srv.InventorySlot("Testificate", player.HotbarSlot); held.Count != 3 {
		t.Errorf("coal count = %d, want 3", held.Count)
	}
}

// TestAFurnaceCartCannotBeRiddenByAClick: the click is the fuel's alone.
// vanilla: MinecartFurnace.interact
func TestAFurnaceCartCannotBeRiddenByAClick(t *testing.T) {
	t.Parallel()
	srv, c := servertest.OpenWindowed(t, servertest.BlockTestConfig())
	c.MoveToGround(2.5, 64, 1.5, true)
	c.Settle()
	id := furnaceCartAt(t, srv, "")
	c.InteractAt(id, protocol.HandMain, 0.5, 0.5, 0.5)
	c.Settle()
	if riders := srv.MinecartPassengerIDs(id); len(riders) != 0 {
		t.Fatalf("a click boarded %v", riders)
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

// TestAFurnaceCartStopsBeingPushedWhenTheFuelRunsOut: the burn time counts
// down a tick at a time, the lit flag follows, and the push is dropped.
// vanilla: MinecartFurnace.tick
func TestAFurnaceCartStopsBeingPushedWhenTheFuelRunsOut(t *testing.T) {
	t.Parallel()
	srv, _ := servertest.OpenWindowed(t, servertest.BlockTestConfig())
	id := furnaceCartAt(t, srv, "{Fuel:5s,PushX:0.5d}")
	srv.TickMinecarts(4)
	if got := savedFuel(t, srv, id); got != 1 {
		t.Fatalf("fuel after 4 ticks = %d, want 1", got)
	}
	if lit, _ := srv.MinecartLit(id); !lit {
		t.Fatal("not lit with one tick of fuel left")
	}
	srv.TickMinecarts(1)
	if got := savedFuel(t, srv, id); got != 0 {
		t.Fatalf("fuel after 5 ticks = %d, want 0", got)
	}
	if lit, _ := srv.MinecartLit(id); lit {
		t.Error("still lit with no fuel")
	}
	if px, _ := nbt.Get[nbt.Double](cartSavedTag(t, srv, id), "PushX"); px != 0 {
		t.Errorf("PushX = %v after the fuel ran out, want 0", px)
	}
	x1, _, _, _, _ := srv.MinecartState(id)
	srv.TickMinecarts(100)
	x2, _, _, _, _ := srv.MinecartState(id)
	srv.TickMinecarts(100)
	x3, _, _, _, _ := srv.MinecartState(id)
	if x2 <= x1 {
		t.Errorf("the cart did not coast on after the fuel ran out: %v, %v", x1, x2)
	}
	if math.Abs(x3-x2) > 0.1 {
		t.Errorf("the cart is still pushed 100 ticks after the fuel ran out: %v, %v", x2, x3)
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

// TestAFurnaceCartItemStandsAFurnaceCartAndDropsWhenBroken: the item lays a
// furnace cart and a blow that breaks it leaves the item.
// vanilla: MinecartItem.useOn, VehicleEntity.destroy
func TestAFurnaceCartItemStandsAFurnaceCartAndDropsWhenBroken(t *testing.T) {
	t.Parallel()
	srv, c := servertest.OpenWindowed(t, servertest.BlockTestConfig())
	c.MoveToGround(0.5, 64, 0.5, true)
	c.Settle()
	srv.GiveSlot("Testificate", player.HotbarSlot, itemOf(t, furnaceCart, 1))
	servertest.CartRails(t, srv, 2, 5)
	c.MoveTo(2.5, 64, 2.9, 0, 60)
	c.Settle()
	c.PlaceBlock(2, 64, 1, protocol.FaceTop)
	typeID, _ := registry.EntityTypeID(furnaceCart)
	spawned := servertest.AwaitBoatSpawn(c.Client, typeID)
	if got := srv.MinecartKind(spawned.EntityID); got != furnaceCart {
		t.Fatalf("the laid cart is a %q, want %s", got, furnaceCart)
	}
	srv.StopTick()
	if !srv.HurtMinecart(spawned.EntityID, 100, false) {
		t.Fatal("the blow did not land")
	}
	srv.TickMinecarts(1)
	if !servertest.BlastLeftItem(srv.ItemsInDimension(world.DimensionName), furnaceCart) {
		t.Error("the broken furnace cart left no item")
	}
}

// TestTheLitFlagReachesWatchers: the fuel flag at metadata index 13 is told
// on the tracking pass after the simulation lights it.
// vanilla: MinecartFurnace.defineSynchedData
func TestTheLitFlagReachesWatchers(t *testing.T) {
	t.Parallel()
	srv, c := servertest.OpenWindowed(t, servertest.BlockTestConfig())
	c.MoveToGround(2.5, 64, 1.5, true)
	c.Settle()
	id := furnaceCartAt(t, srv, "{Fuel:100s}")
	// Simulation lights the cart; the next tracking pass publishes it.
	srv.TickMinecarts(2)
	for packetID, data := range c.Packets(servertest.ExpectTimeout) {
		if packetID != protocol.IDPlayEntityMetadata || servertest.MetadataEntityID(data) != id {
			continue
		}
		r := protocol.NewReader(data)
		r.ReadVarInt()
		entries, ok := servertest.ReadMetadata(r)
		if !ok {
			t.Fatal("unreadable metadata")
		}
		if m := servertest.MetaOf(entries, protocol.MetadataIndexMinecartFurnaceFuel); m != nil {
			if !m.FlagVal {
				t.Fatal("the lit flag was told as false")
			}
			return
		}
	}
	t.Fatal("no lit flag arrived")
}
