package world

import (
	"slices"
	"testing"

	"github.com/golem-mc/golem/registry"
)

func TestPistonRideMembershipKeepsPushOrder(t *testing.T) {
	w, p := newOneChunkPhysics(t)
	a, b, c := [3]int32{8, 70, 8}, [3]int32{9, 70, 8}, [3]int32{10, 70, 8}
	ride := movingPistonStateOf(dirUp, false)
	create := func(pos [3]int32) {
		w.SetBlock(pos[0], pos[1], pos[2], ride)
		p.pistonRideCreate(pos, ride, registry.BlockStone.DefaultState(), dirUp, true, false)
	}
	for _, pos := range [][3]int32{a, b, a, c} {
		create(pos)
	}
	check := func(want [][3]int32) {
		t.Helper()
		p.effects = nil
		p.pistonPushes()
		var got [][3]int32
		for _, e := range p.effects {
			if e.PistonPush != nil {
				got = append(got, [3]int32{e.X, e.Y, e.Z})
			}
		}
		if !slices.Equal(got, want) {
			t.Fatalf("push order = %v, want %v", got, want)
		}
		if len(p.rideSet) != len(p.rides) {
			t.Fatal("ride slice and membership diverged")
		}
		for _, pos := range p.rides {
			if _, ok := p.rideSet[pos]; !ok {
				t.Fatalf("live ride %v missing from membership", pos)
			}
		}
	}
	check([][3]int32{a, b, c})
	w.RemoveBlockEntity(a[0], a[1], a[2])
	w.RemoveBlockEntity(b[0], b[1], b[2])
	w.EnsureBlockEntity(b[0], b[1], b[2], KindChest)
	p.now = 1
	check([][3]int32{c}) // Missing, replaced and finished rides all leave the set.
	create(b)
	create(a)
	check([][3]int32{b, a})
}

func TestPistonPushesSurviveNextTickUntilRecycled(t *testing.T) {
	w, p := newOneChunkPhysics(t)
	pos := [3]int32{8, 70, 8}
	ride := movingPistonStateOf(dirUp, false)
	w.SetBlock(pos[0], pos[1], pos[2], ride)
	p.pistonRideCreate(pos, ride, registry.BlockStone.DefaultState(), dirUp, true, false)
	p.now = 0
	_, _, _, first := p.Tick(0)
	var held *PistonPush
	for _, e := range first {
		if e.PistonPush != nil {
			held = e.PistonPush
		}
	}
	if held == nil {
		t.Fatal("no push on the first tick")
	}
	want := held.Progress
	_, _, _, second := p.Tick(1)
	if held.Progress != want {
		t.Fatalf("second Tick rewrote an unrecycled push: Progress %v, want %v", held.Progress, want)
	}
	p.RecycleTickResults(nil, first)
	if p.sparePushes != nil {
		t.Fatal("recycling an older batch took back the newer push slab")
	}
	p.RecycleTickResults(nil, second)
}

func TestPistonPushSlabSurvivesQuietTick(t *testing.T) {
	w, p := newOneChunkPhysics(t)
	move := func(x int32) {
		pos := [3]int32{x, 70, 8}
		ride := movingPistonStateOf(dirUp, false)
		w.SetBlock(pos[0], pos[1], pos[2], ride)
		p.pistonRideCreate(pos, ride, registry.BlockStone.DefaultState(), dirUp, true, false)
	}
	push := func(effects []Effect) *PistonPush {
		for _, e := range effects {
			if e.PistonPush != nil {
				return e.PistonPush
			}
		}
		return nil
	}
	move(8)
	p.now = 0
	_, _, _, e0 := p.Tick(0)
	first := push(e0)
	p.RecycleTickResults(nil, e0)
	_, _, _, e1 := p.Tick(1)
	p.RecycleTickResults(nil, e1)
	_, _, _, quiet := p.Tick(2)
	p.RecycleTickResults(nil, quiet)
	move(9)
	_, _, _, e3 := p.Tick(3)
	if got := push(e3); got != first {
		t.Fatal("a move after a quiet tick allocated a new push slab")
	}
}
