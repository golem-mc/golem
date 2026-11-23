package plugins_test

import (
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/plugin"
	"github.com/golem-mc/golem/registry"
)

// vanilla: MinecraftServer.setInitialSpawn
func TestPluginEndWorldSpawnOnSurface(t *testing.T) {
	cfg := servertest.BlockTestConfig()
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	srv, h, run := worldPlugin(t, cfg)
	endKey := plugin.Key{Namespace: "test", Path: "end"}
	opts := plugin.WorldOptions{Key: endKey, Dimension: plugin.MinecraftKey("the_end"), Seed: 73}
	var w plugin.World
	create := func() {
		run(func() {
			var err error
			if w, err = h.CreateWorld(opts); err != nil {
				t.Errorf("CreateWorld: %v", err)
			}
		})
		if w == nil {
			t.FailNow()
		}
	}
	create()
	var sp plugin.BlockPos
	run(func() { sp = w.SpawnPoint().Pos.Block() })
	block := func(dy int) uint16 {
		return srv.LoadBlockInDimension(endKey.String(), int32(sp.X), int32(sp.Y+dy), int32(sp.Z))
	}
	air := uint16(registry.BlockAir.DefaultState())
	if !registry.CollisionTopFull(block(-1)) || block(0) != air || block(1) != air {
		t.Fatalf("spawn %v: below %d, feet %d, head %d", sp, block(-1), block(0), block(1))
	}

	servertest.ReleaseTicks(t, srv)
	c := servertest.DialServer(t, srv)
	c.Join("EndSpawnTester")
	srv.MovePlayerToDimension("EndSpawnTester", endKey.String(), float64(sp.X)+0.5, float64(sp.Y), float64(sp.Z)+0.5)
	if _, ok := servertest.ReadUntilViewSent(t, c); !ok {
		t.Fatal("no view in the end world")
	}
	servertest.HoldTicks(t, srv)

	if err := unloadFlat(t, run, h, w, true); err != nil {
		t.Fatal(err)
	}
	create()
	run(func() {
		if got := w.SpawnPoint().Pos.Block(); got != sp {
			t.Errorf("reloaded spawn = %v, want %v", got, sp)
		}
	})
}
