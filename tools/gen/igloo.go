package main

import (
	"fmt"

	"github.com/golem-mc/golem/nbt"
)

func init() {
	register(generator{name: "igloo", doc: "internal/server/world/igloo.zip: igloo templates", run: runIgloo})
}

func runIgloo(e *env) error {
	allowed := map[string]bool{
		"minecraft/structure/igloo/top.nbt":    true,
		"minecraft/structure/igloo/middle.nbt": true,
		"minecraft/structure/igloo/bottom.nbt": true,
	}
	return packTemplates(e, "minecraft/structure/igloo/*.nbt", len(allowed), "internal/server/world/igloo.zip", func(name string, tag nbt.Compound) error {
		if !allowed[name] {
			return fmt.Errorf("unmodeled igloo template %s", name)
		}
		if _, ok := tag["palettes"]; ok {
			return fmt.Errorf("igloos require a single palette")
		}
		entities, _ := nbt.Get[nbt.List](tag, "entities")
		for _, item := range entities.Items {
			entry, ok := item.(nbt.Compound)
			if !ok {
				return fmt.Errorf("igloo entity entry is not a compound")
			}
			n, _ := nbt.Get[nbt.Compound](entry, "nbt")
			id, _ := nbt.Get[nbt.String](n, "id")
			if id != "minecraft:villager" && id != "minecraft:zombie_villager" {
				return fmt.Errorf("unsupported igloo entity %q", id)
			}
		}
		blocks, _ := nbt.Get[nbt.List](tag, "blocks")
		for _, b := range blocks.Items {
			n, _ := nbt.Get[nbt.Compound](b.(nbt.Compound), "nbt")
			id, _ := nbt.Get[nbt.String](n, "id")
			switch id {
			case "":
			case "minecraft:chest":
			case "minecraft:sign":
			case "minecraft:brewing_stand":
			case "minecraft:furnace":
			case "minecraft:structure_block":
				mode, _ := nbt.Get[nbt.String](n, "mode")
				marker, _ := nbt.Get[nbt.String](n, "metadata")
				if mode != "DATA" || marker != "chest" {
					return fmt.Errorf("unsupported marker %q/%q", mode, marker)
				}
			default:
				return fmt.Errorf("unsupported block entity %q", id)
			}
		}
		return nil
	})
}
