package world

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

type templeTerrain struct {
	*goldenTerrain
	mode int32
}

func (t *templeTerrain) cachedTerrain(cx, cz int32) *shapedTerrain {
	t.mu.Lock()
	defer t.mu.Unlock()
	pos := ChunkPos{cx, cz}
	if c := t.chunks[pos]; c != nil {
		return c
	}
	c := newChunk(cx, cz)
	for x := range int32(16) {
		for z := range int32(16) {
			height := int32(65)
			if t.mode != 0 {
				height += floorMod((cx*16+x)*3+(cz*16+z)*5, 7)
			}
			for y := int32(worldMinY); y < height; y++ {
				c.SetBlock(x, y, z, registry.BlockStone.DefaultState())
			}
		}
	}
	s := &shapedTerrain{}
	s.keep(c, false)
	t.chunks[pos] = s
	return s
}
func (t *templeTerrain) terrainOf(cx, cz int32, _ *Chunk) *shapedTerrain {
	return t.cachedTerrain(cx, cz)
}

// TestJungleTempleGolden compares the jungle temple's placement and starts
// with the vanilla recording, read once for both.
func TestJungleTempleGolden(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("testdata/jungletemple_golden.txt")
	if err != nil {
		t.Fatal(err)
	}
	t.Run("JungleTemple", func(t *testing.T) {
		t.Parallel()
		testJungleTemplePlacements(t, data)
	})
	t.Run("JungleTempleStarts", func(t *testing.T) {
		t.Parallel()
		testJungleTempleStarts(t, data)
	})
}

func testJungleTemplePlacements(t *testing.T, data []byte) {
	cases := 0
	for line := range strings.SplitSeq(string(data), "\n") {
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "start ") {
			continue
		}
		var seed int64
		var mode, dir, reverse, height, containers int
		var states, draws, loot, id string
		var flags [4]bool
		if _, err := fmt.Sscanf(line, "temple %d %d %d %d %s %s %d %s %d %s %t %t %t %t", &seed, &mode, &dir, &reverse, &states, &draws, &containers, &loot, &height, &id, &flags[0], &flags[1], &flags[2], &flags[3]); err != nil {
			t.Fatal(err)
		}
		cases++
		// A half fraction of the seed x terrain x direction x order grid: every
		// level of every factor is still checked.
		if (int(seed&1)+mode+dir+reverse)%2 != 0 {
			continue
		}
		t.Run(fmt.Sprintf("%d/%d/%d/%d", seed, mode, dir, reverse), func(t *testing.T) {
			t.Parallel()
			src := &templeTerrain{newGoldenTerrain(), int32(mode)}
			p := newJungleTemple(levelgen.NewLegacyRandom(seed), -3, 7)
			p.dir = dir
			w, d := int32(12), int32(15)
			if dir&1 != 0 {
				w, d = d, w
			}
			p.box = jbox{-3, 64, 7, -3 + w - 1, 73, 7 + d - 1}
			regions := map[ChunkPos]*decorRegion{}
			randoms := sha256.New()
			for i := range 4 {
				index := i
				if reverse != 0 {
					index = 3 - i
				}
				cx, cz := int32(index%2-1), int32(index/2)
				r := newDecorRegion(src, newChunk(cx, cz), worldMinY, worldHeight)
				r.nx, r.nz = cx, cz
				r.bindProtos(ChunkPos{cx, cz})
				r.rng.SetSeed(seed + int64(cx)*31 + int64(cz)*17)
				p.place(r, jbox{cx * 16, -64, cz * 16, cx*16 + 15, 319, cz*16 + 15}, [3]int32{})
				randoms.Write(binary.BigEndian.AppendUint32(nil, uint32(r.rng.NextInt())))
				regions[ChunkPos{cx, cz}] = r
			}
			hash := sha256.New()
			lootHash := sha256.New()
			gotContainers := 0
			for x := int32(-16); x < 16; x++ {
				for y := int32(56); y < 88; y++ {
					for z := range int32(32) {
						r := regions[ChunkPos{x >> 4, z >> 4}]
						state := r.get(x, y, z)
						hash.Write(goldenStateBytes()[state])
						for _, be := range r.protoAt(x, z).entities {
							if be.x == x&15 && be.y == y-r.minY && be.z == z&15 {
								table, _ := nbt.Get[nbt.String](be.tag, "LootTable")
								lootSeed, _ := nbt.LongValue(be.tag["LootTableSeed"])
								fmt.Fprintf(lootHash, "%d,%d,%d:%s:%d\n", x, y, z, table, lootSeed)
								gotContainers++
							}
						}
					}
				}
			}
			if got := fmt.Sprintf("%x", hash.Sum(nil)); got != states {
				t.Errorf("block states: %s, vanilla %s", got, states)
			}
			if got := fmt.Sprintf("%x", randoms.Sum(nil)); got != draws {
				t.Errorf("random draws: %s, vanilla %s", got, draws)
			}
			if got := fmt.Sprintf("%x", lootHash.Sum(nil)); got != loot || gotContainers != containers {
				t.Errorf("loot: %d %s, vanilla %d %s", gotContainers, got, containers, loot)
			}
			tag := p.nbt()
			if tag["id"] != nbt.String(id) || tag["HPos"] != nbt.Int(height) || tag["O"] != nbt.Int((dir+2)&3) {
				t.Errorf("saved piece: %v", tag)
			}
			for i, key := range []string{"placedMainChest", "placedHiddenChest", "placedTrap1", "placedTrap2"} {
				if nbt.BoolField(tag, key) != flags[i] {
					t.Errorf("%s: %v, vanilla %v", key, tag[key], flags[i])
				}
			}
			if box := p.bounds(); box.minY != int32(height) || box.maxY != int32(height+9) {
				t.Errorf("adjusted box: %v", box)
			}
		})
	}
	if cases != 32 {
		t.Fatalf("got %d cases, want 32", cases)
	}
}

func testJungleTempleStarts(t *testing.T, data []byte) {
	generators := map[int64]*OverworldGenerator{0: NewOverworldGenerator(0), 12345: NewOverworldGenerator(12345)}
	cases, starts := 0, 0
	for line := range strings.SplitSeq(string(data), "\n") {
		if !strings.HasPrefix(line, "start ") {
			continue
		}
		var seed int64
		var cx, cz int32
		var status string
		if _, err := fmt.Sscanf(line, "start %d %d %d %s", &seed, &cx, &cz, &status); err != nil {
			t.Fatal(err)
		}
		cases++
		var got *structureStart
		for _, s := range generators[seed].structureStartsIn(cx, cz) {
			if s.def.Name == "minecraft:jungle_pyramid" {
				got = s
			}
		}
		if (got != nil) != (status == "present") {
			t.Errorf("seed %d chunk %d,%d: got start %v, vanilla %s", seed, cx, cz, got, status)
			continue
		}
		if got == nil {
			continue
		}
		starts++
		var orientation int32
		var b jbox
		if _, err := fmt.Sscanf(line, "start %d %d %d present %d %d %d %d %d %d %d", &seed, &cx, &cz, &orientation, &b.minX, &b.minY, &b.minZ, &b.maxX, &b.maxY, &b.maxZ); err != nil {
			t.Fatal(err)
		}
		if len(got.pieces) != 1 || got.pieces[0].bounds() != b || got.pieces[0].nbt()["O"] != nbt.Int(orientation) {
			t.Errorf("seed %d chunk %d,%d: piece differs from %s", seed, cx, cz, line)
		}
	}
	if cases != 98 || starts == 0 {
		t.Fatalf("checked %d candidates, %d starts", cases, starts)
	}
}
