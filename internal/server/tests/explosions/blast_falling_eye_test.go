package explosions

import (
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestBlastPushesFallingBlockFromEyeHeight checks the blast's push on a
// falling block is directed from its eye, 0.833 above the feet.
// vanilla: ServerExplosion.hurtEntities
func TestBlastPushesFallingBlockFromEyeHeight(t *testing.T) {
	t.Parallel()
	cfg := servertest.MobTestConfig()
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	srv, c := servertest.Join(t, cfg, "Watcher")
	servertest.HoldTicks(t, srv)

	sand := srv.SpawnFallingBlock(3, 80, 8, servertest.BlockState(t, "minecraft:sand"))
	tnt := srv.SpawnTNT(0.5, 77, 8.5, 3)
	if sand == 0 || tnt == 0 {
		t.Fatal("failed to spawn sand or TNT")
	}
	explosions := 0
	c.ReadTicks(srv, 3, func(id int32, _ []byte) bool {
		if id == protocol.IDPlayExplosion {
			explosions++
		}
		return false
	})
	if explosions != 1 {
		t.Fatalf("explosions = %d, want 1", explosions)
	}
	vx, vy, vz, ok := srv.FallingBlockVelocity(sand)
	if !ok {
		t.Fatal("sand is gone")
	}
	if math.Abs(vx-0.2957) > 1e-3 || math.Abs(vy-0.2565) > 1e-3 || math.Abs(vz) > 1e-3 {
		t.Fatalf("sand motion = [%.4f %.4f %.4f], want [0.2957 0.2565 0]", vx, vy, vz)
	}
}
