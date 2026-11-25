package entity

import (
	"math"
	"slices"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// BoatCargoSlots is a chest boat's 27 slots (MenuProvider.Generic9x3).
const BoatCargoSlots = 27

// VehicleTooFarSq is handleMoveVehicle's squared single-tick limit of 100.
const VehicleTooFarSq = 100

var chestBoats = [registry.EntityTypeCount]bool{
	registry.EntityTypeAcaciaChestBoat: true, registry.EntityTypeBambooChestRaft: true,
	registry.EntityTypeBirchChestBoat: true, registry.EntityTypeCherryChestBoat: true,
	registry.EntityTypeDarkOakChestBoat: true, registry.EntityTypeJungleChestBoat: true,
	registry.EntityTypeMangroveChestBoat: true, registry.EntityTypeOakChestBoat: true,
	registry.EntityTypePaleOakChestBoat: true, registry.EntityTypePoplarChestBoat: true,
	registry.EntityTypeSpruceChestBoat: true,
}

var rafts = [registry.EntityTypeCount]bool{
	registry.EntityTypeBambooRaft: true, registry.EntityTypeBambooChestRaft: true,
}

// IsChestBoat reports the AbstractChestBoat kinds: chest boats and the
// bamboo chest raft.
func IsChestBoat(kind registry.EntityType) bool {
	return uint32(kind) < uint32(len(chestBoats)) && chestBoats[kind]
}

// IsRaft reports a raft, whose rider sits at Raft.rideHeight instead of a
// third of the hull.
// vanilla: Raft.rideHeight
func IsRaft(kind registry.EntityType) bool {
	return uint32(kind) < uint32(len(rafts)) && rafts[kind]
}

// BoatCapacity is getMaxPassengers: two in a boat, one in a chest boat.
// vanilla: AbstractBoat.getMaxPassengers, AbstractChestBoat
func BoatCapacity(kind registry.EntityType) int {
	if IsChestBoat(kind) {
		return 1
	}
	return 2
}

// BoatSeat is the hull's passenger attachment at index of count, with an
// animal's extra offset along the hull.
func BoatSeat(kind registry.EntityType, count, index int, animal bool, x, y, z float64, yaw float32) (float64, float64, float64) {
	dx, dy, dz := rules.BoatSeatOffset(IsRaft(kind), IsChestBoat(kind), count, index, animal, yaw)
	return x + dx, y + dy, z + dz
}

// BoatPlayerSeat is the player's feet at the hull's passenger attachment.
// vanilla: Entity.positionRider, Avatar.DEFAULT_VEHICLE_ATTACHMENT
func BoatPlayerSeat(kind registry.EntityType, count, index int, x, y, z float64, yaw float32) (float64, float64, float64) {
	x, y, z = BoatSeat(kind, count, index, false, x, y, z, yaw)
	return x, y - playerVehicleY, z
}

// HeldBoat reads the boat entity name an item places (entities are named
// after their items) and whether it is in the minecraft:boats tag at all,
// chest boats included.
func HeldBoat(stack protocol.ItemStack) (name string, boat bool) {
	if stack.Empty() || !registry.Item(stack.ItemID).InTag(registry.ItemTagIDBoats) {
		return "", false
	}
	name, ok := registry.ItemName(stack.ItemID)
	if !ok {
		return "", false
	}
	return name, true
}

// BoatBoxAt is the hull's box for a boat standing at the place given.
func BoatBoxAt(x, y, z float64) (minX, minY, minZ, maxX, maxY, maxZ float64) {
	return x - BoatHalfWidth, y, z - BoatHalfWidth, x + BoatHalfWidth, y + BoatHeight, z + BoatHalfWidth
}

// BoatSpotCrosses reports whether a hull at the position would cross the
// body's box, inflated by 1e-7 so a flush body counts.
// vanilla: EntityGetter.getEntityCollisions
func BoatSpotCrosses(x, y, z, bMinX, bMinY, bMinZ, bMaxX, bMaxY, bMaxZ float64) bool {
	const touch = 1.0e-7
	minX, minY, minZ, maxX, maxY, maxZ := BoatBoxAt(x, y, z)
	return minX-touch < bMaxX && maxX+touch > bMinX &&
		minY-touch < bMaxY && maxY+touch > bMinY &&
		minZ-touch < bMaxZ && maxZ+touch > bMinZ
}

// MovedTooFar reports whether a driver's hull report exceeds a single
// tick's vehicle move, measured from the tick-start position less what the
// hull's motion accounts for; not from the previous report, and not counting
// rotation, which a client could use to hide a move.
// vanilla: handleMoveVehicle's moved-too-quickly
func (b *Boat) MovedTooFar(fromX, fromY, fromZ, x, y, z float64) bool {
	return MovedTooFar(fromX, fromY, fromZ, x, y, z, b.VX*b.VX+b.VY*b.VY+b.VZ*b.VZ)
}

// MovedTooFar is any vehicle's single-tick test: squared distance from the
// first-good position, less the squared expected speed, past
// VehicleTooFarSq.
func MovedTooFar(fromX, fromY, fromZ, x, y, z, expectedSq float64) bool {
	dx, dy, dz := x-fromX, y-fromY, z-fromZ
	return dx*dx+dy*dy+dz*dz-expectedSq > VehicleTooFarSq
}

// BoatPushTouches reports whether a mob's box reaches the hull grown 0.2
// sideways and 0.01 narrower vertically, where a boat's push picks up
// passengers.
// vanilla: AbstractBoat.tick, AABB.inflate(0.2, -0.01, 0.2)
func BoatPushTouches(mobX, mobY, mobZ, mobWidth, mobHeight, boatX, boatY, boatZ float64) bool {
	const inflate = 0.2
	const shrink = 0.01
	half := mobWidth / 2
	return mobX+half > boatX-BoatHalfWidth-inflate &&
		mobX-half < boatX+BoatHalfWidth+inflate &&
		mobZ+half > boatZ-BoatHalfWidth-inflate &&
		mobZ-half < boatZ+BoatHalfWidth+inflate &&
		mobY < boatY+BoatHeight-shrink &&
		mobY+mobHeight > boatY+shrink
}

// BoatBoardable reports whether a boat's push may pick up the mob (riding
// aside): not dead, not still in its boarding cooldown, not in cannot_be_pushed_onto_boats, and not wider than
// the hull.
// vanilla: AbstractBoat.tick's push
func BoatBoardable(m *Mob) bool {
	if m.Dead || m.BoardingCooldown > 0 {
		return false
	}
	if _, ok := m.typeIDOK(); !ok || m.inTag(registry.EntityTypeTagIDCannotBePushedOntoBoats) {
		return false
	}
	return m.BbWidth() < 2*BoatHalfWidth
}

// Bodies is a world plus a snapshot of solid entity boxes, or the pushable
// bodies an unridden boat stops at. The boxes
// are read before the step takes its own lock, so the step never takes
// another.
type Bodies struct {
	World
	Boxes []AABB
	Grid  *BoxGrid // indexes Boxes; nil means test every box
	Skip  []int32  // indices of Boxes left out (the mover and its ride chain)
	// Drop, when set, marks by index more Boxes left out; built once per
	// snapshot, so a long list costs no scan per hit.
	Drop []bool
}

// BodyBoxes returns the snapshot's boxes whose insides meet box (touching
// does not count).
// vanilla: EntityGetter.getEntityCollisions
func (b Bodies) BodyBoxes(box AABB) []AABB {
	var out []AABB
	if b.Grid != nil {
		b.Grid.Query(box, func(i int32) {
			if body := b.Boxes[i]; box.Crosses(body) && !b.dropped(i) {
				out = append(out, body)
			}
		})
		return out
	}
	for i, body := range b.Boxes {
		if box.Crosses(body) && !b.dropped(int32(i)) {
			out = append(out, body)
		}
	}
	return out
}

func (b Bodies) dropped(i int32) bool {
	return b.Drop != nil && b.Drop[i] || slices.Contains(b.Skip, i)
}

// ClampToBorder forwards to the world's, so a random teleport is still held
// to the border while boats are about.
func (b Bodies) ClampToBorder(x, z float64) (float64, float64) {
	if c, ok := b.World.(borderClamper); ok {
		return c.ClampToBorder(x, z)
	}
	return x, z
}

// InBorderPoint forwards point bounds to the world's border when it has one.
func (b Bodies) InBorderPoint(x, z float64) bool {
	if c, ok := b.World.(projectileBorder); ok {
		return c.InBorderPoint(x, z)
	}
	return true
}

// WithBodies pairs a world with the boxes when there are any; otherwise the
// plain world, whose sweeps skip body queries.
func WithBodies(w World, boxes []AABB) World {
	if len(boxes) == 0 {
		return w
	}
	return Bodies{World: w, Boxes: boxes}
}

// BlocksBoatSpot reports whether the mob blocks a boat placed over it
// (BoatItem.use's noCollision, entity half): alive and not on a climbable.
// A spider's onClimbable is only its Climbing flag, so one on a ladder it
// is not climbing still blocks.
// vanilla: LivingEntity.isPushable, Spider.isClimbing
func (m *Mob) BlocksBoatSpot(w World) bool {
	if m.Dead || m.Climbing {
		return false
	}
	fx, fz := int32(math.Floor(m.X)), int32(math.Floor(m.Z))
	fy := int32(math.Floor(m.Y))
	return Family(m.Type) == TypeSpider ||
		!rules.IsClimbable(w.BlockAt(fx, fy, fz), w.BlockAt(fx, fy-1, fz))
}

// CollisionCellsIn forwards sparse block reads through the body wrapper.
func (b Bodies) CollisionCellsIn(x0, y0, z0, x1, y1, z1 int32, buf []world.CollisionCell) []world.CollisionCell {
	return collisionCellsIn(b.World, x0, y0, z0, x1, y1, z1, buf)
}
