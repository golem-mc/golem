package world

import (
	"slices"
	"testing"

	"github.com/golem-mc/golem/registry"
)

func TestPistonCancelledFinalizeKeepsReplacement(t *testing.T) {
	for _, finalize := range []bool{false, true} {
		t.Run(map[bool]string{false: "block", true: "finalize"}[finalize], func(t *testing.T) {
			w, p := newOneChunkPhysics(t)
			key := blockKey{8, 70, 8}
			w.SetBlock(key.x, key.y, key.z, registry.BlockStone.DefaultState())
			p.schedulePistonFinalize(key.x, key.y, key.z, 1)
			p.schedulePistonFinalize(key.x, key.y, key.z, 9) // Keep the first booking.
			if p.finalizeSeq[key] != 1 || len(p.queue) != 1 {
				t.Fatal("a duplicate landing replaced the first booking")
			}
			p.cancelPistonFinalize(key.x, key.y, key.z)
			p.cancelPistonFinalize(key.x, key.y, key.z)
			p.now = 1
			if p.torchWillTick(key.x, key.y, key.z) || p.repeaterWillTick(key.x, key.y, key.z, 1) {
				t.Error("a cancelled landing still counts as a due tick")
			}
			if saved := p.chunkTicks(ChunkPos{}); len(saved.block)+len(saved.fluid) != 0 {
				t.Fatalf("saved a cancelled landing: %+v", saved)
			}
			priority := tickPriorityNormal
			if finalize {
				priority = tickPriorityFinalize
			}
			// Restored landings also enter through schedulePriority.
			p.schedulePriority(key.x, key.y, key.z, 3, priority)
			if !finalize {
				p.cancelPistonFinalize(key.x, key.y, key.z)
			}
			p.cancelPistonFinalize(9, 70, 8) // An unrelated cancellation is inert.
			p.Tick(1)
			if !p.pending[key] || len(p.queue) != 1 || p.queue[0].due != 3 || len(p.cancelled) != 0 {
				t.Fatalf("cancelled entry disturbed its replacement: %+v", p.queue)
			}
			if finalize && p.finalizeSeq[key] != 2 {
				t.Fatal("popping the cancelled entry forgot the new landing")
			}
			p.Tick(3)
			if len(p.queue)+len(p.pending)+len(p.finalizeSeq)+len(p.cancelled) != 0 {
				t.Fatal("popping the replacement left a booking behind")
			}
		})
	}
}

func TestPistonCancelledFinalizeFilters(t *testing.T) {
	for _, unload := range []bool{false, true} {
		t.Run(map[bool]string{false: "clear", true: "unload"}[unload], func(t *testing.T) {
			w, p := newOneChunkPhysics(t)
			w.SetBlock(8, 70, 8, registry.BlockStone.DefaultState())
			p.schedulePistonFinalize(8, 70, 8, 1)
			p.cancelPistonFinalize(8, 70, 8)
			p.schedule(8, 70, 8, 3)
			p.schedulePistonFinalize(9, 70, 8, 4)
			if copied := p.BlockTicksIn(0, 0, 0, 15, 100, 15); len(copied) != 1 || copied[0].tick.due != 3 {
				t.Fatalf("copied ticks = %+v, want only the replacement block tick", copied)
			}
			if unload {
				w.dropChunkTicks(ChunkPos{})
				p.adoptLoadedTicks()
				if len(p.queue)+len(p.pending)+len(p.finalizeSeq) != 0 {
					t.Fatal("unload left ticks or bookings behind")
				}
			} else {
				// Filter an unrelated box: the replacement must stay booked.
				p.ClearTicksIn(0, 0, 0, 1, 1, 1)
				if len(p.queue) != 2 || !p.pending[blockKey{8, 70, 8}] {
					t.Fatal("filtering a cancelled entry forgot its replacement")
				}
				p.ClearTicksIn(0, 0, 0, 15, 100, 15)
				if len(p.queue) != 1 || p.queue[0].priority != tickPriorityFinalize || len(p.finalizeSeq) != 1 {
					t.Fatal("clear must keep a live landing")
				}
			}
			if len(p.cancelled) != 0 {
				t.Fatal("filtering left cancellation tombstones behind")
			}
		})
	}
}

func TestPistonCancelledFinalizeDoesNotSpendTickBudget(t *testing.T) {
	w, p := newOneChunkPhysics(t)
	// A visit to moving_piston spends the update budget even without its entity.
	w.SetBlock(8, 70, 8, movingPistonStateOf(dirUp, false))
	for range maxBlockTicksPerTick {
		p.schedulePistonFinalize(8, 70, 8, 1)
		p.cancelPistonFinalize(8, 70, 8)
	}
	p.schedulePistonFinalize(9, 70, 8, 1)
	p.Tick(1)
	if len(p.queue)+len(p.pending)+len(p.finalizeSeq)+len(p.cancelled) != 0 {
		t.Fatal("cancelled landings delayed the live landing past this tick")
	}
}

func TestPistonFinalizeDoesNotCancelBlockTick(t *testing.T) {
	_, p := newOneChunkPhysics(t)
	p.schedule(8, 70, 8, 3)
	p.schedulePistonFinalize(8, 70, 8, 1)
	p.cancelPistonFinalize(8, 70, 8)
	if len(p.queue) != 1 || p.queue[0].due != 3 || !p.pending[blockKey{8, 70, 8}] || len(p.finalizeSeq)+len(p.cancelled) != 0 {
		t.Fatal("a declined landing cancelled the block's existing tick")
	}
}

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
