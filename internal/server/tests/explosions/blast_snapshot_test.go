package explosions

import (
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

func TestSuccessiveBlastsSeeThePrecedingCrater(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.ViewDistance = config.MinDistance
	cfg.SimulationDistance = config.MinDistance
	srv, _ := servertest.Join(t, cfg, "Watcher")
	servertest.HoldTicks(t, srv)
	stone := registry.BlockStone.DefaultState()
	for x := int32(2); x <= 14; x++ {
		for y := int32(54); y <= 66; y++ {
			for z := int32(2); z <= 14; z++ {
				srv.SetBlock(x, y, z, stone)
			}
		}
	}
	remaining := func() int {
		n := 0
		for x := int32(2); x <= 14; x++ {
			for y := int32(54); y <= 66; y++ {
				for z := int32(2); z <= 14; z++ {
					if srv.BlockAt(x, y, z) == stone {
						n++
					}
				}
			}
		}
		return n
	}
	before := remaining()
	// RunExplosion uses the same ray RNG seed each time. No tick advances
	// between these blasts: only the first blast's removals can extend the second.
	srv.RunExplosion(8.5, 60.5, 8.5, 4)
	afterFirst := remaining()
	srv.RunExplosion(8.5, 60.5, 8.5, 4)
	afterSecond := remaining()
	if afterFirst >= before || afterSecond >= afterFirst {
		t.Fatalf("stone counts %d -> %d -> %d: each blast must deepen the crater", before, afterFirst, afterSecond)
	}
}
