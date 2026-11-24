package server

import (
	"sync"

	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/world"
)

// pistonMove is a body's share of a piston's tick: the carry offset and any
// slime block axis velocity.
type pistonMove struct {
	dx, dy, dz float64
	bounce     bool
}

// bodyPistonGrid indexes one kind's boxes for a dimension's physics step.
// The kind's lock guards the grid and its source list.
type bodyPistonGrid[B comparable] struct {
	grid   entity.BoxGrid
	bodies []B
	hits   []B
	length int
	last   B
	built  bool
	active bool
}

// candidates keeps list order. Outside a physics step positions can change
// without invalidating the grid, so that path must scan the list. The returned
// slice is reused by the next query under the kind's lock.
func (g *bodyPistonGrid[B]) candidates(list []B, pp *world.PistonPush) []B {
	if !g.active {
		return list
	}
	if len(list) == 0 {
		g.built = false
		return nil
	}
	// Kind lists append new bodies and shorten on removal. The last element
	// also catches a removal followed by an append of the same total length.
	if g.length != len(list) || g.last != list[len(list)-1] {
		g.built = false
	}
	if !g.built {
		return list
	}
	r := pp.ReachBox()
	if r[0] > r[3] {
		return nil
	}
	clear(g.hits)
	g.hits = g.hits[:0]
	g.grid.Query(entity.NewAABB(r[0], r[1], r[2], r[3], r[4], r[5]), func(i int32) { g.hits = append(g.hits, g.bodies[i]) })
	return g.hits
}

// build caches boxes only after a collection with no effects, so repeated
// pushes that invalidate the cache keep scanning without construction work.
func (g *bodyPistonGrid[B]) build(list []B, eligible func(B) bool, box func(B) [6]float64) {
	// Half-section cells keep a narrow sweep from visiting distant bodies
	// that share its section.
	g.grid.Cell = 8
	g.grid.Reset()
	clear(g.bodies)
	g.bodies = g.bodies[:0]
	for _, it := range list {
		if !eligible(it) {
			continue
		}
		b := box(it)
		g.grid.Add(int32(len(g.bodies)), entity.NewAABB(b[0], b[1], b[2], b[3], b[4], b[5]))
		g.bodies = append(g.bodies, it)
	}
	g.length, g.last = len(list), list[len(list)-1]
	g.built = true
}

// Bits of Server.pistonPopulated.
const (
	pistonItems uint32 = 1 << iota
	pistonOrbs
	pistonArrows
	pistonTNTs
	pistonFallings
	pistonBoats
	pistonArmorStands
	pistonMinecarts
	pistonEndCrystals
)

type bodyPistonGrids struct {
	items       bodyPistonGrid[*itemEntity]
	orbs        bodyPistonGrid[*orbEntity]
	arrows      bodyPistonGrid[*arrowEntity]
	tnts        bodyPistonGrid[*tntEntity]
	fallings    bodyPistonGrid[*fallingEntity]
	boats       bodyPistonGrid[*boatEntity]
	armorStands bodyPistonGrid[*armorStandEntity]
	fixtures    bodyPistonGrid[*fixtureEntity]
	minecarts   bodyPistonGrid[*minecartEntity]
	endCrystals bodyPistonGrid[*endCrystalEntity]
}

func setBodyPistonGrid[B comparable](mu *sync.Mutex, g *bodyPistonGrid[B], active bool) {
	mu.Lock()
	defer mu.Unlock()
	g.active, g.built = active, false
}

// setTrackedPistonGrid is setBodyPistonGrid for a tracked kind; turning it on
// also refreshes the kind's pistonPopulated bit from its list.
func setTrackedPistonGrid[B comparable, S any](s *Server, k *trackedKind[B, S], g *bodyPistonGrid[B], active bool) {
	mu := k.lock(s)
	mu.Lock()
	defer mu.Unlock()
	g.active, g.built = active, false
	if active {
		if len(*k.list(s)) > 0 {
			s.pistonPopulated.Or(k.pistonBit)
		} else {
			s.pistonPopulated.And(^k.pistonBit)
		}
	}
}

// pistonCarry reads a piston move's sweep of a body's box. A move that would
// put the box into a block is dropped, where vanilla slides the body as far
// as blocks allow.
// vanilla: PistonMovingBlockEntity.moveCollidedEntities, moveStuckEntities
func pistonCarry(w *world.World, pp *world.PistonPush, box [6]float64, onGround bool) (pistonMove, bool) {
	var m pistonMove
	if !pp.Reaches(box) {
		return m, false
	}
	if dx, dy, dz, hit := pp.Shift(box); hit {
		m = pistonMove{dx: dx, dy: dy, dz: dz, bounce: pp.Bounce}
	} else if onGround {
		dx, dz, ok := pp.Carries(box, (box[0]+box[3])/2, (box[2]+box[5])/2)
		if !ok {
			return m, false
		}
		m = pistonMove{dx: dx, dz: dz}
	} else {
		return m, false
	}
	moved := [6]float64{box[0] + m.dx, box[1] + m.dy, box[2] + m.dz, box[3] + m.dx, box[4] + m.dy, box[5] + m.dz}
	if !w.BoxCollides(box[0], box[1], box[2], box[3], box[4], box[5]) &&
		w.BoxCollides(moved[0], moved[1], moved[2], moved[3], moved[4], moved[5]) {
		return m, false
	}
	return m, true
}

// pushByPiston carries w's bodies a moving piston sweeps, each kind under
// its own lock, none held across teleports. Players are skipped: the
// client moves and bounces itself.
// vanilla: PistonMovingBlockEntity.moveCollidedEntities, ServerPlayer
func (s *Server) pushByPiston(w *world.World, pp *world.PistonPush) {
	dim := s.dimensionOfWorld(w)
	// A zero bit is a kind that is always visited.
	collectors := [...]struct {
		bit     uint32
		collect func(*Server, *dimension, *world.PistonPush) []func()
	}{
		{0, (*Server).collectMobsPistonPush},
		{pistonItems, (*Server).collectItemsPistonPush},
		{pistonOrbs, (*Server).collectOrbsPistonPush},
		{pistonArrows, (*Server).collectArrowsPistonPush},
		{pistonTNTs, (*Server).collectTNTsPistonPush},
		{pistonFallings, (*Server).collectFallingsPistonPush},
		{pistonBoats, (*Server).collectBoatsPistonPush},
		{pistonArmorStands, (*Server).collectArmorStandsPistonPush},
		{0, (*Server).collectCushionsPistonPush},
		{pistonMinecarts, (*Server).collectMinecartsPistonPush},
		{pistonEndCrystals, (*Server).collectEndCrystalsPistonPush},
	}
	populated := s.pistonPopulated.Load()
	var moves []func()
	for _, c := range collectors {
		if c.bit != 0 && populated&c.bit == 0 {
			continue
		}
		moves = append(moves, c.collect(s, dim, pp)...)
	}
	for _, move := range moves {
		move()
	}
}

func (s *Server) collectMobsPistonPush(dim *dimension, pp *world.PistonPush) []func() {
	w := dim.w
	s.mobsMu.Lock()
	defer s.mobsMu.Unlock()
	var moves []func()
	herd := dim.mobs
	if len(herd.herd) == 0 {
		herd.pistonGrid.built = false
		return nil
	}
	// A warm cache yields column hits in herd order; cold and inactive
	// collections scan the herd.
	g := &herd.pistonGrid
	candidates := herd.herd
	if g.active {
		if herd.pistonVersion != herd.version {
			g.built = false
			herd.pistonVersion = herd.version
		}
	}
	candidates = g.candidates(candidates, pp)
	var reachTop float64
	if len(candidates) > 0 {
		reachTop = pp.ReachBox()[4]
	}
	for _, m := range candidates {
		// A mob's box starts at its feet, so one standing at or above the
		// reach is out before its size is worked out.
		if !(m.Y < reachTop) {
			continue
		}
		hw := m.BbWidth() / 2
		box := [6]float64{m.X - hw, m.Y, m.Z - hw, m.X + hw, m.Y + m.BbHeight(), m.Z + hw}
		mv, ok := pistonCarry(w, pp, box, m.OnGround)
		if !ok {
			continue
		}
		ent := herd.book[m]
		if ent == nil {
			continue
		}
		to := player.Position{X: m.X + mv.dx, Y: m.Y + mv.dy, Z: m.Z + mv.dz, Yaw: m.Yaw, Pitch: m.Pitch}
		moves = append(moves, keepVelocity(&s.mobsMu, pp.Dir, mv, m.VX, m.VY, m.VZ,
			func(vx, vy, vz float64) {
				ent.mob.VX, ent.mob.VY, ent.mob.VZ, ent.mob.OnGround = vx, vy, vz, false
			},
			func() { s.teleportMob(ent, to, to.Yaw, to.Pitch, "piston", nil) }))
	}
	if len(moves) > 0 {
		// The moves teleport mobs once the lock is down.
		g.built = false
	} else if g.active && !g.built {
		g.build(herd.herd, func(*entity.Mob) bool { return true }, func(m *entity.Mob) [6]float64 {
			hw := m.BbWidth() / 2
			return [6]float64{m.X - hw, m.Y, m.Z - hw, m.X + hw, m.Y + m.BbHeight(), m.Z + hw}
		})
	}
	return moves
}

// bounceVelocity sets the movement axis component to the axis step.
// vanilla: PistonMovingBlockEntity.moveCollidedEntities
func bounceVelocity(dir int, vx, vy, vz float64) (float64, float64, float64) {
	switch dir {
	case 0:
		vy = -1
	case 1:
		vy = 1
	case 2:
		vz = -1
	case 3:
		vz = 1
	case 4:
		vx = -1
	default:
		vx = 1
	}
	return vx, vy, vz
}

// keepVelocity wraps a teleport so the body keeps its velocity (or the
// slime bounce of it), which the teleport helpers would zero.
// vanilla: Entity.move(MoverType.PISTON)
func keepVelocity(mu *sync.Mutex, dir int, m pistonMove, vx, vy, vz float64, set func(vx, vy, vz float64), teleport func()) func() {
	if m.bounce {
		vx, vy, vz = bounceVelocity(dir, vx, vy, vz)
	}
	return func() {
		teleport()
		mu.Lock()
		defer mu.Unlock()
		set(vx, vy, vz)
	}
}

// collectPushedBodies gathers one kind's swept bodies under mu. box is the
// body's box and onGround whether it rests on ground (for the honey carry);
// move reads fields under mu into the closure that sends the carry after mu
// is released.
func collectPushedBodies[T any](
	mu *sync.Mutex,
	list *[]*T,
	g *bodyPistonGrid[*T],
	w *world.World,
	pp *world.PistonPush,
	inWorld func(*T) bool,
	skip func(*T) bool,
	box func(*T) [6]float64,
	onGround func(*T) bool,
	move func(*T, pistonMove) func(),
) []func() {
	mu.Lock()
	defer mu.Unlock()
	var moves []func()
	candidates := *list
	if len(candidates) == 0 {
		g.built = false
		return nil
	}
	candidates = g.candidates(candidates, pp)
	for _, it := range candidates {
		if skip(it) {
			continue
		}
		m, ok := pistonCarry(w, pp, box(it), onGround(it))
		if !ok {
			continue
		}
		moves = append(moves, move(it, m))
	}
	if len(moves) > 0 {
		g.built = false
	} else if g.active && !g.built {
		g.build(*list, inWorld, box)
	}
	return moves
}

func pistonBoxOf(minX, minY, minZ, maxX, maxY, maxZ float64) [6]float64 {
	return [6]float64{minX, minY, minZ, maxX, maxY, maxZ}
}

func (s *Server) collectItemsPistonPush(dim *dimension, pp *world.PistonPush) []func() {
	w := dim.w
	return collectTrackedPistonPush(s, &itemTracker, &dim.pistons.items, w, pp,
		func(it *itemEntity) pistonBodyState {
			b := it.item
			return pistonBodyState{pistonBoxOf(b.BlastBox()), b.OnGround, b.VX, b.VY, b.VZ}
		},
		func(it *itemEntity, vx, vy, vz float64) { it.item.VX, it.item.VY, it.item.VZ = vx, vy, vz },
		(*Server).teleportItem)
}

type pistonBodyState struct {
	box        [6]float64
	onGround   bool
	vx, vy, vz float64
}

// collectTrackedPistonPush snapshots one tracked kind under its lock; moves run after unlocking.
func collectTrackedPistonPush[B comparable, S any](s *Server, k *trackedKind[B, S], g *bodyPistonGrid[B], w *world.World, pp *world.PistonPush,
	read func(B) pistonBodyState, set func(B, float64, float64, float64), teleport func(*Server, B, player.Position, string)) []func() {
	mu := k.lock(s)
	mu.Lock()
	defer mu.Unlock()
	var moves []func()
	candidates := *k.list(s)
	if len(candidates) == 0 {
		g.built = false
		return nil
	}
	candidates = g.candidates(candidates, pp)
	for _, it := range candidates {
		if k.dead(it) || k.world(it) != w {
			continue
		}
		state := read(it)
		move, ok := pistonCarry(w, pp, state.box, state.onGround)
		if !ok {
			continue
		}
		pos := k.pose(it)
		to := player.Position{X: pos.x + move.dx, Y: pos.y + move.dy, Z: pos.z + move.dz, Yaw: pos.yaw}
		moves = append(moves, keepVelocity(mu, pp.Dir, move, state.vx, state.vy, state.vz,
			func(vx, vy, vz float64) { set(it, vx, vy, vz) },
			func() { teleport(s, it, to, "piston") }))
	}
	if len(moves) > 0 {
		g.built = false
	} else if g.active && !g.built {
		g.build(*k.list(s), func(it B) bool { return k.world(it) == w }, func(it B) [6]float64 { return read(it).box })
	}
	return moves
}

func (s *Server) collectOrbsPistonPush(dim *dimension, pp *world.PistonPush) []func() {
	w := dim.w
	return collectTrackedPistonPush(s, &orbTracker, &dim.pistons.orbs, w, pp,
		func(it *orbEntity) pistonBodyState {
			b := it.orb
			return pistonBodyState{pistonBoxOf(b.BlastBox()), b.OnGround, b.VX, b.VY, b.VZ}
		},
		func(it *orbEntity, vx, vy, vz float64) { it.orb.VX, it.orb.VY, it.orb.VZ = vx, vy, vz },
		(*Server).teleportOrb)
}

func (s *Server) collectArrowsPistonPush(dim *dimension, pp *world.PistonPush) []func() {
	w := dim.w
	return collectPushedBodies(&s.arrowsMu, &s.arrows, &dim.pistons.arrows, w, pp,
		func(it *arrowEntity) bool { return it.w == w },
		func(it *arrowEntity) bool { return it.arrow.Dead || it.w != w },
		func(it *arrowEntity) [6]float64 { return pistonBoxOf(it.arrow.BlastBox()) },
		func(*arrowEntity) bool { return false },
		func(it *arrowEntity, m pistonMove) func() {
			to := player.Position{X: it.arrow.X + m.dx, Y: it.arrow.Y + m.dy, Z: it.arrow.Z + m.dz}
			return keepVelocity(&s.arrowsMu, pp.Dir, m, it.arrow.VX, it.arrow.VY, it.arrow.VZ,
				func(vx, vy, vz float64) { it.arrow.VX, it.arrow.VY, it.arrow.VZ = vx, vy, vz },
				func() { s.teleportArrow(it, to, "piston") })
		})
}

func (s *Server) collectTNTsPistonPush(dim *dimension, pp *world.PistonPush) []func() {
	w := dim.w
	return collectTrackedPistonPush(s, &tntTracker, &dim.pistons.tnts, w, pp,
		func(it *tntEntity) pistonBodyState {
			b := it.tnt
			return pistonBodyState{pistonBoxOf(b.BlastBox()), b.OnGround, b.VX, b.VY, b.VZ}
		},
		func(it *tntEntity, vx, vy, vz float64) { it.tnt.VX, it.tnt.VY, it.tnt.VZ = vx, vy, vz },
		(*Server).teleportTNT)
}

func (s *Server) collectFallingsPistonPush(dim *dimension, pp *world.PistonPush) []func() {
	w := dim.w
	return collectTrackedPistonPush(s, &fallingTracker, &dim.pistons.fallings, w, pp,
		func(it *fallingEntity) pistonBodyState {
			b := it.fb
			return pistonBodyState{pistonBoxOf(b.BlastBox()), b.OnGround, b.VX, b.VY, b.VZ}
		},
		func(it *fallingEntity, vx, vy, vz float64) { it.fb.VX, it.fb.VY, it.fb.VZ = vx, vy, vz },
		(*Server).teleportFallingBlock)
}

func (s *Server) collectBoatsPistonPush(dim *dimension, pp *world.PistonPush) []func() {
	w := dim.w
	return collectPushedBodies(&s.boatsMu, &s.boats, &dim.pistons.boats, w, pp,
		func(it *boatEntity) bool { return it.w == w },
		func(it *boatEntity) bool { return it.boat.Dead || it.w != w },
		func(it *boatEntity) [6]float64 { return pistonBoxOf(it.boat.BlastBox()) },
		func(it *boatEntity) bool { return it.boat.OnGround },
		func(it *boatEntity, m pistonMove) func() {
			to := player.Position{X: it.boat.X + m.dx, Y: it.boat.Y + m.dy, Z: it.boat.Z + m.dz}
			yaw := it.boat.Yaw
			return keepVelocity(&s.boatsMu, pp.Dir, m, it.boat.VX, it.boat.VY, it.boat.VZ,
				func(vx, vy, vz float64) { it.boat.VX, it.boat.VY, it.boat.VZ = vx, vy, vz },
				func() { s.teleportBoat(it, to, yaw, "piston") })
		})
}

// collectArmorStandsPistonPush skips markers, which vanilla's push reaction
// ignores. vanilla: ArmorStand.getPistonPushReaction
func (s *Server) collectArmorStandsPistonPush(dim *dimension, pp *world.PistonPush) []func() {
	w := dim.w
	return collectPushedBodies(&s.armorStandsMu, &s.armorStands, &dim.pistons.armorStands, w, pp,
		func(ent *armorStandEntity) bool { return ent.w == w },
		func(ent *armorStandEntity) bool { return ent.stand.Dead || ent.w != w || ent.stand.Marker },
		func(ent *armorStandEntity) [6]float64 { return pistonBoxOf(ent.stand.BlastBox()) },
		func(ent *armorStandEntity) bool { return ent.stand.OnGround },
		func(ent *armorStandEntity, m pistonMove) func() {
			to := player.Position{X: ent.stand.X + m.dx, Y: ent.stand.Y + m.dy, Z: ent.stand.Z + m.dz, Yaw: ent.stand.Yaw}
			return keepVelocity(&s.armorStandsMu, pp.Dir, m, ent.stand.VX, ent.stand.VY, ent.stand.VZ,
				func(vx, vy, vz float64) { ent.stand.VX, ent.stand.VY, ent.stand.VZ = vx, vy, vz },
				func() { s.teleportArmorStand(ent, to, "piston", false) })
		})
}

// collectCushionsPistonPush breaks each cushion the move sweeps, with its
// drop: a block-attached entity dies on any non-zero move, even one blocks
// would stop.
// vanilla: BlockAttachedEntity.move
func (s *Server) collectCushionsPistonPush(dim *dimension, pp *world.PistonPush) []func() {
	w := dim.w
	if !s.cushionsMade.Load() {
		return nil
	}
	s.fixturesMu.Lock()
	defer s.fixturesMu.Unlock()
	var breaks []func()
	g := &dim.pistons.fixtures
	for _, ent := range g.candidates(s.fixtures, pp) {
		if ent.dead || ent.w != w || ent.kind != entity.FixtureCushion {
			continue
		}
		dx, dy, dz, hit := pp.Shift(pistonBoxOf(entity.FixtureBox(ent.kind, ent.tag, ent.x, ent.y, ent.z)))
		if hit && dx*dx+dy*dy+dz*dz > 0 {
			breaks = append(breaks, func() { s.breakCushion(ent, cushionDrops) })
		}
	}
	if len(breaks) > 0 {
		g.built = false
	} else if g.active && !g.built && len(s.fixtures) > 0 {
		g.build(s.fixtures, func(ent *fixtureEntity) bool { return ent.w == w && !ent.dead && ent.kind == entity.FixtureCushion },
			func(ent *fixtureEntity) [6]float64 {
				return pistonBoxOf(entity.FixtureBox(ent.kind, ent.tag, ent.x, ent.y, ent.z))
			})
	}
	return breaks
}

func (s *Server) collectMinecartsPistonPush(dim *dimension, pp *world.PistonPush) []func() {
	w := dim.w
	return collectTrackedPistonPush(s, &minecartTracker, &dim.pistons.minecarts, w, pp,
		func(it *minecartEntity) pistonBodyState {
			b := it.cart
			return pistonBodyState{pistonBoxOf(b.BlastBox()), b.OnGround, b.VX, b.VY, b.VZ}
		},
		func(it *minecartEntity, vx, vy, vz float64) { it.cart.VX, it.cart.VY, it.cart.VZ = vx, vy, vz },
		func(s *Server, it *minecartEntity, to player.Position, _ string) {
			s.minecartsMu.Lock()
			defer s.minecartsMu.Unlock()
			// The client predicts piston carries; retain ordinary tracking.
			// vanilla: PistonMovingBlockEntity.moveEntityByPiston
			it.cart.SetPos(to.X, to.Y, to.Z)
			minecartTracker.invalidatePistonGrid(s, it)
			s.cartCols = nil
		})
}

func (s *Server) collectEndCrystalsPistonPush(dim *dimension, pp *world.PistonPush) []func() {
	w := dim.w
	return collectPushedBodies(&s.endCrystalsMu, &s.endCrystals, &dim.pistons.endCrystals, w, pp,
		func(ent *endCrystalEntity) bool { return ent.w == w },
		func(ent *endCrystalEntity) bool { return ent.crystal.Dead || ent.w != w },
		func(ent *endCrystalEntity) [6]float64 { return pistonBoxOf(ent.crystal.BlastBox()) },
		func(*endCrystalEntity) bool { return false },
		func(ent *endCrystalEntity, m pistonMove) func() {
			to := player.Position{X: ent.crystal.X + m.dx, Y: ent.crystal.Y + m.dy, Z: ent.crystal.Z + m.dz}
			return func() {
				s.endCrystalsMu.Lock()
				defer s.endCrystalsMu.Unlock()
				// The client applies the piston move too; forcing a teleport
				// here can move it a second time. Commanded teleports still sync.
				// vanilla: EntityTypes.END_CRYSTAL, ServerEntity.sendChanges
				ent.crystal.SetPos(to.X, to.Y, to.Z)
				endCrystalTracker.invalidatePistonGrid(s, ent)
			}
		})
}
