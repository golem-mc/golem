package world

//go:generate go run ../../../tools/gen oceanruin-golden

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/server/world/levelgen"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

func TestOceanRuinVanilla(t *testing.T) {
	data, err := os.ReadFile("testdata/oceanruin_golden.txt")
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	generators := map[int64]*OverworldGenerator{}
	starts, processors := 0, 0
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		if strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "start ") {
			var seed int64
			var x, z int32
			var id string
			var count int
			if _, err := fmt.Sscanf(line, "start %d %d %d %s %d", &seed, &x, &z, &id, &count); err != nil {
				t.Fatal(err)
			}
			g := generators[seed]
			if g == nil {
				g = NewOverworldGenerator(seed)
				generators[seed] = g
			}
			rng := levelgen.NewLegacyRandom(0)
			rng.SetLargeFeatureSeed(seed, x, z)
			_, build, _ := (oceanRuinStructure{cold: strings.HasSuffix(id, "cold")}).generationPoint(&structureContext{g: g, chunkX: x, chunkZ: z, rng: rng})
			pieces := build()
			if len(pieces) != count {
				t.Fatalf("%s: got %d pieces", line, len(pieces))
			}
			for _, p := range pieces {
				i++
				want, err := nbt.ParseSNBT(lines[i])
				if err != nil {
					t.Fatal(err)
				}
				got, _ := nbt.Encode("", p.nbt())
				expected, _ := nbt.Encode("", want)
				if string(got) != string(expected) {
					t.Fatalf("%s: got %v, vanilla %v", line, p.nbt(), want)
				}
			}
			starts++
		} else if strings.HasPrefix(line, "process ") {
			var seed int64
			var name, want string
			var rot Rotation
			var count int
			if _, err := fmt.Sscanf(line, "process %d %s %d %d %s", &seed, &name, &rot, &count, &want); err != nil {
				t.Fatal(err)
			}
			large := strings.HasPrefix(name, "big_")
			integrity := float32(0.8)
			if large {
				integrity = 0.9
			}
			if strings.Contains(name, "cracked") {
				integrity = 0.7
			}
			if strings.Contains(name, "mossy") {
				integrity = 0.5
			}
			p := &oceanRuinPiece{template: oceanRuinTemplates()[name], pos: [3]int32{-17, 41, 13}, rotation: rot, cold: !strings.Contains(name, "warm"), integrity: integrity}
			placed := p.process(seed)
			h := sha256.New()
			for i, b := range placed.Changes {
				for _, v := range []int32{b.X, b.Y, b.Z} {
					_ = binary.Write(h, binary.BigEndian, v)
				}
				h.Write(goldenStateBytes()[b.State])
				s, _ := nbt.LongValue(placed.Tags[i]["LootTableSeed"])
				_ = binary.Write(h, binary.BigEndian, s)
			}
			if got := fmt.Sprintf("%x", h.Sum(nil)); got != want || len(placed.Changes) != count {
				t.Fatalf("%s: got %d %s", line, len(placed.Changes), got)
			}
			processors++
		} else {
			t.Fatal(line)
		}
	}
	if starts != 128 || processors != 384 {
		t.Fatalf("checked %d starts, %d processors", starts, processors)
	}
}

func BenchmarkOverworldGenerateOceanRuin(b *testing.B) {
	NewOverworldGenerator(12345).Generate(newChunk(-16, 5))
	b.ReportAllocs()
	for b.Loop() {
		NewOverworldGenerator(12345).Generate(newChunk(-16, 5))
	}
}

func TestOceanRuinMarkers(t *testing.T) {
	r := newDecorRegion(newGoldenTerrain(), newChunk(0, 0), worldMinY, worldHeight)
	r.bindProtos(ChunkPos{})
	p := &oceanRuinPiece{cold: true, large: true}
	r.set(1, 60, 1, registry.BlockWater.DefaultState())
	p.marker(r, 1, 60, 1, "chest")
	if !registry.StateBool(r.get(1, 60, 1), registry.PropWaterlogged) {
		t.Fatal("dry underwater chest")
	}
	p.marker(r, 2, 60, 2, "drowned")
	p.marker(r, 3, 70, 3, "drowned")
	if r.get(2, 60, 2) != registry.BlockWater.DefaultState() || !r.isAir(3, 70, 3) {
		t.Fatal("drowned markers left wrong fluid")
	}
	proto := r.protoAt(1, 1)
	if len(proto.entities) != 3 {
		t.Fatalf("got %d entities", len(proto.entities))
	}
	if proto.entities[0].tag["LootTable"] != nbt.String("minecraft:chests/underwater_ruin_big") {
		t.Fatal(proto.entities[0].tag)
	}
	for _, e := range proto.entities[1:] {
		if e.tag["PersistenceRequired"] != nbt.Byte(1) {
			t.Fatal(e.tag)
		}
	}
}

// Every block and unopened container inside both ruins in the frozen vanilla
// seed-12345 parity world (-24,-24..23,23), not a selection from our output.
func TestOceanRuinWorldFixtures(t *testing.T) {
	files, err := filepath.Glob("testdata/oceanruin_*.nbt")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 {
		t.Fatal("need both vanilla ruin starts")
	}
	g := NewOverworldGenerator(12345)
	// The recording's FEATURES order around each ruin: the seagrass and kelp
	// inside the boxes depend on which neighbour decorated first.
	var order []ChunkPos
	for x := int32(5); x <= 9; x++ {
		for z := int32(2); z <= 7; z++ {
			order = append(order, ChunkPos{x, z})
		}
	}
	g.DecorateInOrder(append(order,
		ChunkPos{-18, 3}, ChunkPos{-18, 4}, ChunkPos{-18, 5}, ChunkPos{-18, 6}, ChunkPos{-18, 7},
		ChunkPos{-17, 4}, ChunkPos{-17, 5}, ChunkPos{-17, 6}, ChunkPos{-17, 3}, ChunkPos{-17, 7},
		ChunkPos{-16, 5}, ChunkPos{-16, 6}, ChunkPos{-16, 7}, ChunkPos{-16, 3}, ChunkPos{-16, 4},
		ChunkPos{-15, 3}, ChunkPos{-15, 4}, ChunkPos{-15, 5}, ChunkPos{-15, 6}, ChunkPos{-15, 7},
		ChunkPos{-14, 3}, ChunkPos{-14, 4}, ChunkPos{-14, 5}, ChunkPos{-14, 6}, ChunkPos{-14, 7}))
	chunks := map[ChunkPos]*Chunk{}
	get := func(x, z int32) *Chunk {
		pos := ChunkPos{x, z}
		if chunks[pos] == nil {
			c := newChunk(x, z)
			g.Generate(c)
			chunks[pos] = c
		}
		return chunks[pos]
	}
	for _, file := range files {
		t.Run(filepath.Base(file), func(t *testing.T) {
			data, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			_, tag, err := nbt.Decode(data)
			if err != nil {
				t.Fatal(err)
			}
			pin := tag.(nbt.Compound)
			start := pin["start"].(nbt.Compound)
			cx, _ := nbt.IntField(start, "ChunkX")
			cz, _ := nbt.IntField(start, "ChunkZ")
			get(cx, cz)
			containers := map[posKey]bool{}
			for _, b := range pin["block_entities"].(nbt.List).Items {
				be := b.(nbt.Compound)
				x, _ := nbt.IntField(be, "x")
				y, _ := nbt.IntField(be, "y")
				z, _ := nbt.IntField(be, "z")
				containers[posKey{x, y, z}] = true
			}
			for _, b := range pin["blocks"].(nbt.List).Items {
				p := b.(nbt.IntArray)
				c := get(p[0]>>4, p[2]>>4)
				got := c.BlockAt(p[0]&15, p[1], p[2]&15)
				_, carried := c.carriedBlockEntityAt(p[0], p[1], p[2])
				if (c.blockEntity(p[0], p[1], p[2]) != nil || carried) != containers[posKey{p[0], p[1], p[2]}] {
					t.Errorf("block entity presence differs at %v", p[:3])
				}
				// Spawn chunks can receive scheduled bubble-column ticks before the
				// reference server freezes; the generation-stage fluid is water.
				if registry.BlockOf(uint16(p[3])) == registry.BlockBubbleColumn && got == registry.BlockWater.DefaultState() {
					continue
				}
				if got != uint16(p[3]) {
					t.Errorf("%v: got %s %v, vanilla %s %v", p[:3], registry.BlockOf(got).Name(), registry.StateProps(got), registry.BlockOf(uint16(p[3])).Name(), registry.StateProps(uint16(p[3])))
				}
			}
			for _, b := range pin["block_entities"].(nbt.List).Items {
				be := b.(nbt.Compound)
				x, _ := nbt.IntField(be, "x")
				y, _ := nbt.IntField(be, "y")
				z, _ := nbt.IntField(be, "z")
				c := get(x>>4, z>>4)
				table := ""
				var gotSeed int64
				if got := c.blockEntity(x, y, z); got != nil {
					table, gotSeed = got.LootTable, got.LootTableSeed
				} else if tag, ok := c.carriedBlockEntityAt(x, y, z); ok {
					v, _ := nbt.Get[nbt.String](tag, "LootTable")
					table = string(v)
					gotSeed, _ = nbt.LongValue(tag["LootTableSeed"])
				}
				seed, _ := nbt.LongValue(be["LootTableSeed"])
				if table != string(be["LootTable"].(nbt.String)) || gotSeed != seed {
					t.Errorf("container at %d,%d,%d: got %s / %d, vanilla %v", x, y, z, table, gotSeed, be)
				}
			}
			found := false
			for _, s := range g.structureStartsIn(cx, cz) {
				if s.def.Name != "minecraft:ocean_ruin_cold" {
					continue
				}
				found = true
				if len(s.pieces) != len(start["Children"].(nbt.List).Items) {
					t.Fatal("saved piece count differs")
				}
				for i, p := range s.pieces {
					want, _ := nbt.Encode("", start["Children"].(nbt.List).Items[i])
					got, _ := nbt.Encode("", p.nbt())
					if string(want) != string(got) {
						t.Errorf("piece: got %v, vanilla %v", p.nbt(), start["Children"].(nbt.List).Items[i])
					}
				}
			}
			if !found {
				t.Fatal("missing start on structure grid")
			}
		})
	}
}

func TestOceanRuinClipsToWritableChunk(t *testing.T) {
	r := newDecorRegion(newGoldenTerrain(), newChunk(0, 0), worldMinY, worldHeight)
	r.bindProtos(ChunkPos{})
	template := &Template{Size: [3]int32{2, 1, 1}, Palettes: [][]TemplateBlock{{
		{Pos: [3]int32{0, 0, 0}, State: registry.BlockGravel.DefaultState()},
		{Pos: [3]int32{1, 0, 0}, State: registry.BlockGravel.DefaultState()},
	}}}
	p := &oceanRuinPiece{template: template, pos: [3]int32{15, 90, 0}, cold: true, integrity: 1}
	p.box = p.templateBox()
	p.place(r, jbox{0, -63, 0, 15, 319, 15}, [3]int32{})
	if registry.BlockOf(r.get(15, p.pos[1], 0)) != registry.BlockSuspiciousGravel {
		t.Fatal("missing archaeology inside chunk")
	}
	if registry.BlockOf(r.get(16, p.pos[1], 0)) == registry.BlockSuspiciousGravel {
		t.Fatal("archaeology escaped writable chunk")
	}
	entities := r.protoAt(15, 0).entities
	if len(entities) != 1 || entities[0].tag["LootTable"] != nbt.String("minecraft:archaeology/ocean_ruin_cold") {
		t.Fatal(entities)
	}
}
