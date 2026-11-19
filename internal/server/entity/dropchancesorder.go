package entity

import (
	"math/rand/v2"
	"slices"
)

// DropChancesOrder fixes equipment enum identity hashes for native codec traversal.
// Contexts are immutable and may be shared by independent simulations.
type DropChancesOrder struct {
	slots [8]int
}

// NewDropChancesOrder uses hashes in EquipmentSlot declaration order, including body and saddle.
func NewDropChancesOrder(hashes [8]uint32) *DropChancesOrder {
	order := &DropChancesOrder{}
	for i := range order.slots {
		order.slots[i] = i
	}
	// vanilla: DropChances.filterDefaultValues copies the full EnumMap into a
	// 16-bucket HashMap before removing defaults. Bucket ties retain enum order;
	// removal neither shrinks the table nor reorders the surviving entries.
	slices.SortStableFunc(order.slots[:], func(a, b int) int {
		bucket := func(i int) int {
			hash := hashes[i] & 0x7fffffff
			return int((hash ^ (hash >> 16)) & 15)
		}
		return bucket(a) - bucket(b)
	})
	return order
}

var defaultDropChancesOrder = func() *DropChancesOrder {
	var hashes [8]uint32
	for i := range hashes {
		hashes[i] = rand.Uint32() & 0x7fffffff
	}
	return NewDropChancesOrder(hashes)
}()

// SetDropChancesOrder selects the native equipment-map traversal context.
func (m *Mob) SetDropChancesOrder(order *DropChancesOrder) {
	m.dropChancesOrder = order
}
