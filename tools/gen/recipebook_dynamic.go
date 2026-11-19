package main

import (
	"fmt"
	"strings"
)

type bookSlot struct {
	kind                   string
	ingredient             bookSquare
	item, count, component int
	children               []bookSlot
}

type craftingSlots struct {
	squares []bookSlot
	result  bookSlot
}

// vanilla: TransmuteRecipe.minMaterialCount, maxMaterialCount
func materialBounds(recipe obj) (int, int) {
	if !recipe.has("material_count") {
		return 1, 1
	}
	value := recipe.field("material_count")
	if n, ok := asNum(value); ok {
		return int(n), int(n)
	}
	bounds := asObj(value, "material count bounds")
	return bounds.intOr("min", 1), bounds.intOr("max", 8)
}

func recipeDisplayCount(recipe obj) int {
	if !hasDisplay(recipe.str("type")) {
		return 0
	}
	if recipe.str("type") == "minecraft:crafting_transmute" {
		lo, hi := materialBounds(recipe)
		return hi - lo + 1
	}
	return 1
}

func ingredientSlot(square bookSquare) bookSlot {
	return bookSlot{kind: "Ingredient", ingredient: square}
}

func stackSlot(item, count int) bookSlot { return bookSlot{kind: "Stack", item: item, count: count} }
func wrapSlot(kind string, children ...bookSlot) bookSlot {
	return bookSlot{kind: kind, children: children}
}

// vanilla: TransmuteRecipe.display, DyeRecipe.display, ImbueRecipe.display
func (r *recipeBookReader) dynamicDisplays(recipes []namedRecipe, nr namedRecipe) ([]craftingDisplay, error) {
	recipe := nr.recipe
	result := asObj(recipe.field("result"), "a result")
	if result.has("components") {
		return nil, fmt.Errorf("recipe %s has a dynamic display result with components", nr.id)
	}
	groupIDs, _ := recipeGroupIDs(recipes)
	group, ok := groupIDs[recipe.strOr("group", "")]
	if !ok {
		group = -1
	}
	d := craftingDisplay{id: displayIDOf(recipes, nr.id), name: "minecraft:" + nr.id, fresh: r.freshIDs["minecraft:"+nr.id], group: group, category: r.bookCategory(recipe), notify: recipe.boolOr("show_notification", true)}
	if result.has("id") {
		d.result = r.itemID(result.str("id"))
	}
	d.resultCount = result.intOr("count", 1)
	slots := &craftingSlots{}
	switch recipe.str("type") {
	case "minecraft:crafting_transmute":
		input, material := r.squareOf(recipe.field("input")), r.squareOf(recipe.field("material"))
		lo, hi := materialBounds(recipe)
		if lo < 1 || hi > 8 || lo > hi {
			return nil, fmt.Errorf("invalid material count in %s", nr.id)
		}
		d.squares = append(d.squares, input)
		for range hi {
			d.squares = append(d.squares, material)
		}
		var displays []craftingDisplay
		squares := []bookSlot{ingredientSlot(input)}
		for count := lo; count <= hi; count++ {
			squares = append(squares, ingredientSlot(material))
			resultCount := d.resultCount
			if recipe.boolOr("add_material_count_to_result", false) {
				resultCount += count
			}
			resultSlot := stackSlot(d.result, resultCount)
			if !result.has("id") {
				resultSlot = wrapSlot("Composite")
				for _, item := range input.items {
					resultSlot.children = append(resultSlot.children, stackSlot(item, resultCount))
				}
			}
			entry := d
			entry.id += count - lo
			entry.slots = &craftingSlots{squares: append([]bookSlot(nil), squares...), result: resultSlot}
			displays = append(displays, entry)
		}
		return displays, nil
	case "minecraft:crafting_dye":
		if r.dyeComponent < 0 {
			return nil, fmt.Errorf("recipe %s needs the missing dye component", nr.id)
		}
		target, dye := r.squareOf(recipe.field("target")), r.squareOf(recipe.field("dye"))
		d.squares = []bookSquare{target, dye}
		dyeSlot := wrapSlot("Component", ingredientSlot(dye))
		dyeSlot.component = r.dyeComponent
		slots.squares = []bookSlot{ingredientSlot(target), dyeSlot}
		slots.result = wrapSlot("Dyed", dyeSlot, ingredientSlot(target))
	case "minecraft:crafting_imbue":
		source, material := r.squareOf(recipe.field("source")), r.squareOf(recipe.field("material"))
		d.shaped, d.width, d.height = true, 3, 3
		for i := range 9 {
			square := material
			slot := ingredientSlot(material)
			if i == 4 {
				square = source
				slot = wrapSlot("Potion", ingredientSlot(source))
			}
			d.squares = append(d.squares, square)
			slots.squares = append(slots.squares, slot)
		}
		slots.result = wrapSlot("Potion", stackSlot(d.result, d.resultCount))
	default:
		return nil, fmt.Errorf("unsupported crafting display type %s", recipe.str("type"))
	}
	d.slots = slots
	return []craftingDisplay{d}, nil
}

func goBookSlot(s bookSlot) string {
	fields := []string{"Kind: BookSlot" + s.kind}
	switch s.kind {
	case "Ingredient":
		fields = append(fields, "Ingredient: BookIngredient"+goSquare(s.ingredient))
	case "Stack":
		fields = append(fields, fmt.Sprintf("ItemID: %d, Count: %d", s.item, s.count))
	case "Component":
		fields = append(fields, fmt.Sprintf("Component: %d", s.component))
	}
	if len(s.children) > 0 {
		var children []string
		for _, child := range s.children {
			children = append(children, goBookSlot(child))
		}
		fields = append(fields, "Children: []BookSlot{"+strings.Join(children, ",")+"}")
	}
	return "{" + strings.Join(fields, ",") + "}"
}

func goCraftingSlots(s *craftingSlots) string {
	if s == nil {
		return "nil"
	}
	var squares []string
	for _, square := range s.squares {
		squares = append(squares, goBookSlot(square))
	}
	return "&CraftingSlots{Squares: []BookSlot{" + strings.Join(squares, ",") + "}, Result: BookSlot" + goBookSlot(s.result) + "}"
}
