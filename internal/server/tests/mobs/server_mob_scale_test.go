package mobs

import (
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestAScaledZombieHasADoubledBox checks that the scale attribute scales a
// mob's box on the server.
// vanilla: LivingEntity.getDimensions
func TestAScaledZombieHasADoubledBox(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.HostileTestConfig(), "Sizer", servertest.OpsListing(t, "Sizer"))

	id := srv.SpawnMob("minecraft:zombie", 3.5, 64, 3.5)
	srv.SetMobBaby(id, false)
	srv.SetMobNoAI(id)
	c.ChatCommand("attribute @e[type=minecraft:zombie,limit=1] minecraft:scale base set 2")
	servertest.AwaitSystemChatSaying(t, c, "commands.attribute.base_value.set.success")
	servertest.RunTicks(t, srv, 1)

	minX, minY, _, maxX, maxY, _, ok := srv.MobBox(id)
	if !ok {
		t.Fatal("zombie is gone")
	}
	if w, h := maxX-minX, maxY-minY; math.Abs(w-1.2) > 1e-6 || math.Abs(h-3.9) > 1e-6 {
		t.Errorf("scaled zombie box = %v wide, %v tall, want 1.2 by 3.9", w, h)
	}
}
