package entity

import (
	"math"
	"math/rand"
	"slices"
	"sort"
	"testing"

	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// The collection's own tick: the push pass run against the grid the tick
// buckets the herd into, held to the pairs the whole-herd walk asks after.

// TestOverlappingCowsStillPushApartAndAFarCowIsLeftAlone walks a herd of
// three through the collection's tick, two of them standing in each other's
// boxes across a cell boundary and one far away over the horizon of every
// grid cell the pair reaches: the overlapping pair shoves itself apart tick
// by tick, and the far body takes no shove at all.
func TestOverlappingCowsStillPushApartAndAFarCowIsLeftAlone(t *testing.T) {
	stone := defaultState(t, "minecraft:stone")
	w := newTestWorld()
	floor(w, -10, -2, 310, 2, 63, stone)

	// The pair spans a cell boundary — one at 3.5, the other at 4.2 of
	// the 4-block cells — their boxes overlapping by a fifth.
	pairA := aimless(NewMob(TypeCow, 1, 3.5, 64, 0.5, 1))
	pairB := aimless(NewMob(TypeCow, 2, 4.2, 64, 0.5, 2))
	far := aimless(NewMob(TypeCow, 3, 300.5, 64, 0.5, 3))
	mobs := Mobs{pairA, pairB, far}
	mobs.Tick(w, nil, nil)
	if pairA.VX >= 0 || pairB.VX <= 0 {
		t.Errorf("the overlapping cows' velocities = (%v, %v), want the pair shoved apart",
			pairA.VX, pairB.VX)
	}
	if far.VX != 0 || far.VZ != 0 {
		t.Errorf("the far cow's velocity = (%v, %v), want it untouched",
			far.VX, far.VZ)
	}
	for range 40 {
		mobs.Tick(w, nil, nil)
	}
	if apart := math.Abs(pairA.X - pairB.X); apart < 0.9 {
		t.Errorf("the pair stands %v apart after forty ticks, want the boxes clear of each other",
			apart)
	}
	if far.X != 300.5 {
		t.Errorf("the far cow stood at x = %v, want where it began", far.X)
	}
}

// herdBlockWorld keeps the herd's environment fixed while reading real chunks.
type herdBlockWorld struct {
	testWorld
	terrain *world.World
}

func (w herdBlockWorld) BlockAt(x, y, z int32) uint16 {
	return w.terrain.BlockAt(x, y, z)
}

func (w herdBlockWorld) BlocksIn(x0, y0, z0, x1, y1, z1 int32, buf []uint16) []uint16 {
	return w.terrain.BlocksIn(x0, y0, z0, x1, y1, z1, buf)
}

// BenchmarkMobsTickHerd steps the collection of a large herd through a tick:
// about eight hundred animals of the natural generation's mix, clustered two
// to five to a spawn point over a 512x512 area of flat stone, no players in
// the world.
func BenchmarkMobsTickHerd(b *testing.B) {
	terrain := world.NewDimensionWorld(b.TempDir(), true, world.NewFlatGenerator())
	defer terrain.Close()
	for cx := int32(-17); cx <= 16; cx++ {
		for cz := int32(-17); cz <= 16; cz++ {
			pos := world.ChunkPos{X: cx, Z: cz}
			terrain.Acquire(pos)
			defer terrain.Release(pos)
		}
	}
	w := herdBlockWorld{testWorld: newTestWorld(), terrain: terrain}

	// The counts scaled to the eight hundred: mostly cows, pigs, sheep and
	// chickens in spawn-time herds, a few monsters whose persistence spares
	// them the despawn the absence of players would otherwise have.
	mix := []struct {
		kind MobType
		n    int
	}{
		{TypeCow, 170}, {TypePig, 186}, {TypeSheep, 203}, {TypeChicken, 142},
		{TypeZombie, 21}, {TypeSkeleton, 19}, {TypeCreeper, 20}, {TypeSpider, 19},
	}
	rng := rand.New(rand.NewSource(1544))
	var id int32
	mobs := make(Mobs, 0, 800)
	for _, kind := range mix {
		for spawned := 0; spawned < kind.n; {
			// One spawn point a cluster of bodies hangs around at, the
			// way the chunk generation plants a group of 2-4.
			x := (rng.Float64() - 0.5) * 512
			z := (rng.Float64() - 0.5) * 512
			for range 2 + rng.Intn(3) {
				if spawned == kind.n {
					break
				}
				spawned++
				m := NewMob(kind.kind, id, x+rng.Float64()*2, 64, z+rng.Float64()*2, int64(id))
				id++
				if m.Hostile() {
					m.Persistence = true
				}
				mobs = append(mobs, m)
			}
		}
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		mobs.Tick(w, nil, nil)
	}
	b.StopTimer()
}

// crowdOfCows stands n cows in one block, the mobs ticked as a herd.
func crowdOfCows(n int) Mobs {
	var herd Mobs
	for i := range n {
		herd = append(herd, aimless(NewMob(TypeCow, int32(i+1), 0.5, 64, 0.5, int64(i+1))))
	}
	return herd
}

// crammingBites ticks the herd for ticks and counts the cramming hurts.
func crammingBites(w testWorld, herd Mobs, ticks int) int {
	bites := 0
	for range ticks {
		for _, ev := range herd.Tick(w, nil, nil) {
			if ev.Kind == MobBurned && ev.DamageType == rules.DamageTypeCramming {
				bites++
			}
		}
	}
	return bites
}

func TestACrowdOverTheCrammingRuleTakesDamage(t *testing.T) {
	stone := defaultState(t, "minecraft:stone")
	w := newTestWorld()
	floor(w, -10, -2, 10, 2, 63, stone)
	herd := crowdOfCows(30)
	if bites := crammingBites(w, herd, 40); bites == 0 {
		t.Fatalf("30 cows in a block took no cramming damage")
	}
	hurt := false
	for _, c := range herd {
		hurt = hurt || c.Health < 10
	}
	if !hurt {
		t.Errorf("no cow lost health to the crowd")
	}
}

// BenchmarkPushStepArena runs the push step of 2000 zombies packed into a
// 49x49 arena, each against the grid bucketing the herd.
func BenchmarkPushStepArena(b *testing.B) {
	const herd = 2000
	rng := rand.New(rand.NewSource(2294))
	w := newTestWorld()
	floor(w, -30, -30, 30, 30, 63, defaultState(b, "minecraft:stone"))
	mobs := make([]*Mob, herd)
	grid := takePushGrid(herd, nil)
	defer grid.release()
	for i := range mobs {
		mobs[i] = NewMob(TypeZombie, int32(i), (rng.Float64()-0.5)*49, 64, (rng.Float64()-0.5)*49, int64(i))
		mobs[i].Persistence = true
	}
	grid.build(mobs)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		for _, m := range mobs {
			m.push = grid
			m.pushStep(w, nil, mobs, nil)
			m.VX, m.VZ = 0, 0
		}
	}
}

// BenchmarkPushCandidates4000 runs the grid's candidate pass for 4000 zombies
// packed into a 70x70 arena.
func BenchmarkPushCandidates4000(b *testing.B) {
	const herd = 4000
	rng := rand.New(rand.NewSource(2429))
	mobs := make([]*Mob, herd)
	grid := takePushGrid(herd, nil)
	defer grid.release()
	for i := range mobs {
		mobs[i] = NewMob(TypeZombie, int32(i), (rng.Float64()-0.5)*70, 64, (rng.Float64()-0.5)*70, int64(i))
	}
	grid.build(mobs)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		for _, m := range mobs {
			grid.candidates(m, 0)
		}
	}
}

// BenchmarkMobsTickVillage steps a village of beds, job sites and a bell
// with homeless and jobless villagers, employed ones and other mobs around.
func BenchmarkMobsTickVillage(b *testing.B) { benchmarkMobsTickVillage(b, nil, 100, 40) }

func BenchmarkMobsTickVillageWithPlayer(b *testing.B) {
	benchmarkMobsTickVillage(b, []Player{{EntityID: 1000, X: 0.5, Y: 64, Z: 0.5}}, 100, 40)
}

// BenchmarkMobsTickVillageBesideHerd steps the village beside 2000 cows on a
// 240x240 floor.
func BenchmarkMobsTickVillageBesideHerd(b *testing.B) {
	benchmarkMobsTickVillage(b, []Player{{EntityID: 1000, X: 0.5, Y: 64, Z: 0.5}}, 2000, 120)
}

func benchmarkMobsTickVillage(b *testing.B, players []Player, cows int, halfFloor int32) {
	stone := defaultState(b, "minecraft:stone")
	w := newTestWorld()
	floor(w, -halfFloor, -halfFloor, halfFloor, halfFloor, 63, stone)
	bed, ok := registry.FindState("minecraft:red_bed", map[string]string{"part": "head", "facing": "north", "occupied": "false"})
	if !ok {
		b.Fatal("no red bed head state")
	}
	sites := []uint16{
		defaultState(b, "minecraft:lectern"), defaultState(b, "minecraft:composter"),
		defaultState(b, "minecraft:smoker"), defaultState(b, "minecraft:loom"),
	}
	put := func(x, z int32, s uint16) { w.set(x, 64, z, s) }
	for i := range int32(30) {
		put(-30+i*2, -10, bed)
	}
	for i := range int32(20) {
		put(-30+i*3, 10, sites[int(i)%len(sites)])
	}
	put(0, 0, defaultState(b, "minecraft:bell"))
	village := indexedVillage{w: w, idx: rules.NewVillageIndex(benchChunks{w}), tick: new(int64)}

	rng := rand.New(rand.NewSource(2162))
	mobs := make(Mobs, 0, 60+cows)
	var id int32
	for i := range 60 {
		m := NewMob(TypeVillager, id, -30+rng.Float64()*60, 64, -5+rng.Float64()*10, int64(id))
		id++
		m.Village = village
		if i%3 == 0 {
			m.Villager.Profession, m.VillagerXp = "minecraft:librarian", 1
		}
		mobs = append(mobs, m)
	}
	spread := float64(halfFloor - 5)
	for range cows {
		mobs = append(mobs, NewMob(TypeCow, id, -spread+rng.Float64()*2*spread, 64, -spread+rng.Float64()*2*spread, int64(id)))
		id++
	}
	b.ReportAllocs()
	b.ResetTimer()
	for n := range b.N {
		*village.tick = int64(n)
		for _, m := range mobs {
			m.GameTime, m.DayTime = int64(n)+1, 2000+int64(n)
		}
		mobs.Tick(w, players, nil)
	}
}

// benchChunks is a test world as the loaded terrain of a VillageIndex.
type benchChunks struct{ testWorld }

func (benchChunks) ChunkLoaded(world.ChunkPos) bool { return true }
func (benchChunks) SurfaceHeight(x, z int32) int32  { return 70 }

// indexedVillage reads POIs through a real VillageIndex, aged by *tick.
type indexedVillage struct {
	fakeVillage
	idx  *rules.VillageIndex
	tick *int64
}

func (v indexedVillage) POIs(match func(rules.POIKind) bool, x, y, z int32, radius int) []Waypoint {
	var out []Waypoint
	for _, p := range v.idx.Around(int(x), int(y), int(z), radius, *v.tick, rules.VillageScanTicks) {
		if match(p.Kind) {
			out = append(out, Waypoint{int32(p.X), int32(p.Y), int32(p.Z)})
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		return manhattan3(out[i], Waypoint{x, y, z}) < manhattan3(out[j], Waypoint{x, y, z})
	})
	return out
}

func TestTickViewVillagerMobs(t *testing.T) {
	v1 := NewMob(TypeVillager, 1, 0, 64, 0, 1)
	cow := NewMob(TypeCow, 2, 1, 64, 0, 2)
	v2 := NewMob(TypeVillager, 3, 2, 64, 0, 3)
	cat := NewMob(TypeCat, 4, 3, 64, 0, 4)
	other := NewMob(TypeVillager, 5, 4, 64, 0, 5)
	herd := []*Mob{v1, cow, v2, cat}

	fallback := tickView{others: herd}
	if got := fallback.villagerMobs(); !slices.Equal(got, []*Mob{v1, v2}) {
		t.Fatalf("fallback villagerMobs = %v, want [%v %v]", got, v1, v2)
	}
	if !fallback.containsMob(v2) || !fallback.containsMob(cat) || fallback.containsMob(other) {
		t.Fatal("fallback containsMob mismatch")
	}

	known := tickView{others: herd, villagers: []*Mob{v1, v2}, villagersKnown: true}
	if got := known.villagerMobs(); !slices.Equal(got, []*Mob{v1, v2}) {
		t.Fatalf("known villagerMobs = %v, want [%v %v]", got, v1, v2)
	}
	if !known.containsMob(v1) || !known.containsMob(cat) || known.containsMob(other) {
		t.Fatal("known containsMob mismatch")
	}
}
