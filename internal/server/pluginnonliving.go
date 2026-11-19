package server

import (
	"slices"

	"github.com/golem-mc/golem/internal/server/pluginruntime"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/plugin"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
)

// askNonlivingDamage fires EntityDamageEvent for a hurt of a body that is
// not a living entity (item, frame, end crystal) and answers the amount to
// land and whether the hurt may. Call it with no lock of the body's kind
// held, so a listener may read the body.
// vanilla: ItemEntity.hurtServer, ItemFrame.hurtServer, EndCrystal.hurtServer
func (s *Server) askNonlivingDamage(w *world.World, id uuid.UUID, damageType registry.DamageType, cause *damageCause, amount float32) (float32, bool) {
	if !s.entityDamageListened() {
		return amount, true
	}
	e := &plugin.EntityDamageEvent{
		Entity: pluginEntity{id: id},
		Source: s.pluginSource(w, damageType, cause),
		Amount: amount,
		Final:  amount,
	}
	s.askBody(func() { s.plugins.Fire(e) })
	return e.Amount, !e.Cancelled()
}

func (s *Server) entityDamageListened() bool {
	return s.plugins != nil && s.plugins.Has(pluginruntime.EntityDamage)
}

// askBlastBodies fires EntityDamageEvent for each body a blast reaches and
// answers those no listener cancelled. Call it with the body kind's lock
// released.
func askBlastBodies[T any](s *Server, b blast, bodies []T, id func(T) uuid.UUID) []T {
	if len(bodies) == 0 || !s.entityDamageListened() {
		return bodies
	}
	cause := blastCause(b)
	return slices.DeleteFunc(bodies, func(body T) bool {
		_, ok := s.askNonlivingDamage(b.w, id(body), b.damageType, cause, 1)
		return !ok
	})
}

// askBlastItems fires EntityDamageEvent for each item a blast would hurt and
// answers the damage each is left with, 0 for a cancelled hurt; items it does
// not list are hurt as computed.
// vanilla: ServerExplosion.hurtEntities, ItemEntity.hurtServer
func (s *Server) askBlastItems(b blast, in *blastInputs) map[*itemEntity]float32 {
	if b.harmless || b.sparesItems() || !s.entityDamageListened() {
		return nil
	}
	type candidate struct {
		it     *itemEntity
		damage float32
	}
	var found []candidate
	s.itemsMu.Lock()
	for _, it := range in.items {
		if it.item.Dead || it.w != b.w || in.spares(it.uuid) {
			continue
		}
		if dist, ok := in.at.Reach(it.item.FeetPosition()); ok {
			found = append(found, candidate{it, float32(in.at.Hit(in.blocks, dist, it.item.BlastBox, it.item.EyePosition).Damage)})
		}
	}
	s.itemsMu.Unlock()
	cause := blastCause(b)
	out := make(map[*itemEntity]float32, len(found))
	for _, c := range found {
		damage, ok := s.askNonlivingDamage(b.w, c.it.uuid, b.damageType, cause, c.damage)
		if !ok {
			damage = 0
		}
		out[c.it] = damage
	}
	return out
}
