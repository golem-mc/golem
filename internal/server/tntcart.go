package server

import (
	"math"
	"math/rand"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// entityEventTNTCartPrimed is the entity event that lights a TNT cart's
// fuse on the client. vanilla: MinecartTNT.handleEntityEvent
const entityEventTNTCartPrimed = 70

// cartBlast is a TNT cart's explosion, run once the cart lock is down.
type cartBlast struct {
	it      *minecartEntity
	w       *world.World
	blast   entity.TNTCartBlast
	inWater bool
	// name is the cart's custom name, read under the cart lock.
	name customName
	// tags are the cart's scoreboard tags, read under the cart lock.
	tags []string
}

// cartPrime is a lit fuse to tell the world about once the cart lock is down.
type cartPrime struct {
	w       *world.World
	id      int32
	x, y, z float64
}

// tntCartEffects collects what a TNT cart's pass owes after the cart lock.
type tntCartEffects struct {
	blasts []cartBlast
	primes []cartPrime
}

// newCartRng is a cart's own dice.
func (s *Server) newCartRng(id int32) *rand.Rand {
	return entity.NewDice(s.entitySeed(id))
}

// tntCartEnv is what the cart's decisions read of the server. The cart lock
// is held.
func tntCartEnv(it *minecartEntity) entity.TNTCartEnv {
	return entity.TNTCartEnv{Rng: it.rng, Explodes: it.w.TNTExplodes()}
}

// collectTNTCart turns the cart's decisions into effects to run after the
// cart lock. The cart lock is held.
func collectTNTCart(it *minecartEntity, out entity.TNTCartEffects, fx *tntCartEffects) {
	for _, p := range out.Primes {
		fx.primes = append(fx.primes, cartPrime{w: it.w, id: it.cart.ID, x: p.X, y: p.Y, z: p.Z})
	}
	for _, b := range out.Blasts {
		fx.blasts = append(fx.blasts, cartBlast{
			it: it, w: it.w, blast: b, name: it.custom, tags: it.tags.Values(),
			inWater: rules.IsWater(it.w.BlockAt(
				int32(math.Floor(b.X)), int32(math.Floor(b.Y)), int32(math.Floor(b.Z)))),
		})
	}
}

// tntCartHurt lays a blow on a TNT cart. The cart lock is held.
func (s *Server) tntCartHurt(it *minecartEntity, damage float64, dropless bool, hit entity.TNTCartHit, fx *tntCartEffects) bool {
	var out entity.TNTCartEffects
	landed := it.tnt.Hurt(it.cart, tntCartEnv(it), damage, dropless, hit, &out)
	collectTNTCart(it, out, fx)
	return landed
}

// tntCartTick runs a TNT cart's part of the tick after the ride. The cart
// lock is held.
func (s *Server) tntCartTick(it *minecartEntity, fx *tntCartEffects) {
	var out entity.TNTCartEffects
	it.tnt.Tick(it.cart, tntCartEnv(it), &out)
	collectTNTCart(it, out, fx)
}

// tntCartBlockResist gives a primed cart's blast zero resistance for a rail
// and the block under one, so rays pass them cheaply.
// vanilla: MinecartTNT.getBlockExplosionResistance
func tntCartBlockResist(w *world.World) func(bx, by, bz int32) (float64, bool) {
	return func(bx, by, bz int32) (float64, bool) {
		// Air has no resistance for the calculator to override.
		if w.BlockAt(bx, by, bz) == 0 {
			return 0, false
		}
		return 0, isRailOrUnderRail(w, bx, by, bz)
	}
}

// isRailOrUnderRail is whether the cell holds a rail or has one above it.
func isRailOrUnderRail(w *world.World, bx, by, bz int32) bool {
	for _, y := range [2]int32{by, by + 1} {
		if registry.BlockOf(w.BlockAt(bx, y, bz)).InTag(registry.TagIDRails) {
			return true
		}
	}
	return false
}

// runTNTCartEffects sends the lit fuses and runs the blasts after the cart
// lock.
func (s *Server) runTNTCartEffects(fx tntCartEffects) {
	for _, p := range fx.primes {
		s.broadcastWorld(p.w, protocol.IDPlayEntityEvent, entityEventPayload(p.id, entityEventTNTCartPrimed))
		s.broadcastSoundID(p.w, registry.SoundEntityTntPrimed, protocol.SoundCategoryBlock, p.x, p.y, p.z, 1.0, 1.0)
	}
	for _, cb := range fx.blasts {
		ownerID, ownerName := s.ownerNameOf(cb.w, cb.blast.OwnerID)
		damageType := rules.DamageTypePlayerExplosion
		if ownerID == 0 {
			damageType = rules.DamageTypeExplosion
		}
		if s.tntCartBlastSeam != nil {
			s.tntCartBlastSeam(cb.blast.Radius)
		}
		killer := s.entityKiller(cb.w, ownerID)
		sourceTeam, _ := s.scoreboard.PlayerTeam(cb.it.uuid.String())
		b := blast{
			w: cb.w, x: cb.blast.X, y: cb.blast.Y, z: cb.blast.Z,
			radius:        cb.blast.Radius,
			causeID:       ownerID,
			causeName:     ownerName,
			causeKiller:   killer,
			directID:      cb.it.cart.ID,
			directUUID:    cb.it.uuid,
			directName:    s.directNameOf(killer, protocol.EntityTypeTntMinecart, cb.it.uuid, cb.name),
			directType:    entity.MinecartTNT,
			directLoot:    &rules.LootEntity{Type: entity.MinecartTNT, Tags: cb.tags},
			directInWater: cb.inWater,
			damageType:    damageType,
			rng:           cb.it.rng,
			interaction:   rules.TNTExplosionInteraction(s.world),
			sourceTeam:    sourceTeam,
		}
		if cb.blast.Primed {
			b.blockResist = tntCartBlockResist(cb.w)
			b.spared = func(bx, by, bz int32) bool { return isRailOrUnderRail(cb.w, bx, by, bz) }
		}
		s.runExplosion(b)
	}
}
