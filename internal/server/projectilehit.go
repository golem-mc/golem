package server

import (
	"math/rand"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
)

// projectileBlockHit is a block with an onProjectileHit hook
// (rules.ProjectileBlockKindOf) a flight met, gathered under the entity
// locks so the write and priming run after them. fire is whether the
// projectile still burned; ownerMob marks a mob shooter, refused by
// mayInteract with mob_griefing off; ownerID and ownerUUID become a primed
// TNT's source.
type projectileBlockHit struct {
	w     *world.World
	block entity.BlockHit
	fire  bool
	// entityType is the projectile's wire type: mayBreak's tag test and the
	// this_entity of a caused break's loot.
	entityType int32
	ownerMob   bool
	ownerID    int32
	ownerName  string
	ownerUUID  uuid.UUID
	ownerMode  int32
	// pickupStack is AbstractArrow.getPickupItemStackOrigin, empty for
	// other projectiles and the default arrow (which has no can_break).
	pickupStack protocol.ItemStack
}

// isProjectileHitBlock reports whether a projectile hook handles state, so
// the gathering pass skips everything else cheaply.
func isProjectileHitBlock(state uint16) bool {
	return rules.ProjectileBlockKindOf(state) != rules.ProjectileBlockNone
}

// hitProjectileBlocks answers each gathered hit with its block's hook.
func (s *Server) hitProjectileBlocks(hits []projectileBlockHit) {
	for _, hit := range hits {
		// Use the live state, not the gathered one: a change since must not be
		// overwritten. vanilla: Projectile.onHitBlock
		b := hit.block
		state := hit.w.BlockAt(b.X, b.Y, b.Z)
		kind := rules.ProjectileBlockKindOf(state)
		if kind.NeedsFire() && !hit.fire {
			continue
		}
		switch kind {
		case rules.ProjectileBlockTNT:
			// TntBlock.prime declines with tnt_explodes off, leaving the block.
			if !hit.w.TNTExplodes() || !s.mayProjectileInteract(hit) {
				continue
			}
			// vanilla: TntBlock.prime
			if hit.ownerMode == protocol.GameModeAdventure && !rules.CanBreakInAdventureMode(hit.pickupStack, adventureTarget(hit.w, b.X, b.Y, b.Z, state)) {
				continue
			}
			s.setBlockAndTell(hit.w, b.X, b.Y, b.Z, airBlockState)
			s.primeTNTFrom(hit.w, world.TNTPrime{X: b.X, Y: b.Y, Z: b.Z},
				hit.ownerID, hit.ownerName, hit.ownerUUID)
		case rules.ProjectileBlockCampfire, rules.ProjectileBlockCandle:
			// Candles skip mayInteract, as vanilla's AbstractCandleBlock does.
			if kind == rules.ProjectileBlockCampfire && !s.mayProjectileInteract(hit) {
				continue
			}
			if lit, ok := rules.FlintLight(state); ok {
				s.setBlockAndTell(hit.w, b.X, b.Y, b.Z, lit)
			}
		case rules.ProjectileBlockBell:
			// vanilla: BellBlock.onHit
			if rules.BellHit(state, b.Face, float32(b.HitY-float64(b.Y))) {
				if s.ringBell(hit.w, b.X, b.Y, b.Z, uint8(b.Face), rules.GameEventContext{Player: hit.ownerUUID, ProjectileOwner: hit.ownerUUID}, nil) {
					if owner := s.entityClient(hit.ownerID); owner != nil {
						owner.awardStat(player.Custom(registry.CustomStatBellRing), 1)
					}
				}
			}
		case rules.ProjectileBlockBreaks:
			if !s.projectileMayBreak(hit) {
				continue
			}
			// Write the crack silently before the break, so the break event and drops
			// read the cracked pot.
			if cracked, ok := rules.CrackedPot(state); ok {
				hit.w.SetBlock(b.X, b.Y, b.Z, cracked)
				state = cracked
			}
			s.breakByProjectile(hit, state, registry.EntityTypes[hit.entityType])
		case rules.ProjectileBlockAmethyst:
			s.tellEffect(hit.w, world.Effect{
				SoundID: registry.SoundBlockAmethystBlockChime,
				X:       b.X, Y: b.Y, Z: b.Z,
				Volume: 1, Pitch: rules.AmethystChimePitch(rand.Float32()),
			})
		case rules.ProjectileBlockDripleaf:
			changes, effects := s.physicsFor(hit.w).DripleafProjectiled(b.X, b.Y, b.Z)
			for _, e := range effects {
				s.tellEffect(hit.w, e)
			}
			s.tellBlockChanges(hit.w, changes)
		case rules.ProjectileBlockSpeleothem:
			// The speleothem's break names no breaker, unlike the pot's.
			// vanilla: SpeleothemBlock.onProjectileHit
			if hit.entityType == protocol.EntityTypeTrident &&
				b.Speed > rules.TridentBreaksSpeleothemSpeed && s.projectileMayBreak(hit) {
				s.breakByProjectile(hit, state, "")
			}
		}
	}
}

// projectileMayBreak is Projectile.mayInteract and mayBreak.
func (s *Server) projectileMayBreak(hit projectileBlockHit) bool {
	return s.mayProjectileInteract(hit) &&
		rules.ProjectileMayBreak(hit.entityType, hit.w.BoolRule(world.RuleProjectilesCanBreakBlocks))
}

// breakByProjectile breaks the block with drops rolled with entityType as
// this_entity ("" for none). vanilla: Level.destroyBlock
func (s *Server) breakByProjectile(hit projectileBlockHit, state uint16, entityType string) {
	b := hit.block
	if s.destroyBlockContentsBy(hit.w, b.X, b.Y, b.Z, state, entityType) {
		s.setBlockAndTell(hit.w, b.X, b.Y, b.Z, world.RemovalLeaves(state))
	}
}

// mayProjectileInteract is Projectile.mayInteract: ownerless always may, a
// mob owner only with mob_griefing, a player owner only outside spawn
// protection (Level.mayInteract). An unknown owner UUID counts as no player.
func (s *Server) mayProjectileInteract(hit projectileBlockHit) bool {
	if hit.ownerMob {
		return hit.w.BoolRule(world.RuleMobGriefing)
	}
	if hit.ownerUUID == uuid.Nil {
		return true
	}
	return !s.spawnProtected(hit.w, hit.ownerUUID, hit.block.X, hit.block.Z)
}

// projectileSplash is a projectile's first touch of water, played and posted
// after the locks, since the warden listener pass takes the mob lock. silent
// skips the sound, not the game event; owner is the shooter, uuid.Nil for
// none.
// vanilla: Entity.doWaterSplashEffect
type projectileSplash struct {
	w             *world.World
	x, y, z       float64
	volume, pitch float32
	silent        bool
	owner         uuid.UUID
}

// splashProjectile plays sp's splash and posts its splash game event.
func (s *Server) splashProjectile(sp projectileSplash) {
	if !sp.silent {
		s.broadcastSoundID(sp.w, registry.SoundEntityGenericSplash, protocol.SoundCategoryNeutral,
			sp.x, sp.y, sp.z, sp.volume, sp.pitch)
	}
	s.gameEvent(sp.w, rules.GameEventSplash, sp.x, sp.y, sp.z,
		rules.GameEventContext{Player: sp.owner, ProjectileOwner: sp.owner})
}

// burnSpot is where a vehicle lava hurt landed, for its burn sound.
type burnSpot struct {
	w       *world.World
	x, y, z float64
}

// burnVehicle plays the fizz of a lava hurt that landed.
// vanilla: Entity.lavaHurt
func (s *Server) burnVehicle(b burnSpot) {
	s.broadcastSoundID(b.w, registry.SoundEntityGenericBurn, protocol.SoundCategoryNeutral, b.x, b.y, b.z, 0.4, s.burnPitch())
}

// fizzleVehicle plays the sound of a burn put out.
// vanilla: Entity.playEntityOnFireExtinguishedSound
func (s *Server) fizzleVehicle(b burnSpot) {
	s.broadcastSoundPacketExcept(b.w, nil, s.extinguishSound(protocol.SoundCategoryNeutral, b.x, b.y, b.z))
}
