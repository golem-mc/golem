package rules

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/world/levelgen"
	"github.com/golem-mc/golem/registry"
)

// TestSheepSpawnColourFollowsTheBiome rolls each palette of the sheep's
// spawn rule twenty thousand times against a seeded source and holds the
// shares to the ones the vanilla weighted lists give: the common colour
// its eighty-two of every hundred, the four leftover dyes their five,
// five, five and three, and the pink the one of every five hundred common
// births it rides on. No colour outside the six may ever come out, and the
// biome decides which palette rolls — the warm one the nether's and the
// desert's biomes, the cold one the end's and the snowy ones, temperate
// everywhere else, a biome the registry has never heard of included.
func TestSheepSpawnColourFollowsTheBiome(t *testing.T) {
	cases := []struct {
		name   string
		biome  string
		shares map[string][2]float64
	}{
		{
			name:  "temperate plains",
			biome: "minecraft:plains",
			shares: map[string][2]float64{
				"white":      {0.79, 0.85},
				"black":      {0.04, 0.06},
				"gray":       {0.04, 0.06},
				"light_gray": {0.04, 0.06},
				"brown":      {0.02, 0.04},
				"pink":       {0.0, 0.005},
			},
		},
		{
			name:  "warm nether",
			biome: "minecraft:nether_wastes",
			shares: map[string][2]float64{
				"brown":      {0.79, 0.85},
				"gray":       {0.04, 0.06},
				"light_gray": {0.04, 0.06},
				"white":      {0.04, 0.06},
				"black":      {0.02, 0.04},
				"pink":       {0.0, 0.005},
			},
		},
		{
			name:  "warm desert",
			biome: "minecraft:desert",
			shares: map[string][2]float64{
				"brown":      {0.79, 0.85},
				"gray":       {0.04, 0.06},
				"light_gray": {0.04, 0.06},
				"white":      {0.04, 0.06},
				"black":      {0.02, 0.04},
				"pink":       {0.0, 0.005},
			},
		},
		{
			name:  "cold end",
			biome: "minecraft:the_end",
			shares: map[string][2]float64{
				"black":      {0.79, 0.85},
				"light_gray": {0.04, 0.06},
				"gray":       {0.04, 0.06},
				"white":      {0.04, 0.06},
				"brown":      {0.02, 0.04},
				"pink":       {0.0, 0.005},
			},
		},
		{
			name:  "cold taiga",
			biome: "minecraft:taiga",
			shares: map[string][2]float64{
				"black":      {0.79, 0.85},
				"light_gray": {0.04, 0.06},
				"gray":       {0.04, 0.06},
				"white":      {0.04, 0.06},
				"brown":      {0.02, 0.04},
				"pink":       {0.0, 0.005},
			},
		},
		{
			name:  "a biome the registry has never heard of",
			biome: "minecraft:nowhere",
			shares: map[string][2]float64{
				"white":      {0.79, 0.85},
				"black":      {0.04, 0.06},
				"gray":       {0.04, 0.06},
				"light_gray": {0.04, 0.06},
				"brown":      {0.02, 0.04},
				"pink":       {0.0, 0.005},
			},
		},
	}

	// The ten dyes no palette of the spawn rules offers a newborn sheep.
	never := map[string]bool{
		"orange": true, "magenta": true, "light_blue": true, "yellow": true,
		"lime": true, "cyan": true, "purple": true, "blue": true,
		"green": true, "red": true,
	}

	const rolls = 20000
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rng := levelgen.NewXoroshiroRandom(1)
			counts := make(map[string]int, 6)
			for range rolls {
				counts[SheepSpawnColour(testBiome(tc.biome), rng)]++
			}
			for colour, span := range tc.shares {
				got := float64(counts[colour]) / rolls
				if got < span[0] || got > span[1] {
					t.Errorf("%s came out %d of %d rolls (%.4f), want within %v..%v",
						colour, counts[colour], rolls, got, span[0], span[1])
				}
			}
			for colour := range never {
				if counts[colour] != 0 {
					t.Errorf("%s came out %d times, a colour no palette of the spawn rules offers", colour, counts[colour])
				}
			}
		})
	}
}

// TestSelectVariantToSpawnFollowsTheBiome holds the variant pick to what a
// vanilla server chose for unsummoned cows, pigs and chickens: cold in
// snowy plains, warm in the desert, temperate in plains.
func TestSelectVariantToSpawnFollowsTheBiome(t *testing.T) {
	rng := levelgen.NewXoroshiroRandom(1)
	for _, reg := range []string{"minecraft:pig_variant", "minecraft:cow_variant", "minecraft:chicken_variant"} {
		for biome, want := range map[string]string{
			"minecraft:snowy_plains":  "minecraft:cold",
			"minecraft:taiga":         "minecraft:cold",
			"minecraft:desert":        "minecraft:warm",
			"minecraft:nether_wastes": "minecraft:warm",
			"minecraft:plains":        "minecraft:temperate",
			"minecraft:no_such_biome": "minecraft:temperate",
		} {
			for range 20 {
				got, ok := SelectVariantToSpawn(reg, SpawnContext{Biome: testBiome(biome)}, rng)
				if !ok || got != want {
					t.Fatalf("SelectVariantToSpawn(%s, %s) = (%s, %v), want (%s, true)", reg, biome, got, ok, want)
				}
			}
		}
	}
}

// TestSelectVariantToSpawnConditions checks moon_brightness and structure
// conditions on cat_variant: swamp_hut forces all_black at priority 1, full
// moon includes all_black at priority 0, and a dark moon excludes it.
func TestSelectVariantToSpawnConditions(t *testing.T) {
	rng := levelgen.NewXoroshiroRandom(7)
	inHut := SpawnContext{
		Biome:          testBiome("minecraft:swamp"),
		MoonBrightness: 0,
		InStructure:    func(id string) bool { return id == "minecraft:swamp_hut" },
	}
	for range 20 {
		got, ok := SelectVariantToSpawn("minecraft:cat_variant", inHut, rng)
		if !ok || got != "minecraft:all_black" {
			t.Fatalf("hut cat = (%s, %v), want (minecraft:all_black, true)", got, ok)
		}
	}
	darkMoon := SpawnContext{Biome: testBiome("minecraft:plains"), MoonBrightness: 0.5}
	for range 200 {
		got, ok := SelectVariantToSpawn("minecraft:cat_variant", darkMoon, rng)
		if !ok || got == "minecraft:all_black" {
			t.Fatalf("dark moon cat = (%s, %v), want non-all_black", got, ok)
		}
	}
	fullMoon := SpawnContext{Biome: testBiome("minecraft:plains"), MoonBrightness: 1.0}
	sawAllBlack := false
	for range 200 {
		got, ok := SelectVariantToSpawn("minecraft:cat_variant", fullMoon, rng)
		if !ok {
			t.Fatal("full moon cat returned no match")
		}
		if got == "minecraft:all_black" {
			sawAllBlack = true
			break
		}
	}
	if !sawAllBlack {
		t.Error("full moon never drew minecraft:all_black in 200 rolls")
	}
}

// testBiome is the biome registered under name, NoBiome for an unknown one.
func testBiome(name string) registry.Biome {
	b, _ := registry.BiomeByName(name)
	return b
}
