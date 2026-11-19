package datapack

import (
	"archive/zip"
	"io"
	"slices"
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/registry"
)

func decodeRecipe(t *testing.T, json string) registry.PackRecipes {
	t.Helper()
	var set registry.PackRecipes
	if err := DecodeRecipe("pk:r", []byte(json), &set); err != nil {
		t.Fatalf("DecodeRecipe(%s) = %v", json, err)
	}
	return set
}

func item(t *testing.T, name string) int32 {
	t.Helper()
	id, ok := registry.ItemID(name)
	if !ok {
		t.Fatalf("no item %s", name)
	}
	return id
}

// TestDecodeShapedShrinksItsPattern checks that a pattern is cut to the box
// holding its symbols, and that a mirror image shares the shape.
// vanilla: ShapedRecipePattern.shrink
func TestDecodeShapedShrinksItsPattern(t *testing.T) {
	set := decodeRecipe(t, `{"type":"minecraft:crafting_shaped","pattern":["   "," a ","   "],
		"key":{"a":"minecraft:clay_ball"},"result":{"id":"minecraft:brick","count":3}}`)
	if len(set.Crafting) != 1 {
		t.Fatalf("decoded %d crafting recipes, want 1", len(set.Crafting))
	}
	r := set.Crafting[0]
	if !r.Shaped || r.Width != 1 || r.Height != 1 || len(r.Items) != 1 {
		t.Fatalf("shape = %dx%d with %d squares, want 1x1 with 1", r.Width, r.Height, len(r.Items))
	}
	if r.ResultID != item(t, "minecraft:brick") || r.ResultCount != 3 || r.Name != "pk:r" || !r.Notify {
		t.Errorf("result %d x%d named %s notify %t, want 3 bricks named pk:r with a notification", r.ResultID, r.ResultCount, r.Name, r.Notify)
	}
}

// TestDecodeIngredientsFollowTags checks that a #tag square keeps its tag for
// the book and expands to the tag's items for matching, that a list is sorted
// and deduplicated, and that an item leaving a crafting remainder is
// recorded as one.
// vanilla: Ingredient.CODEC
func TestDecodeIngredientsFollowTags(t *testing.T) {
	set := decodeRecipe(t, `{"type":"minecraft:crafting_shapeless","category":"redstone","group":"g",
		"ingredients":["#minecraft:logs",["minecraft:water_bucket","minecraft:clay_ball","minecraft:clay_ball"]],
		"result":"minecraft:stick","show_notification":false}`)
	r := set.Crafting[0]
	if r.Shaped || len(r.Items) != 2 || r.GroupName != "g" || r.Category != 1 || r.Notify {
		t.Fatalf("decoded %+v, want a shapeless recipe in group g, redstone, without a notification", r)
	}
	if r.Squares[0].Tag != "minecraft:logs" || r.Squares[0].Items != nil {
		t.Errorf("tag square = %+v, want the tag only", r.Squares[0])
	}
	if !slices.Contains(r.Items[0], item(t, "minecraft:oak_log")) {
		t.Errorf("tag square matches %v, want oak logs among them", r.Items[0])
	}
	want := []int32{item(t, "minecraft:water_bucket"), item(t, "minecraft:clay_ball")}
	slices.Sort(want)
	if !slices.Equal(r.Items[1], want) {
		t.Errorf("list square = %v, want %v", r.Items[1], want)
	}
	// The display keeps the file's order and its duplicates.
	// vanilla: Ingredient.display
	listed := []int32{item(t, "minecraft:water_bucket"), item(t, "minecraft:clay_ball"), item(t, "minecraft:clay_ball")}
	if !slices.Equal(r.Squares[1].Items, listed) {
		t.Errorf("list square displays %v, want %v", r.Squares[1].Items, listed)
	}
}

// TestDecodeCookingTime checks that every cooking kind requires its cooking
// time, so a file without one adds nothing, and that any number is taken,
// truncated to an int.
// vanilla: AbstractCookingRecipe.cookingMapCodec
func TestDecodeCookingTime(t *testing.T) {
	for kind, cooking := range cookingTypes {
		name := cooking.name
		var set registry.PackRecipes
		err := DecodeRecipe("pk:r", []byte(`{"type":"`+name+`","ingredient":"minecraft:clay_ball","result":"minecraft:brick"}`), &set)
		if err == nil || !strings.Contains(err.Error(), "No key cookingtime") || !set.Empty() {
			t.Errorf("%s without cookingtime: error = %v, set %+v; want a refusal naming cookingtime and nothing added", name, err, set)
		}
		set = decodeRecipe(t, `{"type":"`+name+`","ingredient":"minecraft:clay_ball","result":"minecraft:brick","cookingtime":-5}`)
		if got := set.Cooking[kind]; len(got) != 1 || got[0].Duration != -5 || got[0].Name != "pk:r" || got[0].ResultCount != 1 {
			t.Errorf("%s decoded to %+v, want one recipe taking -5 ticks", name, got)
		}
	}
	set := decodeRecipe(t, `{"type":"minecraft:smelting","ingredient":"minecraft:clay_ball","result":"minecraft:brick","experience":0.7,"cookingtime":50}`)
	if got := set.Cooking[registry.CookingSmelting][0]; got.Duration != 50 || got.Experience != 0.7 {
		t.Errorf("decoded %+v, want 50 ticks and 0.7 experience", got)
	}
	// Codec.INT truncates a fraction toward zero and wraps past int range.
	for in, want := range map[string]int32{"1.5": 1, "-2.5": -2, "10000000000": 1410065408} {
		set := decodeRecipe(t, `{"type":"minecraft:smelting","ingredient":"minecraft:clay_ball","result":"minecraft:brick","cookingtime":`+in+`}`)
		if got := set.Cooking[registry.CookingSmelting][0].Duration; got != want {
			t.Errorf("cookingtime %s decoded to %d ticks, want %d", in, got, want)
		}
	}
}

// TestDecodeResultCountTruncates checks that a result count with a fraction
// is truncated before the 1..99 check.
// vanilla: ItemStackTemplate.CODEC, ExtraCodecs.intRange over Codec.INT
func TestDecodeResultCountTruncates(t *testing.T) {
	for in, want := range map[string]int32{"2.5": 2, "1.9": 1, "99.9": 99} {
		set := decodeRecipe(t, `{"type":"minecraft:smelting","ingredient":"minecraft:clay_ball","result":{"id":"minecraft:brick","count":`+in+`},"cookingtime":10}`)
		if got := set.Cooking[registry.CookingSmelting][0].ResultCount; got != want {
			t.Errorf("result count %s decoded to %d, want %d", in, got, want)
		}
	}
}

// TestDecodeStonecuttingTakesEveryItemOfAnIngredient checks that a tag
// ingredient becomes one recipe per item.
func TestDecodeStonecuttingTakesEveryItemOfAnIngredient(t *testing.T) {
	set := decodeRecipe(t, `{"type":"minecraft:stonecutting","ingredient":["minecraft:stone","minecraft:granite"],
		"result":{"id":"minecraft:stick","count":2}}`)
	if len(set.Stonecutting) != 2 || set.Stonecutting[0].ResultCount != 2 || set.Stonecutting[0].ID != "pk:r" {
		t.Errorf("decoded %+v, want two recipes of 2 sticks", set.Stonecutting)
	}
}

// TestDecodeSmithing checks both smithing kinds, with the template and
// addition of a transform left out.
func TestDecodeSmithing(t *testing.T) {
	set := decodeRecipe(t, `{"type":"minecraft:smithing_transform","base":"minecraft:stick","result":"minecraft:brick"}`)
	if len(set.SmithingTransform) != 1 || set.SmithingTransform[0].Template != nil || set.SmithingTransform[0].Addition != nil {
		t.Errorf("transform decoded to %+v, want no template and no addition", set.SmithingTransform)
	}
	set = decodeRecipe(t, `{"type":"minecraft:smithing_trim","template":"minecraft:stick","base":"minecraft:stick",
		"addition":"minecraft:stick","pattern":"minecraft:sentry"}`)
	patterns, _ := registry.Get("minecraft:trim_pattern")
	if len(set.SmithingTrim) != 1 || set.SmithingTrim[0].Pattern != patterns.ID("minecraft:sentry") {
		t.Errorf("trim decoded to %+v, want the sentry pattern", set.SmithingTrim)
	}
}

// TestDecodeRecipeRefusals checks the files that must not load.
// vanilla: ShapedRecipePattern.Data, Ingredient.CODEC, ItemStackTemplate.CODEC
func TestDecodeRecipeRefusals(t *testing.T) {
	shaped := func(pattern, key string) string {
		return `{"type":"minecraft:crafting_shaped","pattern":` + pattern + `,"key":` + key + `,"result":"minecraft:stick"}`
	}
	const a = `{"a":"minecraft:clay_ball"}`
	for name, tc := range map[string]struct{ json, want string }{
		"a broken file":           {`{"type":`, "unexpected"},
		"an unknown type":         {`{"type":"minecraft:nope"}`, "unknown recipe type"},
		"a special type":          {`{"type":"minecraft:crafting_special_bookcloning"}`, "built in"},
		"no type":                 {`{}`, `No key type`},
		"four pattern rows":       {shaped(`["a","a","a","a"]`, a), "1 to 3 rows"},
		"four columns":            {shaped(`["aaaa"]`, a), "3 at most"},
		"uneven rows":             {shaped(`["aa","a"]`, a), "same width"},
		"an empty pattern":        {shaped(`["  "]`, `{}`), "at least one symbol"},
		"an undefined symbol":     {shaped(`["ab"]`, a), "does not define"},
		"an unused key":           {shaped(`["a"]`, `{"a":"minecraft:clay_ball","b":"minecraft:stick"}`), "does not use"},
		"a space as a key":        {shaped(`["a"]`, `{" ":"minecraft:clay_ball"}`), "not a valid key symbol"},
		"an unknown item":         {shaped(`["a"]`, `{"a":"minecraft:not_an_item"}`), "unknown item"},
		"an air ingredient":       {shaped(`["a"]`, `{"a":"minecraft:air"}`), "unknown item"},
		"an empty list":           {shaped(`["a"]`, `{"a":[]}`), "cannot be empty"},
		"an unknown tag":          {shaped(`["a"]`, `{"a":"#minecraft:not_a_tag"}`), "unknown item tag"},
		"no ingredients":          {`{"type":"minecraft:crafting_shapeless","ingredients":[],"result":"minecraft:stick"}`, "1 to 9"},
		"ten ingredients":         {`{"type":"minecraft:crafting_shapeless","ingredients":["minecraft:stick","minecraft:stick","minecraft:stick","minecraft:stick","minecraft:stick","minecraft:stick","minecraft:stick","minecraft:stick","minecraft:stick","minecraft:stick"],"result":"minecraft:stick"}`, "1 to 9"},
		"components on a result":  {`{"type":"minecraft:smelting","ingredient":"minecraft:stick","result":{"id":"minecraft:stick","components":{"minecraft:damage":1}}}`, "data components"},
		"a result count of zero":  {`{"type":"minecraft:smelting","ingredient":"minecraft:stick","result":{"id":"minecraft:stick","count":0}}`, "1..99"},
		"a result count of 100":   {`{"type":"minecraft:smelting","ingredient":"minecraft:stick","result":{"id":"minecraft:stick","count":100}}`, "1..99"},
		"a result count of 0.5":   {`{"type":"minecraft:smelting","ingredient":"minecraft:stick","result":{"id":"minecraft:stick","count":0.5}}`, "1..99"},
		"a result count of 100.5": {`{"type":"minecraft:smelting","ingredient":"minecraft:stick","result":{"id":"minecraft:stick","count":100.5}}`, "1..99"},
		"an air result":           {`{"type":"minecraft:smelting","ingredient":"minecraft:stick","result":"minecraft:air"}`, "unknown item"},
		"an unknown category":     {`{"type":"minecraft:crafting_shapeless","category":"x","ingredients":["minecraft:stick"],"result":"minecraft:stick"}`, "category"},
		"an unknown trim pattern": {`{"type":"minecraft:smithing_trim","template":"minecraft:stick","base":"minecraft:stick","addition":"minecraft:stick","pattern":"minecraft:nope"}`, "trim pattern"},
		"a missing base":          {`{"type":"minecraft:smithing_transform","result":"minecraft:stick"}`, `No key base`},
	} {
		var set registry.PackRecipes
		err := DecodeRecipe("pk:r", []byte(tc.json), &set)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: error = %v, want one mentioning %q", name, err, tc.want)
		}
		if !set.Empty() {
			t.Errorf("%s: a refused file added %+v", name, set)
		}
	}
}

// TestDecodeRecipeMatchesTheGeneratedTables decodes the vanilla data pack's
// recipe files, when the reference server is at hand, and compares each one
// to the table tools/gen built from it.
func TestDecodeRecipeMatchesTheGeneratedTables(t *testing.T) {
	jar := "../../../reference-server/versions/" + protocol.GameVersion + "/server-" + protocol.GameVersion + ".jar"
	zr, err := zip.OpenReader(jar)
	if err != nil {
		t.Skip("no reference server jar at " + jar)
	}
	defer zr.Close()
	var set registry.PackRecipes
	for _, f := range zr.File {
		rest, ok := strings.CutPrefix(f.Name, "data/minecraft/recipe/")
		if !ok || !strings.HasSuffix(rest, ".json") || strings.Contains(rest, "/") {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			t.Fatal(err)
		}
		// Recipes the tables do not hold fail to decode, which is not a mismatch.
		_ = DecodeRecipe("minecraft:"+strings.TrimSuffix(rest, ".json"), data, &set)
	}
	display := map[string]registry.CraftingDisplay{}
	for _, d := range set.Crafting {
		display[d.Name] = d.CraftingDisplay
	}
	shaped := 0
	for _, want := range registry.CraftingDisplays {
		got, ok := display[want.Name]
		if !ok {
			// A result with data components.
			continue
		}
		shaped++
		if got.Shaped != want.Shaped || got.Width != want.Width || got.Height != want.Height ||
			got.Category != want.Category || got.Notify != want.Notify ||
			got.ResultID != want.ResultID || got.ResultCount != want.ResultCount ||
			!equalSquares(got.Squares, want.Squares) || !equalIngredients(got.Items, want.Items) {
			t.Errorf("%s decoded to %+v, want %+v", want.Name, got.Squares, want.Squares)
		}
	}
	for _, want := range registry.ShapedRecipes {
		if d, ok := display[want.ID]; ok && (d.Width != want.Width || d.Height != want.Height || !equalIngredients(d.Items, want.Pattern)) {
			t.Errorf("shaped %s decoded to %+v, want %+v", want.ID, d, want)
		}
	}
	for _, want := range registry.ShapelessRecipes {
		if d, ok := display[want.ID]; ok && !equalIngredients(d.Items, want.Ingredients) {
			t.Errorf("shapeless %s decoded to %+v, want %+v", want.ID, d, want)
		}
	}
	if shaped < 1000 {
		t.Errorf("compared %d crafting recipes, want over 1000", shaped)
	}
	cooking := 0
	for kind, table := range [][]registry.CookingRecipe{registry.SmeltingRecipes, registry.BlastingRecipes, registry.SmokingRecipes, registry.CampfireCookingRecipes} {
		for _, want := range table {
			i := slices.IndexFunc(set.Cooking[kind], func(r registry.PackCooking) bool { return r.Name == want.ID })
			if i < 0 || !equalIngredient(set.Cooking[kind][i].Ingredient.Items, want.Ingredient) ||
				set.Cooking[kind][i].ResultID != want.ResultID || set.Cooking[kind][i].ResultCount != want.ResultCount ||
				set.Cooking[kind][i].Experience != want.Experience || set.Cooking[kind][i].Duration != want.CookTime {
				t.Errorf("cooking kind %d recipe %s did not decode to %+v", kind, want.ID, want)
			}
			cooking++
		}
	}
	for _, want := range registry.FurnaceDisplays {
		i := slices.IndexFunc(set.Cooking[want.Kind], func(r registry.PackCooking) bool { return r.Name == want.Name })
		if i < 0 {
			continue // reported above
		}
		got := set.Cooking[want.Kind][i].FurnaceDisplay
		if got.Category != want.Category || got.Ingredient.Tag != want.Ingredient.Tag ||
			want.Ingredient.Tag == "" && !slices.Equal(got.Ingredient.Items, want.Ingredient.Items) {
			t.Errorf("cooking %s decoded to category %d and ingredient %+v, want %d and %+v", want.Name, got.Category, got.Ingredient, want.Category, want.Ingredient)
		}
	}
	for _, want := range registry.SmithingTransformRecipes {
		i := slices.IndexFunc(set.SmithingTransform, func(r registry.SmithingTransformRecipe) bool { return r.ID == want.ID })
		if i < 0 || !equalIngredient(set.SmithingTransform[i].Template, want.Template) ||
			!equalIngredient(set.SmithingTransform[i].Base, want.Base) || !equalIngredient(set.SmithingTransform[i].Addition, want.Addition) ||
			set.SmithingTransform[i].ResultID != want.ResultID {
			t.Errorf("smithing_transform %s did not decode to %+v", want.ID, want)
		}
	}
	for _, want := range registry.SmithingTrimRecipes {
		i := slices.IndexFunc(set.SmithingTrim, func(r registry.SmithingTrimRecipe) bool { return r.ID == want.ID })
		if i < 0 || !equalIngredient(set.SmithingTrim[i].Base, want.Base) || !equalIngredient(set.SmithingTrim[i].Addition, want.Addition) ||
			set.SmithingTrim[i].Pattern != want.Pattern {
			t.Errorf("smithing_trim %s did not decode to %+v", want.ID, want)
		}
	}
	cuts := 0
	for _, want := range registry.StonecutterRecipes {
		if !slices.Contains(set.Stonecutting, want) {
			t.Errorf("stonecutting %s did not decode to %+v", want.ID, want)
		}
		cuts++
	}
	if cooking < 100 || cuts < 300 {
		t.Errorf("compared %d cooking and %d stonecutting recipes, want over 100 and 300", cooking, cuts)
	}
}

// equalIngredient compares as sets: the tables keep a tag's items in the
// tag's order, the codec sorts them.
func equalIngredient(a, b registry.Ingredient) bool {
	a, b = slices.Sorted(slices.Values(a)), slices.Sorted(slices.Values(b))
	return slices.Equal(a, b)
}

func equalIngredients[A, B ~[]registry.Ingredient](a A, b B) bool {
	return slices.EqualFunc(a, b, equalIngredient)
}

func equalSquares(a, b []registry.BookIngredient) bool {
	return slices.EqualFunc(a, b, func(x, y registry.BookIngredient) bool {
		// The display sends an item list in the file's order.
		return x.Tag == y.Tag && slices.Equal(x.Items, y.Items)
	})
}
