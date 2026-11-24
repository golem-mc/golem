package world

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// worldFileTemplates holds, per seed, the files a created overworld leaves
// behind: level.dat and world_gen_settings.dat. Creating a world searches the
// seed's spawn, which costs generated chunks; a folder with these files is
// opened as an existing world, so the search runs once per seed.
var worldFileTemplates sync.Map // int64 -> func() map[string][]byte

func seedWorldDir(t *testing.T, dir string, seed int64) {
	t.Helper()
	build, _ := worldFileTemplates.LoadOrStore(seed, sync.OnceValue(func() map[string][]byte {
		tmp, err := os.MkdirTemp("", "world-template")
		if err != nil {
			panic(err)
		}
		defer os.RemoveAll(tmp)
		w, err := NewWorld(tmp, NewOverworldGenerator(seed))
		if err != nil {
			panic(err)
		}
		_ = w.Close()
		files := map[string][]byte{}
		for _, name := range []string{"level.dat", "data/minecraft/world_gen_settings.dat"} {
			if files[name], err = os.ReadFile(filepath.Join(tmp, name)); err != nil {
				panic(err)
			}
		}
		return files
	}))
	for name, data := range build.(func() map[string][]byte)() {
		path := filepath.Join(dir, name)
		if _, err := os.Stat(path); err == nil {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// netherWorld opens the nether of a world folder, the way the server does.
func netherWorld(t *testing.T, dir string, seed int64) *World {
	t.Helper()
	seedWorldDir(t, dir, seed)
	ow, err := NewWorld(dir, NewOverworldGenerator(seed))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ow.Close() })
	w := NewDimensionWorld(dir, true, NewNetherGenerator(seed))
	t.Cleanup(func() { _ = w.Close() })
	return w
}

func acquireNetherRange(w *World, x0, z0, x1, z1 int32, out map[ChunkPos][]byte) {
	for cx := x0; cx <= x1; cx++ {
		for cz := z0; cz <= z1; cz++ {
			pos := ChunkPos{X: cx, Z: cz}
			c, _ := w.Acquire(pos)
			out[pos] = encodeWhole(c)
			w.Release(pos)
		}
	}
}

// TestNetherProtosSurviveARestart stops a world halfway through a range and
// goes on with a new one: the chunks must come out as in a run that never
// stopped, which they only do if the unfinished chunks were saved.
func TestNetherProtosSurviveARestart(t *testing.T) {
	t.Parallel()
	const seed = 0
	whole := map[ChunkPos][]byte{}
	acquireNetherRange(netherWorld(t, t.TempDir(), seed), -42, -16, -40, -14, whole)

	dir := t.TempDir()
	got := map[ChunkPos][]byte{}
	first := netherWorld(t, dir, seed)
	acquireNetherRange(first, -42, -16, -41, -14, got)
	first.Close()
	second := netherWorld(t, dir, seed)
	acquireNetherRange(second, -40, -16, -40, -14, got)

	for pos, want := range whole {
		if g, ok := got[pos]; ok && !bytes.Equal(g, want) {
			t.Errorf("chunk %v differs after a restart", pos)
		}
	}
}

// acquireFortressRange acquires a range like acquireNetherRange and records
// the chests and spawners of each chunk, which encodeWhole leaves out.
func acquireFortressRange(w *World, x0, z0, x1, z1 int32, blocks map[ChunkPos][]byte, entities map[ChunkPos]string) {
	for cx := x0; cx <= x1; cx++ {
		for cz := z0; cz <= z1; cz++ {
			pos := ChunkPos{X: cx, Z: cz}
			c, _ := w.Acquire(pos)
			blocks[pos] = encodeWhole(c)
			var list []string
			for k, be := range c.entities {
				list = append(list, fmt.Sprint(k, be.Kind, be.LootTable, be.LootTableSeed))
			}
			slices.Sort(list)
			entities[pos] = strings.Join(list, ";")
			w.Release(pos)
		}
	}
}

// TestNetherFortressSurvivesARestart does the same where a fortress is: its
// fences, chests and spawner live in the protos and their save format.
func TestNetherFortressSurvivesARestart(t *testing.T) {
	t.Parallel()
	const seed = 0
	whole, wholeBE := map[ChunkPos][]byte{}, map[ChunkPos]string{}
	acquireFortressRange(netherWorld(t, t.TempDir(), seed), 17, 0, 20, 5, whole, wholeBE)

	dir := t.TempDir()
	got, gotBE := map[ChunkPos][]byte{}, map[ChunkPos]string{}
	first := netherWorld(t, dir, seed)
	acquireFortressRange(first, 17, 0, 18, 5, got, gotBE)
	first.Close()
	acquireFortressRange(netherWorld(t, dir, seed), 19, 0, 20, 5, got, gotBE)

	withEntities := 0
	for pos, want := range whole {
		if !bytes.Equal(got[pos], want) {
			t.Errorf("chunk %v differs after a restart", pos)
		}
		if gotBE[pos] != wholeBE[pos] {
			t.Errorf("chunk %v: block entities %q after a restart, want %q", pos, gotBE[pos], wholeBE[pos])
		}
		if wholeBE[pos] != "" {
			withEntities++
		}
	}
	if withEntities == 0 {
		t.Error("the range has no fortress block entity, so the test checks nothing")
	}
}

func TestNetherProtoCodecRoundTrips(t *testing.T) {
	t.Parallel()
	p := &decorProto{decorated: true, final: true, finals: 0x1a5, sources: []posKey{{1, 40, 2}}, shapes: []posKey{{15, 77, 0}},
		entities: []protoEntity{{x: 3, y: 75, z: 4, lootSeed: -5}, {spawner: true, x: 1, y: 76, z: 2},
			{x: 9, y: 50, z: 8, tag: nbt.Compound{"id": nbt.String("minecraft:chest"), "LootTableSeed": nbt.Long(77)}}}}
	p.set(5, 70, 6, 1234)
	got, err := decodeDecorProto(appendDecorProto(nil, p))
	if err != nil {
		t.Fatal(err)
	}
	if !got.decorated || !got.final || got.finals != p.finals || !slices.Equal(got.sources, p.sources) ||
		!slices.Equal(got.shapes, p.shapes) || !slices.EqualFunc(got.entities, p.entities, func(a, b protoEntity) bool { return reflect.DeepEqual(a, b) }) {
		t.Errorf("decoded %+v, want %+v", got, p)
	}
	if s, ok := got.get(5, 70, 6); !ok || s != 1234 {
		t.Errorf("block read back as %d, %v", s, ok)
	}
}

// TestGeneratedSculkSensorHasItsBlockEntity pins a sculk sensor a seed-0 sculk
// patch places at (414, -38, -1763), which vanilla saves with a fresh block
// entity although the feature gave it no data.
// vanilla: WorldGenRegion.setBlock, LevelChunk.promotePendingBlockEntity
func TestGeneratedSculkSensorHasItsBlockEntity(t *testing.T) {
	t.Parallel()
	const x, y, z = 414, -38, -1763
	c := newChunk(x>>4, z>>4)
	NewOverworldGenerator(0).Generate(c)
	if got := c.BlockAt(x&15, y, z&15); registry.BlockOf(got) != registry.BlockSculkSensor {
		t.Fatalf("block at %d,%d,%d is %s, want a sculk sensor", x, y, z, registry.BlockOf(got).Name())
	}
	be := c.blockEntity(x, y, z)
	if be == nil {
		t.Fatal("the generated sculk sensor has no block entity")
	}
	if be.Kind != KindSculkSensor {
		t.Fatalf("block entity kind %s, want sculk_sensor", be.Kind)
	}
	// The fresh entity's fields, as vanilla's chunk saves them.
	want := nbt.Compound{
		"id": nbt.String("minecraft:sculk_sensor"), "x": nbt.Int(x), "y": nbt.Int(y), "z": nbt.Int(z),
		"components":               nbt.Compound{},
		"last_vibration_frequency": nbt.Int(0),
		"listener":                 nbt.Compound{"event_delay": nbt.Int(0), "selector": nbt.Compound{"tick": nbt.Long(-1)}},
	}
	if got := be.EncodeNBT(); !nbt.Equal(got, want) {
		t.Errorf("saved %v, want %v", got, want)
	}
}

func spilledProtoBytes() (*decorProto, []byte) {
	p := &decorProto{decorated: true, final: true}
	for y := range int32(64) {
		p.set(y%16, y, y%16, 100+uint16(y))
	}
	buf := new(decorProtoBuf)
	buf.deflate(p)
	return p, bytes.Clone(buf.out.Bytes())
}

func TestInflateRoundTripsAndRejectsGarbage(t *testing.T) {
	t.Parallel()
	_, data := spilledProtoBytes()
	buf := new(decorProtoBuf)
	for range 3 {
		got, err := decodeDecorProto(buf.inflate(data))
		if err != nil {
			t.Fatal(err)
		}
		if s, ok := got.get(5, 5, 5); !ok || s != 105 {
			t.Errorf("block read back as %d, %v", s, ok)
		}
		if _, err := decodeDecorProto(buf.inflate([]byte("garbage"))); err == nil {
			t.Error("garbage decoded")
		}
	}
}

func BenchmarkInflateDecodeProto(b *testing.B) {
	_, data := spilledProtoBytes()
	buf := new(decorProtoBuf)
	b.ReportAllocs()
	for b.Loop() {
		if _, err := decodeDecorProto(buf.inflate(data)); err != nil {
			b.Fatal(err)
		}
	}
}
