package world

import (
	"crypto/sha256"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// TestTrialChamberStartsMatchVanilla grows the trial chambers vanilla saved and
// compares each start's NBT: every piece's template, rotation, position,
// bounding box and junctions, in order.
// vanilla: JigsawStructure.findGenerationPoint
func TestTrialChamberStartsMatchVanilla(t *testing.T) {
	t.Parallel()
	checkJigsawStarts(t, "minecraft:trial_chambers", "testdata/trial_chamber_starts.txt")
}

// TestAncientCityStartsMatchVanilla does the same for the ancient cities of
// seed 12345, which use list and feature elements and block_rot.
// vanilla: JigsawStructure.findGenerationPoint
func TestAncientCityStartsMatchVanilla(t *testing.T) {
	t.Parallel()
	checkJigsawStarts(t, "minecraft:ancient_city", "testdata/ancient_city_starts.txt")
}

// TestPillagerOutpostStartsMatchVanilla does the same for the pillager
// outposts, whose start sits on the surface, whose pieces are legacy elements
// with one terrain-matching plate, and which use the expansion hack.
// vanilla: JigsawStructure.findGenerationPoint
func TestPillagerOutpostStartsMatchVanilla(t *testing.T) {
	t.Parallel()
	checkJigsawStarts(t, "minecraft:pillager_outpost", "testdata/pillager_outpost_starts.txt")
}

// TestVillageStartsMatchVanilla does the same for the villages of every
// biome, whose pools are legacy elements with terrain-matching streets and
// feature elements; a line names its structure after the digest.
// vanilla: JigsawStructure.findGenerationPoint
func TestVillageStartsMatchVanilla(t *testing.T) {
	t.Parallel()
	checkJigsawStarts(t, "", "testdata/village_starts.txt")
}

// checkJigsawStarts compares the starts in file with ours; id names the
// structure unless a line ends with its own.
func checkJigsawStarts(t *testing.T, id, file string) {
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for line := range strings.SplitSeq(string(data), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		var seed int64
		var cx, cz int32
		var pieces int
		var sum string
		var lineID string
		if _, err := fmt.Sscan(line, &seed, &cx, &cz, &pieces, &sum, &lineID); err != nil && (sum == "" || lineID == "" && id == "") {
			t.Fatalf("%q: %v", line, err)
		}
		if lineID == "" {
			lineID = id
		}
		def, id := jigsawStructureNamed(lineID), lineID
		n++
		start := buildJigsawStart(def, seed, cx, cz, worldMinY, worldMinY+worldHeight-1, NewOverworldGenerator(seed).firstFreeHeight)
		if start == nil {
			t.Fatalf("seed %d start (%d, %d): no start", seed, cx, cz)
		}
		enc, err := nbt.Encode("", start.nbt(id))
		if err != nil {
			t.Fatal(err)
		}
		if got := fmt.Sprintf("%x", sha256.Sum256(enc)); len(start.pieces) != pieces || got != sum {
			t.Errorf("seed %d start (%d, %d): %d pieces, NBT digest %s; vanilla %d pieces, %s",
				seed, cx, cz, len(start.pieces), got, pieces, sum)
		}
	}
	if n == 0 {
		t.Fatal("no starts")
	}
}

// trialChamberChunks generates the chunks around the first trial chamber start
// of seed 0 that the chamber's pieces reach.
func trialChamberChunks(g *OverworldGenerator) []*Chunk {
	const cx, cz = -22, -20
	var out []*Chunk
	for z := int32(cz - 3); z <= cz+3; z++ {
		for x := int32(cx - 3); x <= cx+3; x++ {
			c := newChunk(x, z)
			g.Generate(c)
			out = append(out, c)
		}
	}
	return out
}

// TestTrialChamberPlacesBlockEntities generates the chunks of a trial chamber
// and checks what its pieces leave in them: loot seeds on containers, decorated
// pots among them, trial spawners and vaults kept as carried compounds.
// vanilla: StructureTemplate.placeInWorld
func TestTrialChamberPlacesBlockEntities(t *testing.T) {
	t.Parallel()
	g := NewOverworldGenerator(0)
	seeded := map[BlockEntityKind]int{}
	carried := map[string]int{}
	var pots int
	// Seeds of the vanilla 26.3 world: a pot draws from the placement random
	// like any container, so each later container's seed depends on it.
	wantPots := map[[3]int32]int64{
		{-326, -1, -356}: 8393533660682399528,
		{-323, 4, -359}:  -8558504434533287038,
	}
	for _, c := range trialChamberChunks(g) {
		for _, b := range c.blockEntities() {
			if want, ok := wantPots[[3]int32{b.X, b.Y, b.Z}]; ok {
				delete(wantPots, [3]int32{b.X, b.Y, b.Z})
				if b.Kind != KindDecoratedPot || b.LootTableSeed != want {
					t.Errorf("%s at %d %d %d: seed %d, want a decorated pot with seed %d", b.Kind, b.X, b.Y, b.Z, b.LootTableSeed, want)
				}
			}
			switch b.Kind {
			case KindDecoratedPot:
				pots++
				fallthrough
			case KindBarrel, KindDispenser, KindHopper:
				if b.LootTable != "" && b.LootTableSeed != 0 {
					seeded[b.Kind]++
				}
			}
		}
		for _, item := range c.carriedBlockEntities.Items {
			entry := item.(nbt.Compound)
			x, y, z := carriedPos(entry)
			id := string(entry["id"].(nbt.String))
			if got := registry.BlockOf(c.BlockAt(x&15, y, z&15)).Name(); got != id {
				t.Errorf("carried %s at %d %d %d sits on %s", id, x, y, z, got)
			}
			carried[id]++
		}
	}
	for _, id := range []string{"minecraft:trial_spawner", "minecraft:vault"} {
		if carried[id] == 0 {
			t.Errorf("no carried %s", id)
		}
	}
	if pots == 0 {
		t.Error("no decorated pot")
	}
	if seeded[KindDecoratedPot] == 0 {
		t.Error("no decorated pot with a loot table seed")
	}
	for pos := range wantPots {
		t.Errorf("no block entity at %v", pos)
	}
	if len(seeded) == 0 {
		t.Error("no container with a loot table seed")
	}
}

// BenchmarkTrialChambersGenerate measures generating the chunks around the
// seed 0 trial chamber at (-22, -20), which hold its start and pieces.
func BenchmarkTrialChambersGenerate(b *testing.B) {
	g := NewOverworldGenerator(0)
	b.ReportAllocs()
	for b.Loop() {
		trialChamberChunks(g)
	}
}

// TestAncientCityGrowsSculk generates the chunks around the seed 12345 ancient
// city at (-18, -18) and checks its sculk patches left sculk, veins and
// shriekers that can summon.
// vanilla: SculkPatchFeature.place
func TestAncientCityGrowsSculk(t *testing.T) {
	t.Parallel()
	g := NewOverworldGenerator(12345)
	counts := map[string]int{}
	var summoning int
	for z := int32(-22); z <= -14; z++ {
		for x := int32(-22); x <= -14; x++ {
			c := newChunk(x, z)
			g.Generate(c)
			for y := int32(worldMinY); y < 0; y++ {
				for bz := range int32(16) {
					for bx := range int32(16) {
						state := c.BlockAt(bx, y, bz)
						name := registry.BlockOf(state).Name()
						counts[name]++
						if name == "minecraft:sculk_shrieker" && registry.StateProps(state)["can_summon"] == "true" {
							summoning++
						}
					}
				}
			}
		}
	}
	for _, name := range []string{"minecraft:sculk", "minecraft:sculk_vein", "minecraft:sculk_catalyst"} {
		if counts[name] == 0 {
			t.Errorf("no %s", name)
		}
	}
	if summoning == 0 {
		t.Error("no shrieker that can summon")
	}
}
