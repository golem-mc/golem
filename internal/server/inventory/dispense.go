package inventory

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/registry"
)

// Dispenser and dropper decisions: which slot fires, what the item does,
// where a dropped or shot item starts. The server carries them out.
// vanilla: DispenserBlock.dispenseFrom and its behaviours.

// DispenseBehaviour is what a dispenser does with an item; anything
// unmodelled drops.
type DispenseBehaviour int8

// The behaviours.
const (
	DispenseDrop DispenseBehaviour = iota
	DispenseArrow
	DispenseThrown
	DispenseFluidBucket
	DispenseEmptyBucket
	DispenseTNT
	DispenseBoneMeal
	DispenseShears
	DispenseBrush
	DispenseFlintAndSteel
	DispenseGlowstone
	DispenseGlassBottle
	DispenseSpawnEgg
	DispenseBoat
	DispenseShulkerBox
	DispenseFireCharge
	DispenseEquipment
	DispenseOptionalEquipment
	DispenseHoneycomb
	DispenseSulfurBlock
	DispenseMinecart
	DispenseFirework
	DispenseWindCharge
	DispenseArmorStand
	DispenseChest
	DispenseWaterBottle
)

// DispenseBehaviourOf is vanilla's getDispenseMethod for the items kept here.
func DispenseBehaviourOf(stack protocol.ItemStack) DispenseBehaviour {
	switch registry.Item(stack.ItemID) {
	case registry.ItemArmorStand:
		return DispenseArmorStand
	case registry.ItemChest:
		return DispenseChest
	case registry.ItemPotion:
		return DispenseWaterBottle
	case registry.ItemArrow, registry.ItemTippedArrow, registry.ItemSpectralArrow:
		return DispenseArrow
	case registry.ItemEgg, registry.ItemBrownEgg, registry.ItemBlueEgg, registry.ItemSnowball,
		registry.ItemSplashPotion, registry.ItemLingeringPotion, registry.ItemExperienceBottle:
		return DispenseThrown
	case registry.ItemWaterBucket, registry.ItemLavaBucket, registry.ItemPowderSnowBucket,
		registry.ItemCodBucket, registry.ItemSalmonBucket, registry.ItemAxolotlBucket,
		registry.ItemTadpoleBucket, registry.ItemSulfurCubeBucket,
		registry.ItemPufferfishBucket, registry.ItemTropicalFishBucket:
		return DispenseFluidBucket
	case registry.ItemBucket:
		return DispenseEmptyBucket
	case registry.ItemTnt:
		return DispenseTNT
	case registry.ItemBoneMeal:
		return DispenseBoneMeal
	case registry.ItemBrush:
		return DispenseBrush
	case registry.ItemShears:
		return DispenseShears
	case registry.ItemFlintAndSteel:
		return DispenseFlintAndSteel
	case registry.ItemGlowstone:
		return DispenseGlowstone
	case registry.ItemGlassBottle:
		return DispenseGlassBottle
	case registry.ItemMinecart, registry.ItemChestMinecart, registry.ItemFurnaceMinecart,
		registry.ItemTntMinecart, registry.ItemHopperMinecart, registry.ItemCommandBlockMinecart:
		return DispenseMinecart
	case registry.ItemFireworkRocket:
		return DispenseFirework
	case registry.ItemWindCharge:
		return DispenseWindCharge
	case registry.ItemFireCharge:
		return DispenseFireCharge
	case registry.ItemHoneycomb:
		return DispenseHoneycomb
	case registry.ItemCarvedPumpkin, registry.ItemWitherSkeletonSkull:
		return DispenseOptionalEquipment
	}
	if _, egg := registry.SpawnEggEntity(registry.Item(stack.ItemID)); egg {
		return DispenseSpawnEgg
	}
	if registry.Item(stack.ItemID).InTag(registry.ItemTagIDBoats) {
		return DispenseBoat
	}
	if registry.Item(stack.ItemID).InTag(registry.ItemTagIDShulkerBoxes) {
		return DispenseShulkerBox
	}
	if _, worn := registry.StackEquippable(stack); worn {
		return DispenseEquipment
	}
	if registry.Item(stack.ItemID).InTag(registry.ItemTagIDSulfurCubeSwallowable) {
		return DispenseSulfurBlock
	}
	return DispenseDrop
}

// Dispense constants: the default projectile config (vanilla
// ProjectileItem.DispenseConfig: power 1.1, uncertainty 6.0), the thrown
// potion's overrides, a projectile's start offsets along and above the face,
// and the TNT fuse.
const (
	DispenseProjectilePower      = 1.1
	DispenseProjectileInaccuracy = 6.0

	// Power a quarter more, uncertainty halved.
	// vanilla: ThrowablePotionItem.createDispenseConfig and
	// ExperienceBottleItem.createDispenseConfig
	DispensePotionPower      = DispenseProjectilePower * 1.25
	DispensePotionInaccuracy = DispenseProjectileInaccuracy * 0.5

	DispenseTNTFuse = 80

	dispenseProjectileDistance = 0.7
	dispenseProjectileLift     = 0.1

	// dispenseDropSpread is vanilla's 0.0172275 times the six ticks a
	// spawn's settle takes.
	dispenseDropSpread = 0.0172275 * 6
)

// DispenseBoatSpot is a dispensed boat's position (y raised 1 over water in
// front, 0 over air above water) and yaw. The caller checked the cell; width
// is the hull's.
// vanilla: BoatDispenseItemBehavior.execute
func DispenseBoatSpot(x, y, z int32, face int32, width float64, inWater bool) (px, py, pz float64, yaw float32) {
	offset := protocol.FaceOffset[face]
	out := 0.5625 + width/2
	lift := 0.0
	if inWater {
		lift = 1.0
	}
	px = float64(x) + 0.5 + float64(offset[0])*out
	py = float64(y) + 0.5 + float64(float32(offset[1])*1.125) + lift
	pz = float64(z) + 0.5 + float64(offset[2])*out
	switch face {
	case protocol.FaceWest:
		yaw = 90
	case protocol.FaceNorth:
		yaw = 180
	case protocol.FaceEast:
		yaw = 270
	}
	return px, py, pz, yaw
}

// DispenseMinecartSpot returns the rail placement spot, or false to drop the item.
// vanilla: MinecartDispenseItemBehavior.execute
func DispenseMinecartSpot(x, y, z int32, face int32, front, below uint16, frontAir bool) (px, py, pz float64, ok bool) {
	railTag := registry.TagIDRails
	rail := front
	lift := 0.1
	if !registry.BlockOf(front).InTag(railTag) {
		if !frontAir || !registry.BlockOf(below).InTag(railTag) {
			return 0, 0, 0, false
		}
		rail, lift = below, -0.9
	}
	switch registry.StateValue(rail, registry.PropShape) {
	case "ascending_east", "ascending_west", "ascending_north", "ascending_south":
		if lift > 0 || face != protocol.FaceBottom {
			lift += 0.5
		}
	}
	offset := protocol.FaceOffset[face]
	return float64(x) + 0.5 + float64(offset[0])*1.125,
		float64(y) + float64(offset[1]) + lift,
		float64(z) + 0.5 + float64(offset[2])*1.125, true
}

// DispenseFireworkStart puts the rocket just outside the dispenser's face.
// vanilla: FireworkRocketItem.getEntityJustOutsideOfBlockPos
func DispenseFireworkStart(x, y, z int32, face int32) (px, py, pz float64) {
	const out = 0.5000099999997474
	offset := protocol.FaceOffset[face]
	return float64(x) + 0.5 + float64(offset[0])*out,
		float64(y) + 0.5 + float64(offset[1])*out,
		float64(z) + 0.5 + float64(offset[2])*out
}

// DispenseShulkerFacing is a dispensed shulker box's facing: the
// dispenser's own when the cell below the target is empty, else up.
// vanilla: ShulkerBoxDispenseBehavior.execute, ShulkerBoxBlock.getStateForPlacement
func DispenseShulkerFacing(face int32, belowEmpty bool) (int32, string) {
	if !belowEmpty {
		face = protocol.FaceTop
	}
	return face, [...]string{"down", "up", "north", "south", "west", "east"}[face]
}

// DispenseSlot is vanilla's DispenserBlockEntity.getRandomSlot, a reservoir
// draw over the non-empty slots; -1 when all are empty.
func DispenseSlot(b Block, dice *rules.JavaRandom) int {
	slot := -1
	j := int32(1)
	stacks := b.Stacks()
	for i := range stacks {
		if stacks[i].Empty() {
			continue
		}
		if dice.NextIntBound(j) == 0 {
			slot = i
		}
		j++
	}
	return slot
}

// DispenseDropMotion is a dropped item's start, 0.7 along the face and
// lowered by the face's drop, and its velocity.
// vanilla: DefaultDispenseItemBehavior.spawnItem
func DispenseDropMotion(dice *rules.JavaRandom, x, y, z int32, face int32) (px, py, pz, vx, vy, vz float64) {
	offset := protocol.FaceOffset[face]
	dx, dy, dz := float64(offset[0]), float64(offset[1]), float64(offset[2])
	drop := 0.15625
	if face == protocol.FaceBottom || face == protocol.FaceTop {
		drop = 0.125
	}
	r := dice.NextDouble()*0.1 + 0.2
	px = float64(x) + 0.5 + dx*0.7
	py = float64(y) + 0.5 + dy*0.7 - drop
	pz = float64(z) + 0.5 + dz*0.7
	vx = dice.Triangle(dx*r, dispenseDropSpread)
	vy = dice.Triangle(0.2, dispenseDropSpread)
	vz = dice.Triangle(dz*r, dispenseDropSpread)
	return px, py, pz, vx, vy, vz
}

// DispenseProjectileStart is where a dispensed projectile starts: 0.7 along
// the face and 0.1 above it.
func DispenseProjectileStart(x, y, z int32, face int32) (px, py, pz float64) {
	offset := protocol.FaceOffset[face]
	return float64(x) + 0.5 + float64(offset[0])*dispenseProjectileDistance,
		float64(y) + 0.5 + float64(offset[1])*dispenseProjectileDistance + dispenseProjectileLift,
		float64(z) + 0.5 + float64(offset[2])*dispenseProjectileDistance
}

// DispenseFireChargeStart is a block along the face from the centre.
// vanilla: FireChargeItem.createDispenseConfig
func DispenseFireChargeStart(x, y, z int32, face int32) (px, py, pz float64) {
	offset := protocol.FaceOffset[face]
	return float64(x) + 0.5 + float64(offset[0]),
		float64(y) + 0.5 + float64(offset[1]),
		float64(z) + 0.5 + float64(offset[2])
}

// DispenseGiveBack returns a dispense's owed stack to its slot if that takes
// it (empty, or the same item with room), else to any slot, returning what
// did not fit. The owed stack is always one item (kept, or the bucket it
// became), so a partial merge never arises.
// vanilla: DispenserBlock.dispenseFrom
func DispenseGiveBack(b Block, slot int, stack protocol.ItemStack) protocol.ItemStack {
	tryMoveInItem(b, &stack, slot)
	if stack.Empty() {
		return stack
	}
	left, _ := AddItem(b, stack, 0, false)
	return left
}

// DispenseArmorStandYaw returns the facing's rotation, including vertical faces.
// vanilla: Direction.toYRot
func DispenseArmorStandYaw(face int32) float32 {
	return [...]float32{270, 270, 180, 0, 90, 270}[face]
}
