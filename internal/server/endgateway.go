package server

import (
	"maps"
	"math"
	"slices"
	"sync"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/command"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// gatewayKey names one end gateway block.
type gatewayKey struct {
	w       *world.World
	x, y, z int32
}

// gatewayCooldowns holds each cooling gateway's expiry as a ticksRun count,
// so nothing counts down per tick. Not persisted, like vanilla's
// teleportCooldown.
type gatewayCooldowns struct {
	mu    sync.Mutex
	until map[gatewayKey]uint64
	// creating serialises exit creation, so two bodies in one exit-less
	// gateway build one exit.
	creating sync.Mutex
}

// trigger starts the gateway's cooldown at now, returning false if already
// cooling. vanilla: TheEndGatewayBlockEntity.isCoolingDown, triggerCooldown
func (g *gatewayCooldowns) trigger(k gatewayKey, now uint64) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	if end, ok := g.until[k]; ok {
		if now < end {
			return false
		}
		delete(g.until, k)
	}
	if g.until == nil {
		g.until = make(map[gatewayKey]uint64)
	}
	g.until[k] = now + rules.EndGatewayCooldownTicks
	return true
}

// gatewayInBody finds an end gateway touching the player's box or movement.
func (c *client) gatewayInBody(prev, pos player.Position) (x, y, z int32, ok bool) {
	dims := c.dims(c.player.Appearance().Pose)
	hw := dims.Width / 2
	from := entity.NewAABB(prev.X-hw, prev.Y, prev.Z-hw, prev.X+hw, prev.Y+dims.Height, prev.Z+hw)
	to := entity.NewAABB(pos.X-hw, pos.Y, pos.Z-hw, pos.X+hw, pos.Y+dims.Height, pos.Z+hw)
	return entity.EndGatewayInside(c.theWorld(), from, to)
}

// triggerEndGateway starts the gateway's cooldown and tells clients the beam,
// returning false while it is cooling.
// vanilla: TheEndGatewayBlockEntity.triggerCooldown
func (s *Server) triggerEndGateway(w *world.World, x, y, z int32) bool {
	if !s.gateways.trigger(gatewayKey{w, x, y, z}, s.ticksRun.Load()) {
		return false
	}
	var b protocol.Buffer
	b.PutPosition(x, y, z)
	b.PutU8(1)
	b.PutU8(0)
	b.PutVarInt(registry.BlockIDs["minecraft:end_gateway"])
	s.broadcastBlockEvent(w, x, y, z, b.Bytes())
	return true
}

// touchEndGateway handles a player in an end gateway: the gateway starts
// its cooldown and tells clients (the beam), and a player not on cooldown
// moves to the exit its block entity names. No exit, no teleport.
// vanilla: EndGatewayBlock.entityInside, getPortalDestination
func (c *client) touchEndGateway(x, y, z int32) {
	// Spectators have no physics, so no entityInside runs for them.
	// vanilla: Player.tick noPhysics
	if c.player.GameMode() == protocol.GameModeSpectator {
		return
	}
	c.enteredBlockAdvancements(x, y, z)
	// A rider is not a body that can use a portal.
	// vanilla: Entity.canUsePortal
	if c.ridingBoat() || c.ridingMinecart() || c.ridingCushion() {
		return
	}
	w := c.theWorld()
	s := c.srv
	if !s.triggerEndGateway(w, x, y, z) {
		return
	}

	if !c.portalCooldown.CompareAndSwap(0, playerPortalCooldownTicks) {
		return
	}
	s.gatewayWG.Go(func() { c.travelByEndGateway(w, x, y, z) })
}

// travelByEndGateway moves the player to the gateway's exit, keeping
// velocity and facing; the exit search reads possibly unloaded columns, so it
// runs off the tick. vanilla: EndGatewayBlock.getPortalDestination
func (c *client) travelByEndGateway(w *world.World, x, y, z int32) {
	if c.theWorld() != w || c.player.Vitals().Dead {
		return
	}
	px, py, pz, ok := c.srv.gatewayDestination(w, x, y, z)
	if !ok || c.theWorld() != w || c.player.Vitals().Dead {
		return
	}
	c.teleportToGatewayExit(px, py, pz)
}

// teleportToGatewayExit moves the player to a gateway exit, keeping facing.
// The look is sent as zero deltas; Pos holds it for the stored position.
func (c *client) teleportToGatewayExit(px, py, pz float64) {
	cur := c.player.Position()
	c.teleport(command.Move{
		Pos:    player.Position{X: px, Y: py, Z: pz, Yaw: cur.Yaw, Pitch: cur.Pitch},
		DeltaX: true, DeltaY: true, DeltaZ: true,
		RelYaw: true, RelPitch: true,
	})
}

// gatewayTouch is a non-player body found inside an end gateway cell, left
// to run once the tick's locks are released. mu guards portal, the body's
// portal state, whose cooldown restarts at cooldown ticks; move puts the body
// at the landing.
type gatewayTouch struct {
	w        *world.World
	x, y, z  int32
	mu       *sync.Mutex
	portal   *rules.EntityPortal
	cooldown int
	move     func(to player.Position)
}

// gatewayTouchAt notes a body inside the end gateway at its cell. Callers test
// the cell first, so the touch's closure is only built for a body in one.
func gatewayTouchAt(out *[]gatewayTouch, w *world.World, x, y, z float64, g gatewayTouch) {
	g.w, g.x, g.y, g.z = w, rules.BlockOf(x), rules.BlockOf(y), rules.BlockOf(z)
	*out = append(*out, g)
}

// touchGateways runs each body's touch in order.
func (s *Server) touchGateways(touches []gatewayTouch) {
	for _, g := range touches {
		s.touchGateway(g)
	}
}

// touchGateway starts the gateway's cooldown for a body inside it and, if
// the body is off portal cooldown, moves it to the exit. A body in the
// cooldown has the cooldown restarted instead.
// vanilla: EndGatewayBlock.entityInside, Entity.setAsInsidePortal
func (s *Server) touchGateway(g gatewayTouch) {
	if !s.triggerEndGateway(g.w, g.x, g.y, g.z) {
		return
	}
	g.mu.Lock()
	cooling := g.portal.Cooldown > 0
	g.portal.Cooldown = g.cooldown
	g.mu.Unlock()
	if cooling {
		return
	}
	s.gatewayWG.Go(func() {
		if px, py, pz, ok := s.gatewayDestination(g.w, g.x, g.y, g.z); ok {
			// The landing search can outlast a short cooldown; it counts
			// from the move. vanilla: Entity.handlePortal
			g.mu.Lock()
			g.portal.Cooldown = g.cooldown
			g.mu.Unlock()
			g.move(player.Position{X: px, Y: py, Z: pz})
		}
	})
}

// gatewayLanding is rules.EndGatewayLanding with the columns it reads held
// loaded; the search may generate, so it runs off the tick.
func gatewayLanding(w *world.World, ex, ey, ez int32, exact bool) (px, py, pz float64) {
	var held []world.ChunkPos
	for cx := world.ChunkOf(float64(ex - rules.EndGatewayReach)); cx <= world.ChunkOf(float64(ex+rules.EndGatewayReach)); cx++ {
		for cz := world.ChunkOf(float64(ez - rules.EndGatewayReach)); cz <= world.ChunkOf(float64(ez+rules.EndGatewayReach)); cz++ {
			pos := world.ChunkPos{X: cx, Z: cz}
			w.Acquire(pos)
			held = append(held, pos)
		}
	}
	px, py, pz = rules.EndGatewayLanding(w, ex, ey, ez, exact, w.MinY(), w.MinY()+w.Height()-1)
	for _, pos := range held {
		w.Release(pos)
	}
	return px, py, pz
}

// The gateway movers set only the position, so a body keeps its velocity and
// look. vanilla: EndGatewayBlock.getPortalDestination, Relative.DELTA, ROTATION

// gatewayMoveItem puts an item at a gateway's landing, its box's middle there.
func (s *Server) gatewayMoveItem(it *itemEntity, to player.Position) {
	itemTracker.teleport(s, it, func() {
		it.item.X, it.item.Y, it.item.Z = to.X, to.Y+entity.ItemHalfSize, to.Z
	})
}

// gatewayMoveOrb puts an orb at a gateway's landing.
func (s *Server) gatewayMoveOrb(it *orbEntity, to player.Position) {
	orbTracker.teleport(s, it, func() { it.orb.X, it.orb.Y, it.orb.Z = to.X, to.Y, to.Z })
}

// gatewayMoveTNT puts a primed TNT at a gateway's landing.
func (s *Server) gatewayMoveTNT(it *tntEntity, to player.Position) {
	tntTracker.teleport(s, it, func() {
		it.tnt.X, it.tnt.Y, it.tnt.Z = to.X, to.Y, to.Z
		it.usedPortal = true
	})
}

// gatewayMoveFalling puts a falling block at a gateway's landing.
func (s *Server) gatewayMoveFalling(it *fallingEntity, to player.Position) {
	fallingTracker.teleport(s, it, func() { it.fb.X, it.fb.Y, it.fb.Z = to.X, to.Y, to.Z })
}

// gatewayMoveMob puts a mob at a gateway's landing.
func (s *Server) gatewayMoveMob(ent *mobEntity, to player.Position) {
	s.mobsMu.Lock()
	ent.mob.X, ent.mob.Y, ent.mob.Z = to.X, to.Y, to.Z
	ent.teleported = true
	s.herdOf(ent.w).pistonGrid.built = false
	s.mobsMu.Unlock()
}

// gatewayMoveThrown puts a thrown body at a gateway's landing: a pearl stops
// with its look zeroed, any other body keeps its velocity and look.
// vanilla: EndGatewayBlock.getPortalDestination
func (s *Server) gatewayMoveThrown(it *thrownEntity, to player.Position) {
	t := it.throwable
	thrownTracker.teleport(s, it, func() {
		if t.Dead {
			return
		}
		t.X, t.Y, t.Z = to.X, to.Y, to.Z
		if t.Kind == entity.ThrownEnderPearl {
			t.VX, t.VY, t.VZ, t.Yaw, t.Pitch = 0, 0, 0, 0, 0
		}
	})
}

// gatewayMoveArrow puts an arrow at a gateway's landing, its velocity kept.
func (s *Server) gatewayMoveArrow(it *arrowEntity, to player.Position) {
	arrowTracker.teleport(s, it, func() {
		if !it.arrow.Dead {
			it.arrow.X, it.arrow.Y, it.arrow.Z = to.X, to.Y, to.Z
		}
	})
}

// gatewayMoveFirework puts a rocket at the landing with its flight intact.
func (s *Server) gatewayMoveFirework(it *fireworkEntity, to player.Position) {
	fireworkTracker.teleport(s, it, func() {
		if !it.rocket.Dead {
			it.rocket.X, it.rocket.Y, it.rocket.Z = to.X, to.Y, to.Z
		}
	})
}

// gatewayMoveArmorStand puts a stand at the landing without stopping its fall.
func (s *Server) gatewayMoveArmorStand(ent *armorStandEntity, to player.Position) {
	s.armorStandsMu.Lock()
	if !ent.stand.Dead {
		ent.stand.X, ent.stand.Y, ent.stand.Z = to.X, to.Y, to.Z
		standTracker.invalidatePistonGrid(s, ent)
		ent.teleported = true
	}
	s.armorStandsMu.Unlock()
}

// gatewayVehicleMove is the passenger phase of a same-dimension teleport.
// delta preserves each passenger's offset, motion and look.
// vanilla: Entity.calculatePassengerTransition, teleportSameDimension
// The caller releases the vehicle lock before apply takes any other lock.
type gatewayVehicleMove struct {
	w            *world.World
	id           int32
	delta        player.Position
	riders       []*client
	mobs         []*mobEntity
	packet       []byte
	driver       *client
	driverPacket []byte
}

func (g gatewayVehicleMove) apply(s *Server) {
	// Off the tick: touchGateway runs this in its own goroutine.
	lockedDo(&s.mobsMu, func() {
		for _, ent := range g.mobs {
			aboard := ent.vehicle != nil && ent.vehicle.boat.ID == g.id || ent.cart != nil && ent.cart.cart.ID == g.id
			if !aboard || ent.mob.Dead {
				continue
			}
			var carry func(*entity.Mob)
			carry = func(m *entity.Mob) {
				m.X += g.delta.X
				m.Y += g.delta.Y
				m.Z += g.delta.Z
				if body := s.dimensionOfWorld(g.w).mobs.book[m]; body != nil {
					body.teleported = true
				}
				for _, passenger := range m.Passengers {
					carry(passenger)
				}
			}
			carry(ent.mob)
		}
	})
	for _, c := range g.riders {
		c.tp.mu.Lock()
		boat, cart := c.vehicle.Load(), c.minecart.Load()
		if c.theWorld() != g.w || (boat == nil || boat.boat.ID != g.id) && (cart == nil || cart.cart.ID != g.id) {
			c.tp.mu.Unlock()
			continue
		}
		pos := c.player.Position()
		pos.X, pos.Y, pos.Z = pos.X+g.delta.X, pos.Y+g.delta.Y, pos.Z+g.delta.Z
		c.send(protocol.IDPlaySyncPosition, c.awaitTeleport(command.Move{
			Pos: pos, DeltaX: true, DeltaY: true, DeltaZ: true, RotateDelta: true, RelYaw: true, RelPitch: true,
		}))
		c.tp.fly.Reset()
		packet := g.packet
		if c == g.driver {
			packet = g.driverPacket
		}
		c.send(protocol.IDPlayTeleportEntityRelative, packet)
		c.tp.mu.Unlock()
	}
}

// gatewayVehiclePacket sends the transition to the controller and an absolute
// snapshot to the other riders. vanilla: Entity.sendTeleportTransitionToRidingPlayers
func gatewayVehiclePacket(id int32, p bodyPose, relative bool) []byte {
	var flags int32
	if relative {
		p.vx, p.vy, p.vz, p.yaw, p.pitch = 0, 0, 0, 0, 0
		flags = teleportRelativeDeltaX | teleportRelativeDeltaY | teleportRelativeDeltaZ |
			teleportRelativeYaw | teleportRelativePitch | teleportRelativeRotateDelta
	}
	var b protocol.Buffer
	b.PutVarInt(id)
	b.PutF64(p.x)
	b.PutF64(p.y)
	b.PutF64(p.z)
	b.PutF64(p.vx)
	b.PutF64(p.vy)
	b.PutF64(p.vz)
	b.PutF32(p.yaw)
	b.PutF32(p.pitch)
	b.PutI32(flags)
	b.PutBool(p.onGround)
	return b.Bytes()
}

// gatewayMoveBoat carries a hull and all its passengers to the landing.
func (s *Server) gatewayMoveBoat(it *boatEntity, to player.Position) {
	g := gatewayVehicleMove{id: it.boat.ID}
	boatTracker.teleport(s, it, func() {
		b := it.boat
		g.w = it.w
		if b.Dead {
			return
		}
		g.delta = player.Position{X: to.X - b.X, Y: to.Y - b.Y, Z: to.Z - b.Z}
		b.X, b.Y, b.Z = to.X, to.Y, to.Z
		it.firstX, it.firstY, it.firstZ = to.X, to.Y, to.Z
		for _, p := range it.passengers {
			if p.c != nil {
				g.riders = append(g.riders, p.c)
			} else if p.ent != nil {
				g.mobs = append(g.mobs, p.ent)
			}
		}
		pose := bodyPose{x: b.X, y: b.Y, z: b.Z, vx: b.VX, vy: b.VY, vz: b.VZ, yaw: b.Yaw, onGround: b.OnGround}
		g.packet = gatewayVehiclePacket(b.ID, pose, false)
		if len(it.passengers) > 0 {
			g.driver = it.passengers[0].c
			g.driverPacket = gatewayVehiclePacket(b.ID, pose, true)
		}
	})
	g.apply(s)
}

// gatewayMoveMinecart carries a cart and its riders to the landing.
func (s *Server) gatewayMoveMinecart(it *minecartEntity, to player.Position) {
	g := gatewayVehicleMove{id: it.cart.ID}
	minecartTracker.teleport(s, it, func() {
		m := it.cart
		g.w = it.w
		if m.Dead {
			return
		}
		g.delta = player.Position{X: to.X - m.X, Y: to.Y - m.Y, Z: to.Z - m.Z}
		m.X, m.Y, m.Z = to.X, to.Y, to.Z
		g.riders = slices.Clone(it.passengers)
		g.mobs = slices.Clone(it.mobs)
		g.packet = gatewayVehiclePacket(m.ID, bodyPose{x: m.X, y: m.Y, z: m.Z, vx: m.VX, vy: m.VY, vz: m.VZ, yaw: m.Yaw, onGround: m.OnGround}, false)
	})
	g.apply(s)
}

// tickEndGateways ages every ticking gateway of w and sends the beam of one
// that is idle at a multiple of the beam interval.
// vanilla: TheEndGatewayBlockEntity.portalTick
func (s *Server) tickEndGateways(w *world.World, ticks func(world.ChunkPos) bool) {
	for _, pos := range w.TickEndGatewaysIn(rules.EndGatewayBeamInterval, ticks) {
		s.triggerEndGateway(w, pos[0], pos[1], pos[2])
	}
}

// gatewayDestination is where the gateway at (x, y, z) sends a body: the
// bottom centre of its exit, or the landing beside an inexact one. A gateway
// in the End without an exit builds one first. It may generate chunks, so it
// runs off the tick.
// vanilla: TheEndGatewayBlockEntity.getPortalPosition
func (s *Server) gatewayDestination(w *world.World, x, y, z int32) (px, py, pz float64, ok bool) {
	tag, _ := w.BlockEntityTag(x, y, z)
	ex, ey, ez, exact, found := rules.EndGatewayExit(tag)
	if !found && w.DimensionTypeName() == world.EndType {
		s.gateways.creating.Lock()
		defer s.gateways.creating.Unlock()
		tag, _ = w.BlockEntityTag(x, y, z)
		if ex, ey, ez, exact, found = rules.EndGatewayExit(tag); !found {
			ex, ey, ez = s.createGatewayExit(w, x, y, z)
			tag = maps.Clone(tag)
			if tag == nil {
				tag = nbt.Compound{}
			}
			tag["exit_portal"] = nbt.IntArray{ex, ey, ez}
			w.SetBlockEntityTag(x, y, z, "minecraft:end_gateway", tag)
			exact, found = false, true
			if flag, _ := nbt.Get[nbt.Byte](tag, "ExactTeleport"); flag != 0 {
				exact = true
			}
		}
	}
	if !found {
		return 0, 0, 0, false
	}
	px, py, pz = gatewayLanding(w, ex, ey, ez, exact)
	return px, py, pz, true
}

// createGatewayExit finds or builds the exit of the End gateway at (x, y, z):
// the tallest block in a chunk about 1024 blocks out along its direction (an
// island grown there if the chunk has no end stone to stand on), ten above
// which a return gateway is placed that leads back. The chunks it reads are
// held loaded meanwhile.
// vanilla: TheEndGatewayBlockEntity.findOrCreateValidTeleportPos, spawnGatewayPortal
func (s *Server) createGatewayExit(w *world.World, x, y, z int32) (ex, ey, ez int32) {
	var held []world.ChunkPos
	defer func() {
		for _, pos := range held {
			w.Release(pos)
		}
	}()
	hold := func(cx, cz int32) {
		pos := world.ChunkPos{X: cx, Z: cz}
		if slices.Contains(held, pos) {
			return
		}
		w.Acquire(pos)
		held = append(held, pos)
	}
	minY, maxY := w.MinY(), w.MinY()+w.Height()-1
	chunkOf := func(px, pz float64) (int32, int32) {
		return int32(math.Floor(px / 16)), int32(math.Floor(pz / 16))
	}
	top := func(cx, cz int32) (int32, bool) {
		hold(cx, cz)
		return rules.EndChunkTop(w, cx, cz, minY, maxY)
	}
	tx, tz := rules.EndGatewayTentative(x, z, func(px, pz float64) bool {
		_, filled := top(chunkOf(px, pz))
		return !filled
	})
	cx, cz := chunkOf(tx, tz)
	chunkTop, _ := top(cx, cz)
	sx, sy, sz, found := rules.EndGatewayValidSpawn(w, cx, cz, chunkTop)
	if !found {
		sx, sy, sz = int32(math.Floor(tx+0.5)), 75, int32(math.Floor(tz+0.5))
		// The island reaches at most a few blocks past its origin.
		for dx := int32(-1); dx <= 1; dx++ {
			for dz := int32(-1); dz <= 1; dz++ {
				hold((sx>>4)+dx, (sz>>4)+dz)
			}
		}
		s.writeBatch(w, world.EndIslandChanges(sx, sy, sz))
	}
	// The tallest search reads 16 columns around the spot.
	for dx := int32(-1); dx <= 1; dx++ {
		for dz := int32(-1); dz <= 1; dz++ {
			hold((sx>>4)+dx, (sz>>4)+dz)
		}
	}
	ex, ey, ez = rules.EndGatewayTallest(w, sx, sy, sz, 16, true, minY, maxY)
	ey += 10
	// The return gateway leads back to this one; its exit is inexact.
	for dx := int32(-1); dx <= 1; dx++ {
		for dz := int32(-1); dz <= 1; dz++ {
			hold(((ex)>>4)+dx, ((ez)>>4)+dz)
		}
	}
	cells := world.EndGatewayPortalCells(ex, ey, ez)
	changes := make([]world.BlockChange, len(cells))
	for i, c := range cells {
		changes[i] = world.BlockChange{X: c.X, Y: c.Y, Z: c.Z, State: c.State}
	}
	s.writeBatch(w, changes)
	w.SetBlockEntityTag(ex, ey, ez, "minecraft:end_gateway", nbt.Compound{"exit_portal": nbt.IntArray{x, y, z}})
	return ex, ey, ez
}
