package registry

// Rarity is the vanilla Rarity enum, which colours an item's name; an
// enchanted stack's rarity is one above its own. vanilla: ItemStack.getRarity
type Rarity int8

const (
	RarityCommon Rarity = iota
	RarityUncommon
	RarityRare
	RarityEpic
)

// Color is the ChatFormatting name the rarity's colour travels under in a
// text component: the shade the client draws the item's name in.
func (r Rarity) Color() string {
	switch r {
	case RarityUncommon:
		return "yellow"
	case RarityRare:
		return "aqua"
	case RarityEpic:
		return "light_purple"
	default:
		return "white"
	}
}

// RarityOf is an item's own rarity, the common everything the reports leave
// unlisted is. The enchantment bump a stack's enchantments give it is the
// caller's to apply, rarity being the item's rather than the stack's.
func RarityOf(item int32) Rarity {
	if item < 0 || int(item) >= len(Items) {
		return RarityCommon
	}
	if rarity, ok := rarityByName[Items[item]]; ok {
		return rarity
	}
	return RarityCommon
}

// GlintByDefault reports whether the item's own components switch the glint
// on, deciding whether a glint_override of true is worth carrying. Unknown
// items do not glint.
func GlintByDefault(item int32) bool {
	if item < 0 || int(item) >= len(Items) {
		return false
	}
	return glintByDefault[Items[item]]
}
