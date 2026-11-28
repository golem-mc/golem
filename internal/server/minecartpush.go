package server

import (
	"github.com/golem-mc/golem/registry"
	"math"
	"slices"

	"github.com/google/uuid"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/world"
)

// cartReachHalf is how far from a cart's centre, sideways, its box or its
// sweep reaches at any point of the tick: half its width, the 0.2 sweep and
// at most a block of step (the snap onto the rail's line, half a block, and
// the step capped at 0.4). A deliberate bound, not vanilla's; the reach box
// also spans 2 blocks and the fall speed up and down for slopes and falls.
const cartReachHalf = entity.MinecartHalfWidth + 0.2 + 1.0

// cartPushCell is the side of the push grids' columns: carts parked side by
// side would crowd a 16-block one.
const cartPushCell = 4

// standPushRange is the squared distance within which an armor stand pushes a cart.
// vanilla: ArmorStand.pushEntities
const standPushRange = 0.2

// cartPushBody is one body near a cart, frozen before the cart lock.
// A player takes its share of a push on its own client.
type cartPushBody struct {
	box   entity.AABB
	x, z  float64
	c     *client
	ent   *mobEntity
	team  string
	rule  entity.CollisionRule
	golem bool
	// seated is whether the mob rides something.
	seated bool
	// pushes is whether the body's own push step runs; pushable whether
	// it takes a push. vanilla: LivingEntity.pushEntities, LivingEntity.isPushable
	pushes, pushable bool
	collides         bool
	// stand marks an armor stand, which pushes a cart whose centre is within
	// standPushRange of (x, y, z) whatever the teams. vanilla: ArmorStand.pushEntities
	stand *armorStandEntity
	y     float64
	// rootCart excludes the cart itself and its indirect passengers.
	rootCart *minecartEntity
}

// cartPushWorld is one world's frozen bodies and its carts as they stood
// before the tick: each cart's reach box, the cart filed in cartGrid by its
// centre, and all the union of the reach boxes.
type cartPushWorld struct {
	bodies   []cartPushBody
	grid     entity.BoxGrid
	carts    []*minecartEntity
	reach    []entity.AABB
	cartGrid entity.BoxGrid
	all      entity.AABB
}

// cartPushes is the tick's push inputs and the mob shares it leaves; its
// storage is reused from tick to tick, touched by the cart tick alone.
type cartPushes struct {
	worlds map[*world.World]*cartPushWorld
	teams  pushTeams
	mobs   []mobPushShare
	stands []standPushShare
	move   cartBodyWorld
}

// pushTeams is the tick's team of every member and each team's collision
// rule, both nil while no team has members.
type pushTeams struct {
	names map[string]string
	rules map[string]entity.CollisionRule
}

func (s *Server) pushTeams() pushTeams {
	var t pushTeams
	if t.names = s.scoreboard.MemberTeams(); t.names != nil {
		t.rules = s.teamCollisionRules()
	}
	return t
}

// of is the entity's team and its collision rule, "" while it has none.
func (t pushTeams) of(id uuid.UUID) (string, entity.CollisionRule) {
	if t.names == nil {
		return "", entity.CollideAlways
	}
	team := t.names[id.String()]
	return team, t.rules[team]
}

// mobPushShare is a mob's share of a cart's or boat's push, added to its
// velocity once the vehicle lock is down.
type mobPushShare struct {
	w      *world.World
	ent    *mobEntity
	dx, dz float64
}

// standPushShare is an armor stand's share of a cart's push, added to its
// velocity once the vehicle lock is down.
type standPushShare struct {
	ent    *armorStandEntity
	dx, dz float64
}

// near yields each cart whose reach box, as filed before the tick, may meet
// box.
func (wp *cartPushWorld) near(box entity.AABB, yield func(i int32)) {
	if !box.Crosses(wp.all) {
		return
	}
	wp.cartGrid.QueryPoints(box.Inflate(cartReachHalf), func(i int32) {
		if box.Crosses(wp.reach[i]) {
			yield(i)
		}
	})
}

func (wp *cartPushWorld) inReach(box entity.AABB) bool {
	found := false
	wp.near(box, func(int32) { found = true })
	return found
}

// minecartPushInputs reads, before the cart lock, the carts' places under
// the cart lock alone, the players near one from the tick's snapshot and the
// mobs and boats near one under their own locks. The result is valid until
// the next call.
func (s *Server) minecartPushInputs(clients []*client) *cartPushes {
	p := s.cartPushScratch
	if p == nil {
		p = &cartPushes{worlds: make(map[*world.World]*cartPushWorld)}
		s.cartPushScratch = p
	}
	for _, wp := range p.worlds {
		wp.bodies, wp.reach, wp.carts = wp.bodies[:0], wp.reach[:0], wp.carts[:0]
		wp.grid.Reset()
		wp.cartGrid.Reset()
	}
	clear(p.mobs)
	p.mobs = p.mobs[:0]
	clear(p.stands)
	p.stands = p.stands[:0]
	p.teams = pushTeams{}

	add := func(wp *cartPushWorld, b cartPushBody) {
		wp.grid.Add(int32(len(wp.bodies)), b.box)
		wp.bodies = append(wp.bodies, b)
	}
	var live []*world.World
	s.lockTick(&s.minecartsMu)
	for _, it := range s.minecarts {
		m := it.cart
		if m.Dead {
			continue
		}
		wp := p.worlds[it.w]
		if wp == nil {
			wp = &cartPushWorld{grid: entity.BoxGrid{Cell: cartPushCell}, cartGrid: entity.BoxGrid{Cell: cartPushCell}}
			p.worlds[it.w] = wp
		}
		ry := 2 + math.Abs(m.VY)
		box := entity.NewAABB(m.X-cartReachHalf, m.Y-ry, m.Z-cartReachHalf, m.X+cartReachHalf, m.Y+entity.MinecartHeight+ry, m.Z+cartReachHalf)
		if len(wp.carts) == 0 {
			live = append(live, it.w)
			wp.all = box
		}
		wp.cartGrid.Add(int32(len(wp.carts)), entity.NewAABB(m.X, m.Y, m.Z, m.X, m.Y, m.Z))
		wp.carts = append(wp.carts, it)
		wp.reach = append(wp.reach, box)
		wp.all = wp.all.Union(box)
		add(wp, cartPushBody{box: entity.NewAABB(m.Box()), collides: true, rootCart: it})
	}
	s.unlockTick(&s.minecartsMu)
	if len(live) == 0 {
		return p
	}
	p.teams = s.pushTeams()
	s.lockTick(&s.boatsMu)
	for _, it := range s.boats {
		wp := p.worlds[it.w]
		if wp == nil || len(wp.carts) == 0 || it.boat.Dead {
			continue
		}
		box := entity.NewAABB(it.boat.Box())
		if wp.inReach(box) {
			add(wp, cartPushBody{box: box, collides: true})
		}
	}
	s.unlockTick(&s.boatsMu)
	s.lockTick(&s.armorStandsMu)
	for _, ent := range s.armorStands {
		wp := p.worlds[ent.w]
		if wp == nil || len(wp.carts) == 0 || ent.stand.Dead {
			continue
		}
		if box := ent.stand.Box(); wp.inReach(box) {
			add(wp, cartPushBody{box: box, x: ent.stand.X, y: ent.stand.Y, z: ent.stand.Z, stand: ent})
		}
	}
	s.unlockTick(&s.armorStandsMu)
	for _, c := range clients {
		if c.player == nil || c.player.GameMode() == protocol.GameModeSpectator || c.player.Vitals().Dead {
			continue
		}
		wp := p.worlds[c.theWorld()]
		if wp == nil || len(wp.carts) == 0 {
			continue
		}
		pos := c.player.Position()
		dims := c.dims(c.player.Appearance().Pose)
		half := dims.Width / 2
		box := entity.NewAABB(pos.X-half, pos.Y, pos.Z-half, pos.X+half, pos.Y+dims.Height, pos.Z+half)
		if !wp.inReach(box) {
			continue
		}
		team := p.teams.names[c.player.Name]
		add(wp, cartPushBody{box: box, x: pos.X, z: pos.Z, c: c, team: team, rule: p.teams.rules[team],
			pushes: true, pushable: true, collides: true, rootCart: c.minecart.Load()})
	}
	s.lockTick(&s.mobsMu)
	for _, w := range live {
		wp := p.worlds[w]
		h := s.herdOf(w)
		for i := range wp.bodies {
			b := &wp.bodies[i]
			if b.c != nil {
				if mount := b.c.mount.Load(); mount != nil {
					if root := h.book[mount.mob.RootVehicle()]; root != nil {
						b.rootCart = root.cart
					}
				}
			}
		}
		h.mobsNearBoxes(wp.reach, func(m *entity.Mob) bool {
			if m.Dead {
				return true
			}
			box := entity.NewAABB(m.BlastBox())
			if !wp.inReach(box) {
				return true
			}
			ent := h.record(m)
			if ent == nil {
				return true
			}
			var rootCart *minecartEntity
			if root := h.book[m.RootVehicle()]; root != nil {
				rootCart = root.cart
			}
			pushable := m.Pushable()
			add(wp, cartPushBody{box: box, x: m.X, z: m.Z, ent: ent, team: m.Team, rule: m.TeamCollision,
				golem: m.Type == entity.TypeIronGolem, seated: ent.seated(), pushes: m.PushesCarts(), pushable: pushable,
				collides: pushable || m.CollidesWithAny(), rootCart: rootCart})
			return true
		})
	}
	s.unlockTick(&s.mobsMu)
	return p
}

// cartBodyWorld supplies the move's colliders without taking another entity lock.
// vanilla: AbstractMinecart.canCollideWith, AbstractBoat.canVehicleCollide
type cartBodyWorld struct {
	*world.World
	bodies *cartPushWorld
	cart   *minecartEntity
}

// BodyBoxes returns the colliders the cart may meet, excluding its own riders.
func (w cartBodyWorld) BodyBoxes(box entity.AABB) []entity.AABB {
	var boxes []entity.AABB
	w.bodies.grid.Query(box, func(i int32) {
		b := &w.bodies.bodies[i]
		if b.collides && b.rootCart != w.cart && !w.cart.carries(b) && box.Crosses(b.box) {
			boxes = append(boxes, b.box)
		}
	})
	return boxes
}

func (p *cartPushes) bodyWorld(it *minecartEntity) entity.World {
	if wp := p.worlds[it.w]; wp != nil && len(wp.bodies) > 1 {
		// Step consumes the wrapper synchronously; the next cart may reuse it.
		p.move = cartBodyWorld{World: it.w, bodies: wp, cart: it}
		return &p.move
	}
	return it.w
}

// carries reports whether the body rides the cart.
func (it *minecartEntity) carries(b *cartPushBody) bool {
	if b.c != nil {
		return slices.Contains(it.passengers, b.c)
	}
	return slices.Contains(it.mobs, b.ent)
}

// pushedByBodies runs, before the cart's step, the push steps of the
// bodies overlapping it: the cart takes its share now, a mob's share is
// queued. It stands for the bodies ticking ahead of the cart, so a body a
// rolling cart reaches is taken aboard by the cart's sweep before it pushes.
// vanilla: LivingEntity.pushEntities, AbstractMinecart.push
func (p *cartPushes) pushedByBodies(it *minecartEntity) {
	wp := p.worlds[it.w]
	if wp == nil || len(wp.bodies) == 0 {
		return
	}
	cart := it.cart
	team, rule := p.teams.of(it.uuid)
	box := entity.NewAABB(cart.Box())
	wp.grid.Query(box, func(i int32) {
		b := &wp.bodies[i]
		if b.stand != nil {
			dx, dy, dz := b.x-cart.X, b.y-cart.Y, b.z-cart.Z
			if it.kind == registry.EntityTypeMinecart && box.Crosses(b.box) && dx*dx+dy*dy+dz*dz <= standPushRange {
				bx, bz := cart.PushFrom(b.x, b.z, nil)
				p.stands = append(p.stands, standPushShare{ent: b.stand, dx: bx, dz: bz})
			}
			return
		}
		if !b.pushes || !box.Crosses(b.box) || it.carries(b) || !entity.PushableBy(b.team, b.rule, team, rule) {
			return
		}
		if bx, bz := cart.PushFrom(b.x, b.z, nil); b.ent != nil {
			p.mobs = append(p.mobs, mobPushShare{w: it.w, ent: b.ent, dx: bx, dz: bz})
		}
	})
}

// pushOut runs the cart's own sweep after its step: a moving rideable cart
// shoves the players, iron golems and, once it carries someone, anyone it
// meets, and every cart pushes the other carts it meets. The cart takes its
// share now; a mob's share is queued.
// vanilla: OldMinecartBehavior.pushAndPickupEntities
func (p *cartPushes) pushOut(it *minecartEntity) {
	wp := p.worlds[it.w]
	if wp == nil {
		return
	}
	cart := it.cart
	team, rule := p.teams.of(it.uuid)
	sweep := cart.CartPushBox()
	shoves := it.kind == registry.EntityTypeMinecart && cart.Moving()
	if shoves && len(wp.bodies) > 0 {
		ridden := len(it.passengers) > 0 || len(it.mobs) > 0
		wp.grid.Query(sweep, func(i int32) {
			b := &wp.bodies[i]
			if !b.pushable || !sweep.Crosses(b.box) || it.carries(b) || !entity.PushableBy(team, rule, b.team, b.rule) {
				return
			}
			// Anyone else is the pick-up's to take aboard.
			if b.ent != nil && !b.golem && !ridden && !b.seated {
				return
			}
			if bx, bz := cart.ShovedBy(b.x, b.z, !ridden); b.ent != nil {
				p.mobs = append(p.mobs, mobPushShare{w: it.w, ent: b.ent, dx: bx, dz: bz})
			}
		})
	}
	if len(wp.carts) < 2 {
		return
	}
	// The other cart has moved inside its reach box since it was filed.
	wp.near(sweep, func(i int32) {
		o := wp.carts[i]
		if o == it || o.w != it.w || o.cart.Dead || !sweep.Crosses(entity.NewAABB(o.cart.Box())) {
			return
		}
		if shoves {
			if oTeam, oRule := p.teams.of(o.uuid); !entity.PushableBy(team, rule, oTeam, oRule) {
				return
			}
		}
		o.cart.PushFrom(cart.X, cart.Z, cart)
	})
}

// applyMobShares adds each queued share to its mob's velocity, under the
// mob lock and after the vehicle lock is down; a mob gone since is skipped.
// vanilla: Entity.push
func (s *Server) applyMobShares(shares []mobPushShare) {
	if len(shares) == 0 {
		return
	}
	s.lockTick(&s.mobsMu)
	for _, sh := range shares {
		if s.herdOf(sh.w).book[sh.ent.mob] != sh.ent || sh.ent.mob.Dead {
			continue
		}
		sh.ent.mob.VX += sh.dx
		sh.ent.mob.VZ += sh.dz
	}
	s.unlockTick(&s.mobsMu)
}

// applyStandShares adds each queued share to its armor stand's velocity,
// under the stand lock and after the vehicle lock is down.
// vanilla: Entity.push
func (s *Server) applyStandShares(shares []standPushShare) {
	if len(shares) == 0 {
		return
	}
	s.lockTick(&s.armorStandsMu)
	for _, sh := range shares {
		if sh.ent.stand.Dead {
			continue
		}
		sh.ent.stand.VX += sh.dx
		sh.ent.stand.VZ += sh.dz
	}
	s.unlockTick(&s.armorStandsMu)
}
