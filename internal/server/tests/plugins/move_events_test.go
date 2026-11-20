package plugins_test

import (
	"testing"
	"time"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
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

// vanilla: ServerGamePacketListenerImpl.handleMoveVehicle
func TestPluginVehicleMoveEventSteeredBoat(t *testing.T) {
	t.Parallel()
	var moves, playerMoves int
	var last plugin.VehicleMoveEvent
	srv, c := tickScene(t, func(s plugin.Server) {
		plugin.Listen(s, func(e *plugin.VehicleMoveEvent) { moves++; last = *e })
		plugin.Listen(s, func(*plugin.PlayerMoveEvent) { playerMoves++ })
	})
	srv.RunTick()
	boatID := srv.SpawnBoat(4.5, 64, 4.5, "minecraft:oak_boat")
	for range 40 {
		srv.RunTick()
	}
	c.UseOn(boatID)
	servertest.AwaitPassengersFor(c, boatID)
	for range 5 {
		srv.RunTick()
	}
	_, y, _, _, _, _ := srv.BoatState(boatID)
	steer := func(x float64, yaw float32) {
		c.Send(protocol.IDPlayMoveVehicleIn, func(b *protocol.Buffer) {
			b.PutF64(x)
			b.PutF64(y)
			b.PutF64(4.5)
			b.PutF32(yaw)
			b.PutF32(0)
			b.PutBool(true)
		})
		time.Sleep(50 * time.Millisecond)
		srv.RunTick()
		srv.RunTick()
	}
	moves = 0
	steer(5.5, 0)
	if moves == 0 || last.To.Pos.X != 5.5 {
		t.Fatalf("steered boat: %d events, To %v", moves, last.To.Pos)
	}
	// Turning in place is a move too.
	moves = 0
	steer(5.5, 30)
	if moves != 1 || last.To.Pos.X != last.From.Pos.X || last.To.Yaw == last.From.Yaw {
		t.Errorf("turning boat: %d events, From %+v To %+v", moves, last.From, last.To)
	}
	// Nothing changed: nothing fired.
	moves = 0
	steer(5.5, 30)
	if moves != 0 {
		t.Errorf("unchanged boat fired %d events", moves)
	}
}

// vanilla: ServerEntity.sendChanges
func TestPluginEntityMoveEventWalkingZombie(t *testing.T) {
	t.Parallel()
	var moves, vehicle int
	var first, last plugin.EntityMoveEvent
	srv, _ := tickScene(t, func(s plugin.Server) {
		plugin.Listen(s, func(e *plugin.EntityMoveEvent) {
			if moves == 0 {
				first = *e
			}
			moves++
			last = *e
		})
		plugin.Listen(s, func(*plugin.VehicleMoveEvent) { vehicle++ })
	})
	srv.RunTick()
	pos := srv.PlayerPosition("Victim")
	id := srv.SpawnMob("minecraft:zombie", pos.X+8, pos.Y, pos.Z)
	// Settle any fall, then count only what the walk fires.
	for range 40 {
		srv.RunTick()
	}
	if !srv.SetMobTargetPlayer(id, "Victim") {
		t.Fatal("no target set")
	}
	moves = 0
	x0, _, z0, _ := srv.MobPos(id)
	for range 60 {
		srv.RunTick()
	}
	x1, _, z1, _ := srv.MobPos(id)
	if x0 == x1 && z0 == z1 {
		t.Fatal("zombie did not walk")
	}
	if moves == 0 || first.From.Pos == first.To.Pos && first.From.Yaw == first.To.Yaw {
		t.Fatalf("walking zombie: %d events, first %+v", moves, first)
	}
	if last.To.Pos.X != x1 || last.To.Pos.Z != z1 {
		t.Errorf("last event To %v, zombie at %v %v", last.To.Pos, x1, z1)
	}
	if vehicle != 0 {
		t.Errorf("%d vehicle events", vehicle)
	}
}

// A moving player fires PlayerMoveEvent and neither of the entity events.
// vanilla: ServerGamePacketListenerImpl.handleMovePlayer
func TestPluginPlayerMoveFiresOnlyPlayerMoveEvent(t *testing.T) {
	t.Parallel()
	var player, entity, vehicle int
	srv, c := tickScene(t, func(s plugin.Server) {
		plugin.Listen(s, func(*plugin.PlayerMoveEvent) { player++ })
		plugin.Listen(s, func(*plugin.EntityMoveEvent) { entity++ })
		plugin.Listen(s, func(*plugin.VehicleMoveEvent) { vehicle++ })
	})
	srv.RunTick()
	p := srv.PlayerPosition("Victim")
	c.SendPosition(p.X+0.3, p.Y, p.Z, true)
	ticksUntil(t, srv, "player move event", func() bool { return player > 0 })
	if player == 0 || entity != 0 || vehicle != 0 {
		t.Fatalf("player %d, entity %d, vehicle %d events", player, entity, vehicle)
	}
}
