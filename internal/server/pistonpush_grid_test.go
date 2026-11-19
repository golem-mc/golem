package server

import (
	"fmt"
	"slices"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/world"
)

// TestPistonPushGridMatchesScan checks the grid path moves the same mobs to
// the same places as the full herd scan, before and after a push moved one.
func TestPistonPushGridMatchesScan(t *testing.T) {
	s, w := newPistonGridTestServer(t)
	dim := s.allDims()[0]

	spots := [][3]float64{{1.75, 64, 0.5}, {1.9, 64, 0.2}, {3.6, 64, 0.5}, {1.75, 64, 40}, {1.75, 90, 0.5}, {-30, 64, -30}, {2.2, 64.5, 0.8}, {5, 64, 0.5}}
	var mobs []*entity.Mob
	for i, p := range spots {
		s.spawnMobInDim(dim, entity.TypeCow, p[0], p[1], p[2], int64(i), nil)
	}
	s.mobsMu.Lock()
	mobs = append(mobs, dim.mobs.herd...)
	s.mobsMu.Unlock()

	newPush := func() *world.PistonPush {
		return &world.PistonPush{X: 1, Y: 64, Z: 0, Dir: 5, Progress: 0.5, Extending: true, Bounce: true, Boxes: []world.CollisionBox{{MaxX: 1, MaxY: 1, MaxZ: 1}}}
	}
	positions := func() [][3]float64 {
		s.mobsMu.Lock()
		defer s.mobsMu.Unlock()
		out := make([][3]float64, len(mobs))
		for i, m := range mobs {
			out[i] = [3]float64{m.X, m.Y, m.Z}
		}
		return out
	}
	restore := func(p [][3]float64) {
		s.mobsMu.Lock()
		defer s.mobsMu.Unlock()
		for i, m := range mobs {
			m.X, m.Y, m.Z = p[i][0], p[i][1], p[i][2]
		}
	}
	setActive := func(on bool) {
		s.mobsMu.Lock()
		dim.mobs.pistonGrid.active, dim.mobs.pistonGrid.built = on, false
		s.mobsMu.Unlock()
	}
	push := func(grid bool) [][3]float64 {
		setActive(grid)
		if grid {
			away := newPush()
			away.X, away.Z = 100, 100
			if got := len(s.collectMobsPistonPush(dim, away)); got != 0 || !dim.mobs.pistonGrid.built {
				t.Fatalf("prime: moves=%d built=%v", got, dim.mobs.pistonGrid.built)
			}
		}
		s.pushByPiston(w, newPush())
		return positions()
	}

	start := positions()
	for round := range 2 {
		restore(start)
		scan := push(false)
		restore(start)
		grid := push(true)
		moved := 0
		for i := range scan {
			if scan[i] != grid[i] {
				t.Fatalf("round %d: mob %d scan %v grid %v", round, i, scan[i], grid[i])
			}
			if scan[i] != start[i] {
				moved++
			}
		}
		if moved == 0 || moved == len(mobs) {
			t.Fatalf("round %d: %d of %d mobs moved", round, moved, len(mobs))
		}
		// Start the next round with the moved positions and one mob back in reach.
		start = grid
		start[0] = [3]float64{1.75, 64, 0.5}
	}
}

func TestPistonGridGatewayMobMovement(t *testing.T) {
	s, _ := newPistonGridTestServer(t)
	dim := s.allDims()[0]
	s.spawnMobInDim(dim, entity.TypeCow, 1.75, 64, 0.5, 1, nil)
	mob := dim.mobs.herd[0]
	ent := dim.mobs.book[mob]
	s.setPistonGrid(dim, true)
	away := &world.PistonPush{X: 100, Y: 64, Z: 100, Dir: 5, Progress: 0.5, Extending: true, Boxes: []world.CollisionBox{{MaxX: 1, MaxY: 1, MaxZ: 1}}}
	if got := len(s.collectMobsPistonPush(dim, away)); got != 0 || !dim.mobs.pistonGrid.built {
		t.Fatalf("prime: moves=%d built=%v", got, dim.mobs.pistonGrid.built)
	}
	// The asynchronous gateway callback can land between queries in one
	// physics step, after the grid has recorded the mob in its old cell.
	s.gatewayMoveMob(ent, player.Position{X: 17.75, Y: 64, Z: 0.5})
	near := &world.PistonPush{X: 17, Y: 64, Z: 0, Dir: 5, Progress: 0.5, Extending: true, Boxes: []world.CollisionBox{{MaxX: 1, MaxY: 1, MaxZ: 1}}}
	grid := s.collectMobsPistonPush(dim, near)
	s.setPistonGrid(dim, false)
	scan := s.collectMobsPistonPush(dim, near)
	if len(grid) != 1 || len(scan) != 1 {
		t.Fatalf("after gateway: grid collected %d moves, scan %d; want 1 each", len(grid), len(scan))
	}
	grid[0]()
	got := [3]float64{mob.X, mob.Y, mob.Z}
	scan[0]()
	want := [3]float64{mob.X, mob.Y, mob.Z}
	if got != want || got[0] <= 17.75 {
		t.Fatalf("after gateway push: grid position %v, scan %v; want equal eastward moves", got, want)
	}
}

func newPistonGridTestServer(t *testing.T) (*Server, *world.World) {
	t.Helper()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.LevelName = t.TempDir()
	cfg.LevelType = TestFlatLevelType
	s, err := newServer(cfg, 1)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Shutdown)
	dim := s.allDims()[0]
	w := dim.w
	w.Acquire(world.ChunkPos{})
	t.Cleanup(func() { w.Release(world.ChunkPos{}) })
	w.FlushLighting()

	return s, w
}

// TestBodyPistonGridCandidates checks filtering and ordering across cell edges,
// cache reuse, and membership changes without moving a body.
func TestBodyPistonGridCandidates(t *testing.T) {
	type body struct {
		box  [6]float64
		here bool
	}
	near := func(x float64) *body { return &body{[6]float64{x, 64, 0, x + 1, 65, 1}, true} }
	a, b, away, other := near(15.8), near(15.9), near(80), near(15.7)
	other.here = false
	list := []*body{b, away, other, a}
	g := bodyPistonGrid[*body]{active: true}
	pp := &world.PistonPush{X: 16, Y: 64, Z: 0, Dir: 5, Progress: 0.5, Extending: true, Boxes: []world.CollisionBox{{MaxX: 1, MaxY: 1, MaxZ: 1}}}
	reads := 0
	box := func(b *body) [6]float64 {
		reads++
		if !b.here {
			t.Fatal("read another dimension's box")
		}
		return b.box
	}
	candidates := func(want ...*body) {
		t.Helper()
		before := reads
		got := g.candidates(list, pp)
		if reads != before {
			t.Fatal("candidate lookup read boxes")
		}
		if !slices.Equal(got, want) {
			t.Fatalf("candidates %v, want %v", got, want)
		}
	}
	build := func() { g.build(list, func(b *body) bool { return b.here }, box) }
	fallback := func() {
		t.Helper()
		candidates(list...)
		if g.built {
			t.Fatal("fallback built the grid")
		}
	}
	fallback()
	if reads != 0 || len(g.bodies) != 0 {
		t.Fatal("cold lookup constructed boxes")
	}
	build()
	candidates(b, a)
	if reads != 3 || len(g.bodies) != 3 {
		t.Fatalf("built %d boxes, stored %d bodies, want 3", reads, len(g.bodies))
	}
	candidates(b, a)
	if reads != 3 {
		t.Fatal("unchanged list rebuilt the grid")
	}
	c := near(16)
	list = append(list, c)
	fallback()
	build()
	candidates(b, a, c)
	list = list[1:]
	fallback()
	build()
	candidates(a, c)
	d := near(15.6)
	list = append(list[:len(list)-1], d)
	fallback()
	build()
	candidates(a, d)
	// Movement invalidation must discard the previous cell's entries.
	a.box = away.box
	g.built = false
	fallback()
	build()
	candidates(d)
	emptyReach := &world.PistonPush{}
	if got := g.candidates(list, emptyReach); len(got) != 0 {
		t.Fatalf("empty reach returned %d bodies", len(got))
	}
	list = nil
	candidates()
	if g.built {
		t.Fatal("empty list retained built cache")
	}
	// Outside physics, the collector needs the complete list, including bodies
	// whose world/dead checks it applies itself.
	g.active = false
	list = []*body{other, away}
	got := g.candidates(list, pp)
	if !slices.Equal(got, list) {
		t.Fatal("inactive grid did not return the source list")
	}
}

func TestPistonPushKindsUseGrid(t *testing.T) {
	s, _ := newPistonGridTestServer(t)
	dim := s.allDims()[0]
	kinds := []string{"item", "experience_orb", "arrow", "tnt", "falling_block", "oak_boat", "armor_stand", "cushion", "minecart", "end_crystal"}
	for _, kind := range kinds {
		for _, x := range []float64{1.75, 80.5} {
			s.RunRconLine(fmt.Sprintf(`summon minecraft:%s %v 64 0.5 {Item:{id:"minecraft:stone",count:1}}`, kind, x))
		}
	}
	s.setPistonGrid(s.allDims()[0], true)
	grids := &s.allDims()[0].pistons
	checkPistonKindGrid(t, &grids.items, dim, s.collectItemsPistonPush)
	checkPistonKindGrid(t, &grids.orbs, dim, s.collectOrbsPistonPush)
	checkPistonKindGrid(t, &grids.arrows, dim, s.collectArrowsPistonPush)
	checkPistonKindGrid(t, &grids.tnts, dim, s.collectTNTsPistonPush)
	checkPistonKindGrid(t, &grids.fallings, dim, s.collectFallingsPistonPush)
	checkPistonKindGrid(t, &grids.boats, dim, s.collectBoatsPistonPush)
	checkPistonKindGrid(t, &grids.armorStands, dim, s.collectArmorStandsPistonPush)
	checkPistonKindGrid(t, &grids.fixtures, dim, s.collectCushionsPistonPush)
	checkPistonKindGrid(t, &grids.minecarts, dim, s.collectMinecartsPistonPush)
	checkPistonKindGrid(t, &grids.endCrystals, dim, s.collectEndCrystalsPistonPush)
	s.setPistonGrid(s.allDims()[0], false)
	if grids.items.active || grids.endCrystals.active {
		t.Fatal("grids still active after physics step")
	}
}

func checkPistonKindGrid[B comparable](t *testing.T, g *bodyPistonGrid[B], dim *dimension, collect func(*dimension, *world.PistonPush) []func()) {
	t.Helper()
	away := &world.PistonPush{X: 100, Y: 64, Z: 100, Dir: 5, Progress: 0.5, Extending: true, Boxes: []world.CollisionBox{{MaxX: 1, MaxY: 1, MaxZ: 1}}}
	if got := len(collect(dim, away)); got != 0 {
		t.Fatalf("%T: %d moves far from bodies", g, got)
	}
	if !g.active || !g.built || len(g.bodies) != 2 {
		t.Fatalf("%T: active=%v built=%v bodies=%d, want true, true, 2", g, g.active, g.built, len(g.bodies))
	}
	near := &world.PistonPush{X: 1, Y: 64, Z: 0, Dir: 5, Progress: 0.5, Extending: true, Boxes: []world.CollisionBox{{MaxX: 1, MaxY: 1, MaxZ: 1}}}
	if got := len(collect(dim, near)); got != 1 {
		t.Fatalf("%T: %d moves in sweep, want 1", g, got)
	}
	if g.built {
		t.Fatalf("%T: movement did not invalidate the grid", g)
	}
}

// Commands can run between two piston effects in the same physics step.
func TestPistonGridCommandMovement(t *testing.T) {
	for _, command := range []string{`tp @e[type=%s,limit=1] 9.75 64 0.5`, `data merge entity @e[type=%s,limit=1] {Pos:[9.75d,64d,0.5d]}`} {
		t.Run(command, func(t *testing.T) {
			s, _ := newPistonGridTestServer(t)
			dim := s.allDims()[0]
			cases := []struct {
				kind    string
				collect func(*dimension, *world.PistonPush) []func()
			}{
				{"item", s.collectItemsPistonPush},
				{"experience_orb", s.collectOrbsPistonPush},
				{"arrow", s.collectArrowsPistonPush},
				{"tnt", s.collectTNTsPistonPush},
				{"falling_block", s.collectFallingsPistonPush},
				{"oak_boat", s.collectBoatsPistonPush},
				{"armor_stand", s.collectArmorStandsPistonPush},
				{"cushion", s.collectCushionsPistonPush},
				{"minecart", s.collectMinecartsPistonPush},
				{"end_crystal", s.collectEndCrystalsPistonPush},
				{"cow", s.collectMobsPistonPush},
			}
			away := &world.PistonPush{X: 100, Y: 64, Z: 100, Dir: 5, Progress: 0.5, Extending: true, Boxes: []world.CollisionBox{{MaxX: 1, MaxY: 1, MaxZ: 1}}}
			near := &world.PistonPush{X: 9, Y: 64, Z: 0, Dir: 5, Progress: 0.5, Extending: true, Boxes: []world.CollisionBox{{MaxX: 1, MaxY: 1, MaxZ: 1}}}
			for _, tc := range cases {
				t.Run(tc.kind, func(t *testing.T) {
					s.RunRconLine(fmt.Sprintf(`summon minecraft:%s 1.75 64 0.5 {Item:{id:"minecraft:stone",count:1}}`, tc.kind))
					s.setPistonGrid(dim, true)
					if got := len(tc.collect(dim, away)); got != 0 {
						t.Fatalf("%d moves away from body", got)
					}
					s.RunRconLine(fmt.Sprintf(command, tc.kind))
					grid := len(tc.collect(dim, near))
					s.setPistonGrid(dim, false)
					scan := len(tc.collect(dim, near))
					if grid != 1 || scan != 1 {
						t.Fatalf("after command: grid collected %d moves, scan %d; want 1 each", grid, scan)
					}
				})
			}
		})
	}
}

func TestPistonFixtureGridSkipsUnpushable(t *testing.T) {
	s, _ := newPistonGridTestServer(t)
	dim := s.allDims()[0]
	s.RunRconLine(`summon cushion 1.75 64 0.5`)
	cushion := s.fixtures[0]
	s.RunRconLine(`summon interaction 1.75 64 0.5 {width:1024f,height:1f}`)
	s.RunRconLine(`summon cushion 3 64 0.5`)
	s.fixtures[2].dead = true
	s.setPistonGrid(dim, true)
	pp := &world.PistonPush{X: 100, Y: 64, Z: 100, Dir: 5, Progress: 0.5, Extending: true, Boxes: []world.CollisionBox{{MaxX: 1, MaxY: 1, MaxZ: 1}}}
	if got := len(s.collectCushionsPistonPush(dim, pp)); got != 0 {
		t.Fatalf("%d breaks away from cushion", got)
	}
	if got := dim.pistons.fixtures.bodies; !slices.Equal(got, []*fixtureEntity{cushion}) {
		t.Fatalf("indexed %d fixtures, want only the live cushion", len(got))
	}
	pp = &world.PistonPush{X: 1, Y: 64, Z: 0, Dir: 5, Progress: 0.5, Extending: true, Boxes: []world.CollisionBox{{MaxX: 1, MaxY: 1, MaxZ: 1}}}
	if got := len(s.collectCushionsPistonPush(dim, pp)); got != 1 {
		t.Fatalf("%d breaks in sweep, want 1", got)
	}
}

// Repeated effects must keep scanning until a quiet collection can build a cache.
func TestPistonGridBuildsOnlyAfterQuietCollect(t *testing.T) {
	s, _ := newPistonGridTestServer(t)
	dim := s.allDims()[0]
	s.RunRconLine(`summon interaction 1.75 64 0.5 {width:1024f,height:1f}`)
	grids := &dim.pistons
	cases := []struct {
		kind  string
		check func(*testing.T)
	}{
		{"item", func(t *testing.T) { checkQuietPistonKindGrid(t, &grids.items, dim, s.collectItemsPistonPush) }},
		{"experience_orb", func(t *testing.T) { checkQuietPistonKindGrid(t, &grids.orbs, dim, s.collectOrbsPistonPush) }},
		{"arrow", func(t *testing.T) { checkQuietPistonKindGrid(t, &grids.arrows, dim, s.collectArrowsPistonPush) }},
		{"tnt", func(t *testing.T) { checkQuietPistonKindGrid(t, &grids.tnts, dim, s.collectTNTsPistonPush) }},
		{"falling_block", func(t *testing.T) { checkQuietPistonKindGrid(t, &grids.fallings, dim, s.collectFallingsPistonPush) }},
		{"oak_boat", func(t *testing.T) { checkQuietPistonKindGrid(t, &grids.boats, dim, s.collectBoatsPistonPush) }},
		{"armor_stand", func(t *testing.T) {
			checkQuietPistonKindGrid(t, &grids.armorStands, dim, s.collectArmorStandsPistonPush)
		}},
		{"cushion", func(t *testing.T) { checkQuietPistonKindGrid(t, &grids.fixtures, dim, s.collectCushionsPistonPush) }},
		{"minecart", func(t *testing.T) { checkQuietPistonKindGrid(t, &grids.minecarts, dim, s.collectMinecartsPistonPush) }},
		{"end_crystal", func(t *testing.T) {
			checkQuietPistonKindGrid(t, &grids.endCrystals, dim, s.collectEndCrystalsPistonPush)
		}},
		{"cow", func(t *testing.T) { checkQuietPistonKindGrid(t, &dim.mobs.pistonGrid, dim, s.collectMobsPistonPush) }},
	}
	for _, tc := range cases {
		t.Run(tc.kind, func(t *testing.T) {
			s.RunRconLine(fmt.Sprintf(`summon minecraft:%s 1.75 64 0.5 {Item:{id:"minecraft:stone",count:1}}`, tc.kind))
			s.setPistonGrid(dim, true)
			tc.check(t)
		})
	}
}

func checkQuietPistonKindGrid[B comparable](t *testing.T, g *bodyPistonGrid[B], dim *dimension, collect func(*dimension, *world.PistonPush) []func()) {
	t.Helper()
	near := &world.PistonPush{X: 1, Y: 64, Z: 0, Dir: 5, Progress: 0.5, Extending: true, Boxes: []world.CollisionBox{{MaxX: 1, MaxY: 1, MaxZ: 1}}}
	for range 3 {
		if got := len(collect(dim, near)); got != 1 {
			t.Fatalf("cold collection: %d effects, want 1", got)
		}
		if g.built || len(g.bodies) != 0 {
			t.Fatalf("cold collection built=%v bodies=%d, want false, 0", g.built, len(g.bodies))
		}
	}
	away := &world.PistonPush{X: 100, Y: 64, Z: 100, Dir: 5, Progress: 0.5, Extending: true, Boxes: []world.CollisionBox{{MaxX: 1, MaxY: 1, MaxZ: 1}}}
	if got := len(collect(dim, away)); got != 0 || !g.built || len(g.bodies) != 1 {
		t.Fatalf("quiet collection: effects=%d built=%v bodies=%d, want 0, true, 1", got, g.built, len(g.bodies))
	}
	// A cache must be built from the source list, even if the last query had no hits.
	storage := &g.bodies[0]
	if got := len(collect(dim, away)); got != 0 || !g.built || len(g.bodies) != 1 || &g.bodies[0] != storage {
		t.Fatal("second quiet collection did not retain the cache")
	}
	if got := len(collect(dim, near)); got != 1 || g.built {
		t.Fatalf("warm collection: effects=%d built=%v, want 1, false", got, g.built)
	}
}
