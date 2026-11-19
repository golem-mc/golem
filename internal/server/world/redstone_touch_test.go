package world

import (
	"sync"
	"testing"

	"github.com/golem-mc/golem/registry"
)

func TestTouchBlocksInFollowsTheSectionsTouches(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	w, err := NewWorld(dir, NewFlatGenerator())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { w.Close() })
	w.Acquire(ChunkPos{X: -1, Z: -1})
	y := int32(GroundLevel)
	box := func(w *World) bool { return w.TouchBlocksIn(-1, y, -1, 0, y+1, 0) }
	if box(w) {
		t.Fatal("flat ground has touch blocks")
	}
	for _, name := range []string{
		"stone_pressure_plate", "oak_pressure_plate", "light_weighted_pressure_plate", "heavy_weighted_pressure_plate",
		"stone_button", "oak_button", "tripwire", "detector_rail", "big_dripleaf",
	} {
		t.Run(name, func(t *testing.T) {
			state := registry.MustState("minecraft:"+name, nil)
			if !w.SetBlock(-1, y, -1, state) || !box(w) {
				t.Fatal("touch block set in overlapping section was missed")
			}
			for _, bounds := range [][6]int32{
				{-16, y - 16, -16, -1, y - 1, -1},
				{-16, y + 16, -16, -1, y + 31, -1},
				{0, y, -16, 15, y, -1},
				{-16, y, 0, -1, y, 15},
				{-16, worldMinY - 16, -16, -1, worldMinY - 1, -1},
				{-16, worldMinY + worldHeight, -16, -1, worldMinY + worldHeight + 15, -1},
			} {
				if w.TouchBlocksIn(bounds[0], bounds[1], bounds[2], bounds[3], bounds[4], bounds[5]) {
					t.Fatalf("touch block outside sections of %v was included", bounds)
				}
			}
			w.SetBlock(-1, y, -1, uint16(airState))
			if box(w) {
				t.Fatal("removed touch block still counted")
			}
		})
	}
	w.SetBlock(-1, y, -1, registry.MustState("minecraft:stone_pressure_plate", nil))
	w.Save()
	w.Close()
	w, err = NewWorld(dir, NewFlatGenerator())
	if err != nil {
		t.Fatal(err)
	}
	if box(w) {
		t.Fatal("unloaded chunk was included")
	}
	w.Acquire(ChunkPos{X: -1, Z: -1})
	if !box(w) {
		t.Fatal("saved and loaded plate was missed")
	}
	w.SetBlock(-1, y, -1, uint16(airState))
	if box(w) {
		t.Fatal("removed loaded plate still counted")
	}
}

func TestSectionTouchesFollowsBulkWrites(t *testing.T) {
	t.Parallel()
	plate := registry.MustState("minecraft:stone_pressure_plate", nil)
	var s section
	s.setAll(plate)
	if s.touches.Load() != blocksPerSection {
		t.Fatalf("uniform plate count = %d, want %d", s.touches.Load(), blocksPerSection)
	}
	s.recalcCounts()
	if s.touches.Load() != blocksPerSection {
		t.Fatalf("uniform plate recount = %d, want %d", s.touches.Load(), blocksPerSection)
	}
	s.set(0, uint16(airState), false)
	s.recalcCounts()
	if s.touches.Load() != blocksPerSection-1 {
		t.Fatalf("mixed plate recount = %d, want %d", s.touches.Load(), blocksPerSection-1)
	}
	s.setAll(uint16(airState))
	s.recalcCounts()
	if s.touches.Load() != 0 {
		t.Fatalf("cleared section count = %d, want 0", s.touches.Load())
	}
}

func TestEntityInsideCellsDefersDripleafUntilAfterPlates(t *testing.T) {
	t.Parallel()
	w := newTestWorld(t, NewFlatGenerator())
	t.Cleanup(func() { w.Close() })
	w.Acquire(ChunkPos{})
	p := NewPhysics(w)
	p.SetToucher(&touchStub{living: 1, all: 1})
	y := int32(GroundLevel - 1)
	w.SetBlock(0, y, 0, registry.MustState("minecraft:big_dripleaf", nil))
	w.SetBlock(1, y, 0, registry.MustState("minecraft:stone_pressure_plate", nil))
	var changes []BlockChange
	var effects []Effect
	var primes []TNTPrime
	p.EntityInsideCells(0, y, 0, 1, y+1, 0, float64(y)+0.9375, true, &changes, &effects, &primes)
	if !registry.StatePowered(w.BlockAt(1, y, 0)) {
		t.Fatal("plate was not pressed")
	}
	for _, change := range changes {
		if change.X == 0 && change.Y == y && change.Z == 0 {
			t.Fatal("leaf tilted before the later plate could power it")
		}
	}
	if tilt := registry.StateValue(w.BlockAt(0, y, 0), registry.PropTilt); tilt != "none" {
		t.Fatalf("powered leaf tilt = %q, want none", tilt)
	}
}

func TestEntityInsideCellsReusesDripleafScratch(t *testing.T) {
	w := newTestWorld(t, NewFlatGenerator())
	t.Cleanup(func() { w.Close() })
	w.Acquire(ChunkPos{})
	p := NewPhysics(w)
	y := int32(GroundLevel)
	w.SetBlock(0, y, 0, registry.MustState("minecraft:big_dripleaf", map[string]string{"tilt": "unstable"}))
	var changes []BlockChange
	var effects []Effect
	var primes []TNTPrime
	// AllocsPerRun warms the scratch before measuring repeated body visits.
	allocs := testing.AllocsPerRun(100, func() {
		p.EntityInsideCells(0, y, 0, 0, y, 0, float64(y)+0.9375, true, &changes, &effects, &primes)
	})
	if allocs != 0 {
		t.Fatalf("unchanged dripleaf visit allocated %g times, want 0", allocs)
	}
	w.SetBlock(0, y, 0, registry.MustState("minecraft:big_dripleaf", nil))
	p.EntityInsideCells(0, y, 0, 0, y, 0, float64(y)+0.9375, false, &changes, &effects, &primes)
	if len(changes) != 0 || len(effects) != 0 || len(primes) != 0 {
		t.Fatal("airborne visit reused a deferred leaf from the previous body")
	}
}

func TestTouchBlocksInFollowsCacheCollisionsAndUnload(t *testing.T) {
	t.Parallel()
	w := newTestWorld(t, NewFlatGenerator())
	t.Cleanup(func() { w.Close() })
	first, second := ChunkPos{X: -1, Z: -1}, ChunkPos{X: nearSlots - 1, Z: -1}
	w.Acquire(first)
	w.Acquire(second)
	y := int32(GroundLevel)
	w.SetBlock(-1, y, -1, registry.MustState("minecraft:stone_pressure_plate", nil))
	for range 2 {
		if !w.TouchBlocksIn(-1, y, -1, -1, y, -1) {
			t.Fatal("plate missed after cache collision")
		}
		if w.TouchBlocksIn(second.X*16, y, -1, second.X*16, y, -1) {
			t.Fatal("colliding chunk inherited the plate count")
		}
	}
	w.Release(first)
	w.FlushSaves()
	if w.TouchBlocksIn(-1, y, -1, -1, y, -1) {
		t.Fatal("unloaded plate retained in cache")
	}
}

func TestTouchBlocksInConcurrentReadsAndWrites(t *testing.T) {
	t.Parallel()
	w := newTestWorld(t, NewFlatGenerator())
	t.Cleanup(func() { w.Close() })
	w.Acquire(ChunkPos{})
	y := int32(GroundLevel)
	plate := registry.MustState("minecraft:stone_pressure_plate", nil)
	var readers sync.WaitGroup
	done := make(chan struct{})
	for range 2 {
		readers.Go(func() {
			for {
				select {
				case <-done:
					return
				default:
					w.TouchBlocksIn(0, y, 0, 0, y, 0)
				}
			}
		})
	}
	for range 1000 {
		w.SetBlock(0, y, 0, plate)
		if !w.TouchBlocksIn(0, y, 0, 0, y, 0) {
			t.Error("set plate was missed")
		}
		w.SetBlock(0, y, 0, uint16(airState))
		if w.TouchBlocksIn(0, y, 0, 0, y, 0) {
			t.Error("removed plate was retained")
		}
	}
	close(done)
	readers.Wait()
}
