package registry

import (
	"github.com/golem-mc/golem/internal/protocol"
)

// CraftingRemainder is the stack a craft leaves behind when it spends the
// item in a grid cell - a milk bucket's empty bucket, a honey bottle's
// glass bottle - and nothing for an item that leaves no remainder. Where
// the stack goes is the caller's to decide.
// vanilla: Item.getCraftingRemainder
func CraftingRemainder(item int32) protocol.ItemStack {
	name, ok := ItemName(item)
	if !ok {
		return protocol.ItemStack{}
	}
	remainder, ok := craftingRemainders[name]
	if !ok {
		return protocol.ItemStack{}
	}
	return protocol.ItemStack{ItemID: MustItem(remainder), Count: 1}
}
