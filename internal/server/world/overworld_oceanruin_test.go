package world

//go:generate go run ../../../tools/gen oceanruin-golden

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"os"
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
