package main

import (
	"bytes"
	"encoding/json"
	"io"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

func state(t *testing.T, name string, props map[string]string) uint16 {
	t.Helper()
	s, ok := registry.FindState(name, props)
	if !ok {
		t.Fatalf("no state %s", name)
	}
	return s
}

var overworld = dimensions["overworld"]

// filled is an overworld chunk of stone up to y=0 and air above it, all
// plains.
func filled(t *testing.T, x, z int32) *chunkData {
	stone := state(t, "minecraft:stone", nil)
	c := newChunkData(overworld, x, z)
	for y := overworld.MinY; y <= 0; y++ {
		for i := range columnBlocks {
			c.Blocks[c.index(i%16, y, i/16)] = stone
		}
	}
	plains := biomeID("minecraft:plains")
	for i := range c.Biomes {
		c.Biomes[i] = plains
	}
	return c
}

func TestCompareIdenticalChunks(t *testing.T) {
	r := chunkRange{0, 0, 1, 0}
	v := map[[2]int32]*chunkData{{0, 0}: filled(t, 0, 0), {1, 0}: filled(t, 1, 0)}
	o := map[[2]int32]*chunkData{{0, 0}: filled(t, 0, 0), {1, 0}: filled(t, 1, 0)}
	var diff bytes.Buffer
	rep, err := compare(overworld, 1, r, v, o, &diff)
	if err != nil {
		t.Fatal(err)
	}
	if diff.Len() != 0 {
		t.Fatalf("identical chunks wrote a diff: %q", diff.String())
	}
	rep.rankFirst(nil, 5)
	if rep.FirstDiffering != nil {
		t.Fatal("ranked chunks without a recording")
	}
	for name, got := range map[string]ratio{"exact": rep.Exact, "non-air": rep.NonAir, "shape": rep.Shape, "biome": rep.Biome, "surface": rep.Surface} {
		if got.Match != got.Total || got.Total == 0 {
			t.Errorf("%s = %d/%d, want a full match", name, got.Match, got.Total)
		}
	}
	if rep.SurfaceDiff != 0 || len(rep.Confusions) != 0 {
		t.Errorf("surface diff %v, confusions %v; want none", rep.SurfaceDiff, rep.Confusions)
	}
}

func TestCompareCountsEachStage(t *testing.T) {
	v, o := filled(t, 0, 0), filled(t, 0, 0)
	deepslate := state(t, "minecraft:deepslate", nil)
	water := state(t, "minecraft:water", nil)
	// One block of another solid: an exact miss but a shape match.
	o.Blocks[o.index(0, -64, 0)] = deepslate
	// One column raised by three blocks of stone: misses on both, and the
	// surface three blocks off.
	stone := state(t, "minecraft:stone", nil)
	for y := 1; y <= 3; y++ {
		o.Blocks[o.index(5, y, 5)] = stone
	}
	// A waterlogged block where vanilla has water: a shape match.
	o.Blocks[o.index(9, 0, 9)] = water
	v.Blocks[v.index(9, 0, 9)] = state(t, "minecraft:oak_stairs", map[string]string{"waterlogged": "true"})
	// Two biome cells off.
	o.Biomes[0] = biomeID("minecraft:desert")
	o.Biomes[cellIndex(3, 95, 3)] = biomeID("minecraft:desert")

	chunkBlocks, chunkCells := overworld.blocks(), overworld.cells()
	rep, err := compare(overworld, 0, chunkRange{0, 0, 0, 0}, map[[2]int32]*chunkData{{0, 0}: v}, map[[2]int32]*chunkData{{0, 0}: o}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if want := chunkBlocks - 5; rep.Exact.Match != want || rep.Exact.Total != chunkBlocks {
		t.Errorf("exact = %d/%d, want %d/%d", rep.Exact.Match, rep.Exact.Total, want, chunkBlocks)
	}
	if want := chunkBlocks - 3; rep.Shape.Match != want {
		t.Errorf("shape = %d, want %d", rep.Shape.Match, want)
	}
	// Non-air covers the 65 stone layers plus the three raised blocks.
	if want := 65*columnBlocks + 3; rep.NonAir.Total != want || rep.NonAir.Match != want-5 {
		t.Errorf("non-air = %d/%d, want %d/%d", rep.NonAir.Match, rep.NonAir.Total, want-5, want)
	}
	if rep.Biome.Match != chunkCells-2 || rep.Biome.Total != chunkCells {
		t.Errorf("biome = %d/%d, want %d/%d", rep.Biome.Match, rep.Biome.Total, chunkCells-2, chunkCells)
	}
	if rep.Surface.Match != columnBlocks-1 || rep.SurfaceDiff != 3.0/columnBlocks {
		t.Errorf("surface = %d, diff %v; want %d, %v", rep.Surface.Match, rep.SurfaceDiff, columnBlocks-1, 3.0/columnBlocks)
	}
	if s := rep.SectionY[0]; s.Exact.Match != 4096-1 || s.Exact.Total != 4096 {
		t.Errorf("bottom section exact = %d/%d, want 4095/4096", s.Exact.Match, s.Exact.Total)
	}
	want := []confusion{
		{"minecraft:air", "minecraft:stone", 3},
		{"minecraft:oak_stairs", "minecraft:water", 1},
		{"minecraft:stone", "minecraft:deepslate", 1},
	}
	if len(rep.Confusions) != len(want) {
		t.Fatalf("confusions = %v, want %v", rep.Confusions, want)
	}
	for i := range want {
		if rep.Confusions[i] != want[i] {
			t.Errorf("confusion %d = %v, want %v", i, rep.Confusions[i], want[i])
		}
	}

	var buf bytes.Buffer
	rep.writeText(&buf, 5)
	if !strings.Contains(buf.String(), "air -> stone") {
		t.Errorf("text report lacks the top mismatch:\n%s", buf.String())
	}
}

func TestCompareMissingChunk(t *testing.T) {
	_, err := compare(overworld, 0, chunkRange{0, 0, 1, 0},
		map[[2]int32]*chunkData{{0, 0}: filled(t, 0, 0)},
		map[[2]int32]*chunkData{{0, 0}: filled(t, 0, 0), {1, 0}: filled(t, 1, 0)}, nil)
	if err == nil {
		t.Fatal("a chunk missing on the vanilla side compared without error")
	}
}

func TestClassOf(t *testing.T) {
	for name, want := range map[string]blockClass{
		"minecraft:air":           classAir,
		"minecraft:cave_air":      classAir,
		"minecraft:water":         classFluid,
		"minecraft:lava":          classFluid,
		"minecraft:kelp_plant":    classFluid,
		"minecraft:stone":         classSolid,
		"minecraft:short_grass":   classSolid,
		"minecraft:deepslate":     classSolid,
		"minecraft:bubble_column": classFluid,
	} {
		if got := classOf(state(t, name, nil)); got != want {
			t.Errorf("classOf(%s) = %d, want %d", name, got, want)
		}
	}
	if got := classOf(unknownState); got != classSolid {
		t.Errorf("classOf(unknown) = %d, want solid", got)
	}
}

func TestChunkRange(t *testing.T) {
	r := chunkRange{-20, -3, 13, 4}
	if got := r.count(); got != 34*8 {
		t.Errorf("count = %d, want %d", got, 34*8)
	}
	n := 0
	for _, tile := range r.tiles(16) {
		if !r.covers(tile) || tile.count() > 256 {
			t.Errorf("tile %v is outside the range or over 256 chunks", tile)
		}
		n += tile.count()
	}
	if n != r.count() {
		t.Errorf("tiles cover %d chunks, want %d", n, r.count())
	}
	if r.covers(chunkRange{-21, 0, 0, 0}) || !r.covers(chunkRange{-20, -3, -20, -3}) {
		t.Error("covers is wrong at the edges")
	}
}

func TestTerrainTopIgnoresWhatDressesTheTerrain(t *testing.T) {
	c := filled(t, 0, 0)
	// Dirt and grass on the stone, then a tree, a flower, a snow layer and
	// water above it: the terrain top is the grass; the surface is the leaves.
	for y, name := range []string{"minecraft:dirt", "minecraft:grass_block", "minecraft:oak_log", "minecraft:oak_leaves", "minecraft:snow", "minecraft:water"} {
		c.Blocks[c.index(2, y+1, 3)] = state(t, name, nil)
	}
	if got := terrainTopY(c, 2, 3); got != 2 {
		t.Errorf("terrain top = %d, want 2 (the grass)", got)
	}
	if got := surfaceY(c, 2, 3); got != 6 {
		t.Errorf("surface = %d, want 6", got)
	}
	if got := terrainTopY(c, 0, 0); got != 0 {
		t.Errorf("terrain top of bare stone = %d, want 0", got)
	}
}

func TestCompareDiffAndDecorationOrder(t *testing.T) {
	r := chunkRange{-1, -2, 0, -2}
	v0, o0 := filled(t, -1, -2), filled(t, -1, -2)
	v1, o1 := filled(t, 0, -2), filled(t, 0, -2)
	v := map[[2]int32]*chunkData{{-1, -2}: v0, {0, -2}: v1}
	o := map[[2]int32]*chunkData{{-1, -2}: o0, {0, -2}: o1}
	leaf := state(t, "minecraft:leaf_litter", map[string]string{"facing": "west", "segment_amount": "2"})
	o0.Blocks[o0.index(15, -64, 0)] = leaf
	o0.Blocks[o0.index(0, -63, 1)] = state(t, "minecraft:air", nil)
	v1.Blocks[v1.index(1, 1, 2)] = state(t, "minecraft:bubble_column", map[string]string{"drag": "true"})
	o1.Blocks[o1.index(1, 1, 2)] = state(t, "minecraft:water", map[string]string{"level": "0"})
	o1.Blocks[o1.index(2, 1, 2)] = leaf
	var diff bytes.Buffer
	rep, err := compare(overworld, 0, r, v, o, &diff)
	if err != nil {
		t.Fatal(err)
	}
	want := "-1 -64 -32 stone leaf_litter,facing=west,segment_amount=2\n-16 -63 -31 stone air\n1 1 -30 bubble_column,drag=true water,level=0\n2 1 -30 air leaf_litter,facing=west,segment_amount=2\n"
	if diff.String() != want {
		t.Fatalf("diff = %q, want %q", diff.String(), want)
	}
	order := [][2]int32{{50, 50}, {0, -2}, {-1, -2}}
	rep.rankFirst(order, 2)
	if len(rep.FirstDiffering) != 2 || rep.FirstDiffering[0].X != 0 || rep.FirstDiffering[0].OrderIndex != 1 || rep.FirstDiffering[0].DifferingBlocks != 1 || rep.FirstDiffering[0].BubbleColumnWater != 1 || rep.FirstDiffering[1].DifferingBlocks != 2 {
		t.Fatalf("first differing = %+v", rep.FirstDiffering)
	}
	if len(rep.FirstDiffering[0].Pairs) != 1 || rep.FirstDiffering[0].Pairs[0].Vanilla != "minecraft:air" {
		t.Fatalf("pairs = %v", rep.FirstDiffering[0].Pairs)
	}
	var text bytes.Buffer
	rep.writeText(&text, 2)
	if !strings.Contains(text.String(), "first differing chunks in decoration order") || !strings.Contains(text.String(), "0,-2  1  1  air -> leaf_litter (1)  bubble_column -> water: 1  fluid-tick residue: 0  post-process age: 0\n  -1,-2  2  2") {
		t.Fatalf("missing order section: %s", text.String())
	}
	data, err := json.Marshal(rep)
	if err != nil {
		t.Fatal(err)
	}
	var decoded report
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(decoded.FirstDiffering, rep.FirstDiffering) {
		t.Fatalf("JSON first_differing = %+v", decoded.FirstDiffering)
	}
	rep.rankFirst(order, 1)
	if len(rep.FirstDiffering) != 1 {
		t.Fatal("top did not limit first_differing")
	}
	// A chunk with only reference scheduled ticks cannot lead the residual ranking.
	o1.Blocks[o1.index(2, 1, 2)] = state(t, "minecraft:air", nil)
	rep, err = compare(overworld, 0, r, v, o, nil)
	if err != nil {
		t.Fatal(err)
	}
	rep.rankFirst(order, 2)
	if len(rep.FirstDiffering) != 1 || rep.FirstDiffering[0].X != -1 {
		t.Fatalf("bubble-only chunk ranked: %+v", rep.FirstDiffering)
	}
}

func TestCompareDiffWriteError(t *testing.T) {
	v, o := filled(t, 0, 0), filled(t, 0, 0)
	o.Blocks[0] = state(t, "minecraft:air", nil)
	_, err := compare(overworld, 0, chunkRange{0, 0, 0, 0}, map[[2]int32]*chunkData{{0, 0}: v}, map[[2]int32]*chunkData{{0, 0}: o}, failingDiffWriter{})
	if err == nil || !strings.Contains(err.Error(), "write block diff") {
		t.Fatalf("error = %v", err)
	}
}

type failingDiffWriter struct{}

func (failingDiffWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }

func TestCompareFluidTickResidue(t *testing.T) {
	water := state(t, "minecraft:water", map[string]string{"level": "1"})
	lava := state(t, "minecraft:lava", map[string]string{"level": "8"})
	air := state(t, "minecraft:air", nil)
	cases := []struct {
		name   string
		v, o   uint16
		offset [3]int32
		want   bool
	}{
		{"at cell", water, air, [3]int32{}, true},
		{"across negative boundary", water, air, [3]int32{1, 0, 0}, true},
		{"west", water, air, [3]int32{-1, 0, 0}, true},
		{"above", water, air, [3]int32{0, 1, 0}, true},
		{"below", water, air, [3]int32{0, -1, 0}, true},
		{"north", water, air, [3]int32{0, 0, -1}, true},
		{"south", water, air, [3]int32{0, 0, 1}, true},
		{"distance two", water, air, [3]int32{2, 0, 0}, false},
		{"diagonal", water, air, [3]int32{1, 0, 1}, false},
		{"cave air", water, state(t, "minecraft:cave_air", nil), [3]int32{}, true},
		{"water level", water, state(t, "minecraft:water", map[string]string{"level": "0"}), [3]int32{}, true},
		{"water to lava", water, lava, [3]int32{}, true},
		{"lava to water", lava, water, [3]int32{}, true},
		{"lava to air", lava, air, [3]int32{}, true},
		{"plant", water, state(t, "minecraft:short_grass", nil), [3]int32{}, true},
		{"cave vine", water, state(t, "minecraft:cave_vines_plant", map[string]string{"berries": "false"}), [3]int32{1, 0, 0}, true},
		{"obsidian", state(t, "minecraft:obsidian", nil), lava, [3]int32{}, true},
		{"cobblestone", state(t, "minecraft:cobblestone", nil), lava, [3]int32{}, true},
		{"solid beside tick", water, state(t, "minecraft:stone", nil), [3]int32{1, 0, 0}, false},
		{"nonfluid tick", state(t, "minecraft:stone", nil), air, [3]int32{}, false},
		{"reverse", air, water, [3]int32{}, false},
		{"void air", water, state(t, "minecraft:void_air", nil), [3]int32{}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			v0, o0 := filled(t, -2, -2), filled(t, -2, -2)
			v1, o1 := filled(t, -1, -2), filled(t, -1, -2)
			v0.Blocks[v0.index(15, -60, 8)] = tc.v
			o0.Blocks[o0.index(15, -60, 8)] = tc.o
			v1.Blocks[v1.index(1, 1, 2)] = state(t, "minecraft:bubble_column", map[string]string{"drag": "true"})
			o1.Blocks[o1.index(1, 1, 2)] = state(t, "minecraft:water", map[string]string{"level": "0"})
			pos := [3]int32{-17 + tc.offset[0], -60 + tc.offset[1], -24 + tc.offset[2]}
			// Supply the boundary tick through the neighbouring chunk's snapshot.
			owner := o0
			if pos[0] >= -16 {
				owner = o1
			}
			owner.Ticks = map[[3]int32]bool{pos: true}
			var diff bytes.Buffer
			rep, err := compare(overworld, 0, chunkRange{-2, -2, -1, -2},
				map[[2]int32]*chunkData{{-2, -2}: v0, {-1, -2}: v1},
				map[[2]int32]*chunkData{{-2, -2}: o0, {-1, -2}: o1}, &diff)
			if err != nil {
				t.Fatal(err)
			}
			wantResidue, wantDiff := 0, 1
			if tc.want {
				wantResidue, wantDiff = 1, 0
			}
			c := rep.Chunks[0]
			if c.FluidTickResidue != wantResidue || c.DifferingBlocks != wantDiff || len(c.Pairs) != wantDiff {
				t.Fatalf("chunk counts/pairs = %+v", c)
			}
			wantLine := "-17 -60 -24 " + stateText(tc.v) + " " + stateText(tc.o) + "\n-15 1 -30 bubble_column,drag=true water,level=0\n"
			if diff.String() != wantLine || len(rep.Confusions) != 2 || rep.Confusions[0].Count != 1 || rep.Confusions[1].Count != 1 || rep.Chunks[1].BubbleColumnWater != 1 ||
				rep.Exact.Total-rep.Exact.Match != 2 || c.Exact.Total-c.Exact.Match != 1 ||
				rep.NonAir.Total-rep.NonAir.Match != 2 || rep.SectionY[0].Exact.Total-rep.SectionY[0].Exact.Match != 1 {
				t.Fatalf("raw diff/global/exact retention: diff %q, report %+v", diff.String(), rep)
			}
			order := [][2]int32{{-2, -2}, {-1, -2}}
			rep.rankFirst(order, 2)
			if len(rep.FirstDiffering) != wantDiff {
				t.Fatalf("residue-only ranking: %+v", rep.FirstDiffering)
			}
			// Retain one ordinary difference so the classified count is also reported in ranking.
			o0.Blocks[o0.index(0, -63, 1)] = air
			rep, err = compare(overworld, 0, chunkRange{-2, -2, -1, -2},
				map[[2]int32]*chunkData{{-2, -2}: v0, {-1, -2}: v1},
				map[[2]int32]*chunkData{{-2, -2}: o0, {-1, -2}: o1}, nil)
			if err != nil {
				t.Fatal(err)
			}
			rep.rankFirst(order, 2)
			if len(rep.FirstDiffering) != 1 || rep.FirstDiffering[0].FluidTickResidue != wantResidue {
				t.Fatalf("ranking count: %+v", rep.FirstDiffering)
			}
			var text bytes.Buffer
			rep.writeText(&text, 2)
			suffix := "bubble_column -> water: 0  fluid-tick residue: 0  post-process age: 0\n"
			if tc.want {
				suffix = "bubble_column -> water: 0  fluid-tick residue: 1  post-process age: 0\n"
			}
			if !strings.Contains(text.String(), suffix) || !strings.Contains(text.String(), "water, fluid-tick residue and post-process age counted separately") {
				t.Fatalf("text fields missing: %s", text.String())
			}
			data, err := json.Marshal(rep)
			if err != nil {
				t.Fatal(err)
			}
			var decoded report
			if err := json.Unmarshal(data, &decoded); err != nil {
				t.Fatal(err)
			}
			if !bytes.Contains(data, []byte(`"fluid_tick_residue":`)) ||
				!reflect.DeepEqual(decoded.FirstDiffering, rep.FirstDiffering) || !reflect.DeepEqual(decoded.Chunks, rep.Chunks) {
				t.Fatalf("JSON fields lost: %s", data)
			}
		})
	}
}

// TestCompareVineTipResidue: flowing water that washes out a cave vine
// beside a generated tick turns the vine above it into a tip; both count as
// fluid-tick residue, and the tip does not without residue below it.
func TestCompareVineTipResidue(t *testing.T) {
	water := state(t, "minecraft:water", map[string]string{"level": "1"})
	body := state(t, "minecraft:cave_vines_plant", map[string]string{"berries": "false"})
	tip := state(t, "minecraft:cave_vines", map[string]string{"age": "4", "berries": "false"})
	for _, tc := range []struct {
		name         string
		below        uint16
		tick         bool
		residue, dif int
	}{
		{"washed out beside tick", water, true, 2, 0},
		{"washed out without tick", water, false, 0, 2},
		{"vine below in both", body, true, 0, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v, o := filled(t, 0, 0), filled(t, 0, 0)
			v.Blocks[v.index(3, 17, 4)], o.Blocks[o.index(3, 17, 4)] = tip, body
			v.Blocks[v.index(3, 16, 4)], o.Blocks[o.index(3, 16, 4)] = tc.below, body
			if tc.tick {
				o.Ticks = map[[3]int32]bool{{4, 16, 4}: true}
			}
			rep, err := compare(overworld, 0, chunkRange{0, 0, 0, 0}, map[[2]int32]*chunkData{{0, 0}: v}, map[[2]int32]*chunkData{{0, 0}: o}, nil)
			if err != nil {
				t.Fatal(err)
			}
			if c := rep.Chunks[0]; c.FluidTickResidue != tc.residue || c.DifferingBlocks != tc.dif {
				t.Fatalf("residue %d differing %d, want %d %d", c.FluidTickResidue, c.DifferingBlocks, tc.residue, tc.dif)
			}
		})
	}
}

func TestCompareBlockEntities(t *testing.T) {
	v, o := filled(t, 0, 0), filled(t, 0, 0)
	chest := func(x int32, seed int64) nbt.Compound {
		return nbt.Compound{"id": nbt.String("minecraft:chest"), "x": nbt.Int(x), "y": nbt.Int(5), "z": nbt.Int(1),
			"LootTable": nbt.String("minecraft:chests/simple_dungeon"), "LootTableSeed": nbt.Long(seed)}
	}
	spawner := nbt.Compound{"id": nbt.String("minecraft:mob_spawner"), "x": nbt.Int(9), "y": nbt.Int(-20), "z": nbt.Int(9)}
	v.addBlockEntities([]nbt.Compound{chest(1, 7), chest(2, 8), spawner})
	ownChest := chest(1, 7)
	ownChest["keepPacked"] = nbt.Byte(0)
	o.addBlockEntities([]nbt.Compound{ownChest, chest(2, 9)})
	var diff bytes.Buffer
	rep, err := compare(overworld, 1, chunkRange{0, 0, 0, 0}, map[[2]int32]*chunkData{{0, 0}: v}, map[[2]int32]*chunkData{{0, 0}: o}, &diff)
	if err != nil {
		t.Fatal(err)
	}
	be := rep.BlockEntities
	if be.Vanilla != 3 || be.Ours != 2 || be.SameID != 2 || be.Identical != 1 {
		t.Errorf("counts %d / %d / %d / %d, want 3 / 2 / 2 / 1", be.Vanilla, be.Ours, be.SameID, be.Identical)
	}
	want := []blockEntityPair{{"minecraft:chest", "LootTableSeed", 1}, {"minecraft:mob_spawner", "<only vanilla>", 1}}
	if !reflect.DeepEqual(be.Pairs, want) {
		t.Errorf("pairs %v, want %v", be.Pairs, want)
	}
	if got, want := diff.String(), "be 2 5 1 minecraft:chest LootTableSeed\nbe 9 -20 9 minecraft:mob_spawner <only vanilla>\n"; got != want {
		t.Errorf("diff %q, want %q", got, want)
	}
	var text strings.Builder
	rep.writeText(&text, 5)
	if !strings.Contains(text.String(), "block entities (vanilla / ours / same id / identical): 3 / 2 / 2 / 1") {
		t.Errorf("text lacks the block entity section:\n%s", text.String())
	}
}

// mob is a saved entity of the kind at the position, facing yaw.
func mob(id string, x, y, z float64, yaw float32) nbt.Compound {
	return nbt.Compound{
		"id":       nbt.String("minecraft:" + id),
		"Pos":      nbt.List{Elem: nbt.TypeDouble, Items: []nbt.Tag{nbt.Double(x), nbt.Double(y), nbt.Double(z)}},
		"Rotation": nbt.List{Elem: nbt.TypeFloat, Items: []nbt.Tag{nbt.Float(yaw), nbt.Float(0)}},
	}
}

func TestPairEntitiesNearestWithinOneBlock(t *testing.T) {
	v := sortedEntities([]nbt.Compound{mob("sheep", 5, 64, 5, 0), mob("sheep", 1, 64, 1, 0), mob("cow", 1, 64, 1, 0)})
	o := sortedEntities([]nbt.Compound{
		mob("sheep", 1.9, 64, 1, 0), // the nearer of the two for the sheep at 1,64,1
		mob("sheep", 1, 64, 2, 0),   // exactly 1.0 away, but farther
		mob("cow", 2.5, 64, 1, 0),   // too far for the cow
		mob("sheep", 5, 64, 5.5, 0), // ties with the next for the sheep at 5,64,5
		mob("sheep", 5, 64, 4.5, 0),
	})
	// o sorts as cow 2.5; sheep 1,64,2; sheep 1.9; sheep 5,64,4.5; sheep 5,64,5.5.
	if got, want := pairEntities(v, o), []int{-1, 2, 3}; !reflect.DeepEqual(got, want) {
		t.Errorf("pairs %v, want %v", got, want)
	}
}

func TestCompareEntitiesFluidDrops(t *testing.T) {
	var rep entityReport
	pairs := map[[2]string]int{}
	v := []nbt.Compound{mob("item", 2, 64, 2, 0), mob("sheep", 3, 64, 3, 0)}
	if err := rep.addChunk(v, []nbt.Compound{mob("sheep", 3, 64, 3, 0)}, pairs, nil); err != nil {
		t.Fatal(err)
	}
	if rep.Paired != 1 || rep.Identical != 1 || rep.FluidDrops != 1 || len(pairs) != 0 {
		t.Errorf("paired %d, identical %d, drops %d, pairs %v; want 1, 1, 1, none", rep.Paired, rep.Identical, rep.FluidDrops, pairs)
	}
	rep.Recorded = true
	var text strings.Builder
	rep.writeText(&text, 5)
	if !strings.Contains(text.String(), "fluid-flow item drops: 1") {
		t.Errorf("text lacks the drop count:\n%s", text.String())
	}
}

func TestCompareEntities(t *testing.T) {
	v, o := filled(t, 0, 0), filled(t, 0, 0)
	v.EntitiesRecorded, o.EntitiesRecorded = true, true
	sheep := mob("sheep", 3, 64, 3, 90)
	sheep["Color"] = nbt.Byte(0)
	sheep["UUID"] = nbt.IntArray{1, 2, 3, 4}
	sheep["Motion"] = nbt.List{Elem: nbt.TypeDouble, Items: []nbt.Tag{nbt.Double(0), nbt.Double(0), nbt.Double(0)}}
	ourSheep := mob("sheep", 3, 64, 3, 90)
	ourSheep["Color"] = nbt.Byte(0)
	ourSheep["UUID"] = nbt.IntArray{5, 6, 7, 8}
	cow, ourCow := mob("cow", 8, 64, 8, 10), mob("cow", 8.25, 64, 8, 20)
	cow["Age"], ourCow["Age"] = nbt.Int(0), nbt.Int(-24000)
	jockey, ourJockey := mob("spider", 12, 40, 12, 0), mob("spider", 12, 40, 12, 0)
	jockey["Passengers"] = nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{nbt.Compound{"id": nbt.String("minecraft:skeleton"), "Health": nbt.Float(20)}}}
	ourJockey["Passengers"] = nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{nbt.Compound{"id": nbt.String("minecraft:skeleton"), "Health": nbt.Float(19)}}}
	v.Entities = []nbt.Compound{sheep, cow, jockey, mob("pig", 1, 64, 1, 0)}
	o.Entities = []nbt.Compound{ourCow, ourSheep, ourJockey, mob("chicken", 1, 64, 1, 0)}
	var diff bytes.Buffer
	rep, err := compare(overworld, 1, chunkRange{0, 0, 0, 0}, map[[2]int32]*chunkData{{0, 0}: v}, map[[2]int32]*chunkData{{0, 0}: o}, &diff)
	if err != nil {
		t.Fatal(err)
	}
	e := rep.Entities
	if !e.Recorded || e.Vanilla != 4 || e.Ours != 4 || e.Paired != 3 || e.Identical != 2 {
		t.Errorf("recorded %t, counts %d / %d / %d / %d, want true, 4 / 4 / 3 / 2", e.Recorded, e.Vanilla, e.Ours, e.Paired, e.Identical)
	}
	want := []blockEntityPair{
		{"minecraft:chicken", "<only ours>", 1},
		{"minecraft:cow", "Age", 1},
		{"minecraft:cow", "Pos", 1},
		{"minecraft:cow", "Rotation", 1},
		{"minecraft:pig", "<only vanilla>", 1},
	}
	if !reflect.DeepEqual(e.Pairs, want) {
		t.Errorf("pairs %v, want %v", e.Pairs, want)
	}
	if got, want := diff.String(), "entity 8 64 8 minecraft:cow Age\nentity 8 64 8 minecraft:cow Pos\nentity 8 64 8 minecraft:cow Rotation\nentity 1 64 1 minecraft:pig <only vanilla>\nentity 1 64 1 minecraft:chicken <only ours>\n"; got != want {
		t.Errorf("diff %q, want %q", got, want)
	}
	var text strings.Builder
	rep.writeText(&text, 5)
	if !strings.Contains(text.String(), "entities (vanilla / ours / paired / identical): 4 / 4 / 3 / 2") {
		t.Errorf("text lacks the entity section:\n%s", text.String())
	}

	v.EntitiesRecorded = false
	if rep, err = compare(overworld, 1, chunkRange{0, 0, 0, 0}, map[[2]int32]*chunkData{{0, 0}: v}, map[[2]int32]*chunkData{{0, 0}: o}, nil); err != nil {
		t.Fatal(err)
	}
	text.Reset()
	rep.writeText(&text, 5)
	if rep.Entities.Recorded || rep.Entities.Vanilla != 0 || !strings.Contains(text.String(), "entities: not recorded (rerun with -record-entities)") {
		t.Errorf("unrecorded report %+v, text:\n%s", rep.Entities, text.String())
	}
}

// attr is a saved attribute with the base and modifiers, each modifier given
// as id and amount.
func attr(id string, base float64, mods ...any) nbt.Compound {
	c := nbt.Compound{"id": nbt.String("minecraft:" + id), "base": nbt.Double(base)}
	if len(mods) > 0 {
		list := nbt.List{Elem: nbt.TypeCompound}
		for i := 0; i < len(mods); i += 2 {
			list.Items = append(list.Items, nbt.Compound{
				"id":        nbt.String("minecraft:" + mods[i].(string)),
				"amount":    nbt.Double(mods[i+1].(float64)),
				"operation": nbt.String("add_multiplied_base"),
			})
		}
		c["modifiers"] = list
	}
	return c
}

// withAttributes is the mob with the attributes list.
func withAttributes(e nbt.Compound, attrs ...nbt.Compound) nbt.Compound {
	list := nbt.List{Elem: nbt.TypeCompound}
	for _, a := range attrs {
		list.Items = append(list.Items, a)
	}
	e["attributes"] = list
	return e
}

func TestDifferingEntityKeys(t *testing.T) {
	rotated := func(id string, yaw, pitch float32) nbt.Compound {
		e := mob(id, 1, 64, 1, yaw)
		e["Rotation"].(nbt.List).Items[1] = nbt.Float(pitch)
		return e
	}
	tests := []struct {
		name string
		v, o nbt.Compound
		want []string
	}{
		{"attributes in another order",
			withAttributes(mob("pig", 1, 64, 1, 0), attr("max_health", 10), attr("follow_range", 16, "b", 0.2, "a", 0.1)),
			withAttributes(mob("pig", 1, 64, 1, 0), attr("follow_range", 16, "a", 0.1, "b", 0.2), attr("max_health", 10)),
			nil},
		{"attribute base differs",
			withAttributes(mob("pig", 1, 64, 1, 0), attr("max_health", 10)),
			withAttributes(mob("pig", 1, 64, 1, 0), attr("max_health", 12)),
			[]string{"attributes"}},
		{"attribute only one side holds, without modifiers",
			withAttributes(mob("pig", 1, 64, 1, 0), attr("max_health", 10), attr("armor", 0)),
			withAttributes(mob("pig", 1, 64, 1, 0), attr("max_health", 10)),
			nil},
		{"attribute only one side holds, with modifiers",
			withAttributes(mob("cat", 1, 64, 1, 0), attr("follow_range", 16, "random_spawn_bonus", -0.1)),
			withAttributes(mob("cat", 1, 64, 1, 0)),
			[]string{"attributes"}},
		{"zombie rolls on its own random",
			withAttributes(mob("zombie_villager", 1, 64, 1, 0), attr("spawn_reinforcements", 0.07),
				attr("knockback_resistance", 0, "random_spawn_bonus", 0.03),
				attr("follow_range", 35, "zombie_random_spawn_bonus", 0.4, "random_spawn_bonus", 0.02),
				attr("max_health", 20, "leader_zombie_bonus", 1.5)),
			withAttributes(mob("zombie_villager", 1, 64, 1, 0), attr("spawn_reinforcements", 0.01),
				attr("follow_range", 35, "random_spawn_bonus", 0.02), attr("max_health", 20)),
			nil},
		{"zombie modifiers not rolled on its own random still compare",
			withAttributes(mob("husk", 1, 64, 1, 0), attr("follow_range", 35, "random_spawn_bonus", 0.02)),
			withAttributes(mob("husk", 1, 64, 1, 0), attr("follow_range", 35, "random_spawn_bonus", 0.05)),
			[]string{"attributes"}},
		{"another kind's spawn_reinforcements base compares",
			withAttributes(mob("pig", 1, 64, 1, 0), attr("spawn_reinforcements", 0.07)),
			withAttributes(mob("pig", 1, 64, 1, 0), attr("spawn_reinforcements", 0.01)),
			[]string{"attributes"}},
		{"empty lists of other element types",
			func() nbt.Compound {
				e := mob("villager", 1, 64, 1, 0)
				e["Gossips"] = nbt.List{Elem: nbt.TypeEnd}
				e["Brain"] = nbt.Compound{"memories": nbt.List{Elem: nbt.TypeEnd}}
				return e
			}(),
			func() nbt.Compound {
				e := mob("villager", 1, 64, 1, 0)
				e["Gossips"] = nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{}}
				e["Brain"] = nbt.Compound{"memories": nbt.List{Elem: nbt.TypeCompound}}
				return e
			}(),
			nil},
		{"lists with items of other element types",
			func() nbt.Compound {
				e := mob("fox", 1, 64, 1, 0)
				e["Trusted"] = nbt.List{Elem: nbt.TypeInt, Items: []nbt.Tag{nbt.Int(1)}}
				return e
			}(),
			func() nbt.Compound {
				e := mob("fox", 1, 64, 1, 0)
				e["Trusted"] = nbt.List{Elem: nbt.TypeLong, Items: []nbt.Tag{nbt.Long(1)}}
				return e
			}(),
			[]string{"Trusted"}},
		{"yaw past 360", rotated("pig", 381.3, 0), rotated("pig", 21.3, 0), nil},
		{"negative yaw", rotated("cat", -90, 0), rotated("cat", 270, 0), nil},
		{"yaw differs", rotated("cat", 90, 0), rotated("cat", 91, 0), []string{"Rotation"}},
		{"pitch is not folded", rotated("cat", 90, -10), rotated("cat", 90, 350), []string{"Rotation"}},
		{"shulker Rotation is unseeded",
			rotated("shulker", 1.5, 0),
			func() nbt.Compound { e := mob("shulker", 1, 64, 1, 0); delete(e, "Rotation"); return e }(),
			nil},
		{"chicken EggLayTime is unseeded",
			func() nbt.Compound { e := mob("chicken", 1, 64, 1, 0); e["EggLayTime"] = nbt.Int(6000); return e }(),
			func() nbt.Compound { e := mob("chicken", 1, 64, 1, 0); e["EggLayTime"] = nbt.Int(9000); return e }(),
			nil},
		{"armadillo scute_time is unseeded",
			func() nbt.Compound { e := mob("armadillo", 1, 64, 1, 0); e["scute_time"] = nbt.Int(6000); return e }(),
			func() nbt.Compound { e := mob("armadillo", 1, 64, 1, 0); e["scute_time"] = nbt.Int(9000); return e }(),
			nil},
		{"another kind's EggLayTime compares",
			func() nbt.Compound { e := mob("pig", 1, 64, 1, 0); e["EggLayTime"] = nbt.Int(6000); return e }(),
			func() nbt.Compound { e := mob("pig", 1, 64, 1, 0); e["EggLayTime"] = nbt.Int(9000); return e }(),
			[]string{"EggLayTime"}},
	}
	for _, tt := range tests {
		if got := differingEntityKeys(tt.v, tt.o); !reflect.DeepEqual(got, tt.want) {
			t.Errorf("%s: keys %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestComparePostProcessAge(t *testing.T) {
	head := func(age string) uint16 {
		return state(t, "minecraft:cave_vines", map[string]string{"age": age, "berries": "true"})
	}
	cases := []struct {
		name    string
		v, o    uint16
		ageOnly bool
	}{
		{"age only", head("3"), head("17"), true},
		{"berries differ", head("3"), state(t, "minecraft:cave_vines", map[string]string{"age": "3", "berries": "false"}), false},
		{"body", head("3"), state(t, "minecraft:cave_vines_plant", map[string]string{"berries": "true"}), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			v, o := filled(t, 0, 0), filled(t, 0, 0)
			v.Blocks[v.index(3, -14, 4)] = tc.v
			o.Blocks[o.index(3, -14, 4)] = tc.o
			rep, err := compare(overworld, 0, chunkRange{0, 0, 0, 0},
				map[[2]int32]*chunkData{{0, 0}: v}, map[[2]int32]*chunkData{{0, 0}: o}, nil)
			if err != nil {
				t.Fatal(err)
			}
			wantAge, wantDiff := 0, 1
			if tc.ageOnly {
				wantAge, wantDiff = 1, 0
			}
			if c := rep.Chunks[0]; c.PostProcessAge != wantAge || c.DifferingBlocks != wantDiff || rep.PostProcessAge != wantAge {
				t.Fatalf("chunk %+v, total %d; want age %d differing %d", c, rep.PostProcessAge, wantAge, wantDiff)
			}
			var text bytes.Buffer
			rep.writeText(&text, 2)
			if !strings.Contains(text.String(), "post-process head age     "+strconv.Itoa(wantAge)+"\n") {
				t.Fatalf("total missing: %s", text.String())
			}
		})
	}
}
