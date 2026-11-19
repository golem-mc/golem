package world

import (
	"math"
	"slices"
	"testing"

	"github.com/golem-mc/golem/internal/server/world/levelgen"
	"github.com/golem-mc/golem/nbt"
)

// TestNetherLocatesFortress: seed 0 puts the nearest fortress to the nether
// origin in chunk (15, 2), as the vanilla server answers.
func TestNetherLocatesFortress(t *testing.T) {
	g := NewNetherGenerator(0)
	x, z, id, ok := g.NearestStructure([]string{"minecraft:fortress"}, 0, 64, 0, 100)
	if !ok || x != 240 || z != 32 || id != "minecraft:fortress" {
		t.Errorf("fortress at (%d, %d) %q found=%v, want (240, 32)", x, z, id, ok)
	}
	// The scan's first hit is not necessarily the nearest, but it moves with the origin.
	if fx, fz, _, ok := g.NearestStructure([]string{"minecraft:fortress"}, 5000, 64, -5000, 100); !ok || (fx == 240 && fz == 32) {
		t.Errorf("fortress from far away = (%d, %d) found=%v, want another one", fx, fz, ok)
	}
	if _, _, _, ok := g.NearestStructure([]string{"minecraft:stronghold", "minecraft:village_plains"}, 0, 64, 0, 100); ok {
		t.Error("the nether placed a structure it has none of")
	}
	// Other fortresses from the vanilla server on seed 0.
	for _, c := range []struct{ fromX, fromZ, x, z int32 }{{1000, -3000, 544, -2400}, {-5000, 3000, -5152, 2672}} {
		if x, z, _, ok := g.NearestStructure([]string{"minecraft:fortress"}, c.fromX, 64, c.fromZ, 100); !ok || x != c.x || z != c.z {
			t.Errorf("fortress from (%d, %d) at (%d, %d) found=%v, want (%d, %d)", c.fromX, c.fromZ, x, z, ok, c.x, c.z)
		}
	}
	// The origin's own cell holds this fortress, so no wider scan is needed.
	if x, z, _, ok := g.NearestStructure([]string{"minecraft:fortress"}, 0, 64, 0, 0); !ok || x != 240 || z != 32 {
		t.Errorf("radius 0 found (%d, %d) found=%v, want (240, 32)", x, z, ok)
	}
	g.DisableStructures()
	if _, _, _, ok := g.NearestStructure([]string{"minecraft:fortress"}, 0, 64, 0, 100); ok {
		t.Error("generate-structures=false still found a fortress")
	}
}

// TestNetherLocatesBastionRemnant: the bastion remnant shares the fortress's
// placement and takes the starts the fortress does not, as the vanilla server
// answers on seed 0.
func TestNetherLocatesBastionRemnant(t *testing.T) {
	g := NewNetherGenerator(0)
	x, z, id, ok := g.NearestStructure([]string{"minecraft:bastion_remnant"}, 0, 64, 0, 100)
	if !ok || x != -432 || z != -160 || id != "minecraft:bastion_remnant" {
		t.Errorf("bastion remnant at (%d, %d) %q found=%v, want (-432, -160)", x, z, id, ok)
	}
	// Asking for both answers the nearer one.
	if x, z, id, ok := g.NearestStructure([]string{"minecraft:fortress", "minecraft:bastion_remnant"}, 0, 64, 0, 100); !ok || id != "minecraft:fortress" || x != 240 || z != 32 {
		t.Errorf("fortress or bastion at (%d, %d) %q found=%v, want the fortress at (240, 32)", x, z, id, ok)
	}
}

// TestOverworldLocatesStronghold: the nearest stronghold is a ring chunk in a
// stronghold biome, and its answer is the chunk's lower corner.
func TestOverworldLocatesStronghold(t *testing.T) {
	t.Parallel()
	g := NewOverworldGenerator(0)
	x, z, id, ok := g.NearestStructure([]string{"minecraft:stronghold"}, 0, 64, 0, 100)
	if !ok || id != "minecraft:stronghold" || x&15 != 0 || z&15 != 0 {
		t.Fatalf("stronghold at (%d, %d) %q found=%v", x, z, id, ok)
	}
	t.Run("vanilla answer", func(t *testing.T) {
		if x != -208 || z != -1696 {
			t.Errorf("nearest stronghold (%d, %d), want (-208, -1696)", x, z)
		}
	})
	if !slices.Contains(allStrongholdChunks(g), [2]int32{x >> 4, z >> 4}) {
		t.Errorf("(%d, %d) is not a ring chunk", x, z)
	}
	// Moving next to a ring chunk makes it the nearest.
	target := allStrongholdChunks(g)[100]
	if nx, nz, _, ok := g.NearestStructure([]string{"minecraft:stronghold"}, target[0]*16+8, 32, target[1]*16+8, 100); !ok || nx>>4 != target[0] || nz>>4 != target[1] {
		t.Errorf("standing in ring chunk %v found (%d, %d)", target, nx, nz)
	}
	if _, _, _, ok := g.NearestStructure([]string{"minecraft:fortress"}, 0, 64, 0, 100); ok {
		t.Error("the overworld placed a fortress")
	}
	g.noStructures = true
	if _, _, _, ok := g.NearestStructure([]string{"minecraft:stronghold"}, 0, 64, 0, 100); ok {
		t.Error("generate-structures=false still found a stronghold")
	}
}

// allStrongholdChunks lays every stronghold out, one square at a time.
func allStrongholdChunks(g *OverworldGenerator) [][2]int32 {
	probe := g.newBiomeIDProbe()
	return levelgen.ConcentricRingChunks(g.seed, strongholdDistance, strongholdSpread, strongholdCount,
		func(qx, qz int32) bool { return probe(qx, 0, qz).InTag(strongholdPreferredTag) })
}

// TestStrongholdSearchResolvesOnlyTheNearStarts: the nearest stronghold found
// by resolving the starts nearest first is the one the exhaustive scan of all
// 128 picks, from places around and between the rings, and most starts stay
// unresolved.
func TestStrongholdSearchResolvesOnlyTheNearStarts(t *testing.T) {
	t.Parallel()
	g := NewOverworldGenerator(12345)
	chunks := allStrongholdChunks(g)
	starts := levelgen.ConcentricRingStarts(g.seed, strongholdDistance, strongholdSpread, strongholdCount)
	for i, c := range chunks {
		dx := math.Abs(float64(c[0]-starts[i].X) * 16)
		dz := math.Abs(float64(c[1]-starts[i].Z) * 16)
		if dx > ringMoveBlocks || dz > ringMoveBlocks {
			t.Errorf("start %d moved (%.0f, %.0f) blocks, more than ringMoveBlocks %d", i, dx, dz, ringMoveBlocks)
		}
	}
	probe := g.newBiomeIDProbe()
	// A layout of its own, unresolved whatever other tests of the seed did;
	// wider batches hide a bound that is too tight.
	g.rings = &strongholdRings{seed: g.seed, batch: 1}
	exhaustive := func(x, y, z int32) (int32, int32, bool) {
		var bx, bz int32
		best, found := 0.0, false
		for _, c := range chunks {
			dx, dy, dz := float64(c[0]*16+8-x), float64(32-y), float64(c[1]*16+8-z)
			d := dx*dx + dy*dy + dz*dz
			if found && d >= best || !probe(c[0]*4, 0, c[1]*4).InTag(strongholdBiomeTag) {
				continue
			}
			bx, bz, best, found = c[0]*16, c[1]*16, d, true
		}
		return bx, bz, found
	}
	// A grid across the first rings, where the nearest starts are close to each other.
	var places [][3]int32
	for px := int32(-3200); px <= 3200; px += 400 {
		for pz := int32(-3200); pz <= 3200; pz += 400 {
			places = append(places, [3]int32{px, 64, pz})
		}
	}
	for _, p := range places {
		wx, wz, wok := exhaustive(p[0], p[1], p[2])
		gx, gz, _, gok := g.NearestStructure([]string{strongholdID}, p[0], p[1], p[2], 0)
		if gx != wx || gz != wz || gok != wok {
			t.Errorf("from %v: found (%d, %d) %v, exhaustive scan (%d, %d) %v", p, gx, gz, gok, wx, wz, wok)
		}
	}
	resolved := 0
	for _, done := range g.rings.resolved {
		if done {
			resolved++
		}
	}
	if resolved == 0 || resolved > strongholdCount/2 {
		t.Errorf("%d of %d starts resolved", resolved, strongholdCount)
	}
}

// TestStrongholdRingLayout: 128 chunks in rings of 3, 6, 10, ... around the
// origin, the first ring 128 +- 40 chunks out before a start moves to a
// preferred biome within 7 chunks.
// vanilla: ChunkGeneratorStructureState.generateRingPositions
func TestStrongholdRingLayout(t *testing.T) {
	chunks := allStrongholdChunks(NewOverworldGenerator(0))
	if len(chunks) != 128 {
		t.Fatalf("%d ring chunks, want 128", len(chunks))
	}
	for i, c := range chunks[:3] {
		d := math.Hypot(float64(c[0]), float64(c[1]))
		if d < 80 || d > 176 {
			t.Errorf("first-ring chunk %d at %v is %.0f chunks out", i, c, d)
		}
	}
}

func BenchmarkOverworldLocateStronghold(b *testing.B) {
	for b.Loop() {
		g := NewOverworldGenerator(0)
		g.NearestStructure([]string{"minecraft:stronghold"}, 0, 64, 0, 100)
	}
}

// BenchmarkNetherLocateFortressWorstCase scans all 100 rings of cells, as a
// search that finds no fortress does.
func BenchmarkNetherLocateFortressWorstCase(b *testing.B) {
	g := NewNetherGenerator(0)
	for b.Loop() {
		levelgen.NearestRandomSpread(0, levelgen.NetherComplexSpacing, levelgen.NetherComplexSeparation,
			levelgen.NetherComplexSalt, 0, 0, 100, func(cx, cz int32) bool { g.complexIsFortress(cx, cz); return false })
	}
}

// TestStrongholdRingTestResolvesOnlyNearStarts: the ring test of a chunk
// resolves only the starts that can move to it, and finds each ring chunk.
// Six starts keep it at a fraction of a second; all 128 take seconds.
func TestStrongholdRingTestResolvesOnlyNearStarts(t *testing.T) {
	t.Parallel()
	const seed = 4653
	g := NewOverworldGenerator(seed)
	// A layout of its own, unresolved whatever other tests of the seed did.
	g.rings = &strongholdRings{seed: g.seed, batch: ringBatch}
	resolvedCount := func() int {
		n := 0
		for _, done := range g.rings.resolved {
			if done {
				n++
			}
		}
		return n
	}
	if g.inStrongholdRing(0, 0) {
		t.Error("chunk (0, 0) is a ring chunk")
	}
	if n := resolvedCount(); n != 0 {
		t.Errorf("the ring test of chunk (0, 0) resolved %d starts", n)
	}
	probe := g.newBiomeIDProbe()
	preferred := func(qx, qz int32) bool { return probe(qx, 0, qz).InTag(strongholdPreferredTag) }
	starts := levelgen.ConcentricRingStarts(seed, strongholdDistance, strongholdSpread, strongholdCount)[:6]
	want := make([][2]int32, len(starts))
	for i, s := range starts {
		want[i] = s.Resolve(preferred)
		if !g.inStrongholdRing(want[i][0], want[i][1]) {
			t.Errorf("start %d's chunk %v is not a ring chunk", i, want[i])
		}
	}
	if n := resolvedCount(); n >= 16 {
		t.Errorf("the ring tests of %d chunks resolved %d starts", len(starts), n)
	}
	for i, c := range want {
		if !g.rings.resolved[i] || g.rings.chunks[i] != c {
			t.Errorf("start %d resolved %v to %v, want %v", i, g.rings.resolved[i], g.rings.chunks[i], c)
		}
	}
}

// BenchmarkStrongholdRingTestNewSeed is the ring test of the first chunk of a
// seed not seen before: a fresh layout each iteration, as sharedRings holds
// for a new seed. The generator is built once, since building one costs about
// 15 ms of feature compiling that has nothing to do with the rings.
func BenchmarkStrongholdRingTestNewSeed(b *testing.B) {
	g := NewOverworldGenerator(1 << 40)
	for b.Loop() {
		g.rings = &strongholdRings{seed: g.seed, batch: ringBatch}
		g.inStrongholdRing(0, 0)
	}
}

// TestNetherLocatesRuinedPortalsAndFossils: the two structures the nether
// places besides the complexes are located, and the chunk answered starts them.
func TestNetherLocatesRuinedPortalsAndFossils(t *testing.T) {
	g := NewNetherGenerator(0)
	check := func(id string, x, z int32) {
		t.Helper()
		if x%16 != 0 || z%16 != 0 {
			t.Fatalf("%s at (%d, %d) is not a chunk corner", id, x, z)
		}
		c := newChunk(x>>4, z>>4)
		g.saveStructureStarts(c)
		structures, _ := nbt.Get[nbt.Compound](c.savedRoot(), "structures")
		starts, _ := nbt.Get[nbt.Compound](structures, "starts")
		tag, ok := starts[id].(nbt.Compound)
		if !ok {
			t.Fatalf("chunk (%d, %d) has no %s start", x>>4, z>>4, id)
		}
		if got, _ := nbt.Get[nbt.String](tag, "id"); string(got) != id {
			t.Errorf("start id %q, want %q", got, id)
		}
	}
	x, z, got, ok := g.NearestStructure([]string{ruinedPortalID}, 0, 64, 0, 100)
	if !ok || got != ruinedPortalID {
		t.Fatalf("ruined portal found=%v id %q", ok, got)
	}
	check(ruinedPortalID, x, z)
	home := g.ruinedPortalInCell(0, 0)
	if px, pz, _, ok := g.NearestStructure([]string{ruinedPortalID}, 0, 64, 0, 0); !ok || px != home.chunkX*16 || pz != home.chunkZ*16 {
		t.Errorf("radius 0 portal (%d, %d) found=%v", px, pz, ok)
	}
	found := false
	for _, o := range [][2]int32{{0, 0}, {2000, 0}, {0, 2000}, {-2000, 0}, {0, -2000}} {
		fx, fz, got, ok := g.NearestStructure([]string{netherFossilID}, o[0], 64, o[1], 100)
		if !ok {
			continue
		}
		if got != netherFossilID {
			t.Fatalf("fossil search answered %q", got)
		}
		check(netherFossilID, fx, fz)
		found = true
		break
	}
	if !found {
		t.Error("no origin found a nether fossil")
	}
	g.DisableStructures()
	for _, id := range []string{ruinedPortalID, netherFossilID} {
		if _, _, _, ok := g.NearestStructure([]string{id}, 0, 64, 0, 100); ok {
			t.Errorf("generate-structures=false still found %s", id)
		}
	}
}
