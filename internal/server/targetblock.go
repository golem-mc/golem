package server

import (
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// The target block's onProjectileHit for the arrow and thrown ticks: a hit
// is gathered under the entity locks, and grading, writing, reset booking
// and telling run after them.

// targetBlockHit is a target block a projectile hit, gathered for after the
// entity locks. arrow sets the hold: vanilla holds 20 ticks for an arrow, 8
// for anything else.
type targetBlockHit struct {
	w     *world.World
	block entity.BlockHit
	arrow bool
	// ownerID is the entity ID of the shooter, 0 for none.
	ownerID int32
}

// isTargetBlock reports whether state is a target block, so the gathering
// pass skips everything else cheaply.
func isTargetBlock(state uint16) bool {
	return rules.IsTargetBlock(state)
}

// hitTargetBlocks applies gathered hits: power graded from the crossing,
// state written, reset booked for the hold, and the wake's reach told.
// vanilla: TargetBlock.onProjectileHit
func (s *Server) hitTargetBlocks(hits []targetBlockHit) {
	for _, hit := range hits {
		power := rules.TargetHitPower(hit.block.Face, hit.block.HitX, hit.block.HitY, hit.block.HitZ)
		changes, effects, primes := s.physicsFor(hit.w).TargetProjectiled(hit.block.X, hit.block.Y, hit.block.Z, power, rules.TargetHoldTicks(hit.arrow))
		for _, e := range effects {
			s.tellEffect(hit.w, e)
		}
		for _, t := range primes {
			s.primeTNT(hit.w, t)
		}
		s.tellBlockChanges(hit.w, changes)
		if owner := s.entityClient(hit.ownerID); owner != nil {
			owner.awardStat(player.Custom(registry.CustomStatTargetHit), 1)
			// The projectile's position is the hit point, where an arrow
			// stands when it hits. vanilla: AbstractArrow.stepMoveAndHit
			owner.targetHitAdvancements(int32(power), [3]float64{hit.block.HitX, hit.block.HitY, hit.block.HitZ})
		}
	}
}
