package player

import (
	"slices"

	"github.com/golem-mc/golem/registry"
)

// Holder.Reference uses JVM identity hashes. These were recorded from one
// vanilla 26.3 JVM so every attribute map uses the same layout.
var attributeHashes = [registry.NumAttributes]uint32{
	registry.AttributeAirDragModifier:              0x44db6d15,
	registry.AttributeArmor:                        0x77303268,
	registry.AttributeArmorToughness:               0x3e411e92,
	registry.AttributeAttackDamage:                 0x2d760326,
	registry.AttributeAttackKnockback:              0x5c09bc2d,
	registry.AttributeAttackSpeed:                  0x6824b913,
	registry.AttributeBelowNameDistance:            0x6b6c63b4,
	registry.AttributeBlockBreakSpeed:              0x03b6a27e,
	registry.AttributeBlockInteractionRange:        0x0292a68e,
	registry.AttributeBounciness:                   0x0a6b14ab,
	registry.AttributeBurningTime:                  0x0f47d46d,
	registry.AttributeCameraDistance:               0x142dc2d1,
	registry.AttributeExplosionKnockbackResistance: 0x160daefa,
	registry.AttributeEntityInteractionRange:       0x6cccd2f8,
	registry.AttributeFallDamageMultiplier:         0x59776bbd,
	registry.AttributeFlyingSpeed:                  0x21cdff31,
	registry.AttributeFollowRange:                  0x04555e3a,
	registry.AttributeFrictionModifier:             0x591ebb5c,
	registry.AttributeGravity:                      0x06e07e4c,
	registry.AttributeJumpStrength:                 0x7d616564,
	registry.AttributeKnockbackResistance:          0x41a3dd70,
	registry.AttributeLuck:                         0x07bf01cb,
	registry.AttributeMaxAbsorption:                0x1b13467c,
	registry.AttributeMaxHealth:                    0x2db2a05f,
	registry.AttributeMiningEfficiency:             0x61484cef,
	registry.AttributeMovementEfficiency:           0x76793278,
	registry.AttributeMovementSpeed:                0x5eea5627,
	registry.AttributeNameTagDistance:              0x39c4e849,
	registry.AttributeOxygenBonus:                  0x67bb22d6,
	registry.AttributeSafeFallDistance:             0x49e4c2d5,
	registry.AttributeScale:                        0x017ddd05,
	registry.AttributeSneakingSpeed:                0x283dd1b7,
	registry.AttributeSpawnReinforcements:          0x2fa2a189,
	registry.AttributeStepHeight:                   0x22f1d280,
	registry.AttributeSubmergedMiningSpeed:         0x15477336,
	registry.AttributeSweepingDamageRatio:          0x1da4c9be,
	registry.AttributeTemptRange:                   0x35aadef8,
	registry.AttributeWaterMovementEfficiency:      0x2fff00d3,
	registry.AttributeWaypointTransmitRange:        0x6e0e5dec,
	registry.AttributeWaypointReceiveRange:         0x3354d221,
}

// vanilla: AttributeMap.pack, Object2ObjectOpenHashMap.rehash, MapIterator.nextEntry
func packedAttributeOrder(inserted []uint8) []registry.AttributeID {
	slots := make([]registry.AttributeID, 32) // fastutil's default 16 entries at a 0.75 load factor
	for i, id := range inserted {
		putAttributeSlot(slots, registry.AttributeID(id))
		if i+1 > len(slots)*3/4 {
			previous := slots
			slots = make([]registry.AttributeID, len(previous)*2)
			for _, slot := range slices.Backward(previous) {
				if slot != 0 {
					putAttributeSlot(slots, slot-1)
				}
			}
		}
	}
	order := make([]registry.AttributeID, 0, len(inserted))
	for _, slot := range slices.Backward(slots) {
		if slot != 0 {
			order = append(order, slot-1)
		}
	}
	return order
}

// putAttributeSlot stores id+1 so zero marks an empty bucket. vanilla: HashCommon.mix
func putAttributeSlot(slots []registry.AttributeID, id registry.AttributeID) {
	hash := attributeHashes[id] * 0x9e3779b9
	mask := uint32(len(slots) - 1)
	pos := (hash ^ (hash >> 16)) & mask
	for slots[pos] != 0 {
		pos = (pos + 1) & mask
	}
	slots[pos] = id + 1
}
