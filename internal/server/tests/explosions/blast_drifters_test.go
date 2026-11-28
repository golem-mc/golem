package explosions

import (
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/nbt"
)

// TestBlastPushesRocketAndMarker checks a blast pushes a firework rocket and an
// eye of ender and adds its knockback to a marker's, a cloud's and fangs' motion.
// vanilla: ServerExplosion.hurtEntities, Entity.push
func TestBlastPushesRocketAndMarker(t *testing.T) {
	t.Parallel()
	cfg := servertest.MobTestConfig()
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	srv, c := servertest.Join(t, cfg, "Watcher", servertest.OpsListing(t, "Watcher"))
	servertest.HoldTicks(t, srv)

	c.ChatCommand("summon marker 3.5 77 8.5")
	c.Settle()
	cloud := srv.SpawnCloudEntity(3.5, 77, 8.5)
	eye := srv.SpawnEyeEntity(3.5, 77, 8.5, 0, 0, 0)
	srv.SpawnFangs(3.5, 77, 8.5, 0)
	rocket := srv.SpawnFireworkAtAngle(3.5, 77, 8.5, 0, 0, 0, 200)
	srv.SpawnTNT(0.5, 77, 8.5, 1)
	servertest.RunTicks(t, srv, 4)

	x, ok := srv.FireworkPosition(rocket.UUID)
	if !ok {
		t.Fatal("rocket is gone")
	}
	if x-3.5 <= 1 {
		t.Fatalf("rocket x = %v, want it pushed beyond 4.5", x)
	}
	if vx, _, _ := srv.BlastMotion(cloud.UUID); vx <= 0 {
		t.Fatalf("cloud motion x = %v, want a push away from the blast", vx)
	}
	if vx, _, _ := srv.BlastMotion(eye.UUID); vx <= 0 {
		t.Fatalf("eye velocity x = %v, want a push away from the blast", vx)
	}
	if vx, _, _ := srv.NewestFangsMotion(); vx <= 0 {
		t.Fatalf("fangs motion x = %v, want a push away from the blast", vx)
	}
	motion, ok := nbt.Get[nbt.List](srv.FixtureSavedTag(), "Motion")
	if !ok || len(motion.Items) != 3 {
		t.Fatalf("marker Motion = %v", motion)
	}
	if got := float64(motion.Items[0].(nbt.Double)); math.Abs(got-0.625) > 1e-3 {
		t.Fatalf("marker Motion[0] = %v, want 0.625", got)
	}
}
