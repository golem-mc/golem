package entity

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/rules"
)

// standLocated is an armor stand's record of the last pack location_changed
// check.
type standLocated struct {
	cell   [3]int32
	seen   bool
	pieces [6]protocol.ItemStack
	active rules.LocatedState
}

// EnchEquipment is the armor and hands whose enchantments act on the stand.
func (s *ArmorStand) EnchEquipment() []rules.EnchPiece {
	return enchEquipment(s.Worn, s.MainHand, s.OffHand)
}

// Located is the set of location_changed entries active on the stand.
func (s *ArmorStand) Located() *rules.LocatedState { return &s.located.active }

// locatedActive reports whether a worn stack is enchanted or an entry is active.
func (s *ArmorStand) locatedActive() bool {
	return !s.located.active.Empty() || rules.HasLocated(s.MainHand) || rules.HasLocated(s.OffHand) ||
		wornEnchanted(&s.Worn)
}

// locatedStep reports the Level of the located event: a bit per EnchEquipment
// piece whose stack changed, LocatedMoved for a new cell and LocatedTick every
// tick an enchantment is worn; 0 while none is.
// vanilla: LivingEntity.baseTick, collectEquipmentChanges
func (s *ArmorStand) locatedStep() int32 {
	l := &s.located
	if !s.locatedActive() {
		// Nothing to run; seen=false makes the first enchanted tick a full pass.
		l.seen = false
		return 0
	}
	cur := [6]protocol.ItemStack{s.Worn[0], s.Worn[1], s.Worn[2], s.Worn[3], s.MainHand, s.OffHand}
	mask := LocatedTick
	for i := range cur {
		if !cur[i].Equal(l.pieces[i]) {
			mask |= 1 << i
		}
	}
	cell := [3]int32{rules.BlockOf(s.X), rules.BlockOf(s.Y), rules.BlockOf(s.Z)}
	if !l.seen || l.cell != cell {
		mask |= LocatedMoved
	}
	l.cell, l.seen, l.pieces = cell, true, cur
	return mask
}

func wornEnchanted(worn *[4]protocol.ItemStack) bool {
	for i := range worn {
		if rules.HasLocated(worn[i]) {
			return true
		}
	}
	return false
}
