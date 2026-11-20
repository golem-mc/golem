package plugins_test

import (
	"testing"

	"github.com/golem-mc/golem/plugin"
	"github.com/golem-mc/golem/registry"
)

// vanilla: ServerEntity.sendChanges
func TestPluginVehicleMoveEvent(t *testing.T) {
	t.Parallel()
	var cartMoves, boatMoves, idle int
	var lastCart plugin.VehicleMoveEvent
	srv, _ := tickScene(t, func(s plugin.Server) {
		plugin.Listen(s, func(e *plugin.VehicleMoveEvent) {
			switch e.Vehicle.Type() {
			case registry.EntityTypeMinecart:
				cartMoves++
				lastCart = *e
			case registry.EntityTypeOakBoat:
				boatMoves++
			default:
				idle++
			}
		})
	})
	// The first pass only records where the vehicles stand.
	srv.RunTick()
	cart := srv.SpawnMinecart(4.5, 90, 4.5)
	srv.SpawnBoat(8.5, 90, 8.5, "minecraft:oak_boat")
	for range 5 {
		srv.RunTick()
	}
	if cartMoves == 0 || boatMoves == 0 || idle != 0 {
		t.Fatalf("falling vehicles: cart %d, boat %d, other %d events", cartMoves, boatMoves, idle)
	}
	if lastCart.To.Pos.Y >= lastCart.From.Pos.Y {
		t.Errorf("fall From %v To %v, want To lower", lastCart.From.Pos, lastCart.To.Pos)
	}
	if _, y, _, _, ok := srv.MinecartState(cart); !ok || y != lastCart.To.Pos.Y {
		t.Errorf("cart at y %v, event To %v", y, lastCart.To.Pos.Y)
	}
	// Landed and still: no further event.
	ticksUntil(t, srv, "cart landed", func() bool {
		_, y, _, _, _ := srv.MinecartState(cart)
		return y < 70
	})
	for range 20 {
		srv.RunTick()
	}
	before := cartMoves + boatMoves
	for range 20 {
		srv.RunTick()
	}
	if cartMoves+boatMoves != before {
		t.Errorf("idle vehicles fired %d events", cartMoves+boatMoves-before)
	}
}

// vanilla: ServerEntity.sendChanges
func TestPluginEntityMoveEvent(t *testing.T) {
	t.Parallel()
	var moves int
	var cancel bool
	var last plugin.EntityMoveEvent
	srv, _ := tickScene(t, func(s plugin.Server) {
		plugin.Listen(s, func(e *plugin.EntityMoveEvent) {
			moves++
			last = *e
			e.SetCancelled(cancel)
		})
	})
	srv.RunTick()
	id := srv.SpawnMob("minecraft:zombie", 3.5, 90, 3.5)
	srv.RunTick()
	srv.RunTick()
	if moves == 0 || last.To.Pos.Y >= last.From.Pos.Y || last.Entity.Type() != registry.EntityTypeZombie {
		t.Fatalf("falling zombie: %d events, From %v To %v", moves, last.From.Pos, last.To.Pos)
	}
	// Land and settle, then a still zombie fires nothing.
	ticksUntil(t, srv, "zombie landed", func() bool { _, y, _, _ := srv.MobPos(id); return y < 70 })
	for range 5 {
		srv.RunTick()
	}
	settled := moves
	for range 10 {
		srv.RunTick()
	}
	if moves != settled {
		t.Fatalf("a standing zombie fired %d events", moves-settled)
	}
	// A cancelled fall keeps the mob at its spawn point, and every step it
	// tries fires an event.
	cancel = true
	id = srv.SpawnMob("minecraft:zombie", 6.5, 90, 6.5)
	moves = 0
	const ticks = 8
	for range ticks {
		srv.RunTick()
		if x, y, z, _ := srv.MobPos(id); x != 6.5 || y != 90 || z != 6.5 {
			t.Fatalf("cancelled zombie at %v %v %v, want 6.5 90 6.5", x, y, z)
		}
	}
	// The mob steps only on its own update ticks; each step it takes is told.
	if moves < 2 {
		t.Errorf("cancelled zombie fired %d events in %d ticks", moves, ticks)
	}
}
