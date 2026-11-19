package main

// The type declarations and prose recipebook_gen.go carries between its tables.
const recipeBookTypes = `// The tables behind the recipe book packets a client is sent at join: the
// item groups its filters test against, the stonecutter's recipe list, and
// the recipes a player knows the moment they arrive. tools/gen recipebook
// builds them from the data pack's recipes and advancements; the packets
// themselves are written in internal/server/recipes.go.
//
// The orders below are the ones this server sends. The stonecutter's is the
// vanilla server's own - it scans the recipes alphabetically and so does
// this generator. The property sets are sent by name and their items in item
// registry order, because the vanilla server scrambles both per run (each
// set is an immutable set over identity hash codes read out from a random
// offset), and no data pack can say what the next run will read.

// RecipePropertySet is one of the item groups the recipe book's filters are
// made of: the items a furnace, a blast furnace, a smoker, a campfire or a
// smithing table accepts, under the name the client looks the group up by.
type RecipePropertySet struct {
	Name  string
	Items []int32
}

// RecipePropertySets holds the property sets a vanilla {version} server sends at
// join, with the item IDs of Items for the same version.
var RecipePropertySets = []RecipePropertySet{
`

const recipeBookStonecutter = `}

// StonecutterRecipe is one recipe of the stonecutter's list: one input item
// and the stack one cut of it produces, under the ID of its data pack file.
type StonecutterRecipe struct {
	ID          string
	Input       int32
	ResultID    int32
	ResultCount int32
}

// StonecutterRecipes holds the stonecutter recipes a vanilla {version} server
// sends at join, in the order it sends them: the alphabetical order of the
// recipes' own IDs.
var StonecutterRecipes = []StonecutterRecipe{
`

const recipeBookDisplays = `}

// BookIngredient is one square of a crafting display: the item tag the client
// resolves itself, or the items the square asks for. Both unset is a square
// the recipe leaves out.
type BookIngredient struct {
	Tag   string
	Items []int32
}

// CraftingDisplay is one crafting recipe as the recipe book knows it: the
// display ID the packets name it by, the resource location a player file's
// recipeBook lists, the group and book category the client draws it with,
// the grid it is made of and what one craft produces. Squares holds each
// square both ways, as the wire's tag-or-items and as the expanded item IDs
// the crafting matches test; a shaped recipe's squares are row-major
// Width*Height with the blanks the recipe leaves out, a shapeless one
// ingredient a square. Fresh marks the recipes the data pack unlocks the
// moment a player ticks, which is the book a player starts with; the rest
// are the rewards of the recipe advancements.
// vanilla: the recipe advancements' tick criteria, and Recipe.showNotification
type CraftingDisplay struct {
	ID          int32
	Name        string
	Fresh       bool
	Group       int32
	Category    int32
	Shaped      bool
	Width       int32
	Height      int32
	Squares     []BookIngredient
	Items       []Ingredient
	ResultID    int32
	ResultCount int32
	// Notify is the data pack's show_notification flag, off only on the
	// crafting table, whose grant arrives without the toast.
	Notify bool
	// Slots overrides the visuals; Squares and Items remain placement ingredients.
 Slots *CraftingSlots
 // ResultComponents is the fixed patch on the crafting result.
 ResultComponents []protocol.RawComponent
}

// CraftingDisplays holds every crafting recipe of the data pack that has a
// display, by the display ID vanilla's alphabetical scan of every recipe
// assigns. A Recipe Book Add names an entry by it, and the crafting displays
// this server builds - the ghost recipe above all - are the same ones the
// vanilla server sends.
var CraftingDisplays = []CraftingDisplay{
`

const recipeBookFurnaceDisplays = `}

// FurnaceDisplay is one cooking recipe as the recipe book knows it, under the
// display ID the packets name it by. Kind indexes the cooking kinds (smelting,
// blasting, smoking, campfire cooking) as PackRecipes.Cooking does; Group
// is -1 for none. Ingredient is the square as the wire carries it, Matches
// its expanded item IDs, a tag's members included, that placing takes.
// vanilla: AbstractCookingRecipe.display
type FurnaceDisplay struct {
	ID          int32
	Name        string
	Kind        int32
	Group       int32
	Category    int32
	Ingredient  BookIngredient
	Matches     Ingredient
	ResultID    int32
	ResultCount int32
	Duration    int32
	Experience  float32
}

// FurnaceDisplays holds every cooking recipe of the data pack of a vanilla
// {version} server, in display ID order.
var FurnaceDisplays = []FurnaceDisplay{
`

const recipeBookStonecutterDisplays = `}

// StonecutterDisplay is a stonecutting recipe's book display.
// vanilla: StonecutterRecipe.display
type StonecutterDisplay struct {
	ID int32
	Name string
	Group int32
	Category int32
	Ingredient BookIngredient
	ResultID int32
	ResultCount int32
}

// StonecutterDisplays holds the stonecutting displays in vanilla display ID order.
var StonecutterDisplays = []StonecutterDisplay{
`

const recipeBookSmithingDisplays = `}

// SmithingDisplay is a transform or trim recipe's book display.
// vanilla: SmithingTransformRecipe.display, SmithingTrimRecipe.display
type SmithingDisplay struct {
	ID int32
	Name string
	Group int32
	Category int32
	Template BookIngredient
	Base BookIngredient
	Addition BookIngredient
	ResultID int32
	ResultCount int32
	Trim bool
	Pattern int32
}

// SmithingDisplays holds the smithing displays in vanilla display ID order.
var SmithingDisplays = []SmithingDisplay{
`
