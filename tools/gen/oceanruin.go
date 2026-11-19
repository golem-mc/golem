package main

import (
	"fmt"

	"github.com/golem-mc/golem/nbt"
)

func init() {
	register(generator{name: "oceanruin", doc: "internal/server/world/oceanruin.zip: ocean ruin templates", run: runOceanRuin})
	register(generator{name: "oceanruin-golden", doc: "internal/server/world/testdata/oceanruin_golden.txt: vanilla ocean ruin assembly and processors", run: func(e *env) error {
		return runGoldenFile(e, "OceanRuinGolden", "internal/server/world/testdata/oceanruin_golden.txt")
	}})
}

func runOceanRuin(e *env) error {
	allowed := map[string]bool{}
	for _, kind := range []string{"warm", "brick", "cracked", "mossy"} {
		for i := 1; i <= 8; i++ {
			allowed[fmt.Sprintf("minecraft/structure/underwater_ruin/%s_%d.nbt", kind, i)] = true
		}
		for _, i := range []int{1, 2, 3, 8} {
			if kind == "warm" {
				switch i {
				case 1:
					i = 4
				case 2:
					i = 5
				case 3:
					i = 6
				case 8:
					i = 7
				}
			}
			allowed[fmt.Sprintf("minecraft/structure/underwater_ruin/big_%s_%d.nbt", kind, i)] = true
		}
	}
	return packTemplates(e, "minecraft/structure/underwater_ruin/*.nbt", len(allowed), "internal/server/world/oceanruin.zip", func(name string, tag nbt.Compound) error {
		if !allowed[name] {
			return fmt.Errorf("unmodeled ocean ruin template %s", name)
		}
		size, _ := nbt.Get[nbt.List](tag, "size")
		if len(size.Items) != 3 {
			return fmt.Errorf("ocean ruins require three dimensions")
		}
		for _, i := range []int{0, 2} {
			n, ok := nbt.IntValue(size.Items[i])
			if !ok || n < 1 || n > 16 {
				return fmt.Errorf("ocean ruin horizontal size outside modeled reach")
			}
		}
		if _, ok := tag["palettes"]; ok {
			return fmt.Errorf("ocean ruins require a single palette")
		}
		entities, _ := nbt.Get[nbt.List](tag, "entities")
		if len(entities.Items) != 0 {
			return fmt.Errorf("ocean ruin template entities are not modeled")
		}
		blocks, _ := nbt.Get[nbt.List](tag, "blocks")
		for _, b := range blocks.Items {
			n, _ := nbt.Get[nbt.Compound](b.(nbt.Compound), "nbt")
			id, _ := nbt.Get[nbt.String](n, "id")
			switch id {
			case "":
			case "minecraft:structure_block":
				mode, _ := nbt.Get[nbt.String](n, "mode")
				marker, _ := nbt.Get[nbt.String](n, "metadata")
				if mode != "DATA" || marker != "chest" && marker != "drowned" {
					return fmt.Errorf("unsupported ocean ruin marker %q/%q", mode, marker)
				}
			default:
				return fmt.Errorf("unsupported ocean ruin block entity %q", id)
			}
		}
		palette, _ := nbt.Get[nbt.List](tag, "palette")
		if len(palette.Items) == 0 {
			return fmt.Errorf("ocean ruins require a single palette")
		}
		for _, p := range palette.Items {
			entry := p.(nbt.Compound)
			name, ok := nbt.Get[nbt.String](entry, "id")
			if !ok {
				name, _ = nbt.Get[nbt.String](entry, "Name")
			}
			if name == "minecraft:jigsaw" {
				return fmt.Errorf("ocean ruin jigsaws are not modeled")
			}
		}
		return nil
	})
}
