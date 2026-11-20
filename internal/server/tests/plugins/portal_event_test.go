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
	var seen *plugin.EntityPortalEvent
	run(func() {
		plugin.Listen(h, func(e *plugin.EntityPortalEvent) {
			seen = e
			e.To.World = hell
		})
	})
	lightFrameAndStand(t, srv, c)
	c.ExpectWithinTicks(srv, 400, protocol.IDPlayRespawn)
	if got := srv.PlayerDimension("Portaler"); got != hellKey.String() {
		t.Fatalf("dimension = %q, want %q", got, hellKey)
	}
	if seen == nil || seen.SearchRadius != 16 || seen.CreationRadius != 16 || !seen.CanCreatePortal {
		t.Fatalf("event = %+v", seen)
	}
	if seen.To.World.Key() != plugin.MinecraftKey("the_nether") && seen.To.Pos.X != 0 {
		t.Errorf("To = %+v, want the scaled target in the nether", seen.To)
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
	c.ExpectWithinTicks(srv, 400, protocol.IDPlayRespawn)
	servertest.WaitFor(t, srv, 100, func() bool { return srv.PlayerPosition("Portaler").X > 10 })
	pos := srv.PlayerPosition("Portaler")
	if got := srv.PlayerDimension("Portaler"); got != world.DimensionName {
		t.Errorf("dimension = %q, want the overworld", got)
	}
	if math.Abs(pos.X-36) > 20 || math.Abs(pos.Z-36) > 20 {
		t.Errorf("arrival = (%v, %v), want near (36, 36)", pos.X, pos.Z)
	}
}
