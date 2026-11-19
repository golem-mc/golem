package server

import (
	"slices"

	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/pluginruntime"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/plugin"
	"github.com/google/uuid"
)

// arrowHitAsk is a flight's first body hit of a step, held back until the
// locks are released: the step is undone and replayed the next tick with the
// listeners' answer, so a hit is delayed one tick while a listener is
// registered. The undo restores the struct but not its Rand stream, so rolls
// the held step drew (crit, deflection, egg chicks, splash sound) are redrawn
// by the replay.
type arrowHitAsk struct {
	id       uuid.UUID
	passed   *[]int32
	allowed  *int32
	targetID int32
	target   plugin.Entity
	x, y, z  float64
}

// arrowBlockHit is a block a flight stuck in, announced to listeners once the
// locks are released; cancelling it is not honoured.
type arrowBlockHit struct {
	id      uuid.UUID
	w       *world.World
	hit     entity.BlockHit
	x, y, z float64
}

func (s *Server) hitListening() bool {
	return s.plugins != nil && s.plugins.Has(pluginruntime.ProjectileHit)
}

// arrowHitAskOf returns the body hit the step's events hold that no listener
// has answered yet, with the arrow and mob locks held. Only mobs and players
// are asked.
// vanilla: Projectile.onHit
func (s *Server) arrowHitAskOf(it *arrowEntity, bodies *arrowBodies, allowed int32, events []entity.ArrowEvent) (arrowHitAsk, bool) {
	for _, ev := range events {
		if ev.Kind != entity.ArrowHitTarget {
			continue
		}
		if ask, ok := s.hitAskOf(it.uuid, &it.arrow.Passed, &it.arrow.HitAllowed, bodies, allowed, ev.Target.EntityID, ev.X, ev.Y, ev.Z); ok {
			return ask, true
		}
	}
	return arrowHitAsk{}, false
}

// thrownHitAskOf is arrowHitAskOf for a thrown body, whose body hit is a hit
// event or a break on a body.
func (s *Server) thrownHitAskOf(it *thrownEntity, bodies *arrowBodies, allowed int32, events []entity.ThrownEvent) (arrowHitAsk, bool) {
	for _, ev := range events {
		if ev.Kind != entity.ThrownHitTarget && (ev.Kind != entity.ThrownBreak || ev.Target.EntityID == 0) {
			continue
		}
		if ask, ok := s.hitAskOf(it.uuid, &it.throwable.Passed, &it.throwable.HitAllowed, bodies, allowed, ev.Target.EntityID, ev.X, ev.Y, ev.Z); ok {
			return ask, true
		}
	}
	return arrowHitAsk{}, false
}

// hitAskOf is the ask for body targetID, none when a listener already let it
// land or the body is neither a mob nor a player.
func (s *Server) hitAskOf(id uuid.UUID, passed *[]int32, allowedOut *int32, bodies *arrowBodies, allowed, targetID int32, x, y, z float64) (arrowHitAsk, bool) {
	if targetID == allowed {
		return arrowHitAsk{}, false
	}
	ask := arrowHitAsk{id: id, passed: passed, allowed: allowedOut, targetID: targetID, x: x, y: y, z: z}
	if m := bodies.mobs[targetID]; m != nil {
		if e := s.mobEntityOf(m); e != nil {
			ask.target = pluginLivingHandle(e.uuid)
		}
	} else if c := bodies.clients[targetID]; c != nil {
		ask.target = s.pluginAdapter.pluginEntityOf(c)
	}
	return ask, ask.target != nil
}

// resolveHitAsks puts each held hit to listeners with every tick lock
// released. A cancel lets the flight pass the body from the next tick on;
// otherwise the next step lets the hit land.
func (s *Server) resolveHitAsks(asks []arrowHitAsk) {
	for _, a := range asks {
		e := &plugin.ProjectileHitEvent{
			Projectile: pendingProjectile{id: a.id},
			Hit:        plugin.RayHit{Pos: plugin.Vec3{X: a.x, Y: a.y, Z: a.z}, Entity: a.target},
		}
		s.firePluginEvent(e)
		s.lockTick(&s.arrowsMu)
		if e.Cancelled() {
			if !slices.Contains(*a.passed, a.targetID) {
				*a.passed = append(*a.passed, a.targetID)
			}
		} else {
			*a.allowed = a.targetID
		}
		s.unlockTick(&s.arrowsMu)
	}
}

// fireBlockHits announces the blocks flights stuck in.
func (s *Server) fireBlockHits(hits []arrowBlockHit) {
	for _, h := range hits {
		b := s.pluginBlock(h.w, h.hit.X, h.hit.Y, h.hit.Z)
		s.firePluginEvent(&plugin.ProjectileHitEvent{
			Projectile: pendingProjectile{id: h.id},
			Hit: plugin.RayHit{Pos: plugin.Vec3{X: h.x, Y: h.y, Z: h.z}, Block: &b,
				Face: plugin.BlockFace(h.hit.Face)},
		})
	}
}
