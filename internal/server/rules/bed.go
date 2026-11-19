package rules

import (
	"math"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// Bed exits shared by waking and respawning players.

// BedWorld is the blocks, collisions and border a stand-up search reads.
type BedWorld interface {
	BlockAt(x, y, z int32) uint16
	BoxCollides(minX, minY, minZ, maxX, maxY, maxZ float64) bool
	InBorderBox(minX, minZ, maxX, maxZ float64) bool
}

// BedStandUpSpot is the first cell around the bed at (x, y, z) a standing
// body fits on, walking out from the side forward and yaw pick; a bunk bed
// tries around the lower bed before the upper mattress. The whole walk runs
// refusing dangerous cells, then again accepting them. false when boxed in.
// vanilla: BedBlock.findStandUpPosition, BedBlock.findBunkBedStandUpPosition
func BedStandUpSpot(w BedWorld, x, y, z int32, forward [2]int32, yaw float32) (px, py, pz float64, ok bool) {
	right := [2]int32{-forward[1], forward[0]}
	side := right
	if facingAngle(right, yaw) {
		side = [2]int32{-right[0], -right[1]}
	}
	surround := bedSurroundStandUpOffsets(forward, side)
	bunk := isBedAt(w, x, y-1, z)
	for _, checkDangerous := range [2]bool{true, false} {
		if px, py, pz, ok := bedStandUpAt(w, x, y, z, surround, checkDangerous); ok {
			return px, py, pz, true
		}
		if bunk {
			if px, py, pz, ok := bedStandUpAt(w, x, y-1, z, surround, checkDangerous); ok {
				return px, py, pz, true
			}
		}
		if px, py, pz, ok := bedStandUpAt(w, x, y, z, bedAboveStandUpOffsets(forward), checkDangerous); ok {
			return px, py, pz, true
		}
	}
	return 0, 0, 0, false
}

// bedStandUpAt is the first of the offsets whose cell a standing body fits on.
func bedStandUpAt(w BedWorld, x, y, z int32, offsets [][2]int32, checkDangerous bool) (float64, float64, float64, bool) {
	for _, o := range offsets {
		if px, py, pz, ok := safeStandUp(w, x+o[0], y, z+o[1], checkDangerous); ok {
			return px, py, pz, true
		}
	}
	return 0, 0, 0, false
}

// isBedAt reports whether the block at a cell is a bed's half.
func isBedAt(w BedWorld, x, y, z int32) bool {
	return registry.BlockOf(w.BlockAt(x, y, z)).InGroup(registry.GroupBed)
}

// bedSurroundStandUpOffsets is the search ring as offsets from the bed's
// cell: down the picked side from head to past the foot, around the foot,
// up the other side to past the head, then the two cells past the head.
// vanilla: BedBlock.bedSurroundStandUpOffsets
func bedSurroundStandUpOffsets(forward, side [2]int32) [][2]int32 {
	fx, fz := forward[0], forward[1]
	sx, sz := side[0], side[1]
	return [][2]int32{
		{sx, sz},
		{sx - fx, sz - fz},
		{sx - 2*fx, sz - 2*fz},
		{-2 * fx, -2 * fz},
		{-sx - 2*fx, -sz - 2*fz},
		{-sx - fx, -sz - fz},
		{-sx, -sz},
		{-sx + fx, -sz + fz},
		{fx, fz},
		{sx + fx, sz + fz},
	}
}

// bedAboveStandUpOffsets is the two cells the bed's own halves occupy, which
// the search tries last: a body stands on the mattress rather than falling back.
// vanilla: BedBlock.bedAboveStandUpOffsets
func bedAboveStandUpOffsets(forward [2]int32) [][2]int32 {
	return [][2]int32{{0, 0}, {-forward[0], -forward[1]}}
}

// safeStandUp is a player's findSafeDismountLocation: a cell whose floor a
// standing box fits on, clear of blocks. No floor shape, collision reaching
// the cell's ceiling, or an end portal or gateway in the body's two cells
// refuses; checkDangerous also refuses a dangerous cell, or one standing
// directly on a dangerous block.
// vanilla: DismountHelper.findSafeDismountLocation
func safeStandUp(w BedWorld, x, y, z int32, checkDangerous bool) (float64, float64, float64, bool) {
	if checkDangerous && playerBlockDangerous(w.BlockAt(x, y, z)) {
		return 0, 0, 0, false
	}
	floor := bedFloorHeight(w, x, y, z)
	if math.IsInf(floor, 0) || floor >= 1 {
		return 0, 0, 0, false
	}
	if checkDangerous && floor <= 0 && playerBlockDangerous(w.BlockAt(x, y-1, z)) {
		return 0, 0, 0, false
	}
	dims := PlayerDimensionsOf(protocol.PoseStanding)
	px, py, pz := float64(x)+0.5, float64(y)+floor, float64(z)+0.5
	half := dims.Width / 2
	if w.BoxCollides(px-half, py, pz-half, px+half, py+dims.Height, pz+half) {
		return 0, 0, 0, false
	}
	if registry.BlockOf(w.BlockAt(x, y, z)).InTag(registry.TagIDInvalidSpawnInside) ||
		registry.BlockOf(w.BlockAt(x, y+1, z)).InTag(registry.TagIDInvalidSpawnInside) {
		return 0, 0, 0, false
	}
	if !w.InBorderBox(px-half, pz-half, px+half, pz+half) {
		return 0, 0, 0, false
	}
	return px, py, pz, true
}

// playerBlockDangerous reports whether a player, neither fire immune nor
// immune to any block, is hurt in this block.
// vanilla: EntityType.isBlockDangerous, NodeEvaluator.isBurningBlock
func playerBlockDangerous(state uint16) bool {
	switch registry.BlockOf(state) {
	case registry.BlockWitherRose, registry.BlockSweetBerryBush, registry.BlockCactus, registry.BlockPowderSnow:
		return true
	default:
		return registry.IsBurningBlock(state)
	}
}

// bedFloorHeight is how far above a cell's floor feet rest: a non-empty
// collision's top; for an empty cell over a shape at least a block tall,
// that top minus one; otherwise no floor. Per-state collision puts a bed's
// sleeper at 9/16.
// vanilla: BlockGetter.getBlockFloorHeight
func bedFloorHeight(w BedWorld, x, y, z int32) float64 {
	if h := world.CollisionTop(w.BlockAt(x, y, z)); h > 0 {
		return h
	}
	if h := world.CollisionTop(w.BlockAt(x, y-1, z)); h >= 1 {
		return h - 1
	}
	return math.Inf(-1)
}

// facingAngle reports whether dir points along yaw, using vanilla's sine
// table so axis-aligned looks fall on vanilla's side of the dot product.
// vanilla: Direction.isFacingAngle
func facingAngle(dir [2]int32, yaw float32) bool {
	rad := yaw * (float32(math.Pi) / 180)
	lookX := -world.MthSin(float64(rad))
	lookZ := world.MthCos(float64(rad))
	return float32(dir[0])*lookX+float32(dir[1])*lookZ > 0
}

// LookAtYaw is the yaw a body at (fromX, fromZ) faces to look at the centre
// of the block (toX, toZ), which turns the returned body to face the bed.
// vanilla: ServerPlayer.RespawnPosAngle.calculateLookAtYaw
func LookAtYaw(fromX, fromZ float64, toX, toZ int32) float32 {
	dx := float64(toX) + 0.5 - fromX
	dz := float64(toZ) + 0.5 - fromZ
	yaw := math.Atan2(dz, dx)*180/math.Pi - 90
	w := math.Mod(yaw, 360)
	switch {
	case w >= 180:
		w -= 360
	case w < -180:
		w += 360
	}
	return float32(w)
}

// BedCond is when a bed rule's permission holds. vanilla: BedRule.Rule
type BedCond uint8

const (
	BedAlways BedCond = iota
	BedWhenDark
	BedNever
)

// Holds reports whether the condition holds; dark is Level.isDarkOutside.
func (c BedCond) Holds(dark bool) bool {
	return c == BedAlways || c == BedWhenDark && dark
}

// BedRule is what a bed does in a dimension. ErrorMessage is the overlay's
// translation key when sleep is refused, "" for none. vanilla: BedRule
type BedRule struct {
	CanSleep, CanSetSpawn        BedCond
	DestroyOnUse, DestroyOnLeave bool
	ErrorMessage                 string
}

// vanilla: BedRule.CAN_SLEEP_WHEN_DARK, DESTROY_ON_USE, DESTROY_ON_LEAVE
var (
	bedCanSleepWhenDark = BedRule{CanSleep: BedWhenDark, CanSetSpawn: BedAlways, ErrorMessage: "block.minecraft.bed.no_sleep"}
	bedDestroyOnUse     = BedRule{CanSleep: BedNever, CanSetSpawn: BedNever, DestroyOnUse: true}
	bedDestroyOnLeave   = BedRule{CanSleep: BedWhenDark, CanSetSpawn: BedNever, DestroyOnLeave: true, ErrorMessage: "block.minecraft.bed.no_sleep"}
)

// BedRuleOf is the rule of the bed block b for a dimension type: its
// gameplay/straw_bed_rule for a straw bed, gameplay/bed_rule for any other.
// The nether and the end types set both to DESTROY_ON_USE; any other type
// keeps the attributes' defaults, which are the overworld's.
// vanilla: AbstractBedBlock.getBedRule, DimensionTypes.bootstrap,
// EnvironmentAttributes.BED_RULE / STRAW_BED_RULE
func BedRuleOf(dimensionType string, b registry.Block) BedRule {
	switch {
	case dimensionType == world.NetherType, dimensionType == world.EndType:
		return bedDestroyOnUse
	case b == registry.BlockStrawBed:
		return bedDestroyOnLeave
	}
	return bedCanSleepWhenDark
}

// SleptInBedStat is the custom stat a sleep in bed block b awards.
// vanilla: AbstractBedBlock.getSleptInBedStatType, StrawBedBlock
func SleptInBedStat(b registry.Block) registry.CustomStat {
	if b == registry.BlockStrawBed {
		return registry.CustomStatSleepInStrawBed
	}
	return registry.CustomStatSleepInBed
}

// BedHoldsSleeper reports whether the bed half at (x, y, z) still has a
// sleep height: any bed block does, but a straw bed head only with its foot
// behind it. vanilla: AbstractBedBlock.getSleepHeight, StrawBedBlock.getSleepHeight
func BedHoldsSleeper(w BlockReader, x, y, z int32) bool {
	state := w.BlockAt(x, y, z)
	b := registry.BlockOf(state)
	if !b.InGroup(registry.GroupBed) {
		return false
	}
	if b != registry.BlockStrawBed || registry.StateValue(state, registry.PropPart) != "head" {
		return true
	}
	d, known := horizontalSteps[registry.StateValue(state, registry.PropFacing)]
	if !known {
		return false
	}
	foot := w.BlockAt(x-d[0], y, z-d[1])
	return registry.BlockOf(foot) == b && registry.StateValue(foot, registry.PropPart) == "foot"
}

// BedFreeAt reports whether the sleep space is free: freeAt tests
// !isSuffocating, so glass and partial blocks pass and full solid cubes do
// not.
// vanilla: ServerPlayer.bedBlocked, Player.freeAt
func BedFreeAt(w BlockReader, x, y, z int32) bool {
	return !IsSuffocating(w.BlockAt(x, y, z))
}

// BedHeadOf is the head half a click at (x, y, z) answers from: itself, or
// the head a foot faces, with its properties; false unless the other half
// is the same block's opposite part.
// vanilla: AbstractBedBlock.useWithoutItem
func BedHeadOf(w BlockReader, x, y, z int32, state uint16) (hx, hz int32, head uint16, ok bool) {
	d, known := horizontalSteps[registry.StateValue(state, registry.PropFacing)]
	if !known {
		return 0, 0, 0, false
	}
	part := registry.StateValue(state, registry.PropPart)
	if part == "head" {
		d = [2]int32{-d[0], -d[1]}
	}
	other := w.BlockAt(x+d[0], y, z+d[1])
	if registry.BlockOf(other) != registry.BlockOf(state) || registry.StateValue(other, registry.PropPart) == part {
		return 0, 0, 0, false
	}
	if part == "head" {
		return x, z, state, true
	}
	return x + d[0], z + d[1], other, true
}

// BedInRange reports whether a body at px, py, pz may use the bed whose head
// is at (x, y, z) and foot at (footX, y, footZ): within 3 blocks
// horizontally and 2 vertically of either half's bottom centre.
// vanilla: ServerPlayer.bedInRange
func BedInRange(px, py, pz float64, x, y, z, footX, footZ int32) bool {
	inRange := func(bx, bz int32) bool {
		return math.Abs(px-(float64(bx)+0.5)) <= 3 &&
			math.Abs(py-float64(y)) <= 2 &&
			math.Abs(pz-(float64(bz)+0.5)) <= 3
	}
	return inRange(x, z) || inRange(footX, footZ)
}
