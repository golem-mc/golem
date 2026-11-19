package main

import (
	"fmt"
	"path"
	"strings"

	"github.com/golem-mc/golem/nbt"
)

func init() {
	register(generator{name: "endcity", doc: "internal/server/world/endcity.zip: end city templates", run: runEndCity})
}

func runEndCity(e *env) error {
	names := strings.Fields("base_floor base_roof bridge_end bridge_gentle_stairs bridge_piece bridge_steep_stairs fat_tower_base fat_tower_middle fat_tower_top second_floor_1 second_floor_2 second_roof ship third_floor_1 third_floor_2 third_roof tower_base tower_floor tower_piece tower_top")
	allowed := make(map[string]bool, len(names))
	for _, name := range names {
		allowed[name+".nbt"] = true
	}
	return packTemplates(e, "minecraft/structure/end_city/*.nbt", len(names), "internal/server/world/endcity.zip", func(name string, tag nbt.Compound) error {
		if !allowed[path.Base(name)] {
			return fmt.Errorf("unmodeled end city template %s", name)
		}
		if _, ok := tag["palettes"]; ok {
			return fmt.Errorf("end cities require a single palette")
		}
		blocks, _ := nbt.Get[nbt.List](tag, "blocks")
		for _, block := range blocks.Items {
			b, _ := block.(nbt.Compound)
			data, _ := nbt.Get[nbt.Compound](b, "nbt")
			id, _ := nbt.Get[nbt.String](data, "id")
			switch id {
			case "", "minecraft:chest", "minecraft:brewing_stand", "minecraft:skull", "minecraft:ender_chest", "minecraft:banner":
			case "minecraft:structure_block":
				mode, _ := nbt.Get[nbt.String](data, "mode")
				marker, _ := nbt.Get[nbt.String](data, "metadata")
				if mode != "DATA" || !strings.HasPrefix(string(marker), "Chest") && !strings.HasPrefix(string(marker), "Sentry") && !strings.HasPrefix(string(marker), "Elytra") {
					return fmt.Errorf("unsupported marker %q/%q", mode, marker)
				}
			default:
				return fmt.Errorf("unsupported block entity %q", id)
			}
		}
		return nil
	})
}
