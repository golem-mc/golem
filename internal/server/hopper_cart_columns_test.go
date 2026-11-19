package server

import (
	mrand "math/rand"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/world"
)

func testCart(w *world.World, x, y, z float64) *minecartEntity {
	return &minecartEntity{
		w:     w,
		cart:  &entity.Minecart{X: x, Y: y, Z: z},
		cargo: vehicleCargo{slots: make([]protocol.ItemStack, 5)},
	}
}

func TestCartColumnsKeepEveryIntersectingBox(t *testing.T) {
	w := &world.World{}
	// The box spans x -0.49..0.49 and z 1.51..2.49, so a unit query box can
	// reach it from columns -1..0 and 1..2.
	c := testCart(w, 0, 80, 2)
	s := &Server{minecarts: []*minecartEntity{c}, levelRng: mrand.New(mrand.NewSource(1))}
	s.indexCartColumns(w)
	defer s.dropCartColumns()
	for _, x := range []float64{-0.9, 0.9} {
		for _, z := range []float64{1.1, 2.9} {
			if got := s.entityContainerAt(w, x, 80.3, z, nil); got != (cartTarget{s: s, it: c}) {
				t.Errorf("cart not found from %v,%v", x, z)
			}
		}
	}
	if got := s.entityContainerAt(w, 5.5, 80.3, 5.5, nil); got != nil {
		t.Errorf("found distant cart: %v", got)
	}
}

func TestCartColumnsIncludeCartsRegisteredDuringPass(t *testing.T) {
	w := &world.World{}
	s := &Server{levelRng: mrand.New(mrand.NewSource(1))}
	s.indexCartColumns(w)
	defer s.dropCartColumns()
	c := testCart(w, 4.5, 80, 5.5)
	s.registerMinecart(c)
	if got := s.entityContainerAt(w, 4.5, 80.3, 5.5, nil); got != (cartTarget{s: s, it: c}) {
		t.Fatalf("cart registered during the pass not found: %v", got)
	}
}

func TestCartColumnsMatchFullScan(t *testing.T) {
	w := &world.World{}
	var carts []*minecartEntity
	for i := range 6 {
		carts = append(carts, testCart(w, 0.2*float64(i), 80, 0.3*float64(i)))
	}
	dead := testCart(w, 0.5, 80, 0.5)
	dead.cart.Dead = true
	carts = append(carts, dead, testCart(&world.World{}, 0.5, 80, 0.5))
	pick := func(indexed bool) []*minecartEntity {
		s := &Server{minecarts: carts, levelRng: mrand.New(mrand.NewSource(7))}
		if indexed {
			s.indexCartColumns(w)
			defer s.dropCartColumns()
		}
		var out []*minecartEntity
		for i := range 20 {
			out = append(out, s.entityContainerAt(w, 0.5, 80.3, 0.5, carts[i%2]).(cartTarget).it)
		}
		return out
	}
	scan, idx := pick(false), pick(true)
	for i := range scan {
		if scan[i] != idx[i] {
			t.Fatalf("pick %d differs between index and full scan", i)
		}
	}
}

func TestCartColumnsDropAndOtherWorld(t *testing.T) {
	w, other := &world.World{}, &world.World{}
	c := testCart(other, 30.5, 80, 30.5)
	s := &Server{minecarts: []*minecartEntity{c}, levelRng: mrand.New(mrand.NewSource(1))}
	s.indexCartColumns(w)
	if s.cartCols == nil {
		t.Fatal("no index during the pass")
	}
	if got := s.entityContainerAt(other, 30.5, 80.3, 30.5, nil); got != (cartTarget{s: s, it: c}) {
		t.Error("another world must fall back to the full scan")
	}
	s.dropCartColumns()
	if s.cartCols != nil {
		t.Error("index kept after dropCartColumns")
	}
	if got := s.entityContainerAt(other, 30.5, 80.3, 30.5, nil); got != (cartTarget{s: s, it: c}) {
		t.Error("lookup outside the pass must scan")
	}
}

func TestCartColumnsTeleportFallsBackToScan(t *testing.T) {
	w := &world.World{}
	c := testCart(w, 0.5, 80, 0.5)
	s := &Server{minecarts: []*minecartEntity{c}, levelRng: mrand.New(mrand.NewSource(1))}
	s.indexCartColumns(w)
	defer s.dropCartColumns()
	s.teleportMinecart(c, player.Position{X: 40.5, Y: 80, Z: 40.5}, 0, "test")
	if got := s.entityContainerAt(w, 40.5, 80.3, 40.5, nil); got != (cartTarget{s: s, it: c}) {
		t.Error("teleported cart not found at its destination")
	}
	if got := s.entityContainerAt(w, 0.5, 80.3, 0.5, nil); got != nil {
		t.Error("cart still found at its old place")
	}
}

func TestEntityContainersFilterBoats(t *testing.T) {
	w, other := &world.World{}, &world.World{}
	boat := func(w *world.World, x, y, z float64) *boatEntity {
		return &boatEntity{w: w, boat: &entity.Boat{X: x, Y: y, Z: z}, cargo: make([]protocol.ItemStack, 27)}
	}
	dead, plain := boat(w, 0.5, 80, 0.5), boat(w, 0.5, 80, 0.5)
	dead.boat.Dead = true
	plain.cargo = nil
	s := &Server{levelRng: mrand.New(mrand.NewSource(1)), boats: []*boatEntity{
		dead, plain, boat(other, 0.5, 80, 0.5), boat(w, 10, 80, 0.5), boat(w, 0.5, 82, 0.5),
	}}
	s.indexCartColumns(w)
	defer s.dropCartColumns()
	if got := s.entityContainerAt(w, 0.5, 80.5, 0.5, nil); got != nil {
		t.Fatalf("selected ineligible boat: %v", got)
	}
	// A hull's edge overlaps even when its centre is outside the query cell.
	live := boat(w, 1.5, 80, 0.5)
	s.registerBoat(live)
	if got := s.entityContainerAt(w, 0.5, 80.5, 0.5, nil); got != (boatTarget{s: s, it: live}) {
		t.Fatalf("overlapping hull not found with an empty cart index: %v", got)
	}
	live.boat.Y = 81
	if got := s.entityContainerAt(w, 0.5, 80.5, 0.5, nil); got != nil {
		t.Fatalf("touching the query box boundary counts as overlap: %v", got)
	}
}

func TestVehicleColumnsMatchMixedScan(t *testing.T) {
	w, other := &world.World{}, &world.World{}
	carts := []*minecartEntity{testCart(w, 0.5, 80, 0.5), testCart(w, 0.9, 80, 0.5)}
	boats := []*boatEntity{
		{w: w, boat: &entity.Boat{X: -0.5, Y: 80, Z: 0.5}, cargo: make([]protocol.ItemStack, 27)},
		{w: w, boat: &entity.Boat{X: 1.5, Y: 80, Z: 0.5}, cargo: make([]protocol.ItemStack, 27)},
		{w: other, boat: &entity.Boat{X: 0.5, Y: 80, Z: 0.5}, cargo: make([]protocol.ItemStack, 27)},
	}
	pick := func(indexed bool) []containerTarget {
		s := &Server{minecarts: carts, boats: boats, levelRng: mrand.New(mrand.NewSource(7))}
		if indexed {
			s.indexCartColumns(w)
			defer s.dropCartColumns()
		}
		var out []containerTarget
		for i := range 40 {
			got := s.entityContainerAt(w, 0.5, 80.3, 0.5, carts[i%2])
			// Targets carry their server; compare the selected bodies across runs.
			switch got := got.(type) {
			case cartTarget:
				got.s = nil
				out = append(out, got)
			case boatTarget:
				got.s = nil
				out = append(out, got)
			default:
				t.Fatalf("missing mixed container: %v", got)
			}
		}
		if got := s.entityContainerAt(other, 0.5, 80.3, 0.5, nil); got != (boatTarget{s: s, it: boats[2]}) {
			t.Fatalf("boat in another world not found: %v", got)
		}
		return out
	}
	scan, indexed := pick(false), pick(true)
	for i := range scan {
		if scan[i] != indexed[i] {
			t.Fatalf("mixed pick %d differs: scan %v, index %v", i, scan[i], indexed[i])
		}
	}
}

func TestBoatColumnsTeleportFallsBackToScan(t *testing.T) {
	w := &world.World{}
	boat := &boatEntity{w: w, boat: &entity.Boat{X: 0.5, Y: 80, Z: 0.5}, cargo: make([]protocol.ItemStack, 27)}
	s := &Server{boats: []*boatEntity{boat}, levelRng: mrand.New(mrand.NewSource(1))}
	s.indexCartColumns(w)
	defer s.dropCartColumns()
	s.teleportBoat(boat, player.Position{X: 40.5, Y: 80, Z: 40.5}, 0, "test")
	if got := s.entityContainerAt(w, 40.5, 80.3, 40.5, nil); got != (boatTarget{s: s, it: boat}) {
		t.Fatalf("teleported boat not found at its destination: %v", got)
	}
	if got := s.entityContainerAt(w, 0.5, 80.3, 0.5, nil); got != nil {
		t.Fatalf("boat still found at its old place: %v", got)
	}
}
