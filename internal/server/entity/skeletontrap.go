package entity

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/registry"
)

const (
	// skeletonTrapInvulnerable is the hurt window, in ticks, a sprung trap's
	// riders and extra horses arrive with.
	// vanilla: SkeletonTrapGoal.createHorse, createSkeleton
	skeletonTrapInvulnerable = 60
	// skeletonTrapPush is the spread of the push each extra horse gets.
	// vanilla: SkeletonTrapGoal.tick
	skeletonTrapPush = 1.1485
)

// TrapPushes rolls the push of each of a sprung trap's extra horses, x and z,
// from the trap horse's dice.
// vanilla: SkeletonTrapGoal.tick
func (m *Mob) TrapPushes() [3][2]float64 {
	var out [3][2]float64
	for i := range out {
		out[i][0] = skeletonTrapPush * (m.Rand.Float64() - m.Rand.Float64())
		out[i][1] = skeletonTrapPush * (m.Rand.Float64() - m.Rand.Float64())
	}
	return out
}

// ReadyTrapHorse makes a finalized skeleton horse one of a sprung trap's
// extras: briefly invulnerable, persistent, tame and grown.
// vanilla: SkeletonTrapGoal.createHorse
func (m *Mob) ReadyTrapHorse() {
	m.InvulnTicks = skeletonTrapInvulnerable
	m.Persistence = true
	m.SetMetadataHorseTame(true)
	m.SetMetadataBabyAge(0)
}

// ArmTrapRider makes a finalized skeleton a sprung trap's rider: briefly
// invulnerable, persistent, an iron helmet on a bare head, and its hand and
// head enchanted afresh whatever the finalize gave them.
// vanilla: SkeletonTrapGoal.createSkeleton, SkeletonTrapGoal.enchant
func (m *Mob) ArmTrapRider(local rules.LocalDifficulty) {
	m.InvulnTicks = skeletonTrapInvulnerable
	m.Persistence = true
	if m.Worn[wornHead].Empty() {
		m.SetEquipment(protocol.EquipFeet+wornHead, protocol.ItemStack{ItemID: int32(registry.ItemIronHelmet), Count: 1})
	}
	r := rules.NewJavaRandom(m.Rand.Int63())
	for _, slot := range [2]int{protocol.EquipMainHand, protocol.EquipHead} {
		stack := m.EquipmentSlot(slot)
		stack.SetEnchantments(false, nil)
		if !stack.Empty() {
			enchantForSpawn(r, &stack, local)
		}
		m.SetEquipment(slot, stack)
	}
}
