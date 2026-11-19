package world

//go:generate go run ../../../tools/gen smallports-golden

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/server/world/levelgen"
	"github.com/golem-mc/golem/nbt"
)

// TestSmallPortsGolden compares the swamp hut, desert pyramid and igloo
// pieces and starts with the vanilla recording, read once for both.
func TestSmallPortsGolden(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("testdata/smallports_golden.txt")
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	t.Run("SmallPorts", func(t *testing.T) {
		t.Parallel()
		testSmallPortsPlacements(t, lines)
	})
	t.Run("SmallPortsStarts", func(t *testing.T) {
		t.Parallel()
		testSmallPortsStarts(t, lines)
	})
}

// Vanilla executes the pieces on stone terrain; shape updates and mob
// finalization are excluded, as in the jungle temple fixture.
func testSmallPortsPlacements(t *testing.T, lines []string) {
	cases := 0
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		if !strings.HasPrefix(line, "place ") {
			continue
		}
		var kind, states, draws, loot string
		var seed int64
		var mode, dir, reverse, containers int
		if _, err := fmt.Sscanf(line, "place %s %d %d %d %d %s %s %d %s", &kind, &seed, &mode, &dir, &reverse, &states, &draws, &containers, &loot); err != nil {
			t.Fatal(err)
		}
		i++
		want, err := nbt.ParseSNBT(lines[i])
		if err != nil {
			t.Fatal(err)
		}
		cases++
		// A half fraction of the full seed x terrain x direction x order grid:
		// every level of every factor is still checked, with half the pairings.
		if (int(seed&1)+mode+dir+reverse)%2 != 0 {
			continue
		}
		t.Run(fmt.Sprintf("%s/%d/%d/%d/%d", kind, seed, mode, dir, reverse), func(t *testing.T) {
			t.Parallel()
			src := &templeTerrain{newGoldenTerrain(), int32(mode)}
			var p structurePiece
			switch kind {
			case "hut":
				// Legacy seeds select north, east, south and west respectively.
				p = newSwampHut(levelgen.NewLegacyRandom([4]int64{4096, 6144, 0, 256}[dir]), -3, 7)
			case "pyramid":
				h := newDesertPyramid(levelgen.NewLegacyRandom(seed), -3, 7)
				h.dir = dir
				p = h
			default:
				depth := int32(12)
				if kind == "top" {
					depth = 0
				}
				p = newIglooPiece(kind, [3]int32{-3, 90, 7}, Rotation(dir), depth)
			}
			regions := map[ChunkPos]*decorRegion{}
			randoms := sha256.New()
			for j := range 6 {
				index := j
				if reverse != 0 {
					index = 5 - j
				}
				cx, cz := int32(index%3-1), int32(index/3)
				r := newDecorRegion(src, newChunk(cx, cz), worldMinY, worldHeight)
				r.worldSeed = seed
				r.nx, r.nz = cx, cz
				r.bindProtos(ChunkPos{cx, cz})
				r.rng.SetSeed(seed + int64(cx)*31 + int64(cz)*17)
				box := jbox{cx * 16, -64, cz * 16, cx*16 + 15, 319, cz*16 + 15}
				p.place(r, box, [3]int32{})
				if kind == "pyramid" {
					desertPyramidStructure{}.afterPlace(r, box, &structureStart{pieces: []structurePiece{p}})
				}
				randoms.Write(binary.BigEndian.AppendUint32(nil, uint32(r.rng.NextInt())))
				if kind == "pyramid" {
					randoms.Write(binary.BigEndian.AppendUint32(nil, uint32(r.regionRandom().NextInt())))
				}
				regions[ChunkPos{cx, cz}] = r
			}
			hash, lootHash := sha256.New(), sha256.New()
			gotContainers := 0
			for x := int32(-16); x < 32; x++ {
				for y := int32(32); y < 88; y++ {
					for z := range int32(32) {
						r := regions[ChunkPos{x >> 4, z >> 4}]
						state := r.get(x, y, z)
						hash.Write(goldenStateBytes()[state])
						for _, be := range r.protoAt(x, z).entities {
							table, ok := nbt.Get[nbt.String](be.tag, "LootTable")
							if !ok || be.x != x&15 || be.y != y-r.minY || be.z != z&15 || be.tag["id"] != nbt.String("minecraft:chest") {
								continue
							}
							lootSeed, _ := nbt.LongValue(be.tag["LootTableSeed"])
							fmt.Fprintf(lootHash, "%d,%d,%d:%s:%d\n", x, y, z, table, lootSeed)
							gotContainers++
						}
					}
				}
			}
			if got := fmt.Sprintf("%x", hash.Sum(nil)); got != states {
				t.Errorf("blocks %s, vanilla %s", got, states)
			}
			if got := fmt.Sprintf("%x", randoms.Sum(nil)); got != draws {
				t.Errorf("draws %s, vanilla %s", got, draws)
			}
			if got := fmt.Sprintf("%x", lootHash.Sum(nil)); got != loot || gotContainers != containers {
				t.Errorf("loot %d %s, vanilla %d %s", gotContainers, got, containers, loot)
			}
			if got := p.nbt(); !reflect.DeepEqual(got, want) {
				t.Errorf("saved %v, vanilla %v", got, want)
			}
		})
	}
	if cases != 160 {
		t.Fatalf("checked %d placements, want 160", cases)
	}
}

func testSmallPortsStarts(t *testing.T, lines []string) {
	generators := map[int64]*OverworldGenerator{0: NewOverworldGenerator(0), 12345: NewOverworldGenerator(12345)}
	cases, starts := 0, 0
	for i := 0; i < len(lines); i++ {
		if !strings.HasPrefix(lines[i], "start ") {
			continue
		}
		var kind, status string
		var seed int64
		var cx, cz int32
		count := 0
		if _, err := fmt.Sscanf(lines[i], "start %s %d %d %d %s", &kind, &seed, &cx, &cz, &status); err != nil {
			t.Fatal(err)
		}
		cases++
		var got *structureStart
		for _, s := range generators[seed].structureStartsIn(cx, cz) {
			if s.def.Name == "minecraft:"+kind {
				got = s
			}
		}
		if status == "present" {
			if _, err := fmt.Sscanf(lines[i], "start %s %d %d %d %s %d", &kind, &seed, &cx, &cz, &status, &count); err != nil {
				t.Fatal(err)
			}
			starts++
		}
		if (got != nil) != (count > 0) {
			t.Errorf("%s: start presence differs", lines[i])
		}
		for j := 0; j < count; j++ {
			i++
			want, err := nbt.ParseSNBT(lines[i])
			if err != nil {
				t.Fatal(err)
			}
			if got != nil {
				if len(got.pieces) != count {
					t.Fatalf("piece count %d, vanilla %d", len(got.pieces), count)
				}
				if actual := got.pieces[j].nbt(); !reflect.DeepEqual(actual, want) {
					t.Errorf("%s seed %d chunk %d,%d: %v, vanilla %v", kind, seed, cx, cz, actual, want)
				}
			}
		}
	}
	if cases != 306 || starts != 22 {
		t.Fatalf("checked %d candidates, %d starts", cases, starts)
	}
}
