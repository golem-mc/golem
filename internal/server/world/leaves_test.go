package world

import (
	"strconv"
	"testing"

	"github.com/golem-mc/golem/registry"
)

// The leaves, engine-side: the distance a leaf stands at following the
// logs and leaves around it, the canopy a cut trunk rots away, the leaf a
// player owns keeping its place, and the distances the generator's trees
// grow with. The scheduled visits run one game tick out, so the windows
// are ticks; the random ticks run pass by pass, the way the server gathers
// their chunks.

// leafProps reads a leaf state's properties, failing the test when the
// state is not a leaf's.
func leafProps(t *testing.T, state uint16) map[string]string {
	t.Helper()
	_, props, ok := registry.BlockStateName(state)
	if !ok || !registry.BlockOf(state).IsLeaves() {
		t.Fatalf("block state %d is not a leaf", state)
	}
	return registry.ParseStateProps(props)
}

// leafState resolves an oak leaf state, failing the test when the registry
// does not have it.
func leafState(t *testing.T, overrides map[string]string) uint16 {
	t.Helper()
	state, ok := registry.FindState("minecraft:oak_leaves", overrides)
	if !ok {
		t.Fatalf("the registry has no oak leaves with %v", overrides)
	}
	return state
}

// checkLeaf asserts the leaf at a position carries exactly the distance and
// persistence given.
func checkLeaf(t *testing.T, w *World, x, y, z int32, distance string, persistent string, label string) {
	t.Helper()
	props := leafProps(t, w.BlockAt(x, y, z))
	if props["distance"] != distance || props["persistent"] != persistent {
		t.Errorf("%s: leaf at (%d, %d, %d) carries distance=%s persistent=%s, want distance=%s persistent=%s",
			label, x, y, z, props["distance"], props["persistent"], distance, persistent)
	}
}

// TestLeafDistanceFollowsItsNeighbours checks the heart of the leaves: a
// leaf whose neighbour changed stands at the distance a walk out from the
// logs around it gives — the log beside it holds it at one, the log's
// absence lets it go to seven — and the state it was set down with counts
// for nothing once the walk has spoken.
func TestLeafDistanceFollowsItsNeighbours(t *testing.T) {
	w, p := newPhysicsWorld(t)
	log := registry.BlockStates["minecraft:oak_log"]
	leaf := leafState(t, map[string]string{"distance": "2"})

	setBlock(t, w, p, 0, GroundLevel, 0, log)
	// A leaf at a distance the walk does not give, woken by the stone
	// set above it — a placement of its own wakes nothing, the way the
	// vanilla onPlace leaves the placement's own distance alone.
	setBlock(t, w, p, 1, GroundLevel, 0, leaf)
	setBlock(t, w, p, 1, GroundLevel+1, 0, stoneState)

	var clock int64
	clock++
	p.Tick(clock)
	checkLeaf(t, w, 1, GroundLevel, 0, "1", "false", "beside a log")

	// The log's absence: the leaf's next wake recomputes the walk, and
	// nothing shorter than seven is left to count.
	setBlock(t, w, p, 0, GroundLevel, 0, airState)
	clock++
	p.Tick(clock)
	checkLeaf(t, w, 1, GroundLevel, 0, "7", "false", "with the log gone")
}

// TestCuttingTheTrunkRotsTheCanopy checks the decay: a canopy around a
// trunk, its leaves standing at the distances the walks out from the trunk
// give, goes to distance seven when the trunk goes, and the random tick
// pass then takes every distance-seven leaf it can reach — each one handed
// to the server as a decay, the leaf's loot's to roll.
func TestCuttingTheTrunkRotsTheCanopy(t *testing.T) {
	w, p := newPhysicsWorld(t)
	log := registry.BlockStates["minecraft:oak_log"]

	// A trunk of four and the canopy it holds: the eight leaves of the
	// ring and two more a step out, each standing at the distance the
	// walk out from the trunk gives — the ring's edges at one, its
	// corners and the far pair at two. The whole grove stands inside one
	// chunk, the one the random tick pass below is handed.
	for y := int32(GroundLevel); y <= GroundLevel+3; y++ {
		setBlock(t, w, p, 8, y, 8, log)
	}
	var leaves [][3]int32
	for _, c := range [][2]int32{{1, 0}, {-1, 0}, {0, 1}, {0, -1}} {
		setBlock(t, w, p, 8+c[0], GroundLevel+2, 8+c[1], leafState(t, map[string]string{"distance": "1"}))
		leaves = append(leaves, [3]int32{8 + c[0], GroundLevel + 2, 8 + c[1]})
	}
	for _, c := range [][2]int32{{1, 1}, {1, -1}, {-1, 1}, {-1, -1}, {2, 0}, {-2, 0}} {
		setBlock(t, w, p, 8+c[0], GroundLevel+2, 8+c[1], leafState(t, map[string]string{"distance": "2"}))
		leaves = append(leaves, [3]int32{8 + c[0], GroundLevel + 2, 8 + c[1]})
	}
	for _, c := range leaves {
		if d := leafProps(t, w.BlockAt(c[0], c[1], c[2]))["distance"]; d != "1" && d != "2" {
			t.Fatalf("setup: leaf at (%d, %d, %d) stands at distance=%s before the cut", c[0], c[1], c[2], d)
		}
	}

	// The trunk goes; the canopy walks out to seven and settles.
	var clock int64
	for y := int32(GroundLevel); y <= GroundLevel+3; y++ {
		setBlock(t, w, p, 8, y, 8, airState)
	}
	tickToQuiet(t, p, &clock, 120)
	for _, c := range leaves {
		checkLeaf(t, w, c[0], c[1], c[2], "7", "false", "after the trunk went")
	}

	// The random ticks take the canopy down, each rotted leaf a decay.
	rotted := make(map[[3]int32]bool)
	var decays []Decay
	for pass := 1; pass <= 6000 && len(rotted) < len(leaves); pass++ {
		_, more := p.RandomTick(int64(pass), []ChunkPos{{0, 0}}, 3, 1)
		decays = append(decays, more...)
		for _, d := range more {
			rotted[[3]int32{d.X, d.Y, d.Z}] = true
		}
	}
	if len(rotted) != len(leaves) {
		t.Fatalf("6000 random tick passes rotted %d of %d leaves", len(rotted), len(leaves))
	}
	for _, c := range leaves {
		if w.BlockAt(c[0], c[1], c[2]) != airState {
			t.Errorf("leaf at (%d, %d, %d) rotted but stands yet", c[0], c[1], c[2])
		}
	}
	if len(decays) != len(leaves) {
		t.Errorf("the pass handed over %d decays for %d rotted leaves", len(decays), len(leaves))
	}
}

// TestWaterloggedLeafLeavesItsWater checks the decay of a waterlogged leaf:
// the vanilla removeBlock writes the cell's fluid as its block, so the water
// source stands where the leaf rotted rather than air.
func TestWaterloggedLeafLeavesItsWater(t *testing.T) {
	w, p := newPhysicsWorld(t)
	leaf := leafState(t, map[string]string{"distance": "7", "waterlogged": "true"})

	setBlock(t, w, p, 5, GroundLevel, 0, leaf)

	var decays []Decay
	for pass := 1; pass <= 20000 && len(decays) == 0; pass++ {
		_, more := p.RandomTick(int64(pass), []ChunkPos{{0, 0}}, 3, 1)
		decays = append(decays, more...)
	}
	if len(decays) != 1 {
		t.Fatalf("20000 random tick passes handed over %d decays, want the waterlogged leaf's", len(decays))
	}
	if got := w.BlockAt(5, GroundLevel, 0); got != waterByLevel[0] {
		t.Errorf("the decayed waterlogged leaf left state %d, want the water source %d", got, waterByLevel[0])
	}
}

// TestPersistentLeavesKeepTheirPlace checks the player's own leaves: a
// canopy placed persistent goes to distance seven with the trunk gone and
// stays there through the random ticks, while one non-persistent leaf
// among it is what the pass's dice still land on — and what the section's
// ticking count is built from, the pass skipping a section none of whose
// states tick at all.
func TestPersistentLeavesKeepTheirPlace(t *testing.T) {
	w, p := newPhysicsWorld(t)
	log := registry.BlockStates["minecraft:oak_log"]

	// The grove stands inside one chunk, the one the random tick pass
	// below is handed, and the trunk goes at the pass's centre.
	setBlock(t, w, p, 8, GroundLevel, 8, log)
	var leaves [][3]int32
	for _, c := range [][2]int32{{1, 0}, {-1, 0}, {0, 1}, {0, -1}} {
		setBlock(t, w, p, 8+c[0], GroundLevel, 8+c[1], leafState(t, map[string]string{"distance": "1", "persistent": "true"}))
		leaves = append(leaves, [3]int32{8 + c[0], GroundLevel, 8 + c[1]})
	}
	for _, c := range [][2]int32{{1, 1}, {1, -1}, {-1, 1}, {-1, -1}} {
		setBlock(t, w, p, 8+c[0], GroundLevel, 8+c[1], leafState(t, map[string]string{"distance": "2", "persistent": "true"}))
		leaves = append(leaves, [3]int32{8 + c[0], GroundLevel, 8 + c[1]})
	}

	// The trunk goes; the persistent canopy recomputes to seven and
	// stays put, the way the vanilla updateDistance keeps persistent.
	var clock int64
	setBlock(t, w, p, 8, GroundLevel, 8, airState)
	tickToQuiet(t, p, &clock, 120)
	for _, c := range leaves {
		checkLeaf(t, w, c[0], c[1], c[2], "7", "true", "after the trunk went")
	}

	// One non-persistent leaf among the persistent ones: the one state
	// the pass's dice can land on in the section.
	setBlock(t, w, p, 13, GroundLevel, 13, leafState(t, nil))

	var decays []Decay
	for pass := 1; pass <= 12000; pass++ {
		_, more := p.RandomTick(int64(pass), []ChunkPos{{0, 0}}, 3, 1)
		decays = append(decays, more...)
	}
	if len(decays) != 1 {
		t.Fatalf("12000 random tick passes handed over %d decays, want the one loose leaf's", len(decays))
	}
	if w.BlockAt(13, GroundLevel, 13) != airState {
		t.Error("the loose non-persistent leaf never rotted")
	}
	for _, c := range leaves {
		if w.BlockAt(c[0], c[1], c[2]) == airState {
			t.Errorf("persistent leaf at (%d, %d, %d) rotted", c[0], c[1], c[2])
		}
	}
}

// vanillaCanopy is one vanilla tree's canopy, read out of a vanilla world's
// region files (seed 42, the oak whose trunk column is (-45, 71..76, 7)):
// every leaf block the tree grew and the distance the vanilla server's own
// updateLeaves walk left it standing at — quirk and all, the distances one
// step past the graph distance where the walk's set order visited a leaf
// twice. The tree sits wholly inside the chunk that holds it, so a test can
// stand it on a single chunk surface.
var vanillaCanopy = []struct {
	pos  [3]int32
	want int
}{
	{[3]int32{-47, 74, 6}, 3},
	{[3]int32{-47, 74, 7}, 2},
	{[3]int32{-47, 74, 8}, 3},
	{[3]int32{-46, 74, 5}, 3},
	{[3]int32{-46, 74, 6}, 2},
	{[3]int32{-46, 74, 7}, 2},
	{[3]int32{-46, 74, 8}, 3},
	{[3]int32{-46, 74, 9}, 4},
	{[3]int32{-45, 74, 5}, 2},
	{[3]int32{-45, 74, 6}, 2},
	{[3]int32{-45, 74, 8}, 2},
	{[3]int32{-45, 74, 9}, 3},
	{[3]int32{-44, 74, 5}, 3},
	{[3]int32{-44, 74, 6}, 3},
	{[3]int32{-44, 74, 7}, 2},
	{[3]int32{-44, 74, 8}, 3},
	{[3]int32{-44, 74, 9}, 4},
	{[3]int32{-43, 74, 6}, 4},
	{[3]int32{-43, 74, 7}, 3},
	{[3]int32{-43, 74, 8}, 4},
	{[3]int32{-43, 74, 9}, 5},
	{[3]int32{-47, 75, 5}, 4},
	{[3]int32{-47, 75, 6}, 4},
	{[3]int32{-47, 75, 7}, 3},
	{[3]int32{-47, 75, 8}, 4},
	{[3]int32{-46, 75, 5}, 4},
	{[3]int32{-46, 75, 6}, 3},
	{[3]int32{-46, 75, 7}, 2},
	{[3]int32{-46, 75, 8}, 3},
	{[3]int32{-46, 75, 9}, 4},
	{[3]int32{-45, 75, 5}, 3},
	{[3]int32{-45, 75, 6}, 2},
	{[3]int32{-45, 75, 8}, 2},
	{[3]int32{-45, 75, 9}, 3},
	{[3]int32{-44, 75, 5}, 4},
	{[3]int32{-44, 75, 6}, 3},
	{[3]int32{-44, 75, 7}, 2},
	{[3]int32{-44, 75, 8}, 3},
	{[3]int32{-44, 75, 9}, 4},
	{[3]int32{-43, 75, 6}, 4},
	{[3]int32{-43, 75, 7}, 3},
	{[3]int32{-43, 75, 8}, 4},
	{[3]int32{-46, 76, 7}, 1},
	{[3]int32{-45, 76, 6}, 1},
	{[3]int32{-45, 76, 8}, 1},
	{[3]int32{-44, 76, 6}, 2},
	{[3]int32{-44, 76, 7}, 1},
	{[3]int32{-46, 77, 7}, 2},
	{[3]int32{-45, 77, 6}, 2},
	{[3]int32{-45, 77, 7}, 1},
	{[3]int32{-45, 77, 8}, 2},
	{[3]int32{-44, 77, 7}, 2},
}

// TestTheWalkMatchesTheVanillaCanopy lays the fixture tree's canopy down the
// way vanilla's own world has it — leaves at the block's default state over
// the trunk column, at the coordinates the distances were read from — and
// runs the settle walk over it, then asks every leaf for the distance the
// vanilla server left it standing at. A walk that settled for the plain
// graph distance would fail this: twenty-odd of the fixture's leaves stand
// one step further out than their shortest path, which is what the vanilla
// walk's set order visits into them. The coordinates matter — the walk's
// order is the set's, and the set's order is the positions' hashes — so the
// tree stands where the vanilla world had it, inside the chunk that held it.
func TestTheWalkMatchesTheVanillaCanopy(tb *testing.T) {
	tb.Parallel()
	g := NewOverworldGenerator(42)
	c := newChunk(-3, 0)
	r := newDecorRegion(g, c, worldMinY, worldHeight)
	r.nx, r.nz = -3, 0
	t := &treeBuild{r: r, rng: r.rng}
	log := mustState("minecraft:oak_log", nil)
	leaves := mustState("minecraft:oak_leaves", nil)
	for y := int32(71); y <= 76; y++ {
		t.put(&t.trunks, [3]int32{-45, y, 7}, log)
	}
	for _, e := range vanillaCanopy {
		t.put(&t.foliage, e.pos, leaves)
	}
	t.updateLeaves()
	for _, e := range vanillaCanopy {
		props := leafProps(tb, r.get(e.pos[0], e.pos[1], e.pos[2]))
		if props["distance"] != strconv.Itoa(e.want) {
			tb.Errorf("leaf at (%d, %d, %d) stands at distance=%s, want %d",
				e.pos[0], e.pos[1], e.pos[2], props["distance"], e.want)
		}
	}
}

// TestGeneratedTreesCarryLeafDistances checks the generator's canopy for
// shape rather than for the walk's exact distances — the walk's order is
// the positions' hashes, and a test asserting a whole canopy cell by cell
// would be a second copy of the walk — while the fixture above holds the
// exactness. What a generated canopy owes, wherever its walk's dice land:
// every leaf non-persistent, the way the vanilla feature sets them down,
// each standing at a distance the property's range holds, and a canopy
// there at all for the seeds asked.
func TestGeneratedTreesCarryLeafDistances(t *testing.T) {
	// Seeds the plain ground around the origin is forested on; a seed
	// whose origin is open field grows no trees there, and says nothing
	// about the canopy.
	t.Parallel()
	for _, seed := range []int64{2, 3, 42} {
		t.Run(strconv.FormatInt(seed, 10), func(t *testing.T) {
			t.Parallel()
			// One generator for the five-by-five, so neighbours' protos are shared.
			g := NewOverworldGenerator(seed)
			leaves := 0
			for x := int32(-2); x <= 2; x++ {
				for z := int32(-2); z <= 2; z++ {
					c := newChunk(x, z)
					g.Generate(c)
					for si := range c.sections {
						baseY := worldMinY + int32(si)*sectionSize
						blocks := c.sections[si].blocks.Load()
						if blocks == nil {
							continue // a section of one state grows no canopy
						}
						for i := range blocksPerSection {
							state := blocks.get(i)
							_, props, ok := registry.BlockStateName(state)
							if !ok || !registry.BlockOf(state).IsLeaves() {
								continue
							}
							leaves++
							p := registry.ParseStateProps(props)
							lx, ly, lz := int32(i&15), int32(i>>8), int32(i>>4&15)
							d, err := strconv.Atoi(p["distance"])
							if err != nil || d < 1 || d > 7 {
								t.Errorf("seed %d: leaf at (%d, %d, %d) in chunk (%d, %d) stands at distance=%q",
									seed, lx, baseY+ly, lz, x, z, p["distance"])
							}
							if p["persistent"] != "false" {
								t.Errorf("seed %d: leaf at (%d, %d, %d) in chunk (%d, %d) is persistent=%s",
									seed, lx, baseY+ly, lz, x, z, p["persistent"])
							}
						}
					}
				}
			}
			if leaves == 0 {
				t.Errorf("seed %d grew no leaves in the five-by-five around the origin", seed)
			}
		})
	}
}

func TestLeafDistanceFollowsPackBlockTags(t *testing.T) {
	t.Cleanup(func() { registry.ApplyPackTags(nil) })
	stone := registry.MustState("minecraft:stone", nil)
	if d := leafDistanceOf(stone); d != 7 {
		t.Fatalf("stone leaf distance = %d before any pack, want 7", d)
	}
	registry.ApplyPackTags(registry.PackTags{"minecraft:block": {
		"minecraft:oak_logs": {Entries: []registry.PackTagEntry{{ID: "minecraft:stone", Required: true}}},
	}})
	if d := leafDistanceOf(stone); d != 0 {
		t.Errorf("stone leaf distance = %d after a pack added it to oak_logs, want 0", d)
	}
}
