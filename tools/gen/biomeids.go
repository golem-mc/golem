package main

import "github.com/golem-mc/golem/tools/internal/gogen"

func init() {
	register(generator{
		name: "biomeids",
		doc:  "registry/biomeids_gen.go: one Biome constant per minecraft:worldgen/biome entry",
		run:  runBiomeIDs,
	})
}

func runBiomeIDs(e *env) error {
	names, err := syncedEntries(e, "minecraft:worldgen/biome")
	if err != nil {
		return err
	}
	idents, err := idents("Biome", names)
	if err != nil {
		return err
	}
	f := e.file("registry", "by tools/gen biomeids from the minecraft:worldgen/biome entries of the registries table")
	f.WriteString("// The biomes by minecraft:worldgen/biome ID, which is what a chunk stores.\nconst (\n")
	for id, ident := range idents {
		f.Printf("\t%s Biome = %d\n", ident, id)
	}
	f.Printf(")\n\n// BiomeCount is the number of biomes in the registry.\nconst BiomeCount = %d\n\n", len(names))
	f.WriteString("var biomeNames = [BiomeCount]string{\n")
	for _, n := range names {
		f.Printf("\t%s,\n", gogen.Quote(n))
	}
	f.WriteString("}\n")
	e.logf("biomes: %d", len(names))
	return e.emit("registry/biomeids_gen.go", f)
}
