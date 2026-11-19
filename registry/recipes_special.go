package registry

import (
	"github.com/golem-mc/golem/internal/protocol"
)

// The firework special recipes, matched here because their stacks are
// computed from the grid; the data is the crafting_special_firework_*
// files. The banner ones are in recipes_banner.go.

const fireworkRocketCount = 3

// The firework explosion shape IDs the star recipe picks by shape item.
// vanilla: FireworkExplosion.Shape
const (
	fireworkShapeSmallBall = 0
	fireworkShapeLargeBall = 1
	fireworkShapeStar      = 2
	fireworkShapeCreeper   = 3
	fireworkShapeBurst     = 4
)

// fireworkItems are the item IDs the three recipes name, looked up once.
var fireworkItems = struct {
	paper, gunpowder, star, trail, twinkle int32
	shapes                                 map[int32]int32
}{
	paper:     mustItemID("minecraft:paper"),
	gunpowder: mustItemID("minecraft:gunpowder"),
	star:      mustItemID("minecraft:firework_star"),
	trail:     mustItemID("minecraft:diamond"),
	twinkle:   mustItemID("minecraft:glowstone_dust"),
	shapes: map[int32]int32{
		mustItemID("minecraft:fire_charge"): fireworkShapeLargeBall,
		mustItemID("minecraft:feather"):     fireworkShapeBurst,
		mustItemID("minecraft:gold_nugget"): fireworkShapeStar,
	},
}

func mustItemID(name string) int32 {
	id, ok := ItemID(name)
	if !ok {
		panic("registry: no item " + name)
	}
	return id
}

// fireworkDye returns the firework colour of a dye item.
func fireworkDye(stack protocol.ItemStack) (int32, bool) {
	if !Item(stack.ItemID).InTag(ItemTagIDDyes) {
		return 0, false
	}
	c, ok := StackDyeColour(stack)
	if !ok {
		return 0, false
	}
	return FireworkDyeColour(int(c)), true
}

// fireworkShape returns the shape a shape item picks, false for any other.
func fireworkShape(item int32) (int32, bool) {
	if Item(item).InTag(ItemTagIDSkulls) {
		return fireworkShapeCreeper, true
	}
	shape, ok := fireworkItems.shapes[item]
	return shape, ok
}

// specialCraftResult returns what the grid-computed recipes make of the
// grid, the empty stack when none matches.
func specialCraftResult(cells []protocol.ItemStack) protocol.ItemStack {
	filled := 0
	for i := range cells {
		if !cells[i].Empty() {
			filled++
		}
	}
	if filled < 2 {
		return protocol.ItemStack{}
	}
	for _, recipe := range []func([]protocol.ItemStack) (protocol.ItemStack, bool){
		fireworkRocket, fireworkStar, fireworkStarFade, shieldDecoration, bannerDuplicateResult,
		bookCloneResult, mapClone, mapExtend, decoratedPot,
		transmuteDyed, dyedArmour, repairItem,
	} {
		if result, ok := recipe(cells); ok {
			return result
		}
	}
	return protocol.ItemStack{}
}

// fireworkRocket is one paper, one to three gunpowder and any stars.
// vanilla: FireworkRocketRecipe
func fireworkRocket(cells []protocol.ItemStack) (protocol.ItemStack, bool) {
	hasShell := false
	fuel := int32(0)
	var explosions []protocol.FireworkExplosion
	for _, cell := range cells {
		switch {
		case cell.Empty():
		case cell.ItemID == fireworkItems.paper:
			if hasShell {
				return protocol.ItemStack{}, false
			}
			hasShell = true
		case cell.ItemID == fireworkItems.gunpowder:
			if fuel++; fuel > 3 {
				return protocol.ItemStack{}, false
			}
		case cell.ItemID == fireworkItems.star:
			// A star without an explosion adds none.
			if e, ok := cell.FireworkExplosionOf(); ok {
				explosions = append(explosions, e)
			}
		default:
			return protocol.ItemStack{}, false
		}
	}
	if !hasShell || fuel < 1 {
		return protocol.ItemStack{}, false
	}
	rocket, _ := ItemID("minecraft:firework_rocket")
	result := protocol.ItemStack{ItemID: rocket, Count: fireworkRocketCount}
	// A component equal to the item's default is dropped from the patch.
	// vanilla: PatchedDataComponentMap.set
	if fuel != 1 || len(explosions) > 0 {
		result.SetRaw(protocol.FireworksRawComponent(fuel, explosions))
	}
	return result, true
}

// fireworkStar is one gunpowder, at least one dye, and at most one each of
// a shape item, a trail and a twinkle.
// vanilla: FireworkStarRecipe
func fireworkStar(cells []protocol.ItemStack) (protocol.ItemStack, bool) {
	var hasFuel, hasShape, hasTrail, hasTwinkle bool
	explosion := protocol.FireworkExplosion{Shape: fireworkShapeSmallBall}
	for _, cell := range cells {
		if cell.Empty() {
			continue
		}
		if colour, isDye := fireworkDye(cell); isDye {
			explosion.Colors = append(explosion.Colors, colour)
			continue
		}
		shape, isShape := fireworkShape(cell.ItemID)
		var seen *bool
		switch {
		case cell.ItemID == fireworkItems.twinkle:
			seen = &hasTwinkle
		case cell.ItemID == fireworkItems.trail:
			seen = &hasTrail
		case cell.ItemID == fireworkItems.gunpowder:
			seen = &hasFuel
		case isShape:
			seen = &hasShape
			explosion.Shape = shape
		default:
			return protocol.ItemStack{}, false
		}
		if *seen {
			return protocol.ItemStack{}, false
		}
		*seen = true
	}
	if !hasFuel || len(explosion.Colors) == 0 {
		return protocol.ItemStack{}, false
	}
	explosion.Trail, explosion.Twinkle = hasTrail, hasTwinkle
	result := protocol.ItemStack{ItemID: fireworkItems.star, Count: 1}
	result.SetRaw(explosion.RawComponent())
	return result, true
}

// fireworkStarFade is one star and at least one dye: the star with the dyes
// as its fade colours.
// vanilla: FireworkStarFadeRecipe
func fireworkStarFade(cells []protocol.ItemStack) (protocol.ItemStack, bool) {
	var target protocol.ItemStack
	var colours []int32
	for _, cell := range cells {
		if cell.Empty() {
			continue
		}
		if colour, isDye := fireworkDye(cell); isDye {
			colours = append(colours, colour)
			continue
		}
		if cell.ItemID != fireworkItems.star || !target.Empty() {
			return protocol.ItemStack{}, false
		}
		target = cell
	}
	if target.Empty() || len(colours) == 0 {
		return protocol.ItemStack{}, false
	}
	explosion, _ := target.FireworkExplosionOf()
	explosion.FadeColors = colours
	result := target
	result.Count = 1
	result.Raw = append([]protocol.RawComponent(nil), target.Raw...)
	result.SetRaw(explosion.RawComponent())
	return result, true
}

// FireworkDyeColour returns a dye's firework RGB colour in DyeNames order.
func FireworkDyeColour(dye int) int32 { return fireworkDyeColours[DyeNames[dye]] }
