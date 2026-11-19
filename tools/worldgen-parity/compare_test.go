package main

import (
	"bytes"
	"encoding/json"
	"io"
	"reflect"
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

type failingDiffWriter struct{}

func (failingDiffWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }

// mob is a saved entity of the kind at the position, facing yaw.
func mob(id string, x, y, z float64, yaw float32) nbt.Compound {
	return nbt.Compound{
		"id":       nbt.String("minecraft:" + id),
		"Pos":      nbt.List{Elem: nbt.TypeDouble, Items: []nbt.Tag{nbt.Double(x), nbt.Double(y), nbt.Double(z)}},
		"Rotation": nbt.List{Elem: nbt.TypeFloat, Items: []nbt.Tag{nbt.Float(yaw), nbt.Float(0)}},
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
