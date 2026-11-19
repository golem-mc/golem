package rules

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/registry"
)

// vanilla: Item.getUseDuration
func TestDefaultUseIsInstant(t *testing.T) {
	stack := func(item registry.Item) protocol.ItemStack {
		return protocol.ItemStack{ItemID: int32(item), Count: 1}
	}
	rod := stack(registry.ItemFishingRod)
	consumable := rod
	consumable.SetConsumable(protocol.ConsumableComponent{Seconds: 1})
	instantConsumable := rod
	instantConsumable.SetConsumable(protocol.ConsumableComponent{Seconds: 0})
	blocking := rod
	blocking.SetBlocksAttacks(protocol.BlocksAttacksComponent{})
	unblocked := stack(registry.ItemShield)
	unblocked.Remove(protocol.DataComponentBlocksAttacks)
	for _, tc := range []struct {
		name  string
		stack protocol.ItemStack
		want  bool
	}{
		{"rod", rod, true},
		{"consumable", consumable, false},
		{"instant consumable", instantConsumable, true},
		{"blocking", blocking, false},
		{"shield without blocking", unblocked, true},
		{"spear", stack(registry.ItemWoodenSpear), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := DefaultUseIsInstant(tc.stack); got != tc.want {
				t.Errorf("DefaultUseIsInstant = %v, want %v", got, tc.want)
			}
		})
	}
}
