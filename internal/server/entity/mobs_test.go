package entity

import (
	"math"
	"math/rand"
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

// TestTheGridsPushesMatchTheWholeHerdScan walks two identical herds the same
// number of ticks, one through the collection's tick — whose pushing asks
// the grid — and one mob by mob with the whole herd handed to each tick,
// the pass every mob walked when the herd was scanned whole. Six herds mass
// across several cells, with pairs overlapping in each, and the bodies are
// asserted equal to the last float, which is what the same pairs asked in
// the same order at the same positions come to.
func TestTheGridsPushesMatchTheWholeHerdScan(t *testing.T) {
	stone := defaultState(t, "minecraft:stone")
	w := newTestWorld()
	floor(w, -20, -4, 20, 4, 63, stone)

	herd := func() []*Mob {
		var mobs []*Mob
		for i, spot := range []struct{ x, z float64 }{
			{0.30, 0.50}, {0.42, 0.62}, {0.35, 1.10},
			{5.20, 0.50}, {5.61, 0.80},
			{10.10, 2.30}, {9.80, 2.00}, {3.90, 3.50},
			{100.5, 0.50}, {-100.5, 0.50}, {0.50, 100.5},
		} {
			mobs = append(mobs, aimless(NewMob(TypeCow, int32(len(mobs)), spot.x, 64, spot.z, int64(i)+1)))
		}
		return mobs
	}

	collection := Mobs(herd()) // ticked through the grid
	scan := herd()             // ticked mob by mob, the whole herd in each
	for range 3 {
		batch := make([]*Mob, len(scan))
		copy(batch, scan)
		collection.Tick(w, nil, nil)
		for _, m := range scan {
			m.tick(w, nil, batch)
		}
	}
	for i := range scan {
		got, want := collection[i], scan[i]
		if got.X != want.X || got.Y != want.Y || got.Z != want.Z ||
			got.VX != want.VX || got.VY != want.VY || got.VZ != want.VZ {
			t.Errorf("cow %d of the grid herd stands at (%v, %v, %v) with velocity (%v, %v, %v), "+
				"want the whole-herd walk's (%v, %v, %v) and (%v, %v, %v)",
				i, got.X, got.Y, got.Z, got.VX, got.VY, got.VZ,
				want.X, want.Y, want.Z, want.VX, want.VY, want.VZ)
		}
	}
}

// TestACowThatSlidesIntoACellIsReachedThere gives a cow a shove big enough
// to carry it twenty blocks — several cells — into a neighbour's box in the
// one move of its own tick, so it is re-filed into the cell it now stands in
// and the neighbour's own tick finds it there. Without the re-filing the
// neighbour's query would step over the moved body for the rest of the
// tick, and the shove it owes its neighbour would never land.
func TestACowThatSlidesIntoACellIsReachedThere(t *testing.T) {
	stone := defaultState(t, "minecraft:stone")
	w := newTestWorld()
	floor(w, -2, -2, 26, 2, 63, stone)

	slider := aimless(NewMob(TypeCow, 1, 0.5, 64, 0.5, 1))
	slider.VX = 20 // the one move of its tick carries it to x = 20.5
	waiter := aimless(NewMob(TypeCow, 2, 20.8, 64, 0.5, 2))
	mobs := Mobs{slider, waiter}
	mobs.Tick(w, nil, nil)

	// The waiter found the slider once it had slid: the two halves of the
	// pair add up in its velocity, where a body never reached by its own
	// query keeps only the half the slider's tick put on it.
	if waiter.VX < 0.035 {
		t.Errorf("the waiter's velocity x = %v, want both halves of the shove the slid body owes it",
			waiter.VX)
	}
}

// TestACowFindsAWideSlimeTwoCellsOff is the hold on the gathering's widened
// ring: a size-sixteen slime's box reaches more than two cells past the cell
// of its centre, so a cow two cells off overlaps it and must find it in the
// herd's widest half-width. The slime rides a vehicle, keeping its box still
// while the cow's gathering scans out to the herd's width.
func TestACowFindsAWideSlimeTwoCellsOff(t *testing.T) {
	stone := defaultState(t, "minecraft:stone")
	w := newTestWorld()
	floor(w, -2, -2, 14, 2, 63, stone)

	// The slime's centre in cell 0 and the cow's in cell 2, their boxes
	// overlapping across the two cells between them: half of 0.52 times
	// sixteen is 4.16, and the pair's half-widths reach 4.61 over the gap
	// of 4.51.
	slime := NewMob(TypeSlime, 1, 3.99, 64, 0.5, 1)
	slime.SetSize(16, true)
	slime.RidesVehicle = true
	cow := aimless(NewMob(TypeCow, 2, 8.5, 64, 0.5, 2))
	mobs := Mobs{slime, cow}
	mobs.Tick(w, nil, nil)

	if cow.VX <= 0 {
		t.Errorf("the cow's velocity x = %v, want the shove of the slime two cells off", cow.VX)
	}
	if slime.VX >= 0 {
		t.Errorf("the slime's velocity x = %v, want the other half of the same shove", slime.VX)
	}
}

// TestKindsTableReadsAreNilSafeForAnUnsimulatedType calls the table's plain
// reads — and a bare Mob — after a type the kinds map does not name, the way
// the server's summon path asks after any entity type: each answers its zero
// value where a value map did, rather than dereferencing a nil entry.
func TestKindsTableReadsAreNilSafeForAnUnsimulatedType(t *testing.T) {
	const tnt = registry.EntityTypeTnt
	if name, ok := MobName(tnt); ok || name != "" {
		t.Errorf("MobName(%q) = %q, %v, want no name", tnt, name, ok)
	}
	if fam := Family(tnt); fam != tnt {
		t.Errorf("Family(%q) = %q, want the type itself", tnt, fam)
	}
	if s := MobSoundsOf(tnt); s != (MobSounds{}) {
		t.Errorf("MobSoundsOf(%q) = %+v, want the zero sounds", tnt, s)
	}
	if v := MobSoundVolume(tnt); v != 1 {
		t.Errorf("MobSoundVolume(%q) = %v, want 1", tnt, v)
	}
	if !Leashable(tnt) {
		t.Errorf("Leashable(%q) = false, want the zero kind's true", tnt)
	}
	if MonsterKind(tnt) {
		t.Errorf("MonsterKind(%q) = true, want false", tnt)
	}
	w := newSpawnTestWorld()
	if boxCollidesBlocks(w, tnt, 0.5, 64, 0.5) {
		t.Errorf("boxCollidesBlocks(%q) = true, want false in open air", tnt)
	}
	if boxInLiquid(w, tnt, 0.5, 64, 0.5) {
		t.Errorf("boxInLiquid(%q) = true, want false in open air", tnt)
	}
	if box := spawnBox(tnt, 0.5, 64, 0.5); box != (SpawnBody{0.5, 64, 0.5, 0.5, 64, 0.5}) {
		t.Errorf("spawnBox(%q) = %+v, want the point box", tnt, box)
	}
	if m := (&Mob{Type: tnt}); m.Hostile() || m.IsBaby() {
		t.Errorf("a bare %q Mob = hostile %v, baby %v, want both false", tnt, m.Hostile(), m.IsBaby())
	}
}

// TestTheGridIsDroppedOnceTheTickHasRun is the hold on the grid's lifetime:
// a walking body and a passenger both come out of
// the collection's tick with no grid left, so a later tick on the same body
// cannot query the tick before's.
func TestTheGridIsDroppedOnceTheTickHasRun(t *testing.T) {
	stone := defaultState(t, "minecraft:stone")
	w := newTestWorld()
	floor(w, -2, -2, 6, 2, 63, stone)

	cow := aimless(NewMob(TypeCow, 1, 0.5, 64, 0.5, 1))
	passenger := aimless(NewMob(TypeCow, 2, 5.5, 64, 0.5, 2))
	passenger.RidesVehicle = true
	mobs := Mobs{cow, passenger}
	mobs.Tick(w, nil, nil)

	if cow.push != nil || passenger.push != nil {
		t.Errorf("after the tick, the cow's grid = %v and the passenger's = %v, want both dropped",
			cow.push, passenger.push)
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

// TestAHerdOfEightHundredGathersOnlyItsNeighbours holds the push pass to a
// body's neighbourhood, counted in candidates rather than timed: a herd of
// eight hundred cows clustered two to five to a spot over a 240x240 square,
// the layout chunk generation grows, may hand the pass a small multiple of
// the herd in all, where the whole-herd walk handed it 800 x 800. A count
// holds on any machine at any load, which a tick-time budget does not.
func TestAHerdOfEightHundredGathersOnlyItsNeighbours(t *testing.T) {
	const herd = 800
	rng := rand.New(rand.NewSource(1544))
	w := newTestWorld()
	floor(w, -130, -130, 130, 130, 63, defaultState(t, "minecraft:stone"))
	mobs := make([]*Mob, 0, herd)
	for len(mobs) < herd {
		x := (rng.Float64() - 0.5) * 240
		z := (rng.Float64() - 0.5) * 240
		for range 2 + rng.Intn(4) {
			if len(mobs) == herd {
				break
			}
			m := NewMob(TypeCow, int32(len(mobs)), x+rng.Float64()*1.5, 64, z+rng.Float64()*1.5, int64(len(mobs)))
			mobs = append(mobs, m)
		}
	}
	collection := Mobs(mobs)
	total := 0
	collection.tick(w, nil, nil, &total)
	// Each body meets its own cluster and a few neighbouring ones, about
	// six bodies; the bound leaves several times that and sits well under
	// the herd squared.
	if total < herd {
		t.Fatalf("the pass gathered %d candidates for a herd of %d, want at least one each: the tick never used the grid", total, herd)
	}
	if limit := herd * 20; total > limit {
		t.Fatalf("the pass gathered %d candidates for a herd of %d, want at most %d (a whole-herd walk is %d)",
			total, herd, limit, herd*herd)
	}
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

func TestTheCrammingRuleAtZeroOrAboveTheCrowdSpares(t *testing.T) {
	stone := defaultState(t, "minecraft:stone")
	for name, limit := range map[string]int32{"off": 0, "above": 31} {
		w := newTestWorld()
		floor(w, -10, -2, 10, 2, 63, stone)
		w.cram = &limit
		if bites := crammingBites(w, crowdOfCows(30), 60); bites != 0 {
			t.Errorf("rule %s: %d cramming hurts, want none", name, bites)
		}
	}
}

func TestACrowdOneUnderTheLimitTakesNoDamage(t *testing.T) {
	stone := defaultState(t, "minecraft:stone")
	w := newTestWorld()
	floor(w, -10, -2, 10, 2, 63, stone)
	limit := int32(30)
	// 30 cows: each one's box holds 29 others, the limit-1.
	w.cram = &limit
	if bites := crammingBites(w, crowdOfCows(30), 60); bites != 0 {
		t.Errorf("%d cramming hurts with the box at limit-1, want none", bites)
	}
	limit = 29
	if bites := crammingBites(w, crowdOfCows(30), 60); bites == 0 {
		t.Errorf("no cramming hurt with the box over the limit")
	}
}

func TestRidingPlayersDoNotCountTowardCramming(t *testing.T) {
	stone := defaultState(t, "minecraft:stone")
	limit := int32(30)
	for name, passenger := range map[string]bool{"riding": true, "standing": false} {
		w := newTestWorld()
		floor(w, -10, -2, 10, 2, 63, stone)
		w.cram = &limit
		herd := crowdOfCows(28)
		var players []Player
		for range 3 {
			players = append(players, Player{X: 0.5, Y: 64, Z: 0.5, Pushable: true, Passenger: passenger})
		}
		bites := 0
		for range 60 {
			for _, ev := range herd.Tick(w, players, nil) {
				if ev.Kind == MobBurned && ev.DamageType == rules.DamageTypeCramming {
					bites++
				}
			}
		}
		if passenger && bites != 0 {
			t.Errorf("%s: %d cramming hurts, want none", name, bites)
		}
		if !passenger && bites == 0 {
			t.Errorf("%s: no cramming hurt", name)
		}
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
func BenchmarkMobsTickVillage(b *testing.B) { benchmarkMobsTickVillage(b, nil) }

func BenchmarkMobsTickVillageWithPlayer(b *testing.B) {
	benchmarkMobsTickVillage(b, []Player{{EntityID: 1000, X: 0.5, Y: 64, Z: 0.5}})
}

func benchmarkMobsTickVillage(b *testing.B, players []Player) {
	stone := defaultState(b, "minecraft:stone")
	w := newTestWorld()
	floor(w, -40, -40, 40, 40, 63, stone)
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
	mobs := make(Mobs, 0, 160)
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
	for range 100 {
		mobs = append(mobs, NewMob(TypeCow, id, -35+rng.Float64()*70, 64, -35+rng.Float64()*70, int64(id)))
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
