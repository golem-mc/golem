package plugins_test

import (
	"math"
	"sync/atomic"
	"testing"
	"time"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/plugin"
	"github.com/golem-mc/golem/registry"
)

var hellKey = plugin.Key{Namespace: "test", Path: "hell"}

// portalPlugin joins a creative player and creates a nether-typed plugin world.
func portalPlugin(t *testing.T) (*server.Server, plugin.Server, func(func()), *servertest.Client, plugin.World) {
	t.Helper()
	cfg := servertest.BlockTestConfig()
	var h plugin.Server
	srv := servertest.NewTestServerWithPlugins(t, cfg, []plugin.Registration{{Name: "portal", Plugin: &testPlugin{enable: func(s plugin.Server) error { h = s; return nil }}}})
	c := servertest.DialServer(t, srv)
	c.Join("Portaler")
	c.Settle()
	servertest.HoldTicks(t, srv)
	// A joining player has a portal cooldown, which standing in a portal renews.
	servertest.RunTicks(t, srv, 15)
	run := func(f func()) {
		t.Helper()
		ran := false
		h.Scheduler().RunTask(func() { f(); ran = true })
		servertest.RunTicks(t, srv, 1)
		if !ran {
			t.Fatal("plugin task did not complete")
		}
	}
	var hell plugin.World
	run(func() {
		var err error
		hell, err = h.CreateWorld(plugin.WorldOptions{Key: hellKey, Dimension: plugin.MinecraftKey("the_nether"), Seed: 5, Generator: &flatGen{}})
		if err != nil {
			t.Errorf("CreateWorld: %v", err)
		}
	})
	if hell == nil {
		t.FailNow()
	}
	return srv, h, run, c, hell
}

func lightFrameAndStand(t *testing.T, srv *server.Server, c *servertest.Client) {
	t.Helper()
	servertest.BuildFrame(t, srv)
	servertest.Flint(t, c)
	c.PlaceBlock(servertest.PortalInnerX0, servertest.PortalFloorY-1, servertest.PortalInnerZ, 1)
	c.Settle()
	c.MoveTo(float64(servertest.PortalInnerX0)+0.5, servertest.PortalFloorY, servertest.PortalInnerZ+0.5, 0, 0)
	c.Settle()
}

func portalBlocksNear(w plugin.World, cx, cz, r int32) (n int) {
	for x := cx - r; x <= cx+r; x++ {
		for z := cz - r; z <= cz+r; z++ {
			for y := range int32(128) {
				if registry.BlockOf(uint16(w.BlockState(plugin.BlockPos{X: int(x), Y: int(y), Z: int(z)}))) == registry.BlockNetherPortal {
					n++
				}
			}
		}
	}
	return n
}

// vanilla: Entity.handlePortal, PortalForcer.findClosestPortalPosition
func TestPluginPortalEventRetargetsExitSearch(t *testing.T) {
	t.Parallel()
	srv, h, run, c, hell := portalPlugin(t)
	// The listener runs on the tick goroutine; it records the event as it
	// arrived, before changing it.
	var seen atomic.Pointer[plugin.EntityPortalEvent]
	var from, to atomic.Pointer[plugin.Location]
	run(func() {
		plugin.Listen(h, func(e *plugin.EntityPortalEvent) {
			f, t := e.From, e.To
			from.Store(&f)
			to.Store(&t)
			seen.Store(e)
			e.To.World = hell
		})
	})
	lightFrameAndStand(t, srv, c)
	// Portal travel runs off the tick loop; pace ticks while it finishes.
	servertest.ReleaseTicks(t, srv)
	c.ExpectWithinTicks(srv, 400, protocol.IDPlayRespawn)
	servertest.WaitFor(t, srv, 200, func() bool {
		_, _, traveling := srv.EndExitState("Portaler")
		return !traveling && srv.PlayerDimension("Portaler") == hellKey.String()
	})
	servertest.HoldTicks(t, srv)
	if got := srv.PlayerDimension("Portaler"); got != hellKey.String() {
		t.Fatalf("dimension = %q, want %q", got, hellKey)
	}
	ev, f, tl := seen.Load(), from.Load(), to.Load()
	if ev == nil || ev.SearchRadius != 16 || ev.CreationRadius != 16 || !ev.CanCreatePortal {
		t.Fatalf("event = %+v", ev)
	}
	if tl.World.Key() != plugin.MinecraftKey("the_nether") {
		t.Errorf("To world = %v, want the vanilla nether", tl.World.Key())
	}
	if f.World.Key() != plugin.MinecraftKey("overworld") {
		t.Errorf("From world = %v, want the overworld", f.World.Key())
	}
	if math.Abs(tl.Pos.X-math.Floor(f.Pos.X/8)) > 1 || math.Abs(tl.Pos.Z-math.Floor(f.Pos.Z/8)) > 1 {
		t.Errorf("To = %v, want From %v / 8", tl.Pos, f.Pos)
	}
	pos := srv.PlayerPosition("Portaler")
	if math.Abs(pos.X) > 24 || math.Abs(pos.Z) > 24 {
		t.Errorf("arrival = (%v, %v), want near the scaled target", pos.X, pos.Z)
	}
	run(func() {
		if n := portalBlocksNear(hell, 0, 0, 24); n == 0 {
			t.Error("no exit portal was built in the plugin world")
		}
		nether, _ := h.World(plugin.MinecraftKey("the_nether"))
		if n := portalBlocksNear(nether, 0, 0, 24); n != 0 {
			t.Errorf("%d portal blocks in the vanilla nether", n)
		}
	})
}

// With a portal 60 blocks away, SearchRadius 32 and creation off, none is found and the body stays.
func TestPluginPortalEventSearchRadius(t *testing.T) {
	t.Parallel()
	srv, h, run, c, hell := portalPlugin(t)
	_, portalX, _, _ := servertest.PortalStates(t)
	t.Cleanup(srv.HoldChunkInDimension(hellKey.String(), 60, 0))
	if !srv.SetBlockInDimension(hellKey.String(), 60, servertest.PortalFloorY, 0, portalX) {
		t.Fatal("placing distant portal failed")
	}
	var fired atomic.Int32
	run(func() {
		plugin.Listen(h, func(e *plugin.EntityPortalEvent) {
			fired.Add(1)
			e.To.World = hell
			e.To.Pos = plugin.Vec3{X: 0, Y: servertest.PortalFloorY, Z: 0}
			e.SearchRadius = 32
			e.CanCreatePortal = false
		})
	})
	lightFrameAndStand(t, srv, c)
	for deadline := time.Now().Add(10 * time.Second); fired.Load() == 0 && time.Now().Before(deadline); {
		servertest.RunTicks(t, srv, 1)
	}
	if fired.Load() == 0 {
		t.Fatal("the portal event never fired")
	}
	servertest.RunTicks(t, srv, 20)
	if got := srv.PlayerDimension("Portaler"); got != world.DimensionName {
		t.Errorf("dimension = %q, want the overworld", got)
	}
}

func TestPluginPortalEventCannotCreate(t *testing.T) {
	t.Parallel()
	srv, h, run, c, _ := portalPlugin(t)
	var fired atomic.Int32
	run(func() {
		plugin.Listen(h, func(e *plugin.EntityPortalEvent) {
			fired.Add(1)
			e.CanCreatePortal = false
		})
	})
	lightFrameAndStand(t, srv, c)
	for deadline := time.Now().Add(10 * time.Second); fired.Load() == 0 && time.Now().Before(deadline); {
		servertest.RunTicks(t, srv, 1)
	}
	if fired.Load() == 0 {
		t.Fatal("the portal event never fired")
	}
	servertest.RunTicks(t, srv, 20)
	if got := srv.PlayerDimension("Portaler"); got != world.DimensionName {
		t.Errorf("dimension = %q, want the overworld", got)
	}
	run(func() {
		nether, _ := h.World(plugin.MinecraftKey("the_nether"))
		if n := portalBlocksNear(nether, 0, 0, 24); n != 0 {
			t.Errorf("%d portal blocks built", n)
		}
	})
}

// A nether-typed plugin world's portal leads to the overworld, scaled by 8.
func TestPluginNetherTypedWorldPortalLeadsToOverworld(t *testing.T) {
	t.Parallel()
	srv, _, run, c, hell := portalPlugin(t)
	obsidian, portalX, _, _ := servertest.PortalStates(t)
	srv.MovePlayerToDimension("Portaler", hellKey.String(), 4.5, servertest.PortalFloorY, 4.5)
	servertest.WaitFor(t, srv, 100, func() bool { return srv.PlayerDimension("Portaler") == hellKey.String() })
	c.ExpectWithinTicks(srv, 200, protocol.IDPlayRespawn)
	c.Settle()
	// The arrival's chunks load on their own time.
	for deadline, ready := time.Now().Add(10*time.Second), false; !ready && time.Now().Before(deadline); {
		run(func() {
			ready = hell.SetBlockState(plugin.BlockPos{X: 3, Y: servertest.PortalFloorY, Z: servertest.PortalInnerZ}, plugin.BlockState(obsidian), plugin.UpdateClients|plugin.UpdateKnownShape)
		})
	}
	run(func() {
		set := func(x, y, z int32, s uint16) {
			if !hell.SetBlockState(plugin.BlockPos{X: int(x), Y: int(y), Z: int(z)}, plugin.BlockState(s), plugin.UpdateClients|plugin.UpdateKnownShape) && uint16(hell.BlockState(plugin.BlockPos{X: int(x), Y: int(y), Z: int(z)})) != s {
				t.Errorf("set (%d, %d, %d) failed", x, y, z)
			}
		}
		for _, x := range []int32{4, 5} {
			set(x, servertest.PortalFloorY-1, servertest.PortalInnerZ, obsidian)
			set(x, servertest.PortalFloorY+3, servertest.PortalInnerZ, obsidian)
			for y := range int32(3) {
				set(x, servertest.PortalFloorY+y, servertest.PortalInnerZ, portalX)
			}
		}
		for y := range int32(3) {
			set(3, servertest.PortalFloorY+y, servertest.PortalInnerZ, obsidian)
			set(6, servertest.PortalFloorY+y, servertest.PortalInnerZ, obsidian)
		}
	})
	// The player stands in the portal as it appears, and crosses.
	servertest.ReleaseTicks(t, srv)
	c.ExpectWithinTicks(srv, 400, protocol.IDPlayRespawn)
	servertest.WaitFor(t, srv, 100, func() bool {
		_, _, traveling := srv.EndExitState("Portaler")
		return !traveling && srv.PlayerPosition("Portaler").X > 10
	})
	servertest.HoldTicks(t, srv)
	pos := srv.PlayerPosition("Portaler")
	if got := srv.PlayerDimension("Portaler"); got != world.DimensionName {
		t.Errorf("dimension = %q, want the overworld", got)
	}
	if math.Abs(pos.X-36) > 20 || math.Abs(pos.Z-36) > 20 {
		t.Errorf("arrival = (%v, %v), want near (36, 36)", pos.X, pos.Z)
	}
}

// End portals and gateways in a plugin world of type minecraft:the_end behave as
// the end's: retargeting into one builds the arrival platform there, its end
// portals return to the overworld spawn (after credits), and an exit-less end
// gateway builds an exit and teleports the player.
func TestPluginEndTypedWorldPortalAndGateway(t *testing.T) {
	t.Parallel()
	srv, h, run, c, _ := portalPlugin(t)
	endKey := plugin.Key{Namespace: "test", Path: "end"}
	var customEnd plugin.World
	run(func() {
		var err error
		customEnd, err = h.CreateWorld(plugin.WorldOptions{Key: endKey, Dimension: plugin.MinecraftKey("the_end"), Seed: 5, Generator: &flatGen{}})
		if err != nil {
			t.Errorf("CreateWorld: %v", err)
		}
		plugin.Listen(h, func(e *plugin.EntityPortalEvent) {
			if e.Cause == plugin.TeleportEndPortal && e.From.World.Key() == plugin.MinecraftKey("overworld") {
				e.To.World = customEnd
			}
		})
	})
	if customEnd == nil {
		t.FailNow()
	}

	endPortal := srv.EndPortalState()
	arrival := world.EndArrival()
	obsidian, _, _, _ := servertest.PortalStates(t)

	// Entering an overworld end portal retargeted to the custom end builds the
	// obsidian platform in the custom end, not in minecraft:the_end.
	if !srv.SetBlock(10, servertest.PortalFloorY-1, 10, endPortal) {
		t.Fatal("placing overworld end portal failed")
	}
	c.MoveTo(10.5, servertest.PortalFloorY-1, 10.5, 0, 0)
	c.Settle()
	// End-portal travel runs off the tick loop; pace ticks while it finishes
	// and wait for traveling to clear before any subsequent dimension move.
	servertest.ReleaseTicks(t, srv)
	c.ExpectWithinTicks(srv, 400, protocol.IDPlayRespawn)
	servertest.WaitFor(t, srv, 200, func() bool {
		_, _, traveling := srv.EndExitState("Portaler")
		return !traveling && srv.PlayerDimension("Portaler") == endKey.String()
	})
	servertest.WaitFor(t, srv, 200, func() bool {
		return srv.BlockInDimension(endKey.String(), arrival.X, arrival.Y-1, arrival.Z) == obsidian
	})
	servertest.HoldTicks(t, srv)
	if got := srv.BlockInDimension(world.EndName, arrival.X, arrival.Y-1, arrival.Z); got == obsidian {
		t.Error("arrival platform was built in vanilla minecraft:the_end")
	}

	// Standing in an end portal in the custom end shows the end credits on the
	// first exit, then returns to the overworld spawn on respawn.
	servertest.RunTicks(t, srv, 15)
	if !srv.SetBlockInDimension(endKey.String(), arrival.X, arrival.Y, arrival.Z, endPortal) {
		t.Fatal("placing custom end portal failed")
	}
	servertest.ReleaseTicks(t, srv)
	servertest.ExpectEndCredits(t, c, srv)
	c.PerformRespawn()
	c.ExpectWithinTicks(srv, 400, protocol.IDPlayRespawn)
	servertest.WaitFor(t, srv, 200, func() bool {
		_, _, traveling := srv.EndExitState("Portaler")
		return !traveling && srv.PlayerDimension("Portaler") == world.DimensionName
	})
	servertest.HoldTicks(t, srv)

	// Once credits have been seen, an end portal in the custom end returns
	// directly to the overworld without rolling credits again.
	servertest.RunTicks(t, srv, 15)
	srv.MovePlayerToDimension("Portaler", endKey.String(), float64(arrival.X)+0.5, float64(arrival.Y), float64(arrival.Z)+0.5)
	c.ExpectWithinTicks(srv, 0, protocol.IDPlayRespawn)
	// The next respawn is the immediate return from the custom end to the overworld.
	servertest.ReleaseTicks(t, srv)
	c.ExpectWithinTicks(srv, 400, protocol.IDPlayRespawn)
	servertest.WaitFor(t, srv, 200, func() bool {
		_, _, traveling := srv.EndExitState("Portaler")
		return !traveling && srv.PlayerDimension("Portaler") == world.DimensionName
	})
	servertest.HoldTicks(t, srv)

	// An exit-less end gateway in the custom end creates an exit and teleports
	// the player away, as in minecraft:the_end.
	t.Cleanup(srv.HoldChunkInDimension(endKey.String(), 12, 10))
	gateway := registry.MustState("minecraft:end_gateway", nil)
	if !srv.SetBlockInDimension(endKey.String(), 12, 65, 10, gateway) {
		t.Fatal("placing end gateway failed")
	}
	servertest.RunTicks(t, srv, 15)
	srv.MovePlayerToDimension("Portaler", endKey.String(), 12.5, 65, 10.5)
	c.ExpectWithinTicks(srv, 0, protocol.IDPlayRespawn)
	servertest.RunTicks(t, srv, 1)
	srv.WaitGatewayMoves()
	if p := srv.PlayerPosition("Portaler"); srv.PlayerDimension("Portaler") != endKey.String() || math.Hypot(p.X, p.Z) <= 100 {
		t.Errorf("gateway arrival = (%v, %v) in %q, want >100 blocks out in %q", p.X, p.Z, srv.PlayerDimension("Portaler"), endKey)
	}
}
