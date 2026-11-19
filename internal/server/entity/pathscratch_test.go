package entity

import (
	"fmt"
	"math/rand"
	"testing"
)

// randomPathWorld is a seeded 24x24 area with a floor and a scatter of
// solid, liquid and damaging blocks, so searches fill the malus table and
// read many sections.
func randomPathWorld(t testing.TB, seed int64) testWorld {
	names := []string{"stone", "water", "lava", "magma_block", "fire", "cactus", "sweet_berry_bush", "oak_fence"}
	states := make([]uint16, len(names))
	for i, n := range names {
		states[i] = defaultState(t, "minecraft:"+n)
	}
	w := newTestWorld()
	floor(w, -12, -12, 12, 12, 59, states[0])
	rng := rand.New(rand.NewSource(seed))
	for range 220 {
		w.set(int32(rng.Intn(24)-12), int32(59+rng.Intn(3)), int32(rng.Intn(24)-12), states[rng.Intn(len(states))])
	}
	return w
}

// BenchmarkZombieNear is a zombie's search for a goal a few blocks away on
// open ground, the case where fixed setup costs show.
func BenchmarkZombieNear(b *testing.B) {
	stone := defaultState(b, "minecraft:stone")
	w := newTestWorld()
	floor(w, -45, -45, 45, 45, 59, stone)
	foot := footprintOf(0.6, 1.95)
	for _, d := range []int32{1, 3, 4, 5, 8} {
		b.Run(fmt.Sprint(d), func(b *testing.B) {
			for b.Loop() {
				findPath(w, foot, Waypoint{0, 60, 0}, Waypoint{d, 60, 0}, 35, 0)
			}
		})
	}
}

// BenchmarkZombieDetour is a zombie's follow-range search that finds its
// target by walking round a wall through a doorway at the far side.
func BenchmarkZombieDetour(b *testing.B) {
	stone := defaultState(b, "minecraft:stone")
	w := newTestWorld()
	floor(w, -45, -45, 45, 45, 59, stone)
	wall(w, stone, 10, -40, 40, 30, 31, 60)
	foot := footprintOf(0.6, 1.95)
	for b.Loop() {
		findPath(w, foot, Waypoint{0, 60, 0}, Waypoint{20, 60, 0}, 35, 0)
	}
}

// BenchmarkZombieOpen16 is findPath over sixteen blocks of open ground.
func BenchmarkZombieOpen16(b *testing.B) {
	stone := defaultState(b, "minecraft:stone")
	w := newTestWorld()
	floor(w, -40, -40, 40, 40, 59, stone)
	foot := footprintOf(0.6, 1.95)
	for _, r := range []int32{16, 35} {
		b.Run(fmt.Sprint(r), func(b *testing.B) {
			for b.Loop() {
				findPath(w, foot, Waypoint{0, 60, 0}, Waypoint{16, 60, 0}, r, 0)
			}
		})
	}
}

type pathReadCounter struct {
	World
	reads int
}

func (w *pathReadCounter) BlockAt(x, y, z int32) uint16 { w.reads++; return w.World.BlockAt(x, y, z) }

func TestPathGeometryMatchesPrimitivesAcrossBoundsAndReuse(t *testing.T) {
	base := footprintOf(0.6, 1.95)
	variants := []footprint{base, footprintOf(2, 1.95), base, base, base, base}
	variants[2].floats = true
	variants[3].amphibious = true
	variants[4].openDoors = true
	variants[5].leavesMalus = 0
	for _, tiny := range []bool{false, true} {
		s := newPathScratch(tiny)
		// Reuse the same slots across dense boxes, the map fallback, worlds and
		// footprints. A wide footprint's leading edge lies outside the node box.
		for i, r := range []int32{16, 0, 17, 16, 4, 0, 16} {
			w := randomPathWorld(t, int64(i+1))
			from := Waypoint{-3 - int32(i), 60, -5 + int32(i)}
			s.prepare(from, r)
			for _, f := range variants {
				// A new search must invalidate even a different footprint in one world.
				s.reset()
				s.prepare(from, r)
				var rw World = w
				if !tiny {
					s.blocks.World = w
					rw = &s.blocks
				}
				e := pathEvaluator{World: rw, footprint: f}
				if s.side != 0 {
					e.scratch = s
				}
				for _, dx := range []int32{-r - 1, -r, 0, r, r + 1} {
					for _, dy := range []int32{-r, 0, r} {
						for _, dz := range []int32{-r - 1, -r, 0, r, r + 1} {
							c := Waypoint{from.X + dx, from.Y + dy, from.Z + dz}
							for range 2 {
								if got, want := e.floor(c), floorAt(w, f, c); got != want {
									t.Fatalf("range=%d cell=%v floor=%v want=%v", r, c, got, want)
								}
								if got, want := e.floorFrom(c), floorAt(w, f, c); got != want {
									t.Fatalf("range=%d cell=%v source floor=%v want=%v", r, c, got, want)
								}
								if got, want := e.traversable(c), traversableAt(w, f, c.X, c.Y, c.Z); got != want {
									t.Fatalf("range=%d cell=%v traversable=%v want=%v", r, c, got, want)
								}
								if got, want := e.columnOpen(c), pathOpen(w, f, c.X, c.Y, c.Z); got != want {
									t.Fatalf("range=%d cell=%v columnOpen=%v want=%v", r, c, got, want)
								}
							}
							if s.side != 0 && chebyshev(from, c) > r && s.geometry(c) != nil {
								t.Fatalf("cached out-of-box cell %v", c)
							}
							if e.sourceKnown && (s.side == 0 || chebyshev(from, e.source) > r) {
								t.Fatalf("cached out-of-box source floor %v", e.source)
							}
						}
					}
				}
			}
			s.reset()
			if s.blocks.World != nil || len(s.open) != 0 || len(s.walked) != 0 || len(s.maluses) != 0 || len(s.cameFrom) != 0 || len(s.blocks.sections) != 0 {
				t.Fatal("reset retained search state or world")
			}
		}
	}
}
