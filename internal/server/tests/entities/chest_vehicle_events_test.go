package entities

import (
	"sync"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
)

// TestChestVehiclesPostContainerOpenAndClose: a chest boat and a chest
// minecart post container_open and container_close at their position; a
// hopper minecart posts neither.
// vanilla: AbstractChestBoat.stopOpen, MinecartChest.stopOpen
func TestChestVehiclesPostContainerOpenAndClose(t *testing.T) {
	t.Parallel()
	srv, w := chestCartPlayer(t, "survival", 0)
	cartID := summonChestCart(t, srv, "")
	srv.RunLine("summon minecraft:hopper_minecart 5.5 64.0625 2.5")
	boatID := srv.SpawnBoat(2.5, 64, 4.5, "minecraft:oak_chest_boat")
	var hopperID int32
	for _, cart := range srv.MinecartsInDimension("minecraft:overworld") {
		if cart.ID != cartID {
			hopperID = cart.ID
		}
	}
	if hopperID == 0 || boatID == 0 {
		t.Fatal("a vehicle did not stand")
	}
	var mu sync.Mutex
	seen := map[string][][3]float64{}
	srv.ObserveGameEvents(func(event string, x, y, z float64) {
		mu.Lock()
		seen[event] = append(seen[event], [3]float64{x, y, z})
		mu.Unlock()
	})
	w.MoveTo(4.5, 64, 2.7, 0, 60)
	w.Settle()

	count := func(event string, at [3]float64) int {
		mu.Lock()
		defer mu.Unlock()
		n := 0
		for _, p := range seen[event] {
			if p == at {
				n++
			}
		}
		return n
	}
	for _, tc := range []struct {
		name   string
		at     [3]float64
		menu   int32
		open   func()
		events int
	}{
		{"boat", [3]float64{2.5, 64, 4.5}, protocol.MenuGeneric9x3, func() { w.UseOnSneaking(boatID) }, 1},
		{"cart", [3]float64{4.5, 64.0625, 1.5}, protocol.MenuGeneric9x3, func() { w.UseOn(cartID) }, 1},
		{"hopper cart", [3]float64{5.5, 64.0625, 2.5}, protocol.MenuHopper, func() { w.UseOn(hopperID) }, 0},
	} {
		tc.open()
		w.ExpectContainerOpen(tc.menu, true, 0, 0, 0)
		w.Settle()
		if got := count("minecraft:container_open", tc.at); got != tc.events {
			t.Errorf("%s: %d container_open at %v, want %d (%v)", tc.name, got, tc.at, tc.events, seen)
		}
		w.CloseWindow()
		w.Settle()
		if got := count("minecraft:container_close", tc.at); got != tc.events {
			t.Errorf("%s: %d container_close at %v, want %d", tc.name, got, tc.at, tc.events)
		}
	}
}
