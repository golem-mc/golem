package registry

import "testing"

func TestBiomeTagBitsetsMatchRegistry(t *testing.T) {
	for _, r := range Tags {
		if r.Registry != "minecraft:worldgen/biome" {
			continue
		}
		for _, tag := range r.Tags {
			id := BiomeTagOf(tag.Name)
			for b := range Biome(BiomeCount) {
				if got, want := b.InTag(id), BiomeInTag(b.Name(), tag.Name); got != want {
					t.Fatalf("%s in %s: bitset %v, registry %v", b.Name(), tag.Name, got, want)
				}
			}
		}
	}
	if !BiomeDesert.InTag(BiomeTagIDSpawnsWarmVariantFarmAnimals) || BiomeTaiga.InTag(BiomeTagIDSpawnsWarmVariantFarmAnimals) {
		t.Fatal("warm farm animal tag membership is wrong")
	}
	for _, b := range []Biome{NoBiome, BiomeCount} {
		if b.InTag(BiomeTagIDIsJungle) {
			t.Fatalf("out-of-range biome %d reported a member", b)
		}
	}
	if BiomeDesert.InTag(BiomeTagOf("test:unknown")) || BiomeDesert.InTag(noBiomeTag-1) {
		t.Fatal("unknown or out-of-range tag reported a member")
	}
}
