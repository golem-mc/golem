package movement

import (
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
)

// A cart summoned inside a stone block sinks through it to the stone below,
// as a box ignores a shape it already overlaps.
// vanilla: Entity.collide, Shapes.collide
func TestWedgedCartFallsThroughItsBlock(t *testing.T) {
	t.Parallel()
	srv, _ := servertest.Join(t, servertest.BlockTestConfig(), "Watcher")
	servertest.HoldTicks(t, srv)
	stone := servertest.BlockState(t, "minecraft:stone")
	for _, y := range []int32{61, 62} {
		srv.SetBlock(20, y, 20, stone)
	}
	cart := srv.SpawnMinecartOfKind("minecraft:furnace_minecart", 20.5, 62.7, 20.5)
	servertest.RunTicks(t, srv, 20)
	for _, ticks := range []int{20, 100} {
		servertest.RunTicks(t, srv, ticks-20)
		if _, y, _, _, ok := srv.MinecartState(cart); !ok || math.Abs(y-62) > 1e-6 {
			t.Errorf("after %d ticks the cart is at y=%v (present %v), want 62", ticks, y, ok)
		}
	}
}

// A frozen game moves no cart nobody rides, wedged or on its own motion.
// vanilla: TickRateManager.isEntityFrozen
func TestFrozenGameMovesNoUnriddenCart(t *testing.T) {
	t.Parallel()
	srv, _ := servertest.Join(t, servertest.BlockTestConfig(), "Watcher")
	servertest.HoldTicks(t, srv)
	servertest.RunTicks(t, srv, 1)
	stone := servertest.BlockState(t, "minecraft:stone")
	for _, y := range []int32{61, 62} {
		srv.SetBlock(20, y, 20, stone)
	}
	wedged := srv.SpawnMinecartOfKind("minecraft:furnace_minecart", 20.5, 62.7, 20.5)
	moving := srv.SpawnMinecartOfKind("minecraft:furnace_minecart", 30.5, 64, 30.5)
	srv.SetMinecartMotion(moving, 0.3, 0, 0)
	srv.SetTickFrozen(true)
	servertest.RunTicks(t, srv, 100)
	for _, id := range []int32{wedged, moving} {
		x, y, z, _, _ := srv.MinecartState(id)
		if (id == wedged && (x != 20.5 || y != 62.7 || z != 20.5)) || (id == moving && (x != 30.5 || y != 64 || z != 30.5)) {
			t.Errorf("cart %d moved to %v %v %v while frozen", id, x, y, z)
		}
	}
}
