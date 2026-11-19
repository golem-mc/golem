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

func TestBiomeTagIDsSurviveReload(t *testing.T) {
	before := make(map[string]BiomeTagID, len(baseBiomeTagNames))
	for _, tag := range baseBiomeTagNames {
		before[tag] = BiomeTagOf(tag)
	}
	problems := applyForTest(t, PackTags{"minecraft:worldgen/biome": {
		"aaa:pack_only": {Entries: []PackTagEntry{req("minecraft:desert")}},
	}})
	if len(problems) != 0 {
		t.Fatal(problems)
	}
	for tag, id := range before {
		if got := BiomeTagOf(tag); got != id {
			t.Fatalf("%s ID changed from %d to %d", tag, id, got)
		}
	}
	extra := BiomeTagOf("aaa:pack_only")
	if extra == noBiomeTag || int(extra) < len(baseBiomeTagNames) || !BiomeDesert.InTag(extra) || BiomePlains.InTag(extra) {
		t.Fatal("pack-only tag was not resolved after built-in tags")
	}
}
