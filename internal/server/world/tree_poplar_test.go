package world

import (
	"bufio"
	"encoding/json"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/golem-mc/golem/registry"
)

// TestPoplarGolden checks full trees and independent canopies against the
// 26.3 placers, including the next draw after every placement.
func TestPoplarGolden(t *testing.T) {
	t.Parallel()
	file, err := os.Open("testdata/poplar_golden.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	configs := map[int]*treeConfig{}
	src := &treeGoldenTerrain{shaped: map[ChunkPos]*shapedTerrain{}}
	checked := 0
	scan := bufio.NewScanner(file)
	for scan.Scan() {
		line := scan.Text()
		if strings.HasPrefix(line, "#") || line == "" {
			continue
		}
		fields := strings.Fields(line)
		num := func(i int) int64 {
			n, err := strconv.ParseInt(fields[i], 10, 64)
			if err != nil {
				t.Fatal(err)
			}
			return n
		}
		kind, id := fields[0], int(num(1))
		if kind == "config" {
			parts := strings.SplitN(line, " ", 3)
			var config jobj
			if err := json.Unmarshal([]byte(parts[2]), &config); err != nil {
				t.Fatal(err)
			}
			configs[id] = parseTreeConfig(config)
			continue
		}
		var seed int64
		var cx, cz int32
		switch kind {
		case "tree":
			seed = num(2)
			if num(3) != 0 {
				cx, cz = -2, 3
			}
		case "foliage", "trunk":
			seed = int64(id)
		default:
			t.Fatalf("unknown line: %s", line)
		}
		src.ds = decorState{}
		region := newDecorRegion(src, newChunk(cx, cz), worldMinY, worldHeight)
		region.nx, region.nz, region.worldSeed = cx, cz, seed
		decoration := region.rng.SetDecorationSeed(seed, cx*16, cz*16)
		region.rng.SetFeatureSeed(decoration, 3, 8)
		var wantDigest string
		var tail int64
		switch kind {
		case "tree":
			k := int(num(3))
			x, z := cx*16+3, cz*16+7
			if k == 2 {
				x, z = cx*16+15, cz*16+15
			}
			y := int32(80)
			if k == 3 {
				y = 313
			}
			if k == 1 {
				region.setKnownShape(x, y+4, z, registry.BlockStone.DefaultState())
			}
			placed := region.placeTree(configs[id], treePos{x, y, z})
			if placed != (num(4) != 0) {
				t.Errorf("%s: placed=%v", line, placed)
			}
			wantDigest, tail = strings.Join(fields[5:7], " "), num(7)
		case "trunk":
			region.setKnownShape(8, 82, 8, registry.BlockStone.DefaultState())
			region.setKnownShape(8, 85, 7, registry.BlockStone.DefaultState())
			build := &treeBuild{r: region, rng: region.rng, cfg: configs[id%2]}
			attachments := build.cfg.trunk.placeTrunk(build, int32(1+id%12), treePos{8, 80, 8})
			if len(attachments) != 1 {
				t.Fatalf("trunk %d: attachments %v", id, attachments)
			}
			a := attachments[0]
			if a.pos != (treePos{8, int32(num(2)), 8}) || int64(a.radiusOffset) != num(3) || a.doubleTrunk != (fields[4] == "true") {
				t.Errorf("trunk %d: attachment %+v differs from vanilla %v", id, a, fields[2:5])
			}
			wantDigest, tail = strings.Join(fields[5:7], " "), num(7)
		case "foliage":
			radius, height := int32(3+id%6), int32(5+id%3)
			offset := int32(id%3 - 1)
			holes := []float32{0, 0.15, 1}[id%3]
			if id >= 12 {
				region.setKnownShape(8, 81, 8, registry.BlockWater.DefaultState())
				persistent, _ := registry.WithBool(registry.BlockOakLeaves.DefaultState(), registry.PropPersistent, true)
				region.setKnownShape(9, 81, 8, persistent)
				region.setKnownShape(8, 82, 9, registry.BlockStone.DefaultState())
				leaf, _ := registry.WithInt(registry.BlockOakLeaves.DefaultState(), registry.PropDistance, 2)
				region.setKnownShape(7, 82, 8, leaf)
			}
			p := poplarFoliage{offset: compileInt(float64(offset)), sideHoleChance: holes}
			build := &treeBuild{r: region, rng: region.rng, cfg: configs[id%2]}
			p.createFoliage(build, 10, foliageAttach{pos: treePos{8, 80, 8}, radiusOffset: int32(id % 2), heightOffset: int32(id % 2), doubleTrunk: id&2 != 0}, height, radius)
			wantDigest, tail = strings.Join(fields[2:4], " "), num(4)
		}
		if got := treeGoldenDigest(region, src.drop); got != wantDigest {
			t.Errorf("%s %d seed %d: changes %s, vanilla %s", kind, id, seed, got, wantDigest)
		}
		if got := region.rng.NextInt(); int64(got) != tail {
			t.Errorf("%s %d seed %d: next draw %d, vanilla %d", kind, id, seed, got, tail)
		}
		checked++
	}
	if err := scan.Err(); err != nil {
		t.Fatal(err)
	}
	if checked != 112 {
		t.Fatalf("checked %d records, want 112", checked)
	}
}
