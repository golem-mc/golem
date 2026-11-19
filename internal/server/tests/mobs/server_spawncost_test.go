package mobs

import (
	"fmt"
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestWarpedForestEndermenKeepTheirSpawnCostDistance runs the midnight passes
// over a warped forest, whose enderman spawn cost (charge 1, budget 0.12)
// keeps a new enderman more than 1/0.12 blocks from the others.
func TestWarpedForestEndermenKeepTheirSpawnCostDistance(t *testing.T) {
	t.Parallel()
	srv, _ := servertest.Join(t, servertest.SpawningTestConfig(), "Watcher", servertest.OpsListing(t, "Watcher"))
	servertest.AwaitChunksSettled(t, srv)
	// /fillbiome takes at most 32768 blocks, so tile the surface band around
	// the player (blocks -64..63).
	for _, x := range []string{"-64 60 %s -1 67 %s", "0 60 %s 63 67 %s"} {
		for _, z := range [][2]string{{"-64", "-1"}, {"0", "63"}} {
			srv.RunLine("fillbiome " + fmt.Sprintf(x, z[0], z[1]) + " minecraft:warped_forest")
		}
	}
	srv.StopTick()
	srv.SetDayTime(midnight)

	for seed := int64(1); seed <= spawnPasses; seed++ {
		srv.SpawnPass(seed)
	}
	var endermen [][3]float64
	for _, m := range srv.Mobs() {
		if m.Kind == "minecraft:enderman" {
			endermen = append(endermen, [3]float64{math.Floor(m.X), math.Floor(m.Y), math.Floor(m.Z)})
		}
	}
	if len(endermen) < 2 {
		t.Fatalf("%d endermen spawned, want at least 2", len(endermen))
	}
	for i := range endermen {
		for j := i + 1; j < len(endermen); j++ {
			a, b := endermen[i], endermen[j]
			if d := math.Sqrt((a[0]-b[0])*(a[0]-b[0]) + (a[1]-b[1])*(a[1]-b[1]) + (a[2]-b[2])*(a[2]-b[2])); d < 1/0.12 {
				t.Errorf("endermen %v and %v are %.2f blocks apart, want at least %.2f", a, b, d, 1/0.12)
			}
		}
	}
}
