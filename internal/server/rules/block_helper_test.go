package rules

import "github.com/golem-mc/golem/registry"

// blockNamed is the block a registry name such as "minecraft:stone" names.
func blockNamed(name string) registry.Block {
	b, ok := registry.BlockByName(name)
	if !ok {
		panic("no block " + name)
	}
	return b
}
