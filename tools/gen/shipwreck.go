package main

import (
	"fmt"

	"github.com/golem-mc/golem/nbt"
)

func init() {
	register(generator{name: "shipwreck", doc: "internal/server/world/shipwreck.zip: shipwreck templates", run: runShipwreck})
}

func runShipwreck(e *env) error {
	allowed := map[string]bool{}
	for _, base := range []string{"with_mast", "upsidedown_full", "upsidedown_fronthalf", "upsidedown_backhalf", "sideways_full", "sideways_fronthalf", "sideways_backhalf", "rightsideup_full", "rightsideup_fronthalf", "rightsideup_backhalf"} {
		allowed["minecraft/structure/shipwreck/"+base+".nbt"] = true
		allowed["minecraft/structure/shipwreck/"+base+"_degraded.nbt"] = true
	}
	return packTemplates(e, "minecraft/structure/shipwreck/*.nbt", 20, "internal/server/world/shipwreck.zip", func(name string, tag nbt.Compound) error {
		if !allowed[name] {
			return fmt.Errorf("unmodeled shipwreck template %s", name)
		}
		return checkShipwreckTemplate(tag)
	})
}

func checkShipwreckTemplate(tag nbt.Compound) error {
	size, _ := nbt.Get[nbt.List](tag, "size")
	if len(size.Items) != 3 {
		return fmt.Errorf("template needs three size coordinates")
	}
	for i := range 2 {
		n, ok := nbt.IntValue(size.Items[i])
		if !ok || n > 32 || n < 1 {
			return fmt.Errorf("oversized shipwrecks need generation-time height adjustment")
		}
	}
	entities, _ := nbt.Get[nbt.List](tag, "entities")
	if len(entities.Items) != 0 {
		return fmt.Errorf("template entities are not modeled")
	}
	blocks, _ := nbt.Get[nbt.List](tag, "blocks")
	for _, b := range blocks.Items {
		c := b.(nbt.Compound)
		n, _ := nbt.Get[nbt.Compound](c, "nbt")
		id, _ := nbt.Get[nbt.String](n, "id")
		switch id {
		case "", "minecraft:chest":
		case "minecraft:structure_block":
			mode, _ := nbt.Get[nbt.String](n, "mode")
			marker, _ := nbt.Get[nbt.String](n, "metadata")
			if mode != "DATA" || marker != "map_chest" && marker != "treasure_chest" && marker != "supply_chest" {
				return fmt.Errorf("unsupported marker %q/%q", mode, marker)
			}
		default:
			return fmt.Errorf("unsupported block entity %q", id)
		}
	}
	return nil
}
