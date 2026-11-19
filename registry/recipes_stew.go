package registry

import (
	"slices"

	"github.com/golem-mc/golem/internal/protocol"
)

// The recipes whose result carries components computed from the recipe or the
// grid: suspicious stew (effect fixed per recipe) and the tipped arrow imbue
// (potion copied from the grid).

// stewResult matches the shapeless suspicious stew recipes.
func stewResult(cells []protocol.ItemStack) (string, protocol.ItemStack, bool) {
	stew := recipes().stew
	for i := range stew {
		r := &stew[i]
		if !ShapelessIngredientsFit(r.Ingredients, cells) {
			continue
		}
		var effects protocol.Buffer
		effects.PutVarInt(1)
		effects.PutVarInt(MustMobEffect(r.Effect))
		effects.PutVarInt(r.Duration)
		result := protocol.ItemStack{ItemID: r.ResultID, Count: r.ResultCount}
		result.SetRaw(protocol.RawComponent{Type: protocol.DataComponentSuspiciousStew, Data: effects.Bytes()})
		return r.ID, result, true
	}
	return "", protocol.ItemStack{}, false
}

// imbueResult matches a 3x3 grid of the material around the source, the
// result taking the source's potion_contents.
// vanilla: ImbueRecipe.matches, assemble
func imbueResult(cells []protocol.ItemStack) (string, protocol.ItemStack, bool) {
	if len(cells) != 9 {
		return "", protocol.ItemStack{}, false
	}
	imbue := recipes().imbue
	for i := range imbue {
		r := &imbue[i]
		if !imbueFits(r.Source, cells[4]) {
			continue
		}
		fits := true
		for j, cell := range cells {
			if j != 4 && !imbueFits(r.Material, cell) {
				fits = false
				break
			}
		}
		if !fits {
			continue
		}
		result := protocol.ItemStack{ItemID: r.ResultID, Count: r.ResultCount}
		if contents, ok := cells[4].PotionContentsOf(); ok {
			result.SetPotionContents(contents)
		}
		return r.ID, result, true
	}
	return "", protocol.ItemStack{}, false
}

func imbueFits(ingredient Ingredient, cell protocol.ItemStack) bool {
	return !cell.Empty() && slices.Contains(ingredient, cell.ItemID)
}
