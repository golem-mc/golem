package world

import (
	"bytes"
	"fmt"
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/server/world/levelgen"
	"github.com/golem-mc/golem/nbt"
)

// TestMonumentMatchesVanilla compares the ocean monument with vanilla's: its
// starts, and the exterior and rooms placed over the shared synthetic terrain.
func TestMonumentMatchesVanilla(t *testing.T) {
	t.Parallel()
	t.Run("MonumentStarts", testMonumentStarts)
	t.Run("MonumentRooms", func(t *testing.T) {
		t.Parallel()
		testMonumentBlockGolden(t, "testdata/monument_rooms_golden.txt", true)
	})
	t.Run("MonumentExterior", func(t *testing.T) {
		t.Parallel()
		testMonumentBlockGolden(t, "testdata/monument_exterior_golden.txt", false)
	})
}

func testMonumentStarts(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("testdata/monument_starts.txt")
	if err != nil {
		t.Fatal(err)
	}
	generators := map[int64]*OverworldGenerator{}
	counts := map[string]int{}
	for line := range strings.SplitSeq(string(data), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		var seed int64
		var cx, cz int32
		var result string
		if _, err := fmt.Sscan(line, &seed, &cx, &cz, &result); err != nil {
			t.Fatal(err)
		}
		g := generators[seed]
		if g == nil {
			g = NewOverworldGenerator(seed)
			generators[seed] = g
		}
		ctx := &structureContext{g: g, chunkX: cx, chunkZ: cz, rng: levelgen.NewLegacyRandom(0)}
		_, _, surrounding := (monumentStructure{}).generationPoint(ctx)
		if surrounding != (result != "surrounding") {
			t.Fatalf("seed %d chunk %d,%d: surrounding check %v; vanilla %s", seed, cx, cz, surrounding, result)
		}
		s := g.growStructure("minecraft:monument", cx, cz)
		if (s != nil) != (result == "start") {
			t.Fatalf("seed %d chunk %d,%d: start %v; vanilla %s", seed, cx, cz, s != nil, result)
		}
		counts[result]++
		if s == nil {
			continue
		}
		var o int
		var b [6]int32
		if _, err := fmt.Sscan(line, &seed, &cx, &cz, &result, &o, &b[0], &b[1], &b[2], &b[3], &b[4], &b[5]); err != nil {
			t.Fatal(err)
		}
		want := nbt.Compound{"id": nbt.String("minecraft:omb"), "GD": nbt.Int(0), "O": nbt.Int(o), "BB": nbt.IntArray(b[:])}
		if len(s.pieces) != 1 || !reflect.DeepEqual(s.pieces[0].nbt(), want) {
			t.Fatalf("seed %d chunk %d,%d: saved building differs from vanilla %v", seed, cx, cz, want)
		}
		// Each generator reconstructs the unsaved graph from the seed.
		fresh := NewOverworldGenerator(seed).growStructure("minecraft:monument", cx, cz)
		if !reflect.DeepEqual(s.pieces, fresh.pieces) {
			t.Fatal("regenerated room graph differs")
		}
	}
	if counts["start"] == 0 || counts["surrounding"] == 0 || counts["centre"] == 0 {
		t.Fatalf("missing start-validation coverage: %v", counts)
	}
}

func TestMonumentSurvivesRestart(t *testing.T) {
	t.Parallel()
	const seed = -7343414273836990780
	open := func(dir string) *World {
		w, err := NewWorld(dir, NewOverworldGenerator(seed))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = w.Close() })
		return w
	}
	elders := 0
	read := func(w *World, pos ChunkPos) []byte {
		c, _ := w.Acquire(pos)
		defer w.Release(pos)
		blocks := encodeWhole(c)
		var tags []string
		for _, e := range c.generatedEntities {
			if e["id"] == nbt.String("minecraft:elder_guardian") {
				elders++
			}
			tag, _ := nbt.Encode("", e)
			tags = append(tags, string(tag))
		}
		slices.Sort(tags)
		return append(blocks, []byte(strings.Join(tags, ""))...)
	}
	// These chunks contain opposite wings and the penthouse. The second
	// generation resumes partially decorated protos saved by the first one.
	positions := []ChunkPos{{111, 108}, {111, 109}, {111, 111}}
	whole := open(t.TempDir())
	want := make([][]byte, len(positions))
	for i, pos := range positions {
		want[i] = read(whole, pos)
	}
	if elders != 3 {
		t.Fatalf("footprint contains %d elders, want 3", elders)
	}
	dir := t.TempDir()
	first := open(dir)
	read(first, positions[0])
	first.Close()
	second := open(dir)
	for i, pos := range positions[1:] {
		if got := read(second, pos); !bytes.Equal(got, want[i+1]) {
			t.Fatalf("chunk %v differs after restart", pos)
		}
	}
	g := NewOverworldGenerator(seed)
	c := newChunk(111, 110)
	g.saveStructures(c)
	if len(c.StructureStarts()) == 0 || !slices.Contains(structureRefs(c, "minecraft:monument"), packChunk(111, 110)) {
		t.Fatal("missing monument start or reference")
	}
	g.DisableStructures()
	if len(g.structureStartsIn(111, 110)) != 0 {
		t.Fatal("disabled structures generated a monument")
	}
}
