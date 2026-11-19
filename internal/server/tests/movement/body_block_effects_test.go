package movement

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
)

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
