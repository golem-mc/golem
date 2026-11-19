package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"path"
	"slices"
	"strings"

	"github.com/golem-mc/golem/tools/internal/gogen"
	"github.com/golem-mc/golem/tools/internal/vanilla"
)

func init() {
	register(generator{
		name: "enchantmentproviders",
		doc:  "registry/enchantmentproviders_gen.go: the vanilla enchantment_provider entries",
		run:  runEnchantmentProviders,
	})
}

// runEnchantmentProviders writes each data pack enchantment_provider as a
// registry.EnchantmentProvider literal. Only constant int providers and the
// three vanilla types are modelled; anything else stops the run.
func runEnchantmentProviders(e *env) error {
	dp, err := e.ref.DataPack()
	if err != nil {
		return err
	}
	defer dp.Close()
	const dir = "minecraft/enchantment_provider"
	files, err := vanilla.Files(dp, dir, ".json")
	if err != nil {
		return err
	}
	if len(files) == 0 {
		return fmt.Errorf("no enchantment providers under %s", dir)
	}
	slices.Sort(files)
	f := e.file("registry", "by tools/gen enchantmentproviders from the enchantment_provider entries of the vanilla {version} data pack")
	f.WriteString(`// builtinEnchantmentProviders maps a provider's registry key to its definition.
// vanilla: EnchantmentProvider.DIRECT_CODEC
var builtinEnchantmentProviders = map[string]EnchantmentProvider{
`)
	for _, rel := range files {
		b, err := fs.ReadFile(dp, path.Join(dir, rel))
		if err != nil {
			return err
		}
		var v struct {
			Type         string      `json:"type"`
			Enchantments string      `json:"enchantments"`
			Enchantment  string      `json:"enchantment"`
			Level        json.Number `json:"level"`
			Cost         json.Number `json:"cost"`
			MinCost      json.Number `json:"min_cost"`
			MaxCostSpan  json.Number `json:"max_cost_span"`
		}
		dec := json.NewDecoder(bytes.NewReader(b))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&v); err != nil {
			return fmt.Errorf("%s: shape not modelled: %w", rel, err)
		}
		var lit string
		switch v.Type {
		case "minecraft:single":
			lit = fmt.Sprintf("Kind: EnchProviderSingle, Names: []string{%s}, Level: IntRange{%s, %s}",
				gogen.Quote(v.Enchantment), v.Level, v.Level)
		case "minecraft:by_cost_with_difficulty":
			tag, ok := strings.CutPrefix(v.Enchantments, "#")
			if !ok {
				return fmt.Errorf("%s: enchantments %q is not a tag, not modelled", rel, v.Enchantments)
			}
			lit = fmt.Sprintf("Kind: EnchProviderByCostWithDifficulty, Tag: %s, MinCost: %s, MaxCostSpan: %s",
				gogen.Quote(tag), v.MinCost, v.MaxCostSpan)
		default:
			return fmt.Errorf("%s: provider type %q not modelled", rel, v.Type)
		}
		f.Printf("\t%s: {%s},\n", gogen.Quote("minecraft:"+strings.TrimSuffix(rel, ".json")), lit)
	}
	f.WriteString("}\n")
	e.logf("enchantment providers: %d", len(files))
	return e.emit("registry/enchantmentproviders_gen.go", f)
}
