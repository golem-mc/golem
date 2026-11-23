package registry

import "github.com/golem-mc/golem/internal/protocol"

// repairBonusPercent is the share of the maximum durability a repair craft
// adds. vanilla: RepairItemRecipe.assemble
const repairBonusPercent = 5

// repairItem matches two single, wearable stacks of one item: the result has
// both remainders plus the bonus as durability, and carries only the curses
// of the two. Other components, a custom name included, are dropped.
// vanilla: RepairItemRecipe
func repairItem(cells []protocol.ItemStack) (protocol.ItemStack, bool) {
	i, j, ok := gridPair(cells)
	if !ok {
		return protocol.ItemStack{}, false
	}
	first, second := cells[i], cells[j]
	firstMax, secondMax := StackMaxDamage(first), StackMaxDamage(second)
	if first.ItemID != second.ItemID || first.Count != 1 || second.Count != 1 || firstMax <= 0 || secondMax <= 0 ||
		first.Removes(protocol.DataComponentDamage) || second.Removes(protocol.DataComponentDamage) {
		return protocol.ItemStack{}, false
	}
	maxDamage := max(firstMax, secondMax)
	remaining := (firstMax - first.Damage) + (secondMax - second.Damage) + maxDamage*repairBonusPercent/100
	result := protocol.ItemStack{ItemID: first.ItemID, Count: 1, Damage: max(maxDamage-remaining, 0)}
	if maxDamage != MaxDamage(first.ItemID) { // a patch equal to the prototype is dropped
		var b protocol.Buffer
		b.PutVarInt(maxDamage)
		result.SetRaw(protocol.RawComponent{Type: protocol.DataComponentMaxDamage, Data: b.Bytes()})
	}
	for _, source := range []map[int32]int32{first.Enchantments, second.Enchantments} {
		for id, level := range source {
			if !EnchantmentInTag(id, "minecraft:curse") {
				continue
			}
			result.SetEnchantment(false, id, max(result.Enchantments[id], level))
		}
	}
	return result, true
}
