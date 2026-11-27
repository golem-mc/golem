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

// A furnace minecart resting on the top face of another cart (at float32
// height 0.7f) inside a dispenser stays put over 1, 20 and 100 ticks: the
// lower cart's hull supports it and neither cart pushes the other.
// vanilla: EntityDimensions.makeBoundingBox, AbstractMinecart.canCollideWith, OldMinecartBehavior.pushAndPickupEntities
func TestStackedFurnaceMinecartsStayPut(t *testing.T) {
	t.Parallel()
	srv, _ := servertest.Join(t, servertest.BlockTestConfig(), "Watcher")
	servertest.HoldTicks(t, srv)

	air := servertest.BlockState(t, "minecraft:air")
	stone := servertest.BlockState(t, "minecraft:stone")
	dispenser := servertest.BlockState(t, "minecraft:dispenser")
	quartz := servertest.BlockState(t, "minecraft:quartz_block")

	for _, baseY := range []int32{64, -56} {
		const bx, bz int32 = 20, 20
		for dx := int32(0); dx <= 2; dx++ {
			srv.SetBlock(bx+dx, baseY-1, bz, stone)
			srv.SetBlock(bx+dx, baseY, bz, air)
			srv.SetBlock(bx+dx, baseY+1, bz, air)
		}
		srv.SetBlock(bx+1, baseY, bz, dispenser)
		srv.SetBlock(bx+2, baseY, bz, quartz)

		x1, y1, z1 := float64(bx)+0.6079331410046578, float64(baseY), float64(bz)+0.4989917801778301
		x2, y2, z2 := float64(bx)+1.5099999904632568, float64(baseY)+float64(float32(0.7)), float64(bz)+0.4999368508987052
		cart1 := srv.SpawnMinecartOfKind("minecraft:furnace_minecart", x1, y1, z1)
		cart2 := srv.SpawnMinecartOfKind("minecraft:furnace_minecart", x2, y2, z2)

		elapsed := 0
		for _, target := range []int{1, 20, 100} {
			servertest.RunTicks(t, srv, target-elapsed)
			elapsed = target
			if gx, gy, gz, _, ok := srv.MinecartState(cart1); !ok || gx != x1 || gy != y1 || gz != z1 {
				t.Errorf("baseY=%d after %d ticks: cart1=(%v, %v, %v) present=%v, want (%v, %v, %v)", baseY, target, gx, gy, gz, ok, x1, y1, z1)
			}
			if gx, gy, gz, _, ok := srv.MinecartState(cart2); !ok || gx != x2 || gy != y2 || gz != z2 {
				t.Errorf("baseY=%d after %d ticks: cart2=(%v, %v, %v) present=%v, want (%v, %v, %v)", baseY, target, gx, gy, gz, ok, x2, y2, z2)
			}
		}
	}
}
