package world

import (
	"bufio"
	"crypto/sha256"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/golem-mc/golem/registry"
)

// TestOverworldTerrainGolden exercises the production adapter over every natural-biome
// terrain recording, including material rules, flooded caves and surface cave floors.
func TestOverworldTerrainGolden(t *testing.T) {
	t.Parallel()
	f, err := os.Open("terrain/testdata/overworld_terrain_golden.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	generators := map[int64]*OverworldGenerator{}
	sc := bufio.NewScanner(f)
	checked := 0
	for sc.Scan() {
		v := strings.Fields(sc.Text())
		if len(v) != 7 || v[0] != "terrain" {
			continue
		}
		seed, _ := strconv.ParseInt(v[2], 10, 64)
		cx, _ := strconv.ParseInt(v[3], 10, 32)
		cz, _ := strconv.ParseInt(v[4], 10, 32)
		g := generators[seed]
		if g == nil {
			g = NewOverworldGenerator(seed)
			g.DisableStructures()
			generators[seed] = g
		}
		c := newChunk(int32(cx), int32(cz))
		g.shapeTerrain(c)
		h := sha256.New()
		for x := range int32(16) {
			for z := range int32(16) {
				for y := int32(worldMinY); y < worldMinY+worldHeight; y++ {
					h.Write([]byte(strings.TrimPrefix(registry.BlockOf(c.BlockAt(x, y, z)).Name(), "minecraft:") + "\n"))
				}
			}
		}
		if got := fmt.Sprintf("%x", h.Sum(nil)); got != v[6] {
			t.Errorf("seed %d chunk %d,%d: terrain %s, vanilla %s", seed, cx, cz, got, v[6])
		}
		checked++
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	if checked != 42 {
		t.Fatalf("checked %d chunks, want 42", checked)
	}
}
