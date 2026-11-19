package rules

import (
	"math"

	"github.com/golem-mc/golem/internal/server/blocklogic"
	"github.com/golem-mc/golem/registry"
)

// airState is the block state ID of air, which is always 0: air is the first
// entry in every version's block registry.
const airState uint16 = 0

// IsWater reports water held by liquid blocks, waterlogged states or plants.
func IsWater(state uint16) bool {
	return blocklogic.FluidOf(state).Kind == blocklogic.FluidWater
}

// IsWaterBlock reports the water block itself at any level, not
// waterlogged blocks, seagrass, kelp or bubble columns.
// vanilla: EnterBlockTrigger.matches (state.is(Blocks.WATER))
func IsWaterBlock(state uint16) bool {
	return registry.BlockOf(state) == registry.BlockWater
}

// IsWaterThatWouldFlow reports whether a flooded anchor's neighbour water
// would run on: a source (level 0, waterlogged, or a plant) braces; a flow
// only with at least two eighths and no water directly below.
// vanilla: RespawnAnchorBlock.isWaterThatWouldFlow
func IsWaterThatWouldFlow(w BlockReader, x, y, z int32) bool {
	f := blocklogic.FluidOf(w.BlockAt(x, y, z))
	if f.Kind != blocklogic.FluidWater {
		return false
	}
	if f.IsSource() {
		return true
	}
	// Only minecraft:water reaches here: a flow of level 1-7, whose amount
	// thins by the level, or a falling column (level 8 and up), which
	// FluidState.getAmount reads as full.
	amount := blocklogic.FluidAmount(int(f.Level))
	if amount < 2 {
		return false
	}
	return !IsWater(w.BlockAt(x, y-1, z))
}

// IsBubbleColumn reports a bubble column, where breath is not lost.
// vanilla: LivingEntity.baseTick
func IsBubbleColumn(state uint16) bool {
	return registry.BlockOf(state) == registry.BlockBubbleColumn
}

// IsClimbable reports whether feet in state can climb: state's block is in
// minecraft:climbable, or it is an open trapdoor (any *_trapdoor) over a
// ladder (below) of the same facing.
// vanilla: LivingEntity.onClimbable
func IsClimbable(state, below uint16) bool {
	b := registry.BlockOf(state)
	if b.InTag(climbableBlocks) {
		return true
	}
	if !trapdoorBlocks[b] {
		return false
	}
	if !registry.StateBool(state, registry.PropOpen) {
		return false
	}
	return registry.BlockOf(below) == registry.BlockLadder &&
		registry.StateValue(below, registry.PropFacing) == registry.StateValue(state, registry.PropFacing)
}

var (
	climbableBlocks = registry.BlockTagOf("minecraft:climbable")
	trapdoorBlocks  = registry.BlocksInGroup(registry.GroupTrapdoor)
	// The Blocks.BED colours; the straw bed has neither bounceRestitution nor
	// fallDistanceReduction.
	bedBlocks = registry.BlocksOf(func(b registry.Block) bool {
		return b.InGroup(registry.GroupBed) && b != registry.BlockStrawBed
	})
)

// PossibleToRespawnInThis reports a cell a body can be put down in: neither
// solid nor liquid, or a sign, banner or pressure plate, whose classes say
// yes though solid.
// vanilla: Block.isPossibleToRespawnInThis, SignBlock, AbstractBannerBlock,
// BasePressurePlateBlock
func PossibleToRespawnInThis(state uint16) bool {
	if _, _, ok := registry.BlockStateName(state); !ok {
		return false
	}
	block := registry.BlockOf(state)
	switch block.Type() {
	case registry.BlockTypeStandingSign, registry.BlockTypeWallSign,
		registry.BlockTypeCeilingHangingSign, registry.BlockTypeWallHangingSign,
		registry.BlockTypeBanner, registry.BlockTypeWallBanner, registry.BlockTypePressurePlate:
		return true
	}
	return !registry.IsSolid(state) && !isLiquid(block)
}

// isLiquid reports whether a block is one of the pure liquid blocks, water
// or lava, rather than a waterlogged solid the way IsWater reads.
func isLiquid(b registry.Block) bool {
	return b.Type() == registry.BlockTypeLiquid
}

// BlockFriction is a block's slipperiness: 0.6, except ice and packed ice
// 0.98, blue ice a little more, and slime 0.8.
// vanilla: Block.getFriction, Blocks' ICE, PACKED_ICE, BLUE_ICE, SLIME_BLOCK
func BlockFriction(state uint16) float64 {
	switch registry.BlockOf(state) {
	case registry.BlockIce, registry.BlockPackedIce:
		return 0.98
	case registry.BlockBlueIce:
		return 0.989
	case registry.BlockSlimeBlock:
		return 0.8
	}
	return 0.6
}

// SlimeStepScale is the share of its horizontal velocity a body standing on
// slime keeps, given its vertical velocity vy; false when |vy| is 0.1 or more
// and the slime leaves it alone.
// vanilla: SlimeBlock.stepOn
func SlimeStepScale(vy float64) (float64, bool) {
	if a := math.Abs(vy); a < 0.1 {
		return 0.4 + a*0.2, true
	}
	return 0, false
}

// BlockSpeedFactor is Block.getSpeedFactor: the share of its horizontal
// velocity a body keeps after a move through, or standing on, the block.
// vanilla: Blocks' SOUL_SAND, HONEY_BLOCK speedFactor(0.4f)
func BlockSpeedFactor(state uint16) float64 {
	if b := registry.BlockOf(state); b == registry.BlockSoulSand || b == registry.BlockHoneyBlock {
		return float64(float32(0.4))
	}
	return 1
}

// BlockJumpFactor is Block.getJumpFactor, the share of a jump's power a
// body keeps leaving the block.
// vanilla: Blocks' HONEY_BLOCK jumpFactor(0.5f)
func BlockJumpFactor(state uint16) float64 {
	if registry.BlockOf(state) == registry.BlockHoneyBlock {
		return 0.5
	}
	return 1
}

// IsReplaceableState reports whether a placement may overwrite the target:
// air, or the minecraft:replaceable tag (water, lava, fire, snow layers,
// non-collidable plants). Anything else rejects the placement.
// vanilla: BlockPlaceContext.canBeReplaced
func IsReplaceableState(state uint16) bool {
	if state == airState {
		return true
	}
	return registry.BlockOf(state).IsReplaceable()
}

// CanBeReplacedByFluid reports whether a poured fluid takes a cell: any
// replaceable or non-solid block, so a torch, rail, wire or flower washes
// out where a placement would keep it.
// vanilla: BlockBehaviour.canBeReplaced(BlockState, Fluid)
func CanBeReplacedByFluid(state uint16) bool {
	return IsReplaceableState(state) || !registry.IsSolid(state)
}

// BlockPlaceSound is the place event of a block's sound type; stone's when the
// type has none.
func BlockPlaceSound(b registry.Block) registry.Sound {
	if place := registry.BlockSoundGroup(b).Place; place != registry.SoundNone {
		return place
	}
	return registry.SoundBlockStonePlace
}

// BlockPlaceSoundLevels is the volume and pitch a placement plays the place
// sound at; a type with no place event plays at stone's 1.0 and 0.8.
// vanilla: BlockItem.place
func BlockPlaceSoundLevels(b registry.Block) (volume, pitch float32) {
	t := registry.BlockSoundType(b)
	if t.Place == registry.SoundNone {
		return 1.0, 0.8
	}
	return (t.Volume + 1) / 2, t.Pitch * 0.8
}

// SolidBucketPlaceSound is the sound a bucket of solid content plays when it
// sets its block down, in place of the block's own place sound.
// vanilla: SolidBucketItem.getPlaceSound
func SolidBucketPlaceSound(item registry.Item) (registry.Sound, bool) {
	if item == registry.ItemPowderSnowBucket {
		return registry.SoundItemBucketEmptyPowderSnow, true
	}
	return registry.SoundNone, false
}

// BlockFallSound is the fall event a landing on the block plays, with the
// volume and pitch its sound type scales to. ok is false for a type with no
// fall event.
// vanilla: LivingEntity.playBlockFallSound
func BlockFallSound(b registry.Block) (registry.Sound, float32, float32, bool) {
	t := registry.BlockSoundType(b)
	if t.Fall == registry.SoundNone || t.Fall == registry.SoundIntentionallyEmpty {
		return registry.SoundNone, 0, 0, false
	}
	return t.Fall, t.Volume * 0.5, t.Pitch * 0.75, true
}

// BlockStepSound is the step event of the state's block sound type, with the
// volume and pitch the type scales to; false for a state with no block.
// vanilla: SoundType.getStepSound
func BlockStepSound(state uint16) (event registry.Sound, volume, pitch float32, ok bool) {
	sound := registry.BlockSoundType(registry.BlockOf(state))
	return sound.Step, sound.Volume, sound.Pitch, sound.Step != registry.SoundNone
}

// IsStateClimbable is Entity.isStateClimbable: a block in minecraft:climbable
// or powder snow, with no trapdoor-over-ladder rule.
func IsStateClimbable(state uint16) bool {
	b := registry.BlockOf(state)
	return b.InTag(climbableBlocks) || b == registry.BlockPowderSnow
}

// BubbleColumnDrag reads a bubble column's drag property: true for the
// column that pulls down over a magma block, false for the one that lifts
// over soul sand. The second return is false for any other block.
func BubbleColumnDrag(state uint16) (drag, ok bool) {
	if registry.BlockOf(state) != registry.BlockBubbleColumn {
		return false, false
	}
	return registry.StateBool(state, registry.PropDrag), true
}

// BubbleColumnInsideSpeed is the vertical speed a body inside a column ends
// the tick with: down to -0.3 or up to 0.7.
// vanilla: Entity.handleOnInsideBubbleColumn
func BubbleColumnInsideSpeed(vy float64, drag bool) float64 {
	if drag {
		return math.Max(-0.3, vy-0.03)
	}
	return math.Min(0.7, vy+0.06)
}

// BubbleColumnAboveSpeed is the vertical speed a body at a column's surface
// ends the tick with: down to -0.9 or up to 1.8.
// vanilla: Entity.handleOnAboveBubbleColumn
func BubbleColumnAboveSpeed(vy float64, drag bool) float64 {
	if drag {
		return math.Max(-0.9, vy-0.03)
	}
	return math.Min(1.8, vy+0.1)
}

// BlockBounceRestitution is the share of landing speed a block gives back:
// slime 1, bed and shelf mushroom 0.75 (Java floats widened), times 0.8f for
// a non-living body. Honey's tag suppresses the bounce; other blocks have none.
// vanilla: Entity.getBlockBounciness, Blocks.SLIME_BLOCK, Blocks.BED,
// Blocks.SHELF_MUSHROOM
func BlockBounceRestitution(state uint16, living bool) float64 {
	var r float32
	switch b := registry.BlockOf(state); {
	case b == registry.BlockSlimeBlock:
		r = 1.0
	case bedBlocks[b], b == registry.BlockShelfMushroom:
		r = 0.75
	default:
		return 0
	}
	if !living {
		r *= 0.8
	}
	return float64(r)
}

// BounceSound is the sound a block plays when a body bounces on it, at the
// block in the blocks category; SoundNone for none. Item entities and primed TNT
// stay quiet.
// vanilla: ShelfMushroomBlock.bounceOn
func BounceSound(state uint16) registry.Sound {
	if registry.BlockOf(state) == registry.BlockShelfMushroom {
		return registry.SoundBlockShelfMushroomBounce
	}
	return registry.SoundNone
}

// BouncedFall is the vertical velocity after a bounce: vy is the landing
// velocity, moved the vertical distance actually covered. A landing no faster
// than a tick of gravity gives nothing back; gravity compensation and drag
// are scaled by the share of the tick spent falling.
// vanilla: Entity.restituteMovementAfterCollisions
func BouncedFall(vy, moved, gravity, airDrag, restitution float64) float64 {
	if restitution <= 0 || -vy <= gravity {
		return 0
	}
	return RestitutedMovement(vy, moved, gravity, airDrag, restitution)
}

// RestitutedMovement compensates an axis bounce for gravity and drag over the
// portion of the tick before impact. The incoming velocity must be nonzero.
// vanilla: Entity.restituteMovementAfterCollisions
func RestitutedMovement(v, moved, gravity, airDrag, restitution float64) float64 {
	portion := moved / v
	drag := 1 + portion*(airDrag-1)
	return (portion*gravity - v) * drag * restitution
}
