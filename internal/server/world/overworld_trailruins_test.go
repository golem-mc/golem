package world

import (
	"crypto/sha256"
	"fmt"
	"testing"

	"github.com/golem-mc/golem/internal/server/world/levelgen"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

func TestTrailRuinsVanilla(t *testing.T) {
	t.Parallel()
	// Recorded from the reload-saved vanilla seed-0 cache, chunk (-27,44).
	// Only the start chunk is generated; its pieces extend into adjacent chunks.
	c := newChunk(-27, 44)
	NewOverworldGenerator(0).Generate(c)
	h := sha256.New()
	suspicious, bricks := 0, 0
	for y := int32(worldMinY); y < worldMinY+worldHeight; y++ {
		for z := range int32(16) {
			for x := range int32(16) {
				switch b := registry.BlockOf(c.BlockAt(x, y, z)); b {
				case registry.BlockSuspiciousGravel:
					suspicious++
					tag, ok := c.carriedBlockEntityAt(c.x*16+x, y, c.z*16+z)
					if !ok {
						t.Fatalf("no brushable entity at %d,%d,%d", x, y, z)
					}
					wantSeed := levelgen.NewLegacyRandom(blockPosSeed(c.x*16+x, y, c.z*16+z)).NextLong()
					if tag["id"] != nbt.String(KindBrushable.Name()) || tag["LootTableSeed"] != nbt.Long(wantSeed) || tag["LootTable"] != nbt.String("minecraft:archaeology/trail_ruins_common") && tag["LootTable"] != nbt.String("minecraft:archaeology/trail_ruins_rare") {
						t.Fatalf("archaeology data: %v", tag)
					}

				case registry.BlockBricks, registry.BlockMudBricks:
					bricks++
					fmt.Fprintf(h, "%d,%d,%d:%s\n", c.x*16+x, y, c.z*16+z, b.Name())
				}
			}
		}
	}
	if suspicious != 31 || bricks != 136 || fmt.Sprintf("%x", h.Sum(nil)) != "e94e7d652d7e52823691bcbe9b851d655b2c8d77c3da4b15c2727ac46096c541" {
		t.Fatalf("suspicious gravel %d, bricks %d, digest %x", suspicious, bricks, h.Sum(nil))
	}
}

func TestTemplateOrdersTopSlabsAfterFullCubes(t *testing.T) {
	tag, err := nbt.ParseSNBT(`{palette:[{Name:"minecraft:mud_brick_slab",Properties:{type:"top",waterlogged:"false"}},{Name:"minecraft:gravel"}],blocks:[{pos:[0,0,0],state:0},{pos:[0,1,0],state:1}]}`)
	if err != nil {
		t.Fatal(err)
	}
	root := tag
	blocks := loadTemplatePalette(root["palette"].(nbt.List), root["blocks"].(nbt.List))
	if registry.BlockOf(blocks[0].State) != registry.BlockGravel || registry.BlockOf(blocks[1].State) != registry.BlockMudBrickSlab {
		t.Fatalf("placement order: %v", blocks)
	}
}
