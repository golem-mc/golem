package server

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// thornsHitSound plays at an attacker hit by thorns.
// vanilla: LivingEntity.playSecondaryHurtSound
const thornsHitSound = registry.SoundEnchantThornsHit

// retaliationCause is the damage cause of this player's armour hitting back,
// with the player behind it.
func (c *client) retaliationCause() *damageCause {
	at := c.player.Position()
	return &damageCause{
		entityID: c.player.EntityID,
		x:        at.X, y: at.Y, z: at.Z,
		weapon: c.heldItem(protocol.HandMain),
		killer: &rules.Killer{Name: protocol.Text{Text: c.player.Name}, Weapon: c.heldItem(protocol.HandMain)},
		shove:  true,
	}
}

// thornsOnOwner hits back the owner of a projectile that landed on victim.
// vanilla: EnchantmentHelper.doPostAttackEffects
func (s *Server) thornsOnOwner(w *world.World, victim *client, ownerID int32, damageType registry.DamageType, directAttacker int32) {
	if ownerID == 0 {
		return
	}
	ctx := &rules.EnchContext{
		DamageType: damageType, Direct: false,
		Victim: protocol.EntityTypePlayer, Attacker: -1, DirectAttacker: directAttacker,
	}
	if owner := s.entityClient(ownerID); owner != nil {
		ctx.Attacker = protocol.EntityTypePlayer
		s.postAttack(w, attackBody{player: owner}, attackBody{player: victim}, protocol.ItemStack{}, ctx, syncVelocityAfterTravel)
	} else if ent := s.mobByEntityIDAnyDim(ownerID); ent != nil {
		s.thornsOnMobOwner(w, victim, ent.mob, ent.w, ctx)
	}
}

// thornsOnMobOwner hits back a mob owner already resolved, living in ownerW.
func (s *Server) thornsOnMobOwner(w *world.World, victim *client, owner *entity.Mob, ownerW *world.World, ctx *rules.EnchContext) {
	ctx.Attacker = mobEntityType(owner.Type)
	s.postAttack(w, attackBody{mob: owner, w: ownerW}, attackBody{player: victim}, protocol.ItemStack{}, ctx, syncVelocityAfterTravel)
}

// mobThorns defers a mob victim's post-attack dispatch until after the mob lock.
type mobThorns struct {
	mob    *entity.Mob
	ctx    rules.EnchContext
	weapon protocol.ItemStack
}

func mobRetaliations(ent *mobEntity, ctx *rules.EnchContext, weapon protocol.ItemStack) mobThorns {
	return mobThorns{mob: ent.mob, ctx: *ctx, weapon: weapon}
}

func (c *client) thornsFromMob(w *world.World, t mobThorns, when velocitySync) {
	if t.mob != nil {
		c.srv.postAttack(w, attackBody{player: c}, attackBody{mob: t.mob}, t.weapon, &t.ctx, when)
	}
}

// mobByEntityIDAnyDim is mobByEntityIDAnyDimLocked taking mobsMu itself.
func (s *Server) mobByEntityIDAnyDim(id int32) *mobEntity {
	s.mobsMu.Lock()
	defer s.mobsMu.Unlock()
	ent, _ := s.mobByEntityIDAnyDimLocked(id)
	return ent
}
