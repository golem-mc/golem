package rules

import "github.com/golem-mc/golem/registry"

// The blocks a landing projectile acts on besides the target block, and the
// decisions each hook makes. The server dispatches and writes.
// vanilla: TntBlock.onProjectileHit, CampfireBlock.onProjectileHit,
// AbstractCandleBlock.onProjectileHit, BellBlock.onProjectileHit,
// ChorusFlowerBlock.onProjectileHit, DecoratedPotBlock.onProjectileHit,
// AmethystBlock.onProjectileHit, BigDripleafBlock.onProjectileHit,
// SpeleothemBlock.onProjectileHit

// ProjectileBlockKind is which onProjectileHit hook a block state answers
// with.
type ProjectileBlockKind int

// The hooks, ProjectileBlockNone for a block with none.
const (
	ProjectileBlockNone ProjectileBlockKind = iota
	ProjectileBlockTNT
	ProjectileBlockCampfire
	ProjectileBlockCandle
	ProjectileBlockBell
	// ProjectileBlockBreaks is the chorus flower and the decorated pot.
	ProjectileBlockBreaks
	// ProjectileBlockAmethyst is AmethystBlock and its subclasses: budding
	// amethyst, the buds and the cluster.
	ProjectileBlockAmethyst
	ProjectileBlockDripleaf
	ProjectileBlockSpeleothem
)

// ProjectileBlockKindOf is the hook the block state belongs to.
func ProjectileBlockKindOf(state uint16) ProjectileBlockKind {
	if int(state) >= registry.StateCount {
		return ProjectileBlockNone
	}
	switch registry.BlockOf(state).Type() {
	case registry.BlockTypeTnt:
		return ProjectileBlockTNT
	case registry.BlockTypeBell:
		return ProjectileBlockBell
	case registry.BlockTypeChorusFlower, registry.BlockTypeDecoratedPot:
		return ProjectileBlockBreaks
	case registry.BlockTypeAmethyst, registry.BlockTypeBuddingAmethyst, registry.BlockTypeAmethystCluster:
		return ProjectileBlockAmethyst
	case registry.BlockTypeBigDripleaf:
		return ProjectileBlockDripleaf
	case registry.BlockTypePointedDripstone, registry.BlockTypeSulfurSpike:
		return ProjectileBlockSpeleothem
	}
	switch {
	case registry.BlockOf(state).InTag(registry.TagIDCampfires):
		return ProjectileBlockCampfire
	case registry.BlockOf(state).InTag(registry.TagIDCandles),
		registry.BlockOf(state).InTag(registry.TagIDCandleCakes):
		return ProjectileBlockCandle
	}
	return ProjectileBlockNone
}

// NeedsFire reports whether the hook acts only for a burning projectile.
func (k ProjectileBlockKind) NeedsFire() bool {
	switch k {
	case ProjectileBlockTNT, ProjectileBlockCampfire, ProjectileBlockCandle:
		return true
	}
	return false
}

// ProjectileMayBreak is Projectile.mayBreak: the projectile's entity type is
// an impact projectile and the projectiles_can_break_blocks rule is on.
// Ender pearls and thrown potions are not impact projectiles.
// vanilla: Projectile.mayBreak
func ProjectileMayBreak(entityType int32, ruleOn bool) bool {
	return ruleOn && registry.EntityType(entityType).InTag(registry.EntityTypeTagIDImpactProjectiles)
}

// TridentBreaksSpeleothemSpeed is the speed in blocks per tick above which a
// thrown trident breaks the speleothem it hits.
// vanilla: SpeleothemBlock.onProjectileHit
const TridentBreaksSpeleothemSpeed = 0.6

// CrackedPot is the decorated pot state with cracked set, false for a state
// that is no decorated pot.
// vanilla: DecoratedPotBlock.onProjectileHit
func CrackedPot(state uint16) (uint16, bool) {
	if registry.BlockOf(state) != registry.BlockDecoratedPot {
		return 0, false
	}
	return registry.WithBool(state, registry.PropCracked, true)
}

// AmethystChimePitch is the chime's pitch for a roll r in [0, 1).
// vanilla: AmethystBlock.onProjectileHit
func AmethystChimePitch(r float32) float32 {
	return 0.5 + r*1.2
}
