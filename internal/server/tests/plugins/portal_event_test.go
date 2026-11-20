package plugins_test

import (
	"math"
	"testing"

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
	srv, h, run := worldPlugin(t, cfg)
	c := servertest.DialServer(t, srv)
	c.Join("Portaler")
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
	servertest.WaitFor(t, srv, 400, func() bool { return srv.PlayerDimension("Portaler") == hellKey.String() })
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
	fired := 0
	run(func() {
		plugin.Listen(h, func(e *plugin.EntityPortalEvent) {
			fired++
			e.CanCreatePortal = false
		})
	})
	lightFrameAndStand(t, srv, c)
	servertest.WaitFor(t, srv, 100, func() bool { return fired > 0 })
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
	servertest.RunTicks(t, srv, 40)
	run(func() {
		set := func(x, y, z int32, s uint16) {
			if !hell.SetBlockState(plugin.BlockPos{X: int(x), Y: int(y), Z: int(z)}, plugin.BlockState(s), plugin.UpdateClients|plugin.UpdateKnownShape) {
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
	// The arrival inside the portal arms the cooldown; step out, let it run
	// down, and step back in.
	c.MoveTo(10.5, servertest.PortalFloorY, 4.5, 0, 0)
	servertest.RunTicks(t, srv, 15)
	c.MoveTo(4.5, servertest.PortalFloorY, 4.5, 0, 0)
	c.Settle()
	servertest.WaitFor(t, srv, 400, func() bool { return srv.PlayerDimension("Portaler") == world.DimensionName })
	pos := srv.PlayerPosition("Portaler")
	if math.Abs(pos.X-36) > 20 || math.Abs(pos.Z-36) > 20 {
		t.Errorf("arrival = (%v, %v), want near (36, 36)", pos.X, pos.Z)
	}
}
