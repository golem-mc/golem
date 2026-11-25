package server

import (
	"cmp"
	"slices"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/google/uuid"
)

type bobberTargetQuery struct {
	w           *world.World
	box         entity.AABB
	hookedID    int32
	hookedExtra bobberExtraRef
	hooked      entity.ArrowTarget
	extras      []bobberExtraTarget
}

func bobberSearchBox(hook *entity.Bobber) entity.AABB {
	if hook.NeedsCollisionTargets() {
		return hook.TargetSearchBox()
	}
	return entity.AABB{}
}

func (q *bobberTargetQuery) readExtras(s *Server) {
	if ref := q.hookedExtra; ref.kind != nil {
		q.hooked = ref.kind.lookup(s, q.w, q.hookedID, ref.uuid)
	}
	if q.box == (entity.AABB{}) {
		return
	}
	area, indexed := bobberChunkArea(q.box)
	for _, kind := range bobberExtraKinds {
		q.extras = kind.collect(s, q.w, q.extras, q.box, area, indexed)
	}
	slices.SortFunc(q.extras, func(a, b bobberExtraTarget) int { return bobberTargetOrder(a.ArrowTarget, b.ArrowTarget) })
}

func bobberChunkArea(box entity.AABB) (chunkArea, bool) {
	minX, minY, minZ, maxX, maxY, maxZ := box.Corners()
	bounds := [6]float64{minX, minY, minZ, maxX, maxY, maxZ}
	return smallChunkArea(&bounds)
}

func (q *bobberTargetQuery) mobTargets(dst []entity.ArrowTarget, h *mobHerd, roots map[int32]int32) []entity.ArrowTarget {
	if q.box == (entity.AABB{}) {
		return dst
	}
	area, indexed := bobberChunkArea(q.box)
	h.mobsInArea(area, indexed, func(m *entity.Mob) bool {
		if m.Type != entity.TypeEnderDragon {
			minX, minY, minZ, maxX, maxY, maxZ := m.BlastBox()
			if q.box.Crosses(entity.NewAABB(minX, minY, minZ, maxX, maxY, maxZ)) {
				dst = append(dst, entity.ArrowTarget{
					EntityID: m.ID, RootVehicle: roots[m.ID],
					MinX: minX, MinY: minY, MinZ: minZ,
					MaxX: maxX, MaxY: maxY, MaxZ: maxZ,
				})
			}
		}
		return true
	})
	// Dragon parts can cross the sweep while their parent is in another chunk.
	// vanilla: Level.getEntities
	for _, m := range h.dragonBodies {
		for part := range 8 {
			target := entity.DragonPartTarget(m, protocol.EntityTypeEnderDragon, part)
			if q.box.Crosses(target.Box()) {
				target.RootVehicle = roots[m.ID]
				dst = append(dst, target)
			}
		}
	}
	slices.SortFunc(dst, bobberTargetOrder)
	return dst
}

func bobberTargetOrder(a, b entity.ArrowTarget) int { return cmp.Compare(a.EntityID, b.EntityID) }

func bobberPlayerTarget(players []entity.ArrowTarget, id int32) entity.ArrowTarget {
	i, ok := slices.BinarySearchFunc(players, id, func(t entity.ArrowTarget, id int32) int {
		return cmp.Compare(t.EntityID, id)
	})
	if ok {
		return players[i]
	}
	return entity.ArrowTarget{}
}

func (q *bobberTargetQuery) hookedTarget(s *Server, players []entity.ArrowTarget) entity.ArrowTarget {
	id := q.hookedID
	if id == 0 || q.hookedExtra.kind != nil {
		return q.hooked
	}
	if ent := s.mobByEntityIDLocked(q.w, id); ent != nil {
		m := ent.mob
		if m.Type == entity.TypeEnderDragon && id != m.ID {
			return entity.DragonPartTarget(m, protocol.EntityTypeEnderDragon, int(id-m.ID-1))
		}
		return entity.MobTarget(id, mobEntityType(m.Type), m)
	}
	return bobberPlayerTarget(players, id)
}

func (q *bobberTargetQuery) extraRef(id int32) bobberExtraRef {
	i, ok := slices.BinarySearchFunc(q.extras, id, func(t bobberExtraTarget, id int32) int {
		return cmp.Compare(t.EntityID, id)
	})
	if ok {
		return q.extras[i].ref
	}
	return bobberExtraRef{}
}

// bobberVehicleRoots reads only occupied vehicles that can be an angler's
// root. The hull and passengers share its ID for owner exclusion.
func (s *Server) bobberVehicleRoots(c *client, w *world.World, roots map[int32]int32) map[int32]int32 {
	if boat := c.vehicle.Load(); boat != nil {
		s.lockTick(&s.boatsMu)
		if boat.w == w && !boat.boat.Dead {
			if roots == nil {
				roots = make(map[int32]int32)
			}
			for _, p := range boat.passengers {
				if p.c != nil {
					roots[p.c.player.EntityID] = boat.boat.ID
				} else if p.ent != nil {
					roots[p.ent.mob.ID] = boat.boat.ID
				}
			}
		}
		s.unlockTick(&s.boatsMu)
	}
	if cart := c.minecart.Load(); cart != nil {
		s.lockTick(&s.minecartsMu)
		if cart.w == w && !cart.cart.Dead {
			if roots == nil {
				roots = make(map[int32]int32)
			}
			for _, p := range cart.passengers {
				roots[p.player.EntityID] = cart.cart.ID
			}
		}
		s.unlockTick(&s.minecartsMu)
	}
	return roots
}

// bobberExtraRef keeps a hooked body's tracker identity for lookup after it
// leaves the sweep. The network ID is checked in case the UUID was reloaded.
type bobberExtraRef struct {
	kind *bobberExtraKind
	uuid uuid.UUID
}

type bobberExtraTarget struct {
	entity.ArrowTarget
	ref bobberExtraRef
}

type bobberExtraKind struct {
	collect func(*Server, *world.World, []bobberExtraTarget, entity.AABB, chunkArea, bool) []bobberExtraTarget
	lookup  func(*Server, *world.World, int32, uuid.UUID) entity.ArrowTarget
	pull    func(*Server, *world.World, int32, uuid.UUID, float64, float64, float64) bool
}

func bobberExtraKindOf[B any, S any](tracker *trackedKind[B, S], target func(B) entity.ArrowTarget, push func(B, float64, float64, float64)) *bobberExtraKind {
	lookup := func(s *Server, w *world.World, id int32, uuid uuid.UUID) (B, bool) {
		b, ok := tracker.chunks(s).ByUUID(uuid)
		return b, ok && tracker.world(b) == w && tracker.id(b) == id && !tracker.dead(b)
	}
	kind := &bobberExtraKind{
		lookup: func(s *Server, w *world.World, id int32, uuid uuid.UUID) entity.ArrowTarget {
			mu := tracker.lock(s)
			mu.Lock()
			defer mu.Unlock()
			if b, ok := lookup(s, w, id, uuid); ok {
				return target(b)
			}
			return entity.ArrowTarget{}
		},
		pull: func(s *Server, w *world.World, id int32, uuid uuid.UUID, dx, dy, dz float64) bool {
			mu := tracker.lock(s)
			mu.Lock()
			defer mu.Unlock()
			b, ok := lookup(s, w, id, uuid)
			if ok && push != nil {
				push(b, dx, dy, dz)
			}
			return ok
		},
	}
	kind.collect = func(s *Server, w *world.World, dst []bobberExtraTarget, box entity.AABB, area chunkArea, indexed bool) []bobberExtraTarget {
		mu := tracker.lock(s)
		mu.Lock()
		defer mu.Unlock()
		visit := func(b B) bool {
			if tracker.world(b) == w && !tracker.dead(b) {
				t := target(b)
				if !t.Unhittable && box.Crosses(t.Box()) {
					dst = append(dst, bobberExtraTarget{t, bobberExtraRef{kind, tracker.uuid(b)}})
				}
			}
			return true
		}
		if indexed {
			bodiesInChunks(tracker.chunks(s), nil, &area, visit)
		} else {
			for _, b := range *tracker.list(s) {
				visit(b)
			}
		}
		return dst
	}
	return kind
}

type bobberBoxBody interface {
	BlastBox() (minX, minY, minZ, maxX, maxY, maxZ float64)
}

func bobberBoxTarget(id, typ int32, body bobberBoxBody) entity.ArrowTarget {
	minX, minY, minZ, maxX, maxY, maxZ := body.BlastBox()
	return entity.ArrowTarget{EntityID: id, Type: typ, MinX: minX, MinY: minY, MinZ: minZ, MaxX: maxX, MaxY: maxY, MaxZ: maxZ}
}

var bobberItemTargets = bobberExtraKindOf(&itemTracker, func(it *itemEntity) entity.ArrowTarget {
	return bobberBoxTarget(it.item.ID, protocol.EntityTypeItem, it.item)
}, func(it *itemEntity, dx, dy, dz float64) {
	it.item.VX += dx
	it.item.VY += dy
	it.item.VZ += dz
})

var bobberExtraKinds = [...]*bobberExtraKind{
	bobberItemTargets,
	bobberExtraKindOf(&tntTracker, func(it *tntEntity) entity.ArrowTarget {
		return bobberBoxTarget(it.tnt.ID, protocol.EntityTypeTnt, it.tnt)
	}, func(it *tntEntity, dx, dy, dz float64) { it.tnt.Push(dx, dy, dz) }),
	bobberExtraKindOf(&boatTracker, func(it *boatEntity) entity.ArrowTarget {
		target := bobberBoxTarget(it.boat.ID, it.entityTypeID(), it.boat)
		target.RootVehicle = it.boat.ID
		return target
	}, func(it *boatEntity, dx, dy, dz float64) { it.boat.Push(dx, dy, dz) }),
	bobberExtraKindOf(&fallingTracker, func(it *fallingEntity) entity.ArrowTarget {
		return bobberBoxTarget(it.fb.ID, protocol.EntityTypeFallingBlock, it.fb)
	}, func(it *fallingEntity, dx, dy, dz float64) { it.fb.Push(dx, dy, dz) }),
	bobberExtraKindOf(&frameTracker, func(it *frameEntity) entity.ArrowTarget {
		return entity.ArrowTarget{EntityID: it.id, Type: frameEntityType(it), MinX: it.minX, MinY: it.minY, MinZ: it.minZ, MaxX: it.maxX, MaxY: it.maxY, MaxZ: it.maxZ}
	}, nil),
	bobberExtraKindOf(&paintingTracker, func(it *paintingEntity) entity.ArrowTarget {
		return entity.ArrowTarget{EntityID: it.id, Type: protocol.EntityTypePainting, MinX: it.minX, MinY: it.minY, MinZ: it.minZ, MaxX: it.maxX, MaxY: it.maxY, MaxZ: it.maxZ}
	}, nil),
	bobberExtraKindOf(&standTracker, func(it *armorStandEntity) entity.ArrowTarget {
		target := bobberBoxTarget(it.stand.ID, protocol.EntityTypeArmorStand, it.stand)
		target.Unhittable = it.stand.Marker
		return target
	}, func(it *armorStandEntity, dx, dy, dz float64) {
		it.stand.VX += dx
		it.stand.VY += dy
		it.stand.VZ += dz
	}),
}
