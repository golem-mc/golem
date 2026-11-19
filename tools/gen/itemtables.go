package main

import (
	"fmt"
	"math"
	"path/filepath"
	"slices"
	"strings"
)

// itemTableComponents reads defaults only when the release registers the component.
func itemTableComponents(e *env, component string) (map[string]obj, bool) {
	registries, err := e.ref.Registries()
	if err != nil {
		fail("%v", err)
	}
	if _, ok := registries["minecraft:data_component_type"].Entries.Get(component); !ok {
		return nil, false
	}
	items := registries["minecraft:item"].Entries
	if items.Len() == 0 {
		fail("no item registry")
	}
	result := map[string]obj{}
	for name := range items.All() {
		namespace, local, _ := strings.Cut(name, ":")
		file := e.ref.ReportPath(filepath.Join(namespace, "components/item", local+".json"))
		components, ok := readObj(file).obj("components")
		if !ok {
			fail("%s: missing item components", file)
		}
		if value, ok := components.obj(component); ok {
			result[name] = value
		}
	}
	return result, true
}

// cookingNumber evaluates only the two furnace contexts represented by the tables.
// vanilla: CookingFuel, Quotient.getIntUnsafe, ConditionalValue
func cookingNumber(r lootCodec, kind string, v any, fast bool, stack []string) float64 {
	if n, ok := asNum(v); ok {
		if math.IsNaN(n) || math.IsInf(n, 0) || n < 0 || n > math.MaxInt32 || kind == "int" && math.Trunc(n) != n {
			fail("unsupported cooking %s constant: %v", kind, v)
		}
		return n
	}
	if name, ok := v.(string); ok {
		return r.reference("context_"+kind+"_provider", name, stack, func(raw any, stack []string) any {
			return cookingNumber(r, kind, raw, fast, stack)
		}).(float64)
	}
	o := asObj(v, "a cooking provider")
	switch o.str("type") {
	case "minecraft:div":
		if kind != "int" || !keysAre(o, "type", "left", "right") {
			break
		}
		left := cookingNumber(r, kind, o["left"], fast, stack)
		right := cookingNumber(r, kind, o["right"], fast, stack)
		if right == 0 {
			fail("cooking provider divides by zero")
		}
		return math.Trunc(left / right)
	case "minecraft:conditional":
		if !keysAre(o, "type", "condition", "on_false", "on_true") {
			break
		}
		predicate := r.predicate(o["condition"], stack)
		if !keysAre(predicate, "condition", "blocks") || predicate.str("condition") != "minecraft:match_block" {
			break
		}
		var blocks []string
		for _, block := range predicate.list("blocks") {
			blocks = append(blocks, asStr(block, "a fast cooking block"))
		}
		slices.Sort(blocks)
		if !slices.Equal(blocks, []string{"minecraft:blast_furnace", "minecraft:smoker"}) {
			break
		}
		normal := cookingNumber(r, kind, o["on_false"], fast, stack)
		accelerated := cookingNumber(r, kind, o["on_true"], fast, stack)
		if fast {
			return accelerated
		}
		return normal
	}
	fail("cooking provider cannot be represented by the furnace tables: %v", o)
	return 0
}

func componentFuels(e *env) (map[string]int, bool) {
	components, present := itemTableComponents(e, "minecraft:cooking_fuel")
	if !present {
		return nil, false
	}
	dp, err := e.ref.DataPack()
	if err != nil {
		fail("%v", err)
	}
	defer dp.Close()
	r := lootCodec{dp: dp}
	fuels := map[string]int{}
	for name, component := range components {
		if !keysAre(component, "burn_time", "speed_multiplier") {
			fail("%s: unsupported cooking fuel %v", name, component)
		}
		normal := cookingNumber(r, "int", component["burn_time"], false, nil)
		fast := cookingNumber(r, "int", component["burn_time"], true, nil)
		if normal < 1 || fast != math.Trunc(normal/2) ||
			cookingNumber(r, "float", component["speed_multiplier"], false, nil) != 1 ||
			cookingNumber(r, "float", component["speed_multiplier"], true, nil) != 2 {
			fail("%s: cooking fuel has unsupported burn times or speeds", name)
		}
		fuels[name] = int(normal)
	}
	return fuels, true
}

// brewingFuels reads the brews each brewing fuel loads. The table carries no
// speed, so a fuel that does not brew at the plain one stops the generation.
// vanilla: BrewingFuel
func brewingFuels(e *env) map[string]int {
	fuels := map[string]int{}
	components, present := itemTableComponents(e, "minecraft:brewing_fuel")
	if !present {
		return fuels
	}
	dp, err := e.ref.DataPack()
	if err != nil {
		fail("%v", err)
	}
	defer dp.Close()
	r := lootCodec{dp: dp}
	for name, component := range components {
		if !keysAre(component, "uses", "speed_multiplier") {
			fail("%s: unsupported brewing fuel %v", name, component)
		}
		uses := cookingNumber(r, "int", component["uses"], false, nil)
		if uses < 1 || cookingNumber(r, "float", component["speed_multiplier"], false, nil) != 1 {
			fail("%s: brewing fuel has unsupported uses or speed", name)
		}
		fuels[name] = int(uses)
	}
	return fuels
}

// componentCompost keeps ResolvableInt references so packs can override them.
func componentCompost(e *env) (map[string]string, bool) {
	components, present := itemTableComponents(e, "minecraft:compostable")
	if !present {
		return nil, false
	}
	dp, err := e.ref.DataPack()
	if err != nil {
		fail("%v", err)
	}
	defer dp.Close()
	layers := map[string]string{}
	for name, component := range components {
		if !keysAre(component, "layers") {
			fail("%s: unsupported compostable %v", name, component)
		}
		switch v := component["layers"].(type) {
		case int64:
			if v < math.MinInt32 || v > math.MaxInt32 {
				fail("%s: compost layers outside int range", name)
			}
			layers[name] = fmt.Sprintf("LootConstant{Value: %d}", v)
		case string:
			lootCodec{dp: dp}.reference("context_int_provider", v, nil, func(raw any, _ []string) any { return raw })
			layers[name] = fmt.Sprintf("LootProviderRef{Name: %q}", lootQualified(v))
		default:
			fail("%s: unsupported compost layers %v", name, v)
		}
	}
	return layers, true
}
