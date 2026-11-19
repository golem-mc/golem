package mobs

import (
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/server/entity"
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

// vanilla: Shulker.makeBoundingBox, getProgressAabb
func TestAScaledShulkerHasADoubledBox(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.HostileTestConfig(), "Sizer", servertest.OpsListing(t, "Sizer"))
	servertest.HoldTicks(t, srv)
	id := srv.SpawnMob("minecraft:shulker", 3.5, 64, 3.5)
	srv.SetMobNoAI(id)
	c.ChatCommand("attribute @e[type=minecraft:shulker,limit=1] minecraft:scale base set 2")
	servertest.AwaitSystemChatSaying(t, c, "commands.attribute.base_value.set.success")

	minX, minY, minZ, maxX, maxY, maxZ, ok := srv.MobBox(id)
	if !ok {
		t.Fatal("shulker is gone")
	}
	if maxX-minX != 2 || maxY-minY != 2 || maxZ-minZ != 2 || minX != 2.5 || minY != 64 || minZ != 2.5 {
		t.Errorf("scaled shulker box = (%v,%v,%v)..(%v,%v,%v), want (2.5,64,2.5)..(4.5,66,4.5)", minX, minY, minZ, maxX, maxY, maxZ)
	}

	// The upper shell is outside an unscaled shulker's hit box.
	ball := srv.ThrowThrown(entity.ThrownSnowball, 3.5, 65.5, 1.5, 0, 0, 0, 1)
	srv.TickEntities(5)
	for _, body := range srv.ThrownInDimension("minecraft:overworld") {
		if body.ID == ball {
			t.Errorf("snowball passed through the enlarged shell: %+v", body)
		}
	}

	pig := srv.SpawnMob("minecraft:pig", 4.3, 66, 3.5)
	srv.SetMobNoAI(pig)
	srv.SetShulkerPeek(id, 100)
	srv.TickMobs(20)
	if _, _, _, _, top, _, _ := srv.MobBox(id); math.Abs(top-68) > 1e-6 {
		t.Errorf("open scaled shell top = %v, want 68", top)
	}
	if _, y, _, ok := srv.MobPos(pig); !ok || math.Abs(y-68) > 1e-6 {
		t.Errorf("pig on scaled lid at y=%v, alive=%v, want y=68", y, ok)
	}
}
