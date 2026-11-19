package main

func init() {
	register(generator{
		name: "particleids",
		doc:  "internal/protocol/particleids_gen.go: one constant per minecraft:particle_type entry",
		run:  runParticleIDs,
	})
}

func runParticleIDs(e *env) error {
	return runRegistryIDs(e, "minecraft:particle_type", "Particle", "internal/protocol/particleids_gen.go",
		"The particle types by minecraft:particle_type ID, one untyped constant per\n// registry entry.")
}
