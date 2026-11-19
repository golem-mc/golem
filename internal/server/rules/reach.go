package rules

import (
	"math"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/registry"
)

// Interaction reaches and their slack. Vanilla checks every reported act
// against the interaction range attributes: block 4.5 (+0.5 creative) and
// entity 3 (+2 creative), measured from the eyes to the nearest face of the
// box (AABB.distanceToSqr). A verification buffer is added: a block act may
// be a block beyond, an attack or interact three. The attack's boundary is
// inclusive; interact and block are strict, each as its vanilla check
// compares.

// BlockInteractionBuffer and EntityInteractionBuffer are the slack past
// the reach a reported act may carry.
// vanilla: ServerPlayer.BLOCK_INTERACTION_DISTANCE_VERIFICATION_BUFFER and its
// entity twin
const (
	BlockInteractionBuffer  = 1.0
	EntityInteractionBuffer = 3.0
)

// DistToBox is the distance from a point to a box's nearest face, 0 inside:
// per-axis overshoots, squared, summed and rooted.
// vanilla: AABB.distanceToSqr(Vec3)
func DistToBox(px, py, pz, minX, minY, minZ, maxX, maxY, maxZ float64) float64 {
	dx := max(minX-px, px-maxX, 0)
	dy := max(minY-py, py-maxY, 0)
	dz := max(minZ-pz, pz-maxZ, 0)
	return math.Sqrt(dx*dx + dy*dy + dz*dz)
}

// WithinBlockInteractionRange reports whether the eye-to-block-box distance
// is strictly under rng, the block interaction range attribute, plus the
// buffer.
// vanilla: ServerPlayer.isWithinBlockInteractionRange
func WithinBlockInteractionRange(rng, ex, ey, ez, bx, by, bz float64) bool {
	return DistToBox(ex, ey, ez, bx, by, bz, bx+1, by+1, bz+1) < rng+BlockInteractionBuffer
}

// WithinEntityInteractionRange reports whether the eye-to-box distance is
// strictly under rng, the entity interaction range attribute, plus the
// buffer.
// vanilla: ServerPlayer.isWithinEntityInteractionRange
func WithinEntityInteractionRange(rng, ex, ey, ez, minX, minY, minZ, maxX, maxY, maxZ float64) bool {
	return DistToBox(ex, ey, ez, minX, minY, minZ, maxX, maxY, maxZ) < rng+EntityInteractionBuffer
}

// WithinEntityInteractionRangeBuffered is Player.isWithinEntityInteractionRange
// with a caller-chosen buffer in place of EntityInteractionBuffer: the
// distance from the eye to the box under the reach plus buffer, strictly.
func WithinEntityInteractionRangeBuffered(rng, buffer, ex, ey, ez, minX, minY, minZ, maxX, maxY, maxZ float64) bool {
	return DistToBox(ex, ey, ez, minX, minY, minZ, maxX, maxY, maxZ) < rng+buffer
}

// WithinAttackRange reports whether an attack with held in the main hand
// reaches the box from the eyes: the distance to it within the reach span,
// widened by the hitbox margin and the buffer at both ends, inclusive. rng
// is the entity interaction range attribute, the span when held has no
// attack_range.
// vanilla: Player.isWithinAttackRange, AttackRange.isInRange
func WithinAttackRange(held protocol.ItemStack, creative bool, rng, ex, ey, ez, minX, minY, minZ, maxX, maxY, maxZ float64) bool {
	minReach, maxReach, margin := AttackReach(held, creative, rng)
	// The reach and its margin are floats, summed as floats.
	lo := float64(float32(minReach)-float32(margin)) - EntityInteractionBuffer
	hi := float64(float32(maxReach)+float32(margin)) + EntityInteractionBuffer
	d := DistToBox(ex, ey, ez, minX, minY, minZ, maxX, maxY, maxZ)
	return d >= lo && d <= hi
}

// ContainerRangeExtra is the reach beyond the block interaction range at
// which an open menu stays valid. vanilla: Container.stillValidBlockEntity
const ContainerRangeExtra = 4.0

// WithinContainerRange reports whether a player whose eyes are where given
// may keep a container open on the block at bx, by, bz: the distance to the
// block's box under rng, the block interaction range attribute, plus
// ContainerRangeExtra, strictly.
// vanilla: Player.isWithinBlockInteractionRange
func WithinContainerRange(rng, ex, ey, ez, bx, by, bz float64) bool {
	return DistToBox(ex, ey, ez, bx, by, bz, bx+1, by+1, bz+1) < rng+ContainerRangeExtra
}

// AttackReach is the span, from the eyes, an attack with held reaches and
// the margin boxes grow by: AttackRangeWith the held stack.
// vanilla: LivingEntity.getAttackRangeWith, effectiveMinRange, effectiveMaxRange
func AttackReach(held protocol.ItemStack, creative bool, rng float64) (minReach, maxReach, margin float64) {
	r := AttackRangeWith(held, rng)
	if creative {
		return float64(r.MinCreativeReach), float64(r.MaxCreativeReach), float64(r.HitboxMargin)
	}
	return float64(r.MinReach), float64(r.MaxReach), float64(r.HitboxMargin)
}

// AttackRangeWith is the stack's attack_range, else zero to entityRange, the
// holder's entity_interaction_range attribute value held as a float, with no
// margin and a mob factor of one.
// vanilla: LivingEntity.getAttackRangeWith, AttackRange.defaultFor
func AttackRangeWith(held protocol.ItemStack, entityRange float64) registry.AttackRange {
	if r, ok := StackAttackRange(held); ok {
		return r
	}
	f := float32(entityRange)
	return registry.AttackRange{MaxReach: f, MaxCreativeReach: f, MobFactor: 1}
}

// StackAttackRange is the stack's attack_range, false for an empty stack, an
// item without one or a stack that removes it.
func StackAttackRange(held protocol.ItemStack) (registry.AttackRange, bool) {
	if held.Empty() {
		return registry.AttackRange{}, false
	}
	if len(held.Raw) > 0 {
		if own, ok := held.AttackRangeOf(); ok {
			return registry.AttackRange(own), true
		}
	}
	if held.Removes(protocol.DataComponentAttackRange) {
		return registry.AttackRange{}, false
	}
	return registry.AttackRangeOf(held.ItemID)
}
