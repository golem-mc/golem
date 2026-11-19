package main

func init() {
	register(generator{
		name: "entitytypeids",
		doc:  "internal/protocol/entitytypeids_gen.go: one constant per minecraft:entity_type entry",
		run:  runEntityTypeIDs,
	})
}

func runEntityTypeIDs(e *env) error {
	return runRegistryIDs(e, "minecraft:entity_type", "EntityType", "internal/protocol/entitytypeids_gen.go",
		"The entity types by minecraft:entity_type ID, one untyped constant per\n// registry entry.")
}
