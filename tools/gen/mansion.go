package main

import (
	"fmt"

	"github.com/golem-mc/golem/nbt"
)

func init() {
	register(generator{name: "mansion", doc: "internal/server/world/mansion.zip: woodland mansion templates", run: runMansion})
}

func runMansion(e *env) error {
	return packTemplates(e, "minecraft/structure/woodland_mansion/*.nbt", 73, "internal/server/world/mansion.zip", checkMansionTemplate)
}

func checkMansionTemplate(_ string, tag nbt.Compound) error {
	if _, ok := tag["palettes"]; ok {
		return fmt.Errorf("multiple palettes are not modeled")
	}
	blocks, _ := nbt.Get[nbt.List](tag, "blocks")
	for _, b := range blocks.Items {
		block := b.(nbt.Compound)
		n, _ := nbt.Get[nbt.Compound](block, "nbt")
		if id, _ := nbt.Get[nbt.String](n, "id"); id != "minecraft:structure_block" {
			continue
		}
		mode, _ := nbt.Get[nbt.String](n, "mode")
		marker, _ := nbt.Get[nbt.String](n, "metadata")
		if mode != "DATA" {
			return fmt.Errorf("unsupported marker mode %q", mode)
		}
		switch marker {
		case "ChestWest", "ChestEast", "ChestNorth", "ChestSouth", "Mage", "Warrior", "Group of Allays":
		default:
			return fmt.Errorf("unsupported marker %q", marker)
		}
	}
	return nil
}
