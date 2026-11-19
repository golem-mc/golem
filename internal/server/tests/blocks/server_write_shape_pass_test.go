package blocks

import (
	"testing"

	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// watchServerWrite lays the cells with an observer at (ox, oy, oz), runs step
// until the observer pulses or six rounds pass, and fails unless it did.
//
// vanilla: Level.setBlock runs the shape pass for every flag set without 16.
func watchServerWrite(t *testing.T, lay func(t *testing.T, c *servertest.Client), ox, oy, oz int32, facing string, trigger func(srv *server.Server, c *servertest.Client), step func(srv *server.Server)) {
	t.Helper()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Camper", servertest.OpsListing(t, "Camper"))
	srv.StopTick()
	servertest.SeedPhysics(t, srv, 1)
	lay(t, c)
	servertest.Setblock(t, c, ox, oy, oz, "observer[facing="+facing+"]")
	c.Settle()
	srv.TickPhysics(8)
	if _, powered := servertest.ObserverPowerAt(srv, ox, oy, oz); powered {
		t.Fatal("setup: the observer was already pulsing")
	}
	trigger(srv, c)
	for range 6 {
		step(srv)
		if _, powered := servertest.ObserverPowerAt(srv, ox, oy, oz); powered {
			return
		}
	}
	t.Error("the server write did not pulse the observer watching it")
}

func TestAFenceReconnectingPulsesTheObserverAboveIt(t *testing.T) {
	t.Parallel()
	watchServerWrite(t, func(t *testing.T, c *servertest.Client) {
		servertest.Setblock(t, c, 5, 64, 0, "oak_fence")
	}, 5, 65, 0, "down", func(srv *server.Server, c *servertest.Client) { servertest.Setblock(t, c, 5, 64, 1, "stone") }, func(srv *server.Server) { srv.TickPhysics(1) })
}

func TestGrazedGrassPulsesTheObserverBesideIt(t *testing.T) {
	t.Parallel()
	watchServerWrite(t, func(t *testing.T, c *servertest.Client) {
		servertest.Setblock(t, c, 5, 64, 0, "grass_block")
	}, 6, 64, 0, "west", func(srv *server.Server, _ *servertest.Client) { srv.GrazeGroundAt(5, 64, 0) }, func(srv *server.Server) { srv.TickPhysics(1) })
}

func TestALightingFurnacePulsesTheObserverAboveIt(t *testing.T) {
	t.Parallel()
	watchServerWrite(t, func(t *testing.T, c *servertest.Client) {
		servertest.Setblock(t, c, 5, 64, 0, `furnace[facing=north]{Items:[{Slot:0b,id:"minecraft:raw_iron",count:1},{Slot:1b,id:"minecraft:coal",count:1}]}`)
	}, 5, 65, 0, "down", func(*server.Server, *servertest.Client) {}, func(srv *server.Server) { srv.TickBlockEntities(); srv.TickPhysics(1) })
}
