package main

// recipeTypes are the Go types recipes_gen.go declares ahead of its tables.
const recipeTypes = `// Ingredient is one square (or, for a shapeless recipe, one item) of a
// recipe: the set of item IDs that satisfy it. A lone item is a set of one;
// a #tag becomes the items the tag collects, expanded here at generation
// time so the server never has to follow a tag to decide what a recipe
// asks for. An empty set is a square the recipe leaves empty.
type Ingredient []int32

// ShapedRecipe is one crafting_shaped recipe: a Width by Height grid of
// ingredients in row-major order, matching the ingredient at each square
// of the recipe's own bounding box wherever that box fits in the (larger)
// crafting grid, mirrored as well. The result is what one craft produces,
// as an item ID and a count. Every recipe type carries the ID its data pack
// file is saved under, which is how a pack's file of the same ID replaces it.
type ShapedRecipe struct {
	ID            string
	Width, Height int32
	Pattern      []Ingredient
	ResultID     int32
	ResultCount  int32
}

// ShapelessRecipe is one crafting_shapeless recipe: the ingredients of one
// craft, gathered from the grid in any arrangement. The same ingredient
// twice is two squares of it, the way the data pack lists them.
type ShapelessRecipe struct {
	ID          string
	Ingredients []Ingredient
	ResultID   int32
	ResultCount int32
}

// StewRecipe is one shapeless suspicious stew recipe: the ingredients as a
// ShapelessRecipe has them and the one effect the result carries, by name,
// for the given duration in ticks.
type StewRecipe struct {
	ID          string
	Ingredients []Ingredient
	ResultID    int32
	ResultCount int32
	Effect      string
	Duration    int32
}

// ImbueRecipe is one crafting_imbue recipe: a 3x3 grid with the Source in the
// middle and the Material in the other eight squares. The result takes the
// potion_contents of the Source.
type ImbueRecipe struct {
	ID          string
	Source      Ingredient
	Material    Ingredient
	ResultID    int32
	ResultCount int32
}

// TransmuteRecipe is one crafting_transmute recipe: one Input and Min to Max
// Material stacks, nothing else. The result keeps the input's components; its
// item is ResultID, or the input's when that is 0 (air, which no recipe names).
// AddMaterialCount adds the number of materials to the result's count.
type TransmuteRecipe struct {
	ID               string
	Input            Ingredient
	Material         Ingredient
	MinMaterial      int32
	MaxMaterial      int32
	ResultID         int32
	ResultCount      int32
	AddMaterialCount bool
}

// DyeRecipe is one crafting_dye recipe: one Target and at least one Dye. The
// result is the target's item and components recoloured by the dyes.
type DyeRecipe struct {
	ID          string
	Target      Ingredient
	Dye         Ingredient
	ResultID    int32
	ResultCount int32
}

// CookingRecipe is one smelting, blasting, smoking or campfire cooking
// recipe: the id the data pack saved it under - the name a vanilla server's
// RecipesUsed block entity compound keys its smelt counts by - what goes in
// (an ingredient as above), what comes out, and how long one smelt takes in
// ticks. Experience is what a finished smelt pays the player who takes the
// result.
type CookingRecipe struct {
	ID          string
	Ingredient  Ingredient
	ResultID    int32
	ResultCount int32
	Experience  float32
	CookTime    int32
}

// SmithingTransformRecipe is one smithing_transform recipe: the three
// ingredients - the template, the base and the addition - and the stack one
// make of them produces. A nil ingredient matches only an empty slot.
type SmithingTransformRecipe struct {
	ID          string
	Template    Ingredient
	Base        Ingredient
	Addition    Ingredient
	ResultID    int32
	ResultCount int32
}

// SmithingTrimRecipe is one smithing_trim recipe: the three ingredients and
// the trim pattern one make lays on the base, by the ID the
// minecraft:trim_pattern registry numbers the pattern with.
type SmithingTrimRecipe struct {
	ID       string
	Template Ingredient
	Base     Ingredient
	Addition Ingredient
	Pattern  int32
}

// BrewingRecipe is one brewing recipe: the bottle (an item of Input holding
// the potion Potion, by its minecraft:potion registry ID), the reagent, and
// the fresh stack of one a brew leaves in the bottle's place.
type BrewingRecipe struct {
	ID           string
	Input        Ingredient
	Potion       int32
	Reagent      Ingredient
	ResultID     int32
	ResultPotion int32
}

`
