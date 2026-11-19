package entity

import (
	"iter"
	"math"
	"math/bits"
	"math/rand"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// The item simulation as ordinary unit tests, driving the step over a
// world that is nothing but the blocks each case sets. No server is
// involved; the black-box tests in internal/server/*_test.go remain the proof of
// the whole life of an item — spawn, movement, pickup, despawn — and
// these are the cheapest possible checks of the simulation itself.

// testWorld is the plainest world an entity can be stepped against: blocks
// set by their coordinates in a section grid, air everywhere else, and the
// environment facts a mob's mind reads defaulted the way an unloaded chunk
// reads — no light, no rain, no daylight that burns, an easy difficulty —
// with the fields a test that needs other weather sets.
type testWorld struct {
	// inBorder is the border's own test; nil lets every column in.
	inBorder func(x, z int32) bool
	blocks   *blockGrid
	hives    map[[3]int32]int // bees stored by the hive at the cell
	staysIn  bool             // bees_stay_in_hive
	rain     bool
	noDrops  bool // entity_drops is false
	dryBiome bool // rain falls world-wide but no cell is rained on
	burn     bool
	cram     *int32 // max_entity_cramming override
	sky      uint8
	block    uint8
	diff     uint8
	diffUp   bool
	fastLava bool
	nether   bool // the dimension is piglin_safe
	// darken is the sky's darkening the raw light takes the sky light down
	// by: the clear night's eleven a fresh test world starts at.
	darken int
	// bright is whether the sky is the bright kind outside, the gate the
	// sun goals read.
	bright bool
	// dark is the outside darkness the wandering trader's potion waits for.
	dark bool
	// shaded is the stored sky light a test shades cell by cell, over the
	// everywhere-the-same w.sky.
	shaded map[[3]int32]uint8
}

// newTestWorld builds an empty test world, the blocks and shades behind it
// made ready for the set calls a test makes.
func newTestWorld() testWorld {
	return testWorld{
		blocks: &blockGrid{},
		shaded: make(map[[3]int32]uint8),
		darken: 11,
	}
}

// BlockAt is the block set at the cell, or air where none was.
func (w testWorld) BlockAt(x, y, z int32) uint16 { return w.blocks.at(x, y, z) }

// BlocksIn reads the box cell by cell, x outermost and z innermost.
func (w testWorld) BlocksIn(x0, y0, z0, x1, y1, z1 int32, buf []uint16) []uint16 {
	buf = buf[:0]
	for x := x0; x <= x1; x++ {
		for y := y0; y <= y1; y++ {
			for z := z0; z <= z1; z++ {
				buf = append(buf, w.blocks.at(x, y, z))
			}
		}
	}
	return buf
}

// blocksVia is BlocksIn read cell by cell through one fake's BlockAt, so a
// fake that overrides BlockAt sees the box reads too.
func blocksVia(at func(x, y, z int32) uint16, x0, y0, z0, x1, y1, z1 int32, buf []uint16) []uint16 {
	buf = buf[:0]
	for x := x0; x <= x1; x++ {
		for y := y0; y <= y1; y++ {
			for z := z0; z <= z1; z++ {
				buf = append(buf, at(x, y, z))
			}
		}
	}
	return buf
}

// MinY is the bottom of the test's world: the overworld's own, so an orb
// dropped far beneath it is gone the way the real dimension's would be.
func (w testWorld) DimensionName() string { return "minecraft:overworld" }
func (w testWorld) MinY() int32           { return -64 }

// MaxY is the overworld's top.
func (w testWorld) MaxY() int32 { return 320 }

// Height is the overworld's 384 blocks.
func (w testWorld) Height() int32 { return 384 }

// set places a block, for arrangements that read.
func (w testWorld) set(x, y, z int32, state uint16) {
	w.blocks.set(x, y, z, state)
}

// blockGrid is the test world's blocks: 16x16x16 sections in a dense grid
// over the section box the sets have reached, so a read is two slice
// indexes and no hash, near the cost of a chunk section's read. A nil grid
// reads as air everywhere. Copies of a testWorld share one grid.
type blockGrid struct {
	lo       [3]int32 // the grid's lowest section coordinate
	n        [3]int32 // sections along x, y and z; 0 before the first set
	sections []*gridSection
	// far holds the sections a set placed where growing the grid to reach
	// them would pass maxGridSections. The grid's box only grows, so it never
	// comes to cover one: any box that did would pass the bound too.
	far map[[3]int32]*gridSection
}

// gridSection is one 16x16x16 section of a blockGrid, indexed y<<8|z<<4|x.
type gridSection struct {
	states [4096]uint16
	// placed marks the cells a set wrote, air included, so RainingAt and
	// cells see exactly the cells a map keyed by position would hold.
	placed [64]uint64
}

// maxGridSections bounds the dense grid at 8 MiB of section pointers.
const maxGridSections = 1 << 20

// cellIndex is the cell's index within its section.
func cellIndex(x, y, z int32) int { return int(y&15)<<8 | int(z&15)<<4 | int(x&15) }

// section is the section at section coordinates, nil where no set reached.
func (g *blockGrid) section(sx, sy, sz int32) *gridSection {
	ix, iy, iz := sx-g.lo[0], sy-g.lo[1], sz-g.lo[2]
	if uint32(ix) < uint32(g.n[0]) && uint32(iy) < uint32(g.n[1]) && uint32(iz) < uint32(g.n[2]) {
		return g.sections[(ix*g.n[1]+iy)*g.n[2]+iz]
	}
	if g.far == nil {
		return nil
	}
	return g.far[[3]int32{sx, sy, sz}]
}

// at is the block set at the cell, air where none was.
func (g *blockGrid) at(x, y, z int32) uint16 {
	if g == nil {
		return 0
	}
	if s := g.section(x>>4, y>>4, z>>4); s != nil {
		return s.states[cellIndex(x, y, z)]
	}
	return 0
}

// set writes the cell, growing the grid to reach it.
func (g *blockGrid) set(x, y, z int32, state uint16) {
	s := g.sectionFor(x>>4, y>>4, z>>4)
	i := cellIndex(x, y, z)
	s.states[i] = state
	s.placed[i>>6] |= 1 << (i & 63)
}

// unset takes the cell's block away, so it reads as air and was never set.
func (g *blockGrid) unset(x, y, z int32) {
	if s := g.section(x>>4, y>>4, z>>4); s != nil {
		i := cellIndex(x, y, z)
		s.states[i] = 0
		s.placed[i>>6] &^= 1 << (i & 63)
	}
}

// sectionFor is the section at section coordinates, made where missing.
func (g *blockGrid) sectionFor(sx, sy, sz int32) *gridSection {
	if s := g.section(sx, sy, sz); s != nil {
		return s
	}
	key := [3]int32{sx, sy, sz}
	lo, hi := key, key
	if g.n[0] > 0 {
		for a := range 3 {
			lo[a] = min(lo[a], g.lo[a])
			hi[a] = max(hi[a], g.lo[a]+g.n[a]-1)
		}
	}
	n := [3]int32{hi[0] - lo[0] + 1, hi[1] - lo[1] + 1, hi[2] - lo[2] + 1}
	if int64(n[0])*int64(n[1])*int64(n[2]) > maxGridSections {
		s := &gridSection{}
		if g.far == nil {
			g.far = make(map[[3]int32]*gridSection)
		}
		g.far[key] = s
		return s
	}
	if n != g.n {
		g.regrid(lo, n)
	}
	s := &gridSection{}
	g.sections[((sx-g.lo[0])*g.n[1]+sy-g.lo[1])*g.n[2]+sz-g.lo[2]] = s
	return s
}

// regrid moves the sections into a grid over the box from lo spanning n,
// which holds the old box.
func (g *blockGrid) regrid(lo, n [3]int32) {
	old := *g
	g.lo, g.n = lo, n
	g.sections = make([]*gridSection, n[0]*n[1]*n[2])
	for ix := range old.n[0] {
		for iy := range old.n[1] {
			for iz := range old.n[2] {
				s := old.sections[(ix*old.n[1]+iy)*old.n[2]+iz]
				g.sections[((ix+old.lo[0]-lo[0])*n[1]+iy+old.lo[1]-lo[1])*n[2]+iz+old.lo[2]-lo[2]] = s
			}
		}
	}
}

// cells yields every cell a set wrote and no unset took away, in no
// particular order; unsetting the yielded cell during the walk is safe.
func (g *blockGrid) cells() iter.Seq[[3]int32] {
	return func(yield func([3]int32) bool) {
		if g == nil {
			return
		}
		walk := func(sx, sy, sz int32, s *gridSection) bool {
			for wi := range s.placed {
				for word := s.placed[wi]; word != 0; word &= word - 1 {
					i := int32(wi<<6 + bits.TrailingZeros64(word))
					if !yield([3]int32{sx<<4 | i&15, sy<<4 | i>>8, sz<<4 | i>>4&15}) {
						return false
					}
				}
			}
			return true
		}
		for ix := range g.n[0] {
			for iy := range g.n[1] {
				for iz := range g.n[2] {
					s := g.sections[(ix*g.n[1]+iy)*g.n[2]+iz]
					if s != nil && !walk(g.lo[0]+ix, g.lo[1]+iy, g.lo[2]+iz, s) {
						return
					}
				}
			}
		}
		for key, s := range g.far {
			if !walk(key[0], key[1], key[2], s) {
				return
			}
		}
	}
}

// SkyLightAt answers the light the test arranged — the per-cell shade a
// test set where it set one, the everywhere-the-same sky light elsewhere.
func (w testWorld) SkyLightAt(x, y, z int32) uint8 {
	if v, ok := w.shaded[[3]int32{x, y, z}]; ok {
		return v
	}
	return w.sky
}

// shade records the stored sky light of one cell by hand, the way a roof
// edge's spread shades the cells under it without taking the open sky's
// light away from the cells beyond it.
func (w testWorld) shade(x, y, z int32, v uint8) {
	if w.shaded == nil {
		w.shaded = make(map[[3]int32]uint8)
	}
	w.shaded[[3]int32{x, y, z}] = v
}

// InBorder is the test's border, open when none was set.
func (w testWorld) InBorder(x, z int32) bool { return w.inBorder == nil || w.inBorder(x, z) }

// MaxLocalRawLightAt is the brighter of the block light the test set and
// the sky light with the darkening taken off it.
func (w testWorld) MaxLocalRawLightAt(x, y, z int32) uint8 {
	sky := max(int(w.SkyLightAt(x, y, z))-w.darken, 0)
	if int(w.block) > sky {
		return w.block
	}
	return uint8(sky)
}

// BrightOutside reports the outside brightness the test set.
func (w testWorld) BrightOutside() bool { return w.bright }

// DarkOutside reports the darkness the test set.
func (w testWorld) DarkOutside() bool { return w.dark }

// IsThundering is false in the test world.
func (w testWorld) IsThundering() bool { return false }

func (w testWorld) ClockTime(string) (worldAge, total int64, ok bool) { return 0, 0, false }

func (w testWorld) EnvironmentFloat(string, float64, float64, float64) float32 { return 0 }

func (w testWorld) LootReads() world.LootReads { return nil }

// AmbientLight is the nought the test world's dimension carries.
func (w testWorld) AmbientLight() float32 { return 0 }

// BoolRule answers every rule on but universal_anger, which is off by default,
// and entity_drops when the test turned it off.
func (w testWorld) BoolRule(id world.GameRuleIndex) bool {
	return id != world.RuleUniversalAnger && (id != world.RuleEntityDrops || !w.noDrops)
}

// IsRaining reports the weather the test set.
func (w testWorld) IsRaining() bool { return w.rain }

func (w testWorld) BeesStayInHive() bool { return w.staysIn }

func (w testWorld) BeehiveOccupants(x, y, z int32) (int, bool) {
	n, ok := w.hives[[3]int32{x, y, z}]
	return n, ok
}

func (w testWorld) BeehiveCells(x, y, z, r int32) [][3]int32 {
	var out [][3]int32
	for c := range w.hives {
		if dx, dy, dz := c[0]-x, c[1]-y, c[2]-z; dx*dx+dy*dy+dz*dz <= r*r {
			out = append(out, c)
		}
	}
	return out
}

// RainingAt reports rain landing on the cell: the weather the test set,
// with any block the test put above the cell in its column left dry, the
// roof the rain check reads.
func (w testWorld) RainingAt(x, y, z int32) bool {
	if !w.rain || w.dryBiome {
		return false
	}
	for pos := range w.blocks.cells() {
		if pos[0] == x && pos[2] == z && pos[1] > y {
			return false
		}
	}
	return true
}

// MonstersBurn reports the daylight the test set.
func (w testWorld) MonstersBurn() bool { return w.burn }

// PiglinsZombify is false in the Nether the test set.
func (w testWorld) PiglinsZombify() bool { return !w.nether }

// CatGiftChance is 0 in the test world.
func (w testWorld) CatGiftChance() float32 { return 0 }

// Difficulty is the difficulty the test set, easy by default.
func (w testWorld) Difficulty() uint8 {
	if !w.diffUp {
		return 1
	}
	return w.diff
}

// FastLava reports the lava speed the test set, the overworld's slow
// current unless a test that wants the nether's flips it.
func (w testWorld) FastLava() bool { return w.fastLava }

func (w testWorld) SnowGolemMelts(x, y, z int32) bool { return false }

func (w testWorld) SnowLayerSurvives(x, y, z int32) bool { return false }
func (w testWorld) IsAirAt(x, y, z int32) bool           { return false }
func (w testWorld) MobGriefing() bool                    { return false }

// TurtleEggsIn is true so an egg search walks the stub's cells.
func (w testWorld) TurtleEggsIn(x0, y0, z0, x1, y1, z1 int32) bool { return true }

// MaxEntityCramming is the rule's default, 24, unless a test sets cram.
func (w testWorld) Survives(x, y, z int32, state uint16) bool { return true }

func (w testWorld) MaxEntityCramming() int32 {
	if w.cram != nil {
		return *w.cram
	}
	return 24
}

func (w testWorld) SeaLevel() int32 { return 63 }

func (w testWorld) MotionBlockingHeight(x, z int32) int32 { return w.MinY() - 1 }

func (w testWorld) MovingPistonBoxes(x, y, z int32) []world.CollisionBox { return nil }
func (w testWorld) ShulkerBoxBoxes(state uint16, x, y, z int32) []world.CollisionBox {
	return world.CollisionBoxes(state)
}
func (w testWorld) PlacementSurvives(uint16, int32, int32, int32) bool { return true }

// defaultState is the default state of a named block, so the tests name
// what they mean.
func defaultState(t testing.TB, name string) uint16 {
	t.Helper()
	state, ok := registry.FindState(name, nil)
	if !ok {
		t.Fatalf("the block registry has no %s", name)
	}
	return state
}

// closeEnough reports whether two coordinates of the simulation agree to
// within the rounding a handful of floating-point operations picks up.
func closeEnough(got, want float64) bool {
	return math.Abs(got-want) < 1e-9
}

// TestAnItemFallsWithGravityAndDragAndComesToRestOnASolidBlock steps an
// item dropped in the air over a stone floor: the first two steps follow
// vanilla's gravity-then-move-then-drag order, the fall ends with the item
// resting half its box height above the floor, and the item that rests
// stays put through the rest cycle.
func TestAnItemFallsWithGravityAndDragAndComesToRestOnASolidBlock(t *testing.T) {
	stone := defaultState(t, "minecraft:stone")
	w := newTestWorld()
	w.set(3, 60, -7, stone)

	it := &Item{X: 3.5, Y: 62.5, Z: -6.5}
	it.Step(w, nil)
	if !closeEnough(it.VY, (0-gravity)*drag) {
		t.Errorf("vy after one step = %v, want %v", it.VY, (0-gravity)*drag)
	}
	if !closeEnough(it.Y, 62.5-gravity) {
		t.Errorf("y after one step = %v, want %v (the fall at the speed gravity gave it)", it.Y, 62.5-gravity)
	}
	it.Step(w, nil)
	vyOne := (0 - gravity) * drag
	if !closeEnough(it.VY, (vyOne-gravity)*drag) {
		t.Errorf("vy after two steps = %v, want %v", it.VY, (vyOne-gravity)*drag)
	}
	if !closeEnough(it.Y, 62.5-gravity+(vyOne-gravity)) {
		t.Errorf("y after two steps = %v, want %v",
			it.Y, 62.5-gravity+(vyOne-gravity))
	}

	ticks := 2
	for !it.OnGround {
		it.Step(w, nil)
		ticks++
		if ticks > 1000 {
			t.Fatalf("the item did not come to rest in %d ticks", ticks)
		}
	}
	if !closeEnough(it.Y, 61+halfSize) {
		t.Errorf("resting y = %v, want %v (a half box above the floor)", it.Y, 61+halfSize)
	}
	if it.Age != ticks {
		t.Errorf("age = %d ticks, want %d", it.Age, ticks)
	}

	// An item at rest stays where it lies: vanilla's tick moves one only
	// on every fourth of its ticks, and the move that runs finds the block
	// under it and stops the fall it had been gathering again. Eight ticks
	// hold two whole cycles whatever tick the landing landed on.
	settled := false
	for i := range 8 {
		it.Step(w, nil)
		if !closeEnough(it.Y, 61+halfSize) {
			t.Errorf("resting y after %d more ticks = %v, want %v (an item at rest does not move)", i+1, it.Y, 61+halfSize)
		}
		if !it.OnGround {
			t.Errorf("a resting item is off the ground on tick %d of the rest cycle", i+1)
		}
		if it.VY == 0 {
			settled = true
		}
		if it.VY < -0.13 {
			t.Errorf("resting vy after %d more ticks = %v, want the fall spent every fourth tick", i+1, it.VY)
		}
	}
	if !settled {
		t.Error("no tick of the rest cycle spent the gathered fall, want the move's velocity back to nothing")
	}
}

// TestAnItemABlockCoversIsPushedOutTowardsTheNearestFreeSide drops the
// falling sand's own result on a resting item: the block the fall placed
// fills the very cell the item's box lies in, and vanilla's tick answers a
// buried body with the moveTowardsClosestSpace push — the nearest side
// whose neighbour is not a full block's shape taking the random
// tenth-to-third of a block a tick, the other two axes three quarters of
// what they had. This item sits half a block from each flat face and an
// eighth under the top, so the north face wins the walk's own tie — the
// first of the four equal distances the vanilla order asks — and the push
// carries the item north.
func TestAnItemABlockCoversIsPushedOutTowardsTheNearestFreeSide(t *testing.T) {
	stone := defaultState(t, "minecraft:stone")
	sand := defaultState(t, "minecraft:sand")
	w := newTestWorld()
	w.set(10, 40, 10, stone)

	it := &Item{X: 10.5, Y: 41 + halfSize, Z: 10.5, OnGround: true}
	it.Step(w, nil)
	if !it.OnGround || !closeEnough(it.Y, 41+halfSize) {
		t.Fatal("a resting item is not supported on the solid block it lies on")
	}

	// The sand the falling block became fills the item's own cell.
	w.set(10, 41, 10, sand)
	it.Rand = rand.New(rand.NewSource(9))
	it.Step(w, nil)

	if it.VZ >= 0 {
		t.Errorf("vz after the tick buried = %v, want it pushing north", it.VZ)
	}
	if it.VZ <= -0.3 {
		t.Errorf("vz after the tick buried = %v, want no more than the push's third of a block", it.VZ)
	}
	if !closeEnough(it.VX, 0) {
		t.Errorf("vx after the tick buried = %v, want three quarters of nothing", it.VX)
	}
	if !it.OnGround {
		t.Error("the buried push left the item off the ground, want the flag the raw move keeps")
	}
	if it.Z >= 10.5 {
		t.Errorf("z after the tick buried = %v, want the body under way north of where it lay", it.Z)
	}

	// And a few ticks of the same push walk the box out of the sand's
	// cell for good.
	for i := 0; i < 20 && it.insideSolid(w); i++ {
		it.Step(w, nil)
	}
	if it.insideSolid(w) {
		t.Errorf("the item is still inside the sand after twenty ticks, at (%v, %v, %v)", it.X, it.Y, it.Z)
	}
}

// TestAnItemDespawnsAtTheDespawnAge checks the despawn rule the tick asks
// of every item: an item is given five minutes, and the tick its age
// reaches the despawn age is the one that finds its time up.
func TestAnItemDespawnsAtTheDespawnAge(t *testing.T) {
	w := newTestWorld()
	it := &Item{Age: despawnTicks - 1}
	if it.ShouldDespawn() {
		t.Error("an item one tick short of the despawn age despawns, want it still alive")
	}
	it.Step(w, nil)
	if it.Age != despawnTicks {
		t.Fatalf("age = %d, want %d", it.Age, despawnTicks)
	}
	if !it.ShouldDespawn() {
		t.Error("an item at the despawn age does not despawn, want its time up")
	}
	it.Step(w, nil)
	if !it.ShouldDespawn() {
		t.Error("an item past the despawn age does not despawn, want its time to stay up")
	}
}

// TestAnItemsPickupDelayRunsOutBeforeAnyoneMayHaveIt checks the other
// question the tick asks: a freshly spawned item holds whoever dropped it
// off for the delay it was given, and only then becomes one a player
// standing close enough may have.
func TestAnItemsPickupDelayRunsOutBeforeAnyoneMayHaveIt(t *testing.T) {
	w := newTestWorld()
	it := &Item{PickupDelay: ThrownPickupDelay}
	if it.PickupDue() {
		t.Fatal("a thrown item's pickup has already run out, want the two-second wait")
	}
	for range ThrownPickupDelay - 1 {
		it.Step(w, nil)
		if it.PickupDue() {
			t.Fatal("the pickup delay ran out early, want it holding until the last tick")
		}
	}
	it.Step(w, nil)
	if !it.PickupDue() {
		t.Error("the pickup delay has not run out, want the item ready on the tick it ends")
	}
	it.Step(w, nil)
	if !it.PickupDue() {
		t.Error("a due pickup went back to waiting, want the delay to stay spent")
	}
}

// TestAnItemDroppedIntoStillWaterFloatsBackUpOutOfIt drops an item to the
// bottom of a still pool and steps it: the water's drift lifts it off the
// floor and it climbs to bob about the surface instead of lying where it
// landed. The first step is checked against the vanilla arithmetic written
// out, so the composition the tick runs is the one under test.
func TestAnItemDroppedIntoStillWaterFloatsBackUpOutOfIt(t *testing.T) {
	stone := defaultState(t, "minecraft:stone")
	water := defaultState(t, "minecraft:water")
	w := newTestWorld()
	w.set(0, 62, 0, stone)
	w.set(0, 63, 0, water)

	it := &Item{X: 0.5, Y: 63 + halfSize, Z: 0.5, Health: 5}
	it.Step(w, nil)
	if !closeEnough(it.VY, fluidRise*drag) {
		t.Errorf("vy after one tick in water = %v, want %v (the rise through the air's drag)", it.VY, fluidRise*drag)
	}
	if !closeEnough(it.Y, 63+halfSize+fluidRise) {
		t.Errorf("y after one tick in water = %v, want %v (carried by the rise)", it.Y, 63+halfSize+fluidRise)
	}

	// The float: a climb off the bottom, a coast over the surface and a
	// fall back into it, over and over. What marks the float against a
	// sunken stack is the top of the cycle: an item that has found the
	// surface has been up where the water is shallow, which one resting on
	// the floor never is.
	high := it.Y
	for range 250 {
		it.Step(w, nil)
		if it.Y > high {
			high = it.Y
		}
	}
	if high < 63.75 {
		t.Errorf("the highest of the last 250 ticks = %v, want the bob up at the surface the water's drift carries it to", high)
	}
}

// TestAnItemInLavaIsDestroyedInTwoTicks drops a plain item into a pool of
// lava: the lava lights it and hurts it four points a tick, the five an
// item is given running out on the second, and the tick that ends it
// reports both the destruction and the burn sound vanilla plays for it.
func TestAnItemInLavaIsDestroyedInTwoTicks(t *testing.T) {
	stone := defaultState(t, "minecraft:stone")
	lava := defaultState(t, "minecraft:lava")
	w := newTestWorld()
	w.set(0, 62, 0, stone)
	w.set(0, 63, 0, lava)

	it := &Item{X: 0.5, Y: 63 + halfSize, Z: 0.5, Health: 5}
	events := it.Step(w, nil)
	if it.Health != 1 {
		t.Errorf("health after one tick in lava = %d, want 1 (four of the five spent)", it.Health)
	}
	if it.fireTicks != fireTicksLava {
		t.Errorf("fire after one tick in lava = %d, want %d (the fifteen-second burn lit)", it.fireTicks, fireTicksLava)
	}
	if it.Dead {
		t.Fatal("the item is dead after one tick of lava, want it holding on with one point left")
	}
	for _, ev := range events {
		if ev.Kind == ItemDestroyed {
			t.Error("the first tick of lava destroyed the item, want it surviving to the second")
		}
	}
	if len(events) != 1 || events[0].Kind != ItemHurt {
		t.Errorf("the first tick of lava played %v, want only the hurt event (the burn sound waits for the death or the tenth tick)", events)
	}

	events = it.Step(w, nil)
	if !it.Dead {
		t.Fatal("the item survived two ticks of lava, want it burned up")
	}
	var destroyed, burned bool
	for _, ev := range events {
		switch ev.Kind {
		case ItemDestroyed:
			destroyed = true
		case ItemBurned:
			burned = true
		}
	}
	if !destroyed {
		t.Error("the item's death by lava carried no destruction, want the body taken out of the world")
	}
	if !burned {
		t.Error("the item's death by lava played no burn, want the fizz vanilla plays for it")
	}
}

// mergePair stands two items of the same stackable item side by side in
// one block, resting on the floor given, with the counts, pickup delays
// and ages a test wants to merge, and returns them with the neighbour
// list each is stepped with.
func mergePair(t testing.TB, item string, aCount, bCount, aDelay, bDelay, aAge, bAge int, floor int32) (a, b *Item, neighbours func(*Item) []*Item) {
	t.Helper()
	id, ok := registry.ItemID(item)
	if !ok {
		t.Fatalf("the item registry has no %s", item)
	}
	stack := func(count int) protocol.ItemStack {
		return protocol.ItemStack{ItemID: id, Count: int32(count)}
	}
	a = &Item{ID: 4, Stack: stack(aCount), X: 3.5, Y: float64(floor) + 1 + halfSize, Z: -6.5,
		OnGround: true, PickupDelay: aDelay, Age: aAge, Health: 5}
	b = &Item{ID: 8, Stack: stack(bCount), X: 3.6, Y: float64(floor) + 1 + halfSize, Z: -6.5,
		OnGround: true, PickupDelay: bDelay, Age: bAge, Health: 5}
	return a, b, func(*Item) []*Item { return []*Item{a, b} }
}

// TestAMergeTakesTheSmallerStackIntoTheLarger steps the smaller of two
// resting stacks on the tick its settled scan comes due: the larger
// neighbour takes it in — its own count grown by the whole of the
// smaller's, its own place kept, the longer of the two pickup delays and
// the younger of the two ages its own — and the smaller is left emptied
// and dead for the server to take out of the world.
func TestAMergeTakesTheSmallerStackIntoTheLarger(t *testing.T) {
	w := newTestWorld()
	w.set(3, 60, -7, defaultState(t, "minecraft:stone"))

	a, b, neighbours := mergePair(t, "minecraft:dirt", 2, 5, 10, 40, 100, 7, 60)
	a.tickCount = 39 // the step below is the fortieth tick, the scan's due
	a.Step(w, neighbours)

	if !a.Dead || a.Stack.Count != 0 {
		t.Errorf("the smaller stack holds %+v dead=%v, want it emptied into its neighbour", a.Stack, a.Dead)
	}
	if b.Stack.Count != 7 {
		t.Errorf("the survivor holds %d, want the five and the two taken in", b.Stack.Count)
	}
	if b.PickupDelay != 40 {
		t.Errorf("the survivor's pickup delay = %d, want the longer of the two waits", b.PickupDelay)
	}
	if b.Age != 7 {
		t.Errorf("the survivor's age = %d, want the younger of the two ages", b.Age)
	}
	if b.X != 3.6 {
		t.Errorf("the survivor lies at x=%v, want its own 3.6 and not the taken-in body's 3.5", b.X)
	}
	if !b.TakeMerge() {
		t.Error("the survivor left no merge mark, want the server owed the stack's metadata update")
	}
	if b.TakeMerge() {
		t.Error("the survivor's merge mark survived being taken, want one packet per merge")
	}
}

// TestAnEnchantedStackDoesNotMergeWithAPlainOne steps a resting enchanted
// stack and a plain one of the same item through the same scans: the
// components two stacks carry are part of what makes them of a kind, the
// test vanilla's isSameItemSameComponents puts behind every merge, and two
// stacks of one item that disagree there stay two.
func TestAnEnchantedStackDoesNotMergeWithAPlainOne(t *testing.T) {
	sharpness, ok := registry.EnchantmentID("minecraft:sharpness")
	if !ok {
		t.Fatal("the enchantment registry has no sharpness")
	}
	diamond, ok := registry.ItemID("minecraft:diamond")
	if !ok {
		t.Fatal("the item registry has no diamond")
	}
	plain := protocol.ItemStack{ItemID: diamond, Count: 2}
	enchanted := protocol.ItemStack{ItemID: diamond, Count: 2,
		Enchantments: map[int32]int32{sharpness: 5}}
	if areMergable(plain, enchanted) || areMergable(enchanted, plain) {
		t.Error("a plain stack merged with an enchanted one")
	}
	if !areMergable(enchanted, enchanted) {
		t.Error("two enchanted stacks refused to merge")
	}
	// A stored enchantment is a component of its own: a written book does
	// not pour into a plain one, whatever the counts.
	book, ok := registry.ItemID("minecraft:enchanted_book")
	if !ok {
		t.Fatal("the item registry has no enchanted_book")
	}
	plainBook := protocol.ItemStack{ItemID: book, Count: 1}
	stored := protocol.ItemStack{ItemID: book, Count: 1,
		StoredEnchantments: map[int32]int32{sharpness: 3}}
	if areMergable(plainBook, stored) {
		t.Error("a plain book merged with a written one")
	}
}

// TestRainPutsOutABurningItemOnlyWhereItLands burns three items in rain: the
// one under open sky is put out, the one under a roof and the one in a dry
// biome keep burning.
func TestRainPutsOutABurningItemOnlyWhereItLands(t *testing.T) {
	stone := defaultState(t, "minecraft:stone")
	cases := []struct {
		name string
		dry  bool
		roof bool
		want int
	}{
		{"open sky", false, false, -1},
		{"under a roof", false, true, 100},
		{"dry biome", true, false, 100},
	}
	for _, c := range cases {
		w := newTestWorld()
		w.rain, w.dryBiome = true, c.dry
		if c.roof {
			w.set(0, 66, 0, stone)
		}
		it := &Item{X: 0.5, Y: 64 + halfSize, Z: 0.5, Health: 5, fireTicks: 100}
		it.insideEffects(w, new([]ItemEvent))
		if it.fireTicks != c.want {
			t.Errorf("%s: fire after rain = %d, want %d", c.name, it.fireTicks, c.want)
		}
	}
}

// TestAnItemOnAnIceEdgeSlidesOnTheIce: the friction is read through the
// supporting block, so an item whose centre hangs over air but whose box
// rests on ice keeps the ice's drift.
// vanilla: Entity.getBlockPosBelowThatAffectsMyMovement
func TestAnItemOnAnIceEdgeSlidesOnTheIce(t *testing.T) {
	for _, tc := range []struct {
		block string
		drag  float64
	}{{"minecraft:ice", 0.98}, {"minecraft:stone", 0.6}} {
		w := newTestWorld()
		floor := defaultState(t, tc.block)
		for z := int32(8); z <= 14; z++ {
			w.set(10, 40, z, floor)
		}
		it := &Item{X: 11.05, Y: 41 + halfSize, Z: 10.5, VZ: 0.2, OnGround: true}
		it.Step(w, nil)
		want := 0.2 * float64(float32(0.98)) * tc.drag
		if math.Abs(it.VZ-want) > 1e-4 {
			t.Errorf("%s: vz = %v, want %v", tc.block, it.VZ, want)
		}
	}
}

// TestAnItemOnSlimeLosesMostOfItsSlide holds an item crossing slime to
// stepOn's cut: its horizontal velocity is scaled by 0.4 + |vy|*0.2 before
// the ground friction, so it keeps well under 0.6 of what it keeps on stone.
// vanilla: SlimeBlock.stepOn
func TestAnItemOnSlimeLosesMostOfItsSlide(t *testing.T) {
	vz := func(block string) float64 {
		w := newTestWorld()
		floor := defaultState(t, block)
		for z := int32(8); z <= 14; z++ {
			w.set(10, 40, z, floor)
		}
		it := &Item{X: 10.5, Y: 41 + halfSize, Z: 10.5, VZ: 0.2, OnGround: true}
		it.Step(w, nil)
		return it.VZ
	}
	slime, stone := vz("minecraft:slime_block"), vz("minecraft:stone")
	if !(slime > 0 && slime < 0.6*stone) {
		t.Errorf("vz on slime = %v, on stone = %v; want slime under 0.6 of stone's", slime, stone)
	}
}

func (w testWorld) InBorderBox(minX, minZ, maxX, maxZ float64) bool {
	return w.InBorder(int32(math.Floor(minX)), int32(math.Floor(minZ))) && w.InBorder(int32(math.Floor(maxX-1e-5)), int32(math.Floor(maxZ-1e-5)))
}

func (w testWorld) BorderEdges() (minX, minZ, maxX, maxZ float64, ok bool) { return }

func (w testWorld) NoLeavesHeight(x, z int32) int32 {
	return (spawnTestWorld{testWorld: w}).NoLeavesHeight(x, z)
}

// PrecipitatesAt is RainingAt for the test world, which has no snow.
func (w testWorld) PrecipitatesAt(x, y, z int32) bool { return w.RainingAt(x, y, z) }

// InClouds is false: the test world has no cloud layer.
func (w testWorld) InClouds(x, y, z, height float64) bool { return false }
