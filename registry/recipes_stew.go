package registry

import "github.com/golem-mc/golem/internal/protocol"

// The suspicious stew recipes: the result carries an effect fixed per recipe.

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
