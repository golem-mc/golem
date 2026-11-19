package entity

import (
	"fmt"
	"math/rand"
	"slices"
	"testing"
)

// TestBlockCacheReadsLikeTheWorld covers every cell of a box over sections'
// edges, negative coordinates and the world's minimum height.
func TestBlockCacheReadsLikeTheWorld(t *testing.T) {
	w := newTestWorld()
	rng := rand.New(rand.NewSource(1))
	for range 400 {
		w.set(int32(rng.Intn(40)-20), int32(rng.Intn(40)-64), int32(rng.Intn(40)-20), uint16(1+rng.Intn(50)))
	}
	c := blockCache{World: w, sections: map[cacheKey]*cacheSection{}}
	for x := int32(-20); x < 20; x++ {
		for y := int32(-64); y < -24; y++ {
			for z := int32(-20); z < 20; z++ {
				if got, want := c.BlockAt(x, y, z), w.BlockAt(x, y, z); got != want {
					t.Fatalf("BlockAt(%d, %d, %d) = %d, want %d", x, y, z, got, want)
				}
			}
		}
	}
}

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

// TestPathScratchResetsBetweenSearches runs a search on a scratch another
// world's search has just left behind: a malus, section or cache way that
// survived reset gives a different result than on a fresh scratch.
func TestPathScratchResetsBetweenSearches(t *testing.T) {
	foot := footprintOf(0.6, 1.95)
	from, to := Waypoint{0, 60, 0}, Waypoint{9, 60, 7}
	for _, tiny := range []bool{false, true} {
		used := newPathScratch(tiny)
		for seed := int64(1); seed <= 40; seed++ {
			w := randomPathWorld(t, seed)
			gotPath, gotOK := used.search(w, foot, from, to, pathMaxRange, 0)
			used.reset()
			wantPath, wantOK := newPathScratch(tiny).search(w, foot, from, to, pathMaxRange, 0)
			if gotOK != wantOK || !slices.Equal(gotPath, wantPath) {
				t.Fatalf("tiny=%v seed %d: reused scratch gave %v (%v), fresh gave %v (%v)",
					tiny, seed, gotPath, gotOK, wantPath, wantOK)
			}
		}
	}
}

// TestFindPathStartsFromCleanScratch runs one search after another over
// worlds that differ by a wall, through the pools.
func TestFindPathStartsFromCleanScratch(t *testing.T) {
	stone := defaultState(t, "minecraft:stone")
	foot := footprintOf(0.6, 1.95)
	build := func(walled bool) testWorld {
		w := newTestWorld()
		floor(w, -10, -10, 10, 10, 59, stone)
		if walled {
			wall(w, stone, 3, -10, 10, 99, 99, 60)
		}
		return w
	}
	for _, to := range []Waypoint{{6, 60, 0}, {3, 60, 0}} {
		for range 3 {
			if _, ok := findPath(build(true), foot, Waypoint{0, 60, 0}, to, pathMaxRange, 0); ok && to.X > 3 {
				t.Fatal("found a way through the wall")
			}
			path, ok := findPath(build(false), foot, Waypoint{0, 60, 0}, to, pathMaxRange, 0)
			if !ok || len(path) != int(to.X) {
				t.Fatalf("open ground path to %v = %v (reached %v), want %d cells", to, path, ok, to.X)
			}
		}
	}
}

// TestPathScratchKinds pins which pool a search draws from and which
// scratches go back.
func TestPathScratchKinds(t *testing.T) {
	for d, wantTiny := range map[int32]bool{0: true, 4: true, 5: false, 40: false} {
		if s := getPathScratch(d); s.tiny != wantTiny {
			t.Errorf("distance %d drew tiny=%v, want %v", d, s.tiny, wantTiny)
		}
	}
	for _, tiny := range []bool{true, false} {
		s := newPathScratch(tiny)
		limit := scratchKeep
		if tiny {
			limit = tinyHint
		}
		for i := range limit {
			s.walked[Waypoint{int32(i), 0, 0}] = 0
		}
		if !s.fits() {
			t.Errorf("tiny=%v: scratch at its limit of %d is dropped", tiny, limit)
		}
		s.walked[Waypoint{-1, 0, 0}] = 0
		if s.fits() {
			t.Errorf("tiny=%v: scratch past its limit of %d is kept", tiny, limit)
		}
	}
}

// TestFrontierPopsByPriceThenArrival pins the heap order the search's
// determinism rests on.
func TestFrontierPopsByPriceThenArrival(t *testing.T) {
	rng := rand.New(rand.NewSource(2))
	var f frontier
	var want []openCell
	for i := range 300 {
		c := openCell{price: float64(rng.Intn(20)), order: i}
		f.push(c)
		want = append(want, c)
	}
	slices.SortFunc(want, func(a, b openCell) int {
		if a.price != b.price {
			return int(a.price - b.price)
		}
		return a.order - b.order
	})
	for i, w := range want {
		if got := f.pop(); got != w {
			t.Fatalf("pop %d = %+v, want %+v", i, got, w)
		}
	}
	if len(f) != 0 {
		t.Fatalf("%d cells left", len(f))
	}
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

func TestDensePathScratchStampsAndBounds(t *testing.T) {
	s := newPathScratch(false)
	w := newTestWorld()
	stone := defaultState(t, "minecraft:stone")
	foot := footprintOf(0.6, 1.95)
	// Change both the origin and box dimensions between uses of the same slots.
	for _, r := range []int32{16, 0, 4, 16, -1, 17, 35, 16} {
		from := Waypoint{-30 + r, -40 - r, 70 + r}
		s.prepare(from, r)
		if got := s.side != 0; got != (r >= 0 && r <= 16) {
			t.Fatalf("range %d: dense=%v", r, got)
		}
		offsets := slices.Compact([]int32{-r, 0, r})
		for _, dx := range offsets {
			for _, dy := range offsets {
				for _, dz := range offsets {
					c := Waypoint{from.X + dx, from.Y + dy, from.Z + dz}
					if _, ok := s.cost(c); ok {
						t.Fatalf("range %d: stale cost at %v", r, c)
					}
					s.visit(c, from, float64(dx+dy+dz))
					if cost, ok := s.cost(c); !ok || cost != float64(dx+dy+dz) {
						t.Fatalf("range %d: cost %v, %v", r, cost, ok)
					}
					// Reading a malus must not discard a previously recorded predecessor.
					if got := s.malus(w, &foot, c); got != 0 {
						t.Fatalf("range %d: malus %v", r, got)
					}
					if c != from && !slices.Equal(s.reconstruct(from, c), []Waypoint{c}) {
						t.Fatalf("range %d: lost predecessor", r)
					}
				}
			}
		}

		s.reset()
	}
	// Overflow must invalidate slots even beyond the current, smaller box.
	s.prepare(Waypoint{}, 16)
	s.visit(Waypoint{16, 16, 16}, Waypoint{}, 7)
	s.cell(Waypoint{16, 16, 16}).walkedEpoch = 1
	s.cell(Waypoint{16, 16, 16}).malusEpoch = 1
	s.cell(Waypoint{16, 16, 16}).malus = 99
	s.cell(Waypoint{16, 16, 16}).geometryEpoch = 1
	s.cell(Waypoint{16, 16, 16}).geometryFlags = floorKnown | traversableKnown | traversableValue | columnOpenKnown | columnOpenValue
	s.cell(Waypoint{16, 16, 16}).floor = 99
	s.epoch = ^uint32(0)
	s.prepare(Waypoint{}, 0)
	if got := s.cells[len(s.cells)-1]; got != (pathCell{}) {
		t.Fatalf("retained cell survived wrap through a smaller box: %+v", got)
	}
	s.reset()
	s.prepare(Waypoint{}, 16)
	if _, ok := s.cost(Waypoint{16, 16, 16}); ok {
		t.Fatal("stamp survived wrap")
	}
	if got := s.malus(w, &foot, Waypoint{16, 16, 16}); got != 0 {
		t.Fatalf("malus stamp survived wrap: %v", got)
	}
	s.reset()
	// A malus cached in one world must not survive reuse in a different world.
	c := Waypoint{-10, 60, -10}
	for _, state := range []uint16{defaultState(t, "minecraft:fire"), 0} {
		w.set(c.X, c.Y-1, c.Z, stone)
		w.set(c.X, c.Y, c.Z, state)
		s.prepare(c, 16)
		want := cellMalus(w, foot, c.X, c.Y, c.Z)
		if got := s.malus(w, &foot, c); got != want {
			t.Fatalf("malus %v, want %v", got, want)
		}
		s.reset()
	}
}

func TestDensePathSearchKeepsTiesAndBestNodes(t *testing.T) {
	w := newTestWorld()
	stone := defaultState(t, "minecraft:stone")
	floor(w, -20, -20, 20, 20, 59, stone)
	for y := int32(60); y < 63; y++ {
		w.set(2, y, 0, stone)
	}
	around := []Waypoint{{1, 60, 1}, {2, 60, 1}, {3, 60, 1}, {4, 60, 0}}
	edge := slices.Clone(around)
	for x := int32(5); x <= 16; x++ {
		edge = append(edge, Waypoint{x, 60, 0})
	}
	// Equal detours pass north or south of the pillar; arrival order picks +Z.
	cases := []struct {
		to      Waypoint
		want    []Waypoint
		reached bool
	}{
		{Waypoint{4, 60, 0}, around, true},
		{Waypoint{40, 60, 0}, edge, false},
		{Waypoint{2, 60, 0}, []Waypoint{{1, 60, 0}}, false},
		{Waypoint{-4, 60, -2}, []Waypoint{{-1, 60, -1}, {-2, 60, -2}, {-3, 60, -2}, {-4, 60, -2}}, true},
	}
	for _, tiny := range []bool{false, true} {
		s := newPathScratch(tiny)
		for _, tc := range cases {
			got, reached := s.search(w, footprintOf(0.6, 1.95), Waypoint{0, 60, 0}, tc.to, 16, 0)
			if reached != tc.reached || !slices.Equal(got, tc.want) {
				t.Fatalf("tiny=%v to %v: path %v (%v), want %v (%v)", tiny, tc.to, got, reached, tc.want, tc.reached)
			}
			s.reset()
		}
	}
}

// Geometry stamps are independent of visited nodes and maluses, in either
// initialization order. False primitive results must also be remembered.
func TestPathGeometryStampsAndLazyResults(t *testing.T) {
	stone := defaultState(t, "minecraft:stone")
	c, parent := Waypoint{-12, 60, -20}, Waypoint{-13, 60, -20}
	for _, blocked := range []bool{false, true} {
		w := newTestWorld()
		w.set(c.X, c.Y-1, c.Z, stone)
		if blocked {
			w.set(c.X, c.Y, c.Z, stone)
		}
		counted := &pathReadCounter{World: w}
		s := newPathScratch(true)
		s.prepare(c, 4)
		s.visit(c, parent, 7)
		foot := footprintOf(0.6, 1.95)
		malus := s.malus(w, &foot, c)
		n := s.geometry(c)
		if n.geometryFlags != 0 || counted.reads != 0 {
			t.Fatal("geometry computed eagerly")
		}
		e := pathEvaluator{World: counted, footprint: footprintOf(0.6, 1.95), scratch: s}
		if got := e.floor(c); got != 60 || n.geometryFlags != floorKnown {
			t.Fatalf("floor=%v flags=%b", got, n.geometryFlags)
		}
		if got := e.traversable(c); got == blocked {
			t.Fatalf("traversable=%v blocked=%v", got, blocked)
		}
		if got := e.columnOpen(c); got == blocked {
			t.Fatalf("open=%v blocked=%v", got, blocked)
		}
		flags := floorKnown | traversableKnown | columnOpenKnown
		if !blocked {
			flags |= traversableValue | columnOpenValue
		}
		if n.geometryFlags != flags {
			t.Fatalf("flags=%b want %b", n.geometryFlags, flags)
		}
		reads := counted.reads
		e.floor(c)
		e.traversable(c)
		e.columnOpen(c)
		if counted.reads != reads {
			t.Fatal("cached primitive read the world again")
		}
		if cost, ok := s.cost(c); !ok || cost != 7 || n.parent != parent || n.malus != malus || n.malusEpoch != s.epoch {
			t.Fatalf("geometry discarded graph state: %+v", n)
		}
		n.geometryEpoch--
		s.geometry(c)
		if n.geometryFlags != 0 || n.walkedEpoch != s.epoch || n.malusEpoch != s.epoch || n.parent != parent {
			t.Fatalf("geometry reset graph state: %+v", n)
		}
		e.columnOpen(c)
		s.visit(c, parent, 6)
		s.malus(w, &e.footprint, c)
		if n.geometryFlags&columnOpenKnown == 0 {
			t.Fatal("graph state discarded geometry")
		}
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

func TestPathGeometryInvalidatesWorldAndFootprint(t *testing.T) {
	stone := defaultState(t, "minecraft:stone")
	water := defaultState(t, "minecraft:water")
	c := Waypoint{-20, 60, -30}
	s := newPathScratch(true)
	for _, r := range []int32{0, 16, 17, 0, 16} {
		for _, state := range []uint16{stone, 0, water} {
			w := newTestWorld()
			w.set(c.X, c.Y-1, c.Z, stone)
			w.set(c.X, c.Y, c.Z, state)
			w.set(c.X+1, c.Y, c.Z, stone)
			w.set(c.X, c.Y+2, c.Z, stone)
			for _, f := range []footprint{footprintOf(0.6, 1.95), footprintOf(2, 1.95), footprintOf(0.6, 2.95), {fw: 1, fh: 2, floats: true}} {
				s.prepare(c, r)
				e := pathEvaluator{World: w, footprint: f}
				if s.side != 0 {
					e.scratch = s
				}
				if e.floor(c) != floorAt(w, f, c) || e.floorFrom(c) != floorAt(w, f, c) || e.traversable(c) != traversableAt(w, f, c.X, c.Y, c.Z) || e.columnOpen(c) != pathOpen(w, f, c.X, c.Y, c.Z) {
					t.Fatalf("stale geometry: range=%d state=%d footprint=%+v", r, state, f)
				}
				s.reset()
			}
		}
	}
}

func TestPathBlockCacheWayIndexMatchesPackedKey(t *testing.T) {
	coords := []int32{-1 << 31, -1 << 21, -1024, -9, -8, -1, 0, 7, 8, 9, 1024, 1 << 21, 1<<31 - 1}
	for _, x := range coords {
		for _, y := range coords {
			for _, z := range coords {
				key := sectionKey(x, y, z)
				want := (key ^ key>>21 ^ key>>42) & (cacheWays - 1)
				got := cacheKey(sectionWay(x, y, z))
				if got != want {
					t.Fatalf("(%d,%d,%d): way=%d want=%d", x, y, z, got, want)
				}
			}
		}
	}
}
