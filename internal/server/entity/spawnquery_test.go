package entity

import (
	"fmt"
	"math"
	"math/rand"
	"testing"

	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/registry"
)

type spawnDieSource struct {
	values []int32
	calls  int
}

func (s *spawnDieSource) Seed(int64) { s.calls = 0 }
func (s *spawnDieSource) Int63() int64 {
	v := s.values[s.calls%len(s.values)]
	s.calls++
	return int64(v) << 32
}

func TestSpawnWalkDie(t *testing.T) {
	for seed := int64(1); seed <= 300; seed++ {
		a, b := rand.New(rand.NewSource(seed)), rand.New(rand.NewSource(seed))
		for range 1000 {
			if got, want := spawnWalkDie(a), int32(b.Intn(6)); got != want {
				t.Fatalf("seed %d: %d != %d", seed, got, want)
			}
		}
		if a.Int63() != b.Int63() {
			t.Fatal("different source consumption")
		}
	}
	for _, values := range [][]int32{{2147483645, 1}, {2147483646, 2147483645, 2}, {2147483647, 2147483646, 2147483645, 3}} {
		as, bs := &spawnDieSource{values: values}, &spawnDieSource{values: values}
		a, b := rand.New(as), rand.New(bs)
		if got, want := spawnWalkDie(a), int32(b.Intn(6)); got != want || as.calls != bs.calls {
			t.Fatalf("%v: die %d/%d calls %d/%d", values, got, want, as.calls, bs.calls)
		}
		if a.Int63() != b.Int63() || as.calls != bs.calls {
			t.Fatal("different next source draw")
		}
	}
}

func TestNaturalSpawnPlayerQueries(t *testing.T) {
	for _, d := range []float64{24, 64, 128} {
		for _, x := range []float64{math.Nextafter(d, 0), d, math.Nextafter(d, math.Inf(1))} {
			for _, point := range [][3]float64{{x, 0, 0}, {0, x, 0}, {0, 0, x}, {-x, 0, 0}, {0, -x, 0}, {0, 0, -x}} {
				players := []SpawnPlayer{{EntityID: 1, X: point[0], Y: point[1], Z: point[2], Dead: true}, {EntityID: 2, Spectator: true}}
				c := naturalSpawnContext{}
				c.preparePlayers(ChunkPos{}, players)
				got, exists := c.playerDistance(players, 0, 0, 0)
				want := Dist2(0, 0, 0, point[0], point[1], point[2])
				if want > 128*128 {
					want = math.MaxFloat64
				}
				if !exists || got != want {
					t.Fatalf("distance at %v: %g/%t, want %g", point, got, exists, want)
				}
				for _, limit := range []float64{24 * 24, 64 * 64, 128 * 128} {
					full := Dist2(0, 0, 0, point[0], point[1], point[2])
					if (got <= limit) != (full <= limit) {
						t.Fatalf("changed boundary %v at %g", point, limit)
					}
				}
			}
		}
	}
	players := []SpawnPlayer{{EntityID: 1, X: 1000, Y: 64}, {EntityID: 2, X: 8, Y: 200}, {EntityID: 3, X: 8, Y: 64, Spectator: true}}
	c := naturalSpawnContext{}
	c.preparePlayers(ChunkPos{}, players)
	if len(c.nearPlayers) != 1 || c.nearPlayers[0] != 1 {
		t.Fatalf("candidates %v", c.nearPlayers)
	}
	if d, ok := c.playerDistance(players, 8, 64, 0); d != math.MaxFloat64 || !ok {
		t.Fatalf("distant observers: %g/%t", d, ok)
	}
	counts := SpawnCounts{PerPlayer: map[int32]int{2: 70}}
	if c.localCapOpen(players, ChunkPos{}, &counts, 70) {
		t.Fatal("cap ignored")
	}
	counts.PerPlayer[2] = 69
	if !c.localCapOpen(players, ChunkPos{}, &counts, 70) {
		t.Fatal("cached counts")
	}
	for _, p := range []SpawnPlayer{{X: math.NaN()}, {Y: math.Inf(1)}, {Z: math.Inf(-1)}, {X: math.Nextafter(1<<40, math.Inf(1))}, {Y: -1 << 41, Spectator: true}} {
		c.preparePlayers(ChunkPos{}, []SpawnPlayer{p})
		if c.query {
			t.Fatalf("optimized invalid coordinates: %v", p)
		}
	}
	for _, pos := range []ChunkPos{{X: math.MaxInt32}, {Z: math.MinInt32}, {X: math.MaxInt32 / 16}, {Z: math.MinInt32 / 16}} {
		c.preparePlayers(pos, nil)
		if c.query {
			t.Fatalf("optimized overflowing rectangle %v", pos)
		}
	}
	c.preparePlayers(ChunkPos{}, nil)
	if _, ok := c.playerDistance(nil, 0, 0, 0); ok {
		t.Fatal("invented player")
	}
	c.preparePlayers(ChunkPos{}, []SpawnPlayer{{X: 1000}})
	if d, ok := c.playerDistance([]SpawnPlayer{{X: 1000}}, 0, 0, 0); !ok || d != math.MaxFloat64 {
		t.Fatal("lost distant player")
	}
}

func TestNaturalSpawnPackRangeProof(t *testing.T) {
	for _, d := range []float64{64, 128} {
		for _, x := range []float64{math.Nextafter(d, 0), d, math.Nextafter(d, math.Inf(1))} {
			players := []SpawnPlayer{{X: x}}
			c := naturalSpawnContext{}
			c.preparePlayers(ChunkPos{}, players)
			if got := c.packOutOfRange(players, 0, 0, 0, 0, d*d); got != (x*x > d*d) {
				t.Fatalf("last step at %.17g: discarded=%t", x, got)
			}
		}
	}
	players := []SpawnPlayer{{X: 129, Y: 64}}
	c := naturalSpawnContext{}
	c.preparePlayers(ChunkPos{}, players)
	if !c.packOutOfRange(players, 0, 64, 0, 0, 128*128) {
		t.Fatal("last point in range")
	}
	if c.packOutOfRange(players, 0, 64, 0, 1, 128*128) {
		t.Fatal("remaining walk can enter range")
	}
	for _, mob := range []MobType{TypeCow, TypePillager} {
		if !selectedKind(mob).far {
			t.Fatalf("%s cannot spawn far", mob.Name())
		}
	}
}

// Compare requests and dice after the complete walk, including draining a
// selected pack and synchronous finalization in the sink.
func TestNaturalSpawnQueryWalkMatchesOracle(t *testing.T) {
	for _, tc := range []struct {
		name        string
		mob         MobType
		category    SpawnCategory
		unsupported bool
		unregister  bool
	}{
		{"monster", TypeZombie, CategoryMonster, false, false}, {"unsupported", TypeZombie, CategoryMonster, true, false},
		{"creature", TypeCow, CategoryCreature, false, false}, {"pillager", TypePillager, CategoryMonster, false, false},
		{"fish", TypeCod, CategoryWaterAmbient, false, false},
		{"sink-registration", TypeCow, CategoryCreature, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			old := naturalBiomeLists[tc.category][registry.BiomePlains]
			naturalBiomeLists[tc.category][registry.BiomePlains] = describeSpawnList([]spawnData{{mob: tc.mob, weight: 1, minCount: 8, maxCount: 8}})
			defer func() { naturalBiomeLists[tc.category][registry.BiomePlains] = old }()
			k := kinds[tc.mob]
			if tc.unsupported {
				kinds[tc.mob] = nil
				defer func() { kinds[tc.mob] = k }()
			}
			w := placementDiceWorld{spawnTestWorld: newSpawnTestWorld(), ground: registry.BlockGrassBlock.DefaultState()}
			w.sky = 0
			if tc.category == CategoryCreature {
				w.sky = 15
				w.darken = 0
			}
			for _, distance := range []float64{24, 64, 128, 129, 200} {
				for _, x := range []float64{math.Nextafter(distance, 0), distance, math.Nextafter(distance, math.Inf(1))} {
					for seed := int64(1); seed <= 300; seed++ {
						players := []SpawnPlayer{{EntityID: 1, X: x, Y: 64, Dead: true}, {EntityID: 2, X: 400, Y: 64}, {EntityID: 3, X: 0, Y: 64, Spectator: true}}
						run := func(fast bool) string {
							if tc.unregister {
								kinds[tc.mob] = k
								defer func() { kinds[tc.mob] = k }()
							}
							rng := rand.New(rand.NewSource(seed))
							c := naturalSpawnContext{w: w}
							if fast {
								c.preparePlayers(ChunkPos{}, players)
							}
							var bodies []SpawnBody
							counts := SpawnCounts{}
							potential := SpawnPotential{}
							result := ""
							c.spawnCategoryForPosition(readSpawnPoint(w), rng, ChunkPos{}, 64, 0, 0, players, &bodies, &potential, &counts, tc.category, func(r SpawnRequest) {
								m := NewMob(r.Type, 1, r.X, r.Y, r.Z, seed)
								m.FinalizeSpawn(GoDice{rng}, w.Biome(int32(r.X), int32(r.Y), int32(r.Z)), r.Group, rules.LocalDifficulty{})
								if tc.unregister {
									kinds[tc.mob] = nil
								}
								result += fmt.Sprintf("%v %g %g %g %g %t\n", r.Type, r.X, r.Y, r.Z, r.Yaw, m.IsBaby())
							})
							return fmt.Sprintf("%s%d %v %v", result, rng.Int63(), counts, potential)
						}
						if a, b := run(true), run(false); a != b {
							t.Fatalf("x %.17g seed %d:\n%s\n%s", x, seed, a, b)
						}
					}
				}
			}
		})
	}
}

func TestNaturalSpawnQueryFallbackDice(t *testing.T) {
	w := placementDiceWorld{spawnTestWorld: newSpawnTestWorld(), ground: registry.BlockGrassBlock.DefaultState()}
	w.sky = 0
	for _, tc := range []struct {
		pos     ChunkPos
		players []SpawnPlayer
	}{
		{pos: ChunkPos{X: math.MaxInt32}, players: []SpawnPlayer{{X: 40, Y: 64}}},
		{pos: ChunkPos{Z: math.MinInt32}, players: []SpawnPlayer{{X: 40, Y: 64}}},
		{players: []SpawnPlayer{{X: 40, Y: 64}, {X: math.NaN(), Spectator: true}}},
		{players: []SpawnPlayer{{X: math.Inf(1), Y: 64}}},
		{players: []SpawnPlayer{{X: 40, Y: 64}, {Y: math.Inf(-1)}}},
		{players: []SpawnPlayer{{X: 40, Y: 64}, {Z: 1 << 41}}},
		{},
	} {
		for seed := int64(1); seed <= 300; seed++ {
			run := func(fast bool) string {
				c := naturalSpawnContext{w: w}
				if fast {
					c.preparePlayers(tc.pos, tc.players)
				}
				rng := rand.New(rand.NewSource(seed))
				var bodies []SpawnBody
				counts := SpawnCounts{}
				potential := SpawnPotential{}
				result := ""
				c.spawnCategoryForPosition(readSpawnPoint(w), rng, tc.pos, 64, 0, 0, tc.players, &bodies, &potential, &counts, CategoryMonster, func(r SpawnRequest) { result += fmt.Sprintf("%v %g %g %g %g\n", r.Type, r.X, r.Y, r.Z, r.Yaw) })
				return fmt.Sprintf("%s%d %v %v", result, rng.Int63(), counts, potential)
			}
			if a, b := run(true), run(false); a != b {
				t.Fatalf("%v seed %d:\n%s\n%s", tc, seed, a, b)
			}
		}
	}
}

// initialPositionWorld stops the pass at its first all-stone collision query.
type initialPositionWorld struct {
	SpawnWorld
	minY, surface int32
	reads         []string
}

func (w *initialPositionWorld) MinY() int32 {
	w.reads = append(w.reads, "minY")
	return w.minY
}
func (w *initialPositionWorld) SurfaceHeight(x, z int32) int32 {
	w.reads = append(w.reads, fmt.Sprintf("height %d %d", x, z))
	return w.surface
}
func (w *initialPositionWorld) BlockAt(x, y, z int32) uint16 {
	w.reads = append(w.reads, fmt.Sprintf("block %d %d %d", x, y, z))
	return registry.BlockStone.DefaultState()
}

// vanilla: NaturalSpawner.getRandomPosWithin, ChunkAccess.getHeight
func vanillaInitialPosition(w *initialPositionWorld, rng *rand.Rand, pos ChunkPos) {
	bx := pos.X*16 + int32(rng.Intn(16))
	bz := pos.Z*16 + int32(rng.Intn(16))
	minY := w.MinY()
	surface := w.SurfaceHeight(bx, bz)
	span := (surface + 1) - minY + 1
	if span <= 0 {
		return
	}
	y := minY + int32(rng.Intn(int(span)))
	if y < minY+1 {
		return
	}
	w.BlockAt(bx, y, bz)
}

func TestNaturalSpawnInitialPositionDice(t *testing.T) {
	check := func(pos ChunkPos, minY, surface int32, a, b *rand.Rand) {
		t.Helper()
		wa := &initialPositionWorld{minY: minY, surface: surface}
		wb := &initialPositionWorld{minY: minY, surface: surface}
		c := naturalSpawnContext{w: wa}
		c.spawnCategoryForChunk(spawnPoint{}, a, pos, nil, nil, nil, nil, CategoryMonster, nil)
		vanillaInitialPosition(wb, b, pos)
		if fmt.Sprint(wa.reads) != fmt.Sprint(wb.reads) {
			t.Fatalf("pos %v minY %d surface %d: %v != %v", pos, minY, surface, wa.reads, wb.reads)
		}
		for range 4 {
			if a.Int63() != b.Int63() {
				t.Fatal("initial position changed RNG tail")
			}
		}
	}
	for seed := int64(1); seed <= 300; seed++ {
		for _, pos := range []ChunkPos{{}, {X: 7, Z: 9}, {X: -7, Z: -9}, {X: math.MaxInt32, Z: math.MinInt32}, {X: math.MaxInt32 / 16, Z: math.MinInt32 / 16}, {X: math.MaxInt32/16 + 1, Z: math.MinInt32/16 - 1}} {
			for _, span := range []int32{-1, 0, 1, 2, 3, 16, 129, 130, 384, math.MaxInt32} {
				check(pos, -64, span-66, rand.New(rand.NewSource(seed)), rand.New(rand.NewSource(seed)))
			}
			for _, bounds := range [][2]int32{{0, math.MaxInt32}, {math.MinInt32, math.MaxInt32}, {math.MaxInt32, math.MinInt32}, {math.MaxInt32, math.MaxInt32}, {math.MinInt32, math.MinInt32}} {
				check(pos, bounds[0], bounds[1], rand.New(rand.NewSource(seed)), rand.New(rand.NewSource(seed)))
			}
		}
	}
	for _, values := range [][]int32{{0, 0, 0, 17}, {15, 14, 2147483646, 2147483647, 2147483645, 19}, {15, 14, 2147483647, 2147483646, 2147483645, 19}} {
		for _, span := range []int32{1, 3} {
			as, bs := &spawnDieSource{values: values}, &spawnDieSource{values: values}
			check(ChunkPos{X: -1, Z: 1}, -64, span-66, rand.New(as), rand.New(bs))
			if as.calls != bs.calls {
				t.Fatal("changed rejection/Intn(1) consumption")
			}
		}
	}
}
