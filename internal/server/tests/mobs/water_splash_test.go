package mobs

import (
	"sync/atomic"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// splashPool builds a stone basin with a 3x3 pool of water at y 140 and
// stands the player beside it, above any terrain.
func splashPool(t *testing.T, prep ...func(*server.Server)) (*server.Server, *servertest.Client, *atomic.Int32, *atomic.Int32, *atomic.Int32) {
	t.Helper()
	srv, connect := servertest.Start(t, servertest.MobTestConfig(), prep...)
	srv.SetDayTime(18000)
	c := connect().Join("Swimmer")
	stone := registry.BlockStone.DefaultState()
	for x := int32(-1); x <= 9; x++ {
		for z := int32(-1); z <= 9; z++ {
			srv.SetBlock(x, 139, z, stone)
		}
	}
	for x := int32(2); x <= 6; x++ {
		for z := int32(2); z <= 6; z++ {
			ring := x == 2 || x == 6 || z == 2 || z == 6
			state := registry.BlockWater.DefaultState()
			if ring {
				state = stone
			}
			srv.SetBlock(x, 140, z, state)
		}
	}
	c.MoveToGround(0.5, 140, 0.5, true)
	c.Settle()
	var splashes, flaps, bounces atomic.Int32
	srv.ObserveGameEvents(func(event string, x, y, z float64) {
		switch event {
		case "minecraft:splash":
			splashes.Add(1)
		case "minecraft:flap":
			flaps.Add(1)
		case "minecraft:bounce":
			bounces.Add(1)
		}
	})
	return srv, c, &splashes, &flaps, &bounces
}

// soundsHeard returns the names of the sounds the client receives next.
func soundsHeard(t *testing.T, c *servertest.Client) map[string]bool {
	t.Helper()
	names := map[string]bool{}
	c.SettleAndCollect(func(id int32, data []byte) {
		if id == protocol.IDPlaySoundEffect {
			name, _, _, _, _, _, _ := servertest.ReadAmbient(t, data)
			names[name] = true
		}
	})
	return names
}

// projectileSplashes summons a projectile falling into the pool and reports
// whether the client heard entity.generic.splash once the splash game event
// was posted within 20 ticks.
// vanilla: Entity.updateFluidInteraction, Entity.doWaterSplashEffect
func projectileSplashes(t *testing.T, kind string) {
	t.Helper()
	srv, c, splashes, _, _ := splashPool(t)
	srv.RunLine("summon " + kind + " 4.5 143 4.5 {Motion:[0d,-0.6d,0d]}")
	servertest.UntilTicks(t, srv, 20, "the splash game event", func() bool { return splashes.Load() >= 1 })
	if names := soundsHeard(t, c); !names["minecraft:entity.generic.splash"] {
		t.Fatalf("heard %v, want entity.generic.splash", names)
	}
}

func TestAnArrowSplashesIntoWater(t *testing.T) {
	t.Parallel()
	projectileSplashes(t, "minecraft:arrow")
}
