package rules

import (
	"math"
	"math/rand"

	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// RandomTeleportWorld is the slice of the world a random teleport reads.
type RandomTeleportWorld interface {
	BlockReader
	BoxCollides(minX, minY, minZ, maxX, maxY, maxZ float64) bool
	ChunkLoadedAt(x, z int32) bool
	ClampToBorder(x, z float64) (float64, float64)
	MinY() int32
	LogicalHeight() int32
}

// randomTeleportAttempts is how many spots a teleport_randomly tries.
// vanilla: TeleportRandomlyConsumeEffect.apply
const randomTeleportAttempts = 16

// RandomTeleport picks a chorus fruit destination: up to 16 tries within
// diameter/2 per axis (y clamped to the logical height), each dropped to the
// first motion-blocking block and kept if a 0.6 x 1.8 body fits clear of
// blocks and liquid, with neither the floor nor a block in the body in
// #consumable_does_not_teleport_to. false when none fits.
// vanilla: TeleportRandomlyConsumeEffect.apply, LivingEntity.randomTeleport
func RandomTeleport(w RandomTeleportWorld, x, y, z float64, diameter float32, dice *rand.Rand) (nx, ny, nz float64, ok bool) {
	d := float64(diameter)
	minY := float64(w.MinY())
	maxY := minY + float64(w.LogicalHeight()) - 1
	for range randomTeleportAttempts {
		tx := x + (dice.Float64()-0.5)*d
		ty := math.Min(math.Max(y+(dice.Float64()-0.5)*d, minY), maxY)
		tz := z + (dice.Float64()-0.5)*d
		if nx, ny, nz, ok := landRandomTeleport(w, tx, ty, tz); ok {
			return nx, ny, nz, true
		}
	}
	return 0, 0, 0, false
}

// landRandomTeleport clamps the point into the border, drops it to the
// ground below and reports where a player's body fits there.
// vanilla: LivingEntity.randomTeleport, LivingEntity.checkPositionAndTeleport
func landRandomTeleport(w RandomTeleportWorld, x, y, z float64) (nx, ny, nz float64, ok bool) {
	x, z = w.ClampToBorder(x, z)
	bx, bz := int32(math.Floor(x)), int32(math.Floor(z))
	if !w.ChunkLoadedAt(bx, bz) {
		return 0, 0, 0, false
	}
	by := int32(math.Floor(y))
	landed := false
	for !landed && by > w.MinY() {
		if CanTeleportTo(w.BlockAt(bx, by-1, bz)) {
			landed = true
			continue
		}
		y--
		by--
	}
	if !landed {
		return 0, 0, 0, false
	}
	avoid := func(state uint16) bool {
		return registry.BlockOf(state).InTag(registry.TagIDConsumableDoesNotTeleportTo)
	}
	if avoid(w.BlockAt(bx, by-1, bz)) {
		return 0, 0, 0, false
	}
	const half, height = 0.3, 1.8
	minX, minZ, maxX, maxY, maxZ := x-half, z-half, x+half, y+height, z+half
	if w.BoxCollides(minX, y, minZ, maxX, maxY, maxZ) ||
		AnyBlockIn(w, minX, y, minZ, maxX, maxY, maxZ, func(s uint16) bool { return world.HoldsFluid(s) || avoid(s) }) {
		return 0, 0, 0, false
	}
	return x, y, z, true
}

// AnyBlockIn reports whether a block whose cell the box touches, edges
// included, matches.
// vanilla: LevelReader.findBlocksIn
func AnyBlockIn(w BlockReader, minX, minY, minZ, maxX, maxY, maxZ float64, match func(uint16) bool) bool {
	for cy := int32(math.Floor(minY)); cy <= int32(math.Floor(maxY)); cy++ {
		for cz := int32(math.Floor(minZ)); cz <= int32(math.Floor(maxZ)); cz++ {
			for cx := int32(math.Floor(minX)); cx <= int32(math.Floor(maxX)); cx++ {
				if match(w.BlockAt(cx, cy, cz)) {
					return true
				}
			}
		}
	}
	return false
}

// CanTeleportTo reports a block a random teleport can land on.
// vanilla: LivingEntity.randomTeleport
func CanTeleportTo(state uint16) bool {
	return registry.BlockOf(state).InTag(registry.TagIDEntitiesCanTeleportTo)
}
