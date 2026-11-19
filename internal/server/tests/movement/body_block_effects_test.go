package movement

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
)

// vanilla: FallingBlockEntity.tick, WebBlock.entityInside
func TestFallingSandSinksSlowlyThroughCobweb(t *testing.T) {
	t.Parallel()
	srv, _ := servertest.Join(t, servertest.BlockTestConfig(), "Watcher")
	servertest.HoldTicks(t, srv)
	srv.SetBlock(4, 70, 4, servertest.BlockState(t, "minecraft:cobweb"))
	id := srv.SpawnFallingBlock(4, 70, 4, servertest.BlockState(t, "minecraft:sand"))
	_, before, _, ok := srv.FallingBlockPosition(id)
	if !ok {
		t.Fatal("sand did not spawn")
	}
	srv.TickFallings(20)
	if n := srv.FallingCount(); n != 1 {
		t.Fatalf("falling bodies = %d, want 1", n)
	}
	_, after, _, ok := srv.FallingBlockPosition(id)
	if drop := before - after; !ok || drop <= 0 || drop >= 0.1 {
		t.Fatalf("sand fell %g blocks in twenty ticks (present %v), want between 0 and 0.1", drop, ok)
	}
}

// vanilla: PrimedTnt.tick, WebBlock.entityInside
func TestPrimedTNTSlowsInCobweb(t *testing.T) {
	t.Parallel()
	srv, _ := servertest.Join(t, servertest.BlockTestConfig(), "Watcher")
	servertest.HoldTicks(t, srv)
	srv.SetBlock(4, 70, 4, servertest.BlockState(t, "minecraft:cobweb"))
	id := srv.SpawnTNT(4.5, 70, 4.5, 80)
	if id == 0 {
		t.Fatal("TNT did not spawn")
	}
	srv.TickEntities(10)
	bodies := srv.TNTsInDimension(world.DimensionName)
	if len(bodies) != 1 || bodies[0].ID != id {
		t.Fatalf("TNT bodies = %v, want %d", bodies, id)
	}
	if drop := 70 - bodies[0].Y; drop <= 0 || drop >= 0.1 {
		t.Fatalf("TNT fell %g blocks in ten ticks, want between 0 and 0.1", drop)
	}
	if bodies[0].Fuse != 70 {
		t.Fatalf("fuse = %d, want 70", bodies[0].Fuse)
	}
}

// vanilla: Entity.restituteMovementAfterCollisions, LivingEntity.travelInAir
func TestAnArmorStandBouncesOnSlime(t *testing.T) {
	t.Parallel()
	for _, block := range []string{"minecraft:slime_block", "minecraft:red_bed", "minecraft:stone"} {
		t.Run(block, func(t *testing.T) {
			t.Parallel()
			srv, _ := servertest.Join(t, servertest.BlockTestConfig(), "Watcher")
			servertest.HoldTicks(t, srv)
			srv.SetBlock(4, 63, 4, servertest.BlockState(t, block))
			id := srv.SpawnArmorStand(4.5, 69, 4.5)
			bounced := false
			for range 20 {
				srv.TickArmorStands(1)
				_, vy, _, ok := srv.ArmorStandVelocity(id)
				if !ok {
					t.Fatal("stand disappeared")
				}
				bounced = bounced || vy > 0
			}
			if want := block != "minecraft:stone"; bounced != want {
				t.Fatalf("stand bounced = %v on %s, want %v", bounced, block, want)
			}
		})
	}
}
