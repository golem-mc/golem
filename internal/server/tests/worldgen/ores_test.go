package worldgen

import (
	"testing"

	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// The 5x5 window keeps hundreds of blocks of every ore and blob, enough for
// the existing ±15% yield bounds and height/variant assertions.
// The overworld's ores and stone blobs, black-box: a seeded world is read
// back through the server's own chunk path over the 5x5 chunks around the
// origin, and every ore has to stand in the Y band vanilla keeps it in, with
// the deepslate variant of it below y 0 and none of it outside the band, and
// the blobs of coarse rock have to be through the stone. The per-chunk counts
// are pinned to this seed's measured yield within ±15%, so a size or count
// regression fails.

// oreBand is one ore's place in the ground: the two states a vein of it
// writes — the plain ore and the deepslate variant — the Y band vanilla
// holds it to. want is this seed's measured per-chunk yield of the band;
// highY and maxHigh check a triangular band, where only a small share of
// the ore lands above highY (0 disables the check).
type oreBand struct {
	plain, deepslate string
	lo, hi           int32
	want             float64
	highY            int32
	maxHigh          float64
}

// scanOres walks the chunks of the given server counting the states of the
// bands: per ore, how many of its blocks stood inside the band, how many
// outside it, how many below y 0 wore the deepslate variant, and how many
// above the band's highY.
func scanOres(t *testing.T, srv *server.Server, bands []oreBand) []oreScan {
	t.Helper()

	// Which band a state belongs to, and which of its two states is the
	// deepslate one. Only a dozen states are in play, so maps keep the
	// lookup small rather than a 64k-entry table.
	oreOf := make(map[uint16]int, 2*len(bands))
	deepOf := make(map[uint16]int, len(bands))
	for i, band := range bands {
		plain, ok := registry.FindState(band.plain, nil)
		if !ok {
			t.Fatalf("the block registry has no %s", band.plain)
		}
		deep, ok := registry.FindState(band.deepslate, nil)
		if !ok {
			t.Fatalf("the block registry has no %s", band.deepslate)
		}
		// Both variants count towards the band; only the deepslate one
		// counts towards the below-zero tally.
		oreOf[plain], oreOf[deep], deepOf[deep] = i, i, i
	}

	scanned := make([]oreScan, len(bands))
	srv.ScanOverworldBlocks(-32, -32, 47, 47, -64, 319, func(y int32, state uint16) {
		i, ok := oreOf[state]
		if !ok {
			return
		}
		if y < bands[i].lo || y > bands[i].hi {
			scanned[i].outside++
		} else {
			scanned[i].inBand++
		}
		if deepOf[state] == i && y < 0 {
			scanned[i].deepslate++
		}
		if bands[i].highY != 0 && y > bands[i].highY {
			scanned[i].high++
		}
	})
	return scanned
}

// oreScan is what one ore's states added up to over the scanned area.
type oreScan struct {
	inBand, outside, deepslate, high int
}

// overworldGrowsTheOtherOres checks the half of the underground the
// generator never wrote: diamond, gold, redstone, lapis and copper in their
// vanilla Y bands, each deepslate-clad where its band dips below y 0, none
// of them straying outside its band, and each near this seed's measured
// amount per chunk. Diamond is a triangle: it peaks deep and only a small
// share of it lands above y -16.
func overworldGrowsTheOtherOres(t *testing.T, srv *server.Server) {
	bands := []oreBand{
		{plain: "minecraft:gold_ore", deepslate: "minecraft:deepslate_gold_ore", lo: -64, hi: 32, want: 24.8},
		{plain: "minecraft:redstone_ore", deepslate: "minecraft:deepslate_redstone_ore", lo: -64, hi: 15, want: 35.1},
		{plain: "minecraft:lapis_ore", deepslate: "minecraft:deepslate_lapis_ore", lo: -64, hi: 64, want: 21.8},
		{plain: "minecraft:diamond_ore", deepslate: "minecraft:deepslate_diamond_ore", lo: -64, hi: 16, want: 24.5, highY: -16, maxHigh: 0.25},
		{plain: "minecraft:copper_ore", deepslate: "minecraft:deepslate_copper_ore", lo: -16, hi: 112, want: 67.6},
	}
	got := scanOres(t, srv, bands)
	for i, band := range bands {
		perChunk := float64(got[i].inBand) / 25
		t.Logf("%s: %.1f blocks per chunk in band %d..%d (want ~%.1f), %d outside, %d deepslate below y 0",
			band.plain, perChunk, band.lo, band.hi, band.want, got[i].outside, got[i].deepslate)
		// The wants are this seed's measured yield; blobs clipped by the
		// world floor and by caves land under the nominal. The ±15% band
		// still catches one that moves.
		if perChunk < 0.85*band.want || perChunk > 1.15*band.want {
			t.Errorf("%s: %.1f blocks per chunk, want about %.1f (this seed's measured yield)", band.plain, perChunk, band.want)
		}
		// A blob's spheres reach a few blocks past its origin's band.
		if got[i].outside*100 > got[i].inBand {
			t.Errorf("%s: %d blocks outside its band %d..%d", band.plain, got[i].outside, band.lo, band.hi)
		}
		if got[i].deepslate < 100 {
			t.Errorf("%s: %d deepslate blocks below y 0, want the deepslate variant the band's low end owes",
				band.deepslate, got[i].deepslate)
		}
		if band.maxHigh > 0 {
			frac := float64(got[i].high) / float64(got[i].inBand)
			t.Logf("%s: %.0f%% of it above y %d", band.plain, 100*frac, band.highY)
			if frac > band.maxHigh {
				t.Errorf("%s: %.0f%% above y %d, want a triangle peaking below it (at most %.0f%%)",
					band.plain, 100*frac, band.highY, 100*band.maxHigh)
			}
		}
	}
}

// overworldEmeraldOnlyInMountainBiomes checks that emerald ore appears
// only beside a biome that lists it. vanilla: BiomeDefaultFeatures.addExtraEmeralds
func overworldEmeraldOnlyInMountainBiomes(t *testing.T, srv *server.Server) {
	emerald := map[uint16]bool{
		registry.BlockStates["minecraft:emerald_ore"]:           true,
		registry.BlockStates["minecraft:deepslate_emerald_ore"]: true,
	}
	mountain := map[string]bool{
		"minecraft:snowy_slopes": true, "minecraft:windswept_gravelly_hills": true,
		"minecraft:grove": true, "minecraft:windswept_hills": true,
		"minecraft:windswept_forest": true, "minecraft:meadow": true,
		"minecraft:jagged_peaks": true, "minecraft:stony_peaks": true,
		"minecraft:cherry_grove": true, "minecraft:frozen_peaks": true,
	}
	// Chunk (7, -8) contains emeralds beside mountain biomes.
	count := 0
	srv.ScanOverworldBlocksAt(112, -128, 127, -113, -64, 319, func(x, y, z int32, state uint16) {
		if !emerald[state] {
			return
		}
		count++
		// A vein spreads a block or two from the origin whose biome gates it;
		// the biome can change within that reach vertically as well.
		for dx := int32(-4); dx <= 4; dx += 2 {
			for dz := int32(-4); dz <= 4; dz += 2 {
				for dy := int32(-4); dy <= 4; dy += 2 {
					if mountain[srv.OverworldBiomeAt(x+dx, y+dy, z+dz)] {
						return
					}
				}
			}
		}
		t.Errorf("emerald ore at %d %d %d, %s, with no mountain biome beside it", x, y, z, srv.OverworldBiomeAt(x, y, z))
	})
	if count == 0 {
		t.Fatal("no emerald in chunk (7, -8), so the biome filter was not checked")
	}
	t.Logf("minecraft:emerald_ore: %d blocks in chunk (7, -8)", count)
}

// overworldOresGetTheDeepslateVariant checks that a vein whose band runs
// into the deepslate writes the deepslate block: iron (up to y 40) crosses
// the boundary, so the deepslate_iron_ore below y 0 is the marker.
func overworldOresGetTheDeepslateVariant(t *testing.T, srv *server.Server) {
	deepIron := registry.BlockStates["minecraft:deepslate_iron_ore"]
	count := 0
	srv.ScanOverworldBlocks(-32, -32, 47, 47, -64, -1, func(y int32, state uint16) {
		if state == deepIron {
			count++
		}
	})
	t.Logf("minecraft:deepslate_iron_ore: %d blocks below y 0 over the 5x5 chunks", count)
	if count < 200 {
		t.Errorf("only %d deepslate_iron_ore blocks below y 0 over the 5x5 chunks, want iron's deepslate variant", count)
	}
}

// overworldScattersStoneBlobs checks the coarse rock scattered through
// the stone: granite, diorite and andesite in the upper ground, tuff by the
// deepslate, and pockets of dirt and gravel deep enough down — below y 40,
// where no soil reaches, a surface block at 50 or more with three or four of
// soil under it — that only a blob put them there. Each kind is pinned to
// this seed's measured per-chunk amount within ±15%, so a size or count
// regression fails.
func overworldScattersStoneBlobs(t *testing.T, srv *server.Server) {
	blobs := []struct {
		name  string
		state uint16
		want  float64
	}{
		{"minecraft:granite", registry.BlockStates["minecraft:granite"], 626.4},
		{"minecraft:diorite", registry.BlockStates["minecraft:diorite"], 671.8},
		{"minecraft:andesite", registry.BlockStates["minecraft:andesite"], 612.6},
		{"minecraft:tuff", registry.BlockStates["minecraft:tuff"], 1146.5},
		{"minecraft:dirt pocket", registry.BlockStates["minecraft:dirt"], 164.6},
		{"minecraft:gravel pocket", registry.BlockStates["minecraft:gravel"], 375.7},
	}
	index := make(map[uint16]int, len(blobs))
	for i, b := range blobs {
		index[b.state] = i
	}
	const yTop = 39 // below any soil, so what is counted is the blobs
	counts := make([]int, len(blobs))
	srv.ScanOverworldBlocks(-32, -32, 47, 47, -64, yTop, func(y int32, state uint16) {
		if i, ok := index[state]; ok {
			counts[i]++
		}
	})
	for i, b := range blobs {
		perChunk := float64(counts[i]) / 25
		t.Logf("%s: %.1f blocks per chunk below y %d (want ~%.1f)", b.name, perChunk, yTop+1, b.want)
		if perChunk < 0.85*b.want || perChunk > 1.15*b.want {
			t.Errorf("%s: %.1f blocks per chunk below y %d of seed 12345, want about %.1f (this seed's measured yield)",
				b.name, perChunk, yTop+1, b.want)
		}
	}
}

// theNetherGrowsVanillaOres sweeps the nether's own generator over the
// 8x8 chunks around the origin, counting the quartz and gold the veins leave
// in the netherrack, per chunk. Vanilla places ore_quartz_nether as 16 veins
// of size 14 and ore_gold_nether as 10 veins of size 10 per chunk, both
// uniform from ten above the floor to ten below the ceiling (a nominal 224
// and 100 blocks); our walk replaces netherrack and stops where a cavern
// opens, so it lands short. Each count is pinned to this seed's measured
// yield within ±15%, so a count, size or band regression fails.
// vanilla: ore_quartz_nether, ore_gold_nether
func theNetherGrowsVanillaOres(t *testing.T, srv *server.Server) {
	ores := []struct {
		name  string
		want  float64
		state uint16
	}{
		{"minecraft:nether_quartz_ore", 180.5, 0},
		{"minecraft:nether_gold_ore", 63.8, 0},
	}
	index := make(map[uint16]int, len(ores))
	for i := range ores {
		state, ok := registry.FindState(ores[i].name, nil)
		if !ok {
			t.Fatalf("the block registry has no %s", ores[i].name)
		}
		ores[i].state = state
		index[state] = i
	}

	const chunks = 64 // the 8x8 around the origin
	counts := make([]int, len(ores))
	for cz := range int32(8) {
		for cx := range int32(8) {
			wx, wz := cx*16, cz*16
			release := srv.HoldChunkInDimension(world.NetherName, wx, wz)
			for x := range int32(16) {
				for z := range int32(16) {
					for y := range int32(128) {
						if i, ok := index[srv.BlockInDimension(world.NetherName, wx+x, y, wz+z)]; ok {
							counts[i]++
						}
					}
				}
			}
			release()
		}
	}
	for i, ore := range ores {
		per := float64(counts[i]) / chunks
		t.Logf("%s: %.1f blocks per chunk (want ~%.1f)", ore.name, per, ore.want)
		if per < 0.85*ore.want || per > 1.15*ore.want {
			t.Errorf("%s: %.1f blocks per chunk of seed 12345, want about %.1f (this seed's measured yield)",
				ore.name, per, ore.want)
		}
	}
}

// theNetherBuriesAncientDebris sweeps the nether's 8x8 chunks around the
// origin for ancient debris: the large veins heaped in y 8..24 and the small
// ones anywhere in the band, and never a block of it with air on one of its
// six faces, across chunk borders too.
// vanilla: ore_ancient_debris_large, ore_debris_small, ScatteredOreFeature
func theNetherBuriesAncientDebris(t *testing.T, srv *server.Server) {
	debris, ok := registry.FindState("minecraft:ancient_debris", nil)
	if !ok {
		t.Fatal("the block registry has no minecraft:ancient_debris")
	}
	air, ok := registry.FindState("minecraft:air", nil)
	if !ok {
		t.Fatal("the block registry has no minecraft:air")
	}

	// The swept chunks are 0..7 on each axis; the ring around them is held
	// too, so the air check reads the generated neighbour across a border.
	for cz := int32(-1); cz <= 8; cz++ {
		for cx := int32(-1); cx <= 8; cx++ {
			t.Cleanup(srv.HoldChunkInDimension(world.NetherName, cx*16, cz*16))
		}
	}
	faces := [6][3]int32{{1, 0, 0}, {-1, 0, 0}, {0, 1, 0}, {0, -1, 0}, {0, 0, 1}, {0, 0, -1}}
	var total, low int
	for x := range int32(128) {
		for z := range int32(128) {
			for y := range int32(128) {
				if srv.BlockInDimension(world.NetherName, x, y, z) != debris {
					continue
				}
				total++
				if y >= 8 && y <= 24 {
					low++
				}
				// The large veins draw y 8..24, the small 8..119, and a
				// try strays at most two blocks from either.
				if y < 6 || y > 121 {
					t.Errorf("ancient debris at (%d, %d, %d), outside y 6..121", x, y, z)
				}
				for _, d := range faces {
					if srv.BlockInDimension(world.NetherName, x+d[0], y+d[1], z+d[2]) == air {
						t.Errorf("ancient debris at (%d, %d, %d) touches air", x, y, z)
						break
					}
				}
			}
		}
	}
	t.Logf("%d ancient debris in 64 chunks, %d of them in y 8..24", total, low)
	if total < 32 {
		t.Errorf("%d ancient debris blocks in 64 chunks, want the two scattered veins every chunk draws", total)
	}
	if low < total/3 {
		t.Errorf("%d of %d ancient debris in y 8..24, want the large veins heaped there", low, total)
	}
}
