package server

import (
	"log/slog"
	"slices"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/command"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/pluginruntime"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/plugin"
	"github.com/golem-mc/golem/registry"
)

// Nether and end portals: lighting, collapsing, travel. Shape and collapse
// decisions are internal/server/rules'; this writes blocks, moves bodies and tells
// everyone.

const (
	portalCooldownTicks        = 300 // 15 s; vanilla: Entity.getDimensionChangingDelay
	projectileCooldownTicks    = 2   // vanilla: Projectile.getDimensionChangingDelay
	vehiclePortalCooldownTicks = 10  // vanilla: VehicleEntity.getDimensionChangingDelay
	playerPortalCooldownTicks  = 10  // 0.5 s; vanilla: Player.getDimensionChangingDelay
)

// --- Lighting ---

// primeTntWithSteel primes a clicked TNT block with a held flint and steel
// or fire charge (TntBlock.useItemOn), reporting whether
// the click was one. The primed body and sound come before the block's
// removal, vanilla's order. With tnt_explodes off nothing is primed or
// spent, the player is told, and the click falls through to the item's use.
// The caller's useItemOnTail sends the ack.
func (c *client) primeTntWithSteel(hand, x, y, z int32) (bool, error) {
	held := c.heldItem(hand)
	if held.Empty() || (held.ItemID != int32(registry.ItemFlintAndSteel) && held.ItemID != int32(registry.ItemFireCharge)) {
		return false, nil
	}
	w := c.theWorld()
	if registry.BlockOf(w.BlockAt(x, y, z)) != registry.BlockTnt {
		return false, nil
	}
	if !w.TNTExplodes() {
		c.send(protocol.IDPlaySystemChat,
			systemChatPayload(protocol.Text{Translate: "block.minecraft.tnt.disabled"}, true))
		return false, nil
	}
	// vanilla: TntBlock.prime; refusal still consumes the block use.
	if c.player.GameMode() == protocol.GameModeAdventure && !rules.CanBreakInAdventureMode(held, adventureTarget(w, x, y, z, w.BlockAt(x, y, z))) {
		return true, nil
	}
	c.srv.primeTNTFrom(w, world.TNTPrime{X: x, Y: y, Z: z}, c.player.EntityID, c.player.Name, c.player.UUID)
	c.log.Info("tnt block primed", "x", x, "y", y, "z", z)
	c.setUsedBlock(w, x, y, z, airBlockState)
	// vanilla: TntBlock.useItemOn
	c.awardUsedHeld(hand)
	if held.ItemID == int32(registry.ItemFireCharge) {
		c.spendHeld(hand)
	} else {
		c.wearHeld(hand, 1)
	}
	return true, nil
}

// useFlintAndSteel handles a flint and steel click (see lightWithFire),
// reporting whether it was one.
func (c *client) useFlintAndSteel(hand, x, y, z, face int32) (bool, error) {
	held := c.heldItem(hand)
	if held.Empty() || held.ItemID != int32(registry.ItemFlintAndSteel) {
		return false, nil
	}
	return true, c.lightWithFire(hand, x, y, z, face, false, func() float32 {
		return 0.8 + c.srv.soundRng.Float32()*0.4
	}, registry.SoundItemFlintandsteelUse, c, func() { c.wearHeld(hand, 1) })
}

// useFireCharge handles a fire charge click, reporting whether it was one;
// it lights what flint and steel does, spending a charge instead of wear.
// vanilla: FireChargeItem.useOn
func (c *client) useFireCharge(hand, x, y, z, face int32) (bool, error) {
	held := c.heldItem(hand)
	if held.Empty() || held.ItemID != int32(registry.ItemFireCharge) {
		return false, nil
	}
	return true, c.lightWithFire(hand, x, y, z, face, true, func() float32 {
		return (c.srv.soundRng.Float32()-c.srv.soundRng.Float32())*0.2 + 1.0
	}, registry.SoundItemFirechargeUse, nil, func() { c.spendHeld(hand) })
}

// lightWithFire is a fire-starting item's click: lights an unlit campfire,
// candle or candle cake; otherwise fire goes into the air beyond the face,
// or a complete obsidian frame there fills with portal. The sound goes to
// everyone in the dimension but deaf (the flint and steel user); the fire
// charge's to all. placeEventAtFire picks where block_place is posted. use spends the item once something lit. The handler
// records the ack for the tick end (see ackBlock).
func (c *client) lightWithFire(hand, x, y, z, face int32, placeEventAtFire bool, pitch func() float32, sound registry.Sound, deaf *client, use func()) error {
	c.useSwing = swingNone
	if face < 0 || int(face) >= len(protocol.FaceOffset) {
		return nil
	}
	w := c.theWorld()
	held := c.heldItem(hand).ItemID
	spend := use
	use = func() {
		c.useSwing = swingPredicted
		// vanilla: ItemStack.useOn
		c.awardItemStat(registry.StatTypeUsed, held, 1)
		spend()
	}
	play := func(x, y, z int32) {
		c.srv.broadcastSoundIDExcept(w, deaf,
			sound, protocol.SoundCategoryBlock,
			float64(x)+0.5, float64(y)+0.5, float64(z)+0.5, 1.0, pitch())

	}
	// A BlockIgniteEvent listener may refuse the lighting: nothing lights,
	// sounds or wears.
	// vanilla: FlintAndSteelItem.useOn, FireChargeItem.useOn
	cancelled := func(bx, by, bz int32) bool { return c.pluginIgnite(bx, by, bz, placeEventAtFire) }
	if lit, ok := rules.FlintLight(w.BlockAt(x, y, z)); ok {
		if cancelled(x, y, z) {
			return nil
		}
		play(x, y, z)
		c.setUsedBlock(w, x, y, z, lit)
		c.gameEventAtCell(rules.GameEventBlockChange, x, y, z)
		use()
		return nil
	}
	// Only into air (any of the three air blocks), never into the clicked
	// block or water. vanilla: BaseFireBlock.canBePlacedAt
	offset := protocol.FaceOffset[face]
	nx, ny, nz := x+offset[0], y+offset[1], z+offset[2]
	if state := w.BlockAt(nx, ny, nz); !world.IsKnownAirState(state) {
		return nil
	}
	// The fire charge's event is at the fire's cell, flint and steel's at the
	// clicked block. vanilla: FireChargeItem.useOn, FlintAndSteelItem.useOn
	placed := func() {
		if placeEventAtFire {
			c.gameEventAtCell(rules.GameEventBlockPlace, nx, ny, nz)
		} else {
			c.gameEventAtCell(rules.GameEventBlockPlace, x, y, z)
			// vanilla: FlintAndSteelItem.useOn; reads the resulting portal too.
			c.placedBlockAdvancements(w, nx, ny, nz, held)
		}
	}
	// A complete frame lights whether or not fire could survive there.
	if c.pluginListening(pluginruntime.BlockIgnite) && c.srv.portalFrameLit(w, nx, ny, nz) && cancelled(nx, ny, nz) {
		return nil
	}
	if c.srv.ignitePortalAt(w, nx, ny, nz) {
		play(nx, ny, nz)
		placed()
		use()
		return nil
	}
	// vanilla: FireBlock.canSurvive
	if !rules.FireSurvivesAt(w, nx, ny, nz) {
		return nil
	}
	if cancelled(nx, ny, nz) {
		return nil
	}
	c.srv.portalChange(w, nx, ny, nz, rules.FireStateForPlacement(w, nx, ny, nz))
	placed()
	use()
	play(nx, ny, nz)
	return nil
}

// useEnderEye puts the held eye into the clicked end portal frame,
// reporting whether it did: a frame with an eye takes none, and every mode
// but creative spends one. The frame completing a ring of twelve
// fills the 3x3 inside with end portal, heard server-wide. The caller's
// useItemOnTail sends the ack.
// vanilla: EnderEyeItem.useOn, EndPortalFrameBlock.getOrCreatePortalShape
func (c *client) useEnderEye(hand, x, y, z int32) (bool, error) {
	held := c.heldItem(hand)
	if held.Empty() || held.ItemID != int32(registry.ItemEnderEye) {
		return false, nil
	}
	w := c.theWorld()
	state := w.BlockAt(x, y, z)
	if registry.BlockOf(state) != registry.BlockEndPortalFrame || registry.StateBool(state, registry.PropEye) {
		return false, nil
	}
	// The eye goes into the slot the frame kept: the facing does not turn.
	eyeState, ok := registry.WithBool(state, registry.PropEye, true)
	if !ok {
		return false, nil
	}
	// A flag-2 setBlock (see portalChange); a comparator behind the frame reads
	// 15 from now on.
	oldFrame := w.BlockAt(x, y, z)
	c.srv.portalChange(w, x, y, z, eyeState)
	// The frame's shape grows with the eye, lifting whoever stands on it.
	// vanilla: EnderEyeItem.useOn, Block.pushEntitiesUp
	c.srv.pushEntitiesUp(w, world.BlockChange{
		X: x, Y: y, Z: z, State: eyeState,
		PushUp: world.CollisionTop(eyeState) - world.CollisionTop(oldFrame),
	})
	c.awardUsedHeld(hand)
	if err := c.consumeHeldOutsideCreative(hand); err != nil {
		return true, err
	}
	// SUCCESS is predicted locally; only observers need the server's swing.
	// vanilla: ServerGamePacketListenerImpl.handleUseItemOn
	c.useSwing = swingPredicted
	// The fill's own sound, to everyone around the frame.
	c.srv.broadcastNear(w, float64(x), float64(y), float64(z), 64,
		protocol.IDPlayWorldEvent,
		worldEvent(protocol.WorldEventEndPortalFrameFill, x, y, z, 0))
	interior, complete := rules.EndPortalInterior(w, x, y, z)
	if !complete {
		return true, nil
	}
	for _, cell := range interior {
		// An occupied cell breaks normally (spilling, loot, break event) before the
		// portal takes its place. vanilla: EnderEyeItem.useOn
		c.srv.destroyBlockContents(w, cell[0], cell[1], cell[2],
			w.BlockAt(cell[0], cell[1], cell[2]))
		c.srv.portalChange(w, cell[0], cell[1], cell[2], rules.EndPortalState)
	}
	centre := interior[4]
	c.srv.tellGlobalLevelEvent(w, protocol.WorldEventEndPortalSpawn, centre[0], centre[1], centre[2], 0)
	return true, nil
}

// tellGlobalLevelEvent plays a world event to every player
// global_sound_events reaches, where rules.GlobalEventAt puts it in the
// same dimension and at the player in another. With the rule off it is a
// plain event within 64 blocks.
// vanilla: ServerLevel.globalLevelEvent
func (s *Server) tellGlobalLevelEvent(w *world.World, event, x, y, z, data int32) {
	if !s.world.BoolRule(world.RuleGlobalSoundEvents) {
		s.broadcastNear(w, float64(x), float64(y), float64(z), 64,
			protocol.IDPlayWorldEvent,
			worldEvent(event, x, y, z, data))
		return
	}
	s.mu.RLock()
	recipients := make([]*client, 0, len(s.clients))
	for _, c := range s.clients {
		recipients = append(recipients, c)
	}
	s.mu.RUnlock()
	for _, c := range recipients {
		pos := c.player.Position()
		px, py, pz := pos.X, pos.Y, pos.Z
		if c.theWorld() == w {
			px, py, pz = rules.GlobalEventAt(px, py, pz, float64(x)+0.5, float64(y)+0.5, float64(z)+0.5)
		}
		c.send(protocol.IDPlayWorldEvent,
			worldEventGlobal(event,
				rules.BlockOf(px), rules.BlockOf(py), rules.BlockOf(pz), data))
	}
}

// portalFrameLit reports whether a frame would light around the fire at (x, y, z).
func (s *Server) portalFrameLit(w *world.World, x, y, z int32) bool {
	_, ok := rules.LightPortal(w, w.DimensionName(), x, y, z)
	return ok
}

// ignitePortalAt fills a valid obsidian frame around the fire at (x, y, z),
// reporting whether there was one (rules.LightPortal). It runs for every
// fire the physics places too (vanilla: BaseFireBlock.onPlace), so spreading
// fire lights frames.
func (s *Server) ignitePortalAt(w *world.World, x, y, z int32) bool {
	lit, ok := rules.LightPortal(w, w.DimensionName(), x, y, z)
	if ok {
		if lit = s.portalWritesAllowed(w, lit, plugin.PortalFire); lit == nil {
			return false
		}
		s.writeBatch(w, lit)
		slog.Info("nether portal lit", "x", x, "y", y, "z", z)
	}
	return ok
}

// portalChange sets one block and tells players, light and physics,
// skipping portal revalidation.
func (s *Server) portalChange(w *world.World, x, y, z int32, state uint16) {
	s.writeBatch(w, []world.BlockChange{{X: x, Y: y, Z: z, State: state}})
}

// writeBatch writes changes as one batch, telling players (a packet per
// section), physics, and light (one recompute over the span), skipping
// revalidation: ignition and collapse write whole portals, which must not be
// checked half-written. The chunks must be loaded.
func (s *Server) writeBatch(w *world.World, changes []world.BlockChange) {
	applied := w.SetBlocks(changes)
	if len(applied) == 0 {
		return
	}
	sent := make([]world.BlockChange, len(applied))
	for i, old := range applied {
		sent[i] = world.BlockChange{X: old.X, Y: old.Y, Z: old.Z, State: w.BlockAt(old.X, old.Y, old.Z)}
	}
	s.tellSectionBlocks(w, sent)
	minX, minZ, maxX, maxZ := sent[0].X, sent[0].Z, sent[0].X, sent[0].Z
	for _, ch := range sent[1:] {
		minX, minZ = min(minX, ch.X), min(minZ, ch.Z)
		maxX, maxZ = max(maxX, ch.X), max(maxZ, ch.Z)
	}
	s.relightAreaAsync(w, minX, minZ, maxX, maxZ)
	physics := s.physicsFor(w)
	for _, old := range applied {
		changes, effects, primes := physics.NotifyChanged(old.X, old.Y, old.Z, old.State)
		for _, e := range effects {
			s.tellEffect(w, e)
		}
		for _, t := range primes {
			s.primeTNT(w, t)
		}
		s.tellBlockChanges(w, changes)
	}
}

// --- Breaking ---

// updatePortalNear collapses whole every portal around (x, y, z) that lost
// its frame (vanilla: NetherPortalBlock.updateShape). Single portal blocks,
// as /setblock places, are left.
func (s *Server) updatePortalNear(w *world.World, x, y, z int32) {
	for _, cell := range rules.CollapsingPortals(w, x, y, z) {
		if cleared := rules.CollapsedPortal(w, cell[0], cell[1], cell[2]); len(cleared) > 0 {
			s.writeBatch(w, cleared)
			slog.Info("nether portal collapsed", "x", cell[0], "y", cell[1], "z", cell[2])
		}
	}
}

// updatePortalsNear runs updatePortalNear for each change.
func (s *Server) updatePortalsNear(w *world.World, changes []world.BlockChange) {
	for _, ch := range changes {
		s.updatePortalNear(w, ch.X, ch.Y, ch.Z)
	}
}

// --- Travelling ---

// tickPortals advances every player's portal state, on the main loop.
func (s *Server) tickPortals() {
	s.mu.RLock()
	clients := make([]*client, 0, len(s.clients))
	for _, c := range s.clients {
		clients = append(clients, c)
	}
	s.mu.RUnlock()

	for _, c := range clients {
		c.tickPortal()
	}
}

// tickPortal counts a player's cooldown down and nether portal time up; an
// end portal takes them at once. A crossing in flight holds the cooldown:
// vanilla sets it and changes dimension in one tick, so all of it runs after
// the arrival.
func (c *client) tickPortal() {
	if cooldown := c.portalCooldown.Load(); cooldown > 0 && !c.traveling.Load() {
		c.portalCooldown.Store(cooldown - 1)
	}
	if c.player.Vitals().Dead {
		c.portalIn.Store(0)
		return
	}
	pos := c.player.Position()
	// One read of the dimension: a travel already in flight can switch it
	// mid-tick, and the portal fired is the one the feet were read in.
	dim := c.dimension()
	// Last tick's position for the gateway sweep; a new world, first tick,
	// teleport or respawn sweeps nothing.
	prev, prevWorld := c.portalPrev, c.portalPrevWorld
	c.portalPrev, c.portalPrevWorld = pos, dim.w
	if prevWorld != c.portalPrevWorld || c.portalPrevStale.Swap(false) {
		prev = pos
	}
	x, y, z := rules.BlockOf(pos.X), rules.BlockOf(pos.Y), rules.BlockOf(pos.Z)
	state := dim.w.BlockAt(x, y, z)
	if it := c.vehicle.Load(); it != nil {
		// This clock carries the boat's players, but portal contact
		// belongs to the hull; passengers cannot activate a portal themselves.
		// vanilla: Entity.canUsePortal, Entity.teleportCrossDimension
		c.srv.boatsMu.Lock()
		state = it.boat.PortalState(it.w)
		c.srv.boatsMu.Unlock()
	}

	switch {
	case rules.IsPortalState(state):
		// A mob carries its players on its own portal clock.
		// vanilla: Entity.canUsePortal
		if c.ridingMob() {
			c.decayPortalIn()
			return
		}
		// Spectators have no physics, so entityInside never runs for them.
		// vanilla: Player.tick noPhysics
		if c.player.GameMode() == protocol.GameModeSpectator {
			c.decayPortalIn()
			return
		}
		// Standing in a portal during cooldown re-arms it without counting
		// (vanilla: Entity.setAsInsidePortal), so an arrival must step out and back.
		if c.portalCooldown.Load() > 0 {
			c.portalCooldown.Store(playerPortalCooldownTicks)
			c.decayPortalIn()
			return
		}
		// The count is compared before this tick's add, so survival crosses on the
		// 81st tick inside and creative on the first (vanilla:
		// PortalProcessor.processPortalTeleportation, portalTime++ >= 80).
		needed := rules.PlayerPortalWait(c.player.GameMode(), c.ridingMinecart() || c.ridingBoat(), c.srv.world.IntRule)
		if c.portalIn.Add(1) <= needed {
			return
		}
		// The cooldown is taken before the travel; the CAS from zero keeps one
		// crossing from firing twice. A travel that finds nowhere to stand still
		// spends it rather than searching every tick. The standing count is not
		// reset: the same PortalProcessor crosses and decays in the new dimension
		// (vanilla: Entity.handlePortal keeps this.portalProcess over the teleport).
		if !c.portalCooldown.CompareAndSwap(0, playerPortalCooldownTicks) {
			return
		}
		go c.travelByNetherPortal(dim)

	case state == rules.EndPortalState:
		// No collision, so the client's own fall puts the player inside.
		// Spectators have no physics, so entityInside never runs for them.
		// vanilla: Player.tick noPhysics
		if c.player.GameMode() == protocol.GameModeSpectator {
			c.decayPortalIn()
			return
		}
		// Take the cooldown as for the nether: the travel runs on its own goroutine,
		// and until it switches dimension the next tick still reads the feet in the
		// overworld's portal, so without it one crossing becomes two.
		if !c.portalCooldown.CompareAndSwap(0, playerPortalCooldownTicks) {
			return
		}
		go c.travelByEndPortal(dim)

	default:
		if gx, gy, gz, ok := c.gatewayInBody(prev, pos); ok {
			c.touchEndGateway(gx, gy, gz)
			return
		}
		c.decayPortalIn()
	}
}

// decayPortalIn is rules.PortalDecay on the player's count.
func (c *client) decayPortalIn() {
	if in := c.portalIn.Load(); in > 0 {
		c.portalIn.Store(int32(rules.PortalDecay(int(in))))
	}
}

// travelByNetherPortal sends the player to the destination's nearest
// portal, building one if none is in range.
func (c *client) travelByNetherPortal(from *dimension) {
	defer c.srv.portalTripsDecided.Add(1)
	// A travel running after the body already left the portal's dimension (an
	// earlier crossing landed first) is over.
	if c.dimension() != from {
		return
	}
	spectator := c.player.GameMode() == protocol.GameModeSpectator
	pos := c.player.Position()
	dims := c.dims(c.player.Appearance().Pose)
	width, height := dims.Width, dims.Height
	if it := c.vehicle.Load(); it != nil {
		// The hull crosses; the rider's seat can be below the portal's floor
		// or displaced along it when another passenger is aboard.
		c.srv.boatsMu.Lock()
		if it.w == from.w {
			pos.X, pos.Y, pos.Z = it.boat.X, it.boat.Y, it.boat.Z
			width, height = boatTracker.portalDimensions(it)
		}
		c.srv.boatsMu.Unlock()
	}
	to, spot, exitAxis, ok := c.srv.entityPortalArrival(c, rules.PortalNether, from, pos, spectator)
	if !ok {
		return
	}
	// Hold the exit before reading its opening and collisions: the portal
	// search releases its chunks, and the client has not loaded them yet.
	c.srv.holdArrival(to, spot)
	at := netherArrivalPosition(from.w, to.w, pos, spot, width, height, true)
	turn := portalTurnAt(from.w, rules.PortalNether, pos, exitAxis)
	at.Yaw = pos.Yaw + turn
	at.Pitch = pos.Pitch
	if err := c.enterDimension(to, command.Move{Pos: at, Offset: player.Position{Yaw: turn}, RelYaw: true}, respawnNone, true); err != nil {
		c.log.Warn("nether portal travel failed", "error", err)
		c.conn.Close()
		return
	}
	c.playPortalTravel()
}

// playPortalTravel plays the portal-travel sound to this player alone, after
// the arrival's respawn and position. vanilla: TeleportTransition.playPortalSound
func (c *client) playPortalTravel() {
	c.send(protocol.IDPlayWorldEvent, worldEvent(protocol.WorldEventPortalTravel, 0, 0, 0, 0))
}

// travelByEndPortal sends the player from the overworld or the nether to the
// end's arrival platform (built first), or from the end to their spawn point
// (the world spawn for a boat or cart they ride), the first exit showing the
// credits instead. There is no standing count; the
// firing tick takes the cooldown so the next tick cannot fire again while
// the first travels.
func (c *client) travelByEndPortal(from *dimension) {
	// One travel at a time: a crossing that outlasts the cooldown has the
	// next tick fire another from the same portal, which must not see the
	// body arrive between its checks.
	if !c.endTravel.CompareAndSwap(false, true) {
		return
	}
	defer c.endTravel.Store(false)
	// A travel running after the body left the portal's dimension (an earlier
	// crossing, or the credits' respawn) is over; continuing would read the new
	// dimension as the overworld side and send the body back to the end.
	if c.dimension() != from {
		return
	}
	if c.player.Vitals().Dead {
		return
	}
	// A mob's or cushion's rider does not use an end portal: vanilla's
	// entityInside starts with canUsePortal(false), which a passenger fails,
	// so no credits or crossing. A boat or cart rider's crossing carries the
	// vehicle, which vanilla's own portal pass moves with its passengers.
	// vanilla: EndPortalBlock.entityInside
	if c.ridingMob() || c.ridingCushion() {
		return
	}
	if from.name == world.EndName {
		// A vehicle leaves the end with no passenger who has yet to see
		// the credits. vanilla: Entity.canTeleport
		if (c.ridingBoat() || c.ridingMinecart()) && !c.player.SeenCredits() {
			return
		}
		// The first exit shows the credits and keeps the player in the end; while
		// they run the portal also holds (tickPortal fires this every tick the
		// player stands there) until the Client Command their end sends. A player
		// past the credits crosses home at once.
		// vanilla: EndPortalBlock.entityInside
		if !c.player.SeenCredits() || c.wonGame.Load() {
			c.showEndCredits()
			return
		}
		// The return runs the same respawn search a death there would, telling the
		// client of a missing bed or anchor with the game event, and spends nothing
		// of the block it lands at. A boat or cart crosses instead of its rider, so
		// it goes, rider aboard, to the world spawn any other body would.
		// vanilla: EndPortalBlock.getPortalDestination,
		// ServerPlayer.teleport
		var dest *dimension
		var at player.Position
		vehicle := c.ridingBoat() || c.ridingMinecart()
		if vehicle {
			var spot world.SpawnPos
			dest, spot = c.srv.endPortalDestination(c.dimension())
			// The arrival is held before the hull moves there: a hull in an
			// unloaded chunk is saved out of the world, unseating its rider.
			// vanilla: TeleportTransition.PLACE_PORTAL_TICKET
			c.srv.holdArrival(dest, spot)
			at = standingAt(spot)
		} else {
			var place respawnPlacement
			var missing bool
			place, dest, missing = c.respawnPlacement(c.spawnPoint.Load(), false)
			if missing {
				c.sendNoRespawnBlock()
			}
			at = place.position()
		}
		block := world.SpawnPos{X: rules.BlockOf(at.X), Y: rules.BlockOf(at.Y), Z: rules.BlockOf(at.Z)}
		moved, spot, allowed := c.srv.pluginPortal(c, c.player.Position(), dest, block, plugin.TeleportEndPortal)
		if !allowed {
			return
		}
		if moved != dest || spot != block {
			dest, at = moved, standingAt(spot)
		}
		c.arriveAt(dest, at)
		// A player on foot takes the respawn search's transition, which
		// plays no sound.
		if vehicle {
			c.playPortalTravel()
		}
		return
	}
	c.enterEnd()
}

// arriveAt moves the player to at in dest; a forced point in the same
// dimension is a teleport, as enterDimension refuses the current one.
func (c *client) arriveAt(dest *dimension, at player.Position) {
	if dest == c.dimension() {
		c.teleport(command.Move{Pos: at})
		return
	}
	if err := c.enterDimension(dest, command.Move{Pos: at}, respawnNone, true); err != nil {
		c.log.Warn("end portal travel failed", "error", err)
		c.conn.Close()
	}
}

// enterEnd sends the player to the end's arrival platform, building it first.
func (c *client) enterEnd() {
	// Hold the arrival chunk as the nether crossing does: nothing else holds
	// the platform after landing, and a released chunk reloads with only the
	// platform.
	end := c.srv.dimensionByName(world.EndName)
	spot := world.EndArrival()
	end, spot, ok := c.srv.pluginPortal(c, c.player.Position(), end, spot, plugin.TeleportEndPortal)
	if !ok {
		return
	}
	c.srv.holdArrival(end, spot)
	c.srv.buildEndArrivalPlatform()
	// Faces west (Direction.WEST.toYRot() = 90), toward the main island, and
	// keeps the pitch: it is sent as a zero offset, Pos holds it for the store.
	// vanilla: EndPortalBlock.getPortalDestination
	arrive := standingAt(spot)
	arrive.Yaw = 90
	arrive.Pitch = c.player.Position().Pitch
	if err := c.enterDimension(end, command.Move{Pos: arrive, RelPitch: true}, respawnNone, true); err != nil {
		c.log.Warn("end portal travel failed", "error", err)
		c.conn.Close()
		return
	}
	c.playPortalTravel()
}

// spectatorUsePortal is a spectator's use of a portal block: they travel
// as the block's destination says, or are told they cannot; reports whether
// the block was a portal. No credits, no cooldown.
// vanilla: ServerPlayerGameMode.useItemOn
func (c *client) spectatorUsePortal(x, y, z int32, state uint16) (bool, error) {
	cannot := func() {
		c.send(protocol.IDPlaySystemChat, systemChatPayload(protocol.Text{Translate: "spectator.cannot_teleport", Color: "red"}, true))
	}
	switch {
	case rules.IsPortalState(state):
		from := c.dimension()
		to, spot, _, ok := c.srv.entityPortalArrival(nil, rules.PortalNether, from, c.player.Position(), true)
		if !ok {
			cannot()
			return true, nil
		}
		// vanilla: Entity.teleportToPortalDestination, silent when refused.
		if !c.srv.portalEntryAllowed(to) {
			return true, nil
		}
		c.clearCamera()
		c.srv.holdArrival(to, spot)
		if err := c.enterDimension(to, command.Move{Pos: standingAt(spot)}, respawnNone, true); err != nil {
			c.log.Warn("nether portal travel failed", "error", err)
			c.conn.Close()
			return true, nil
		}
		c.playPortalTravel()
	case state == rules.EndPortalState:
		c.clearCamera()
		if c.dimension().name != world.EndName {
			c.enterEnd()
			return true, nil
		}
		place, dest, missing := c.respawnPlacement(c.spawnPoint.Load(), false)
		if missing {
			c.sendNoRespawnBlock()
		}
		c.arriveAt(dest, place.position())
	case registry.BlockOf(state) == registry.BlockEndGateway:
		px, py, pz, ok := c.srv.gatewayDestination(c.theWorld(), x, y, z)
		if !ok {
			cannot()
			return true, nil
		}
		c.clearCamera()
		c.teleportToGatewayExit(px, py, pz)
	default:
		return false, nil
	}
	return true, nil
}

// showEndCredits shows the end credits instead of crossing: the player is
// unseated and hidden from the end, and the client rolls the credits,
// ending with the Client Command's PERFORM_RESPAWN. The flag shows them
// once: tickPortal fires the travel every tick in the portal, and repeats
// must not resend or cross. Deliberate difference: the body keeps ticking
// where vanilla removes it; the respawn ending the credits keeps everything
// it holds then. vanilla: ServerPlayer.showEndCredits
func (c *client) showEndCredits() {
	if !c.wonGame.CompareAndSwap(false, true) {
		return
	}
	c.dismountBoat(false)
	c.dismountCushion(false)
	c.srv.broadcastWorldExcept(c.theWorld(), c, protocol.IDPlayRemoveEntities, removeEntity(c.player))
	c.srv.forgetBodies(c)
	c.player.SetSeenCredits(true)
	// The 0 value is the one the reference server sends with the event.
	c.send(protocol.IDPlayGameEvent, gameEventPayload(protocol.GameEventWinGame, 0))
}

// buildEndArrivalPlatform rebuilds the end's 5x5 obsidian platform and
// clears the air above on every arrival, in case it was dug out. A cell
// already holding its block is left alone; any other is broken with its
// drops and contents first. vanilla: EndPlatformFeature.createEndPlatform
func (s *Server) buildEndArrivalPlatform() {
	end := s.dimensionByName(world.EndName)
	w := end.w
	platform := world.EndArrivalPlatform()
	defer s.holdChanges(w, platform)()
	changes := platform[:0]
	for _, ch := range platform {
		old := w.BlockAt(ch.X, ch.Y, ch.Z)
		if registry.BlockOf(old) == registry.BlockOf(ch.State) || (world.IsAirState(old) && world.IsAirState(ch.State)) {
			continue
		}
		changes = append(changes, ch)
	}
	changes = s.portalWritesAllowed(w, changes, plugin.PortalEndPlatform)
	for _, ch := range changes {
		s.destroyBlockContents(w, ch.X, ch.Y, ch.Z, w.BlockAt(ch.X, ch.Y, ch.Z))
	}
	// The platform lies inside the build band, so no band filter.
	s.writeBatch(w, changes)
}

// holdChanges holds loaded the chunks of the changes inside the build band
// and returns the release.
func (s *Server) holdChanges(w *world.World, changes []world.BlockChange) (release func()) {
	chunks := make(map[world.ChunkPos]struct{})
	minY, maxY := w.MinY(), w.MinY()+w.Height()-1
	for _, ch := range changes {
		if ch.Y < minY || ch.Y > maxY {
			continue
		}
		chunks[world.ChunkPos{X: world.ChunkOf(float64(ch.X)), Z: world.ChunkOf(float64(ch.Z))}] = struct{}{}
	}
	for pos := range chunks {
		w.Acquire(pos)
	}
	return func() {
		for pos := range chunks {
			w.Release(pos)
		}
	}
}

// applyChanges writes block changes, holding their chunks loaded so the
// writes land, and tells players, light and physics. Portal revalidation is
// the caller's, so a portal under construction is not checked halfway.
func (s *Server) applyChanges(dim *dimension, changes []world.BlockChange) {
	if len(changes) == 0 {
		return
	}
	w := dim.w
	defer s.holdChanges(w, changes)()
	minY, maxY := w.MinY(), w.MinY()+w.Height()-1
	inBand := make([]world.BlockChange, 0, len(changes))
	for _, ch := range changes {
		if ch.Y >= minY && ch.Y <= maxY {
			inBand = append(inBand, ch)
		}
	}
	s.writeBatch(w, inBand)
}

// buildPortalAt builds the missing exit portal near at along axis and
// returns its bottom cell at the low end of the axis; false when the
// dimension has no room in its logical height.
func (s *Server) buildPortalAt(dim *dimension, at world.SpawnPos, radius int32, axis [2]int32) (world.SpawnPos, bool) {
	w := dim.w
	// The site's reads load and generate its chunks, as vanilla's do.
	reach := rules.PortalSearch{At: at, Radius: radius + rules.PortalBuildMargin}
	chunks := slices.Collect(reach.Chunks())
	for range w.AcquireMany(chunks) {
	}
	defer func() {
		for _, pos := range chunks {
			w.Release(pos)
		}
	}()
	minY := w.MinY()
	maxPlaceableY := min(minY+w.Height(), minY+w.LogicalHeight()) - 1
	corner, changes, ok := rules.BuildNetherPortal(w, dim.border, at, radius, axis, minY, maxPlaceableY)
	if !ok {
		return world.SpawnPos{}, false
	}
	if changes = s.portalWritesAllowed(w, changes, plugin.PortalNetherPair); changes == nil {
		return world.SpawnPos{}, false
	}
	s.applyChanges(dim, changes)
	slog.Info("nether portal built",
		"dimension", dim.name, "x", corner.X, "y", corner.Y, "z", corner.Z)
	return corner, true
}

// findPortalNear finds the portal block nearest at within the square of
// radius around it, inside dim's border, in existing chunks only (never
// generating), returning its column's bottom cell.
func (s *Server) findPortalNear(dim *dimension, at world.SpawnPos, radius int32) (world.SpawnPos, bool) {
	w := dim.w
	search := rules.PortalSearch{At: at, Radius: radius, InBorder: dim.border.ContainsBlock}
	minY, maxY := w.MinY(), w.MinY()+w.Height()-1
	for pos := range search.Chunks() {
		if !w.HasChunk(pos) {
			continue
		}
		// Held so it cannot unload mid-scan.
		if chunk, _ := w.Acquire(pos); chunk != nil {
			search.ScanChunk(w, pos, minY, maxY)
			w.Release(pos)
		}
	}
	return search.Found()
}

// --- Travelling, the non-player kinds ---

// Non-player bodies cross at once (vanilla: Entity.handlePortal) with a
// 300-tick cooldown, arriving as a new entity (new ID, same UUID) built from
// the old one's tag, which is removed.

// portalEntryAllowed reports whether a portal trip may enter to: the rule
// refuses only the nether. The destination portal is already found or
// built when asked.
// vanilla: ServerLevel.isAllowedToEnterPortal
func (s *Server) portalEntryAllowed(to *dimension) bool {
	return to.name != world.NetherName || s.world.BoolRule(world.RuleAllowEnteringNether)
}

// endPortalDestination is the world spawn column, in its dimension, at its
// motion-blocking surface when leaving the end, or the arrival platform
// (built first) when entering it.
// vanilla: EndPortalBlock.getPortalDestination, Entity.adjustSpawnLocation
func (s *Server) endPortalDestination(from *dimension) (to *dimension, spot world.SpawnPos) {
	if from.name == world.EndName {
		to, spot = s.worldSpawn()
		pos := world.ChunkPos{X: world.ChunkOf(float64(spot.X)), Z: world.ChunkOf(float64(spot.Z))}
		to.w.Acquire(pos)
		// The height map value is the first free y, one above the block.
		spot.Y = to.w.NoLeavesHeight(spot.X, spot.Z) + 2
		to.w.Release(pos)
		return to, spot
	}
	s.buildEndArrivalPlatform()
	return s.dimensionByName(world.EndName), world.EndArrival()
}

// entityPortalArrival returns where a crossing of kind arrives and, for a
// nether portal, the arrival axis; false when a nether crossing found no
// portal and builds none, a spectator never building one (vanilla's nil
// getExitPortal), so the body stays.
//
// who is the crossing body, told to plugins before the exit is searched for
// (nil for none): a listener may retarget the search and its radii.
func (s *Server) entityPortalArrival(who command.Entity, kind rules.PortalKind, from *dimension, pos player.Position, spectator bool) (to *dimension, spot world.SpawnPos, axis [2]int32, ok bool) {
	switch kind {
	case rules.PortalNether:
		toNether, at, radius := rules.NetherPortalExit(from.w.DimensionTypeName() == world.NetherType, pos.X, pos.Y, pos.Z)
		to = s.dimensionByName(world.DimensionName)
		if toNether {
			to = s.dimensionByName(world.NetherName)
		}
		at.X, at.Z = to.border.ClampBlock(at.X, at.Z)
		createRadius, canCreate := int32(rules.PortalBuildRadius), !spectator
		if who != nil {
			var allowed, pluginCreate bool
			if to, at, radius, createRadius, pluginCreate, allowed = s.pluginNetherPortal(who, pos, to, at, radius, createRadius); !allowed {
				return nil, world.SpawnPos{}, [2]int32{}, false
			}
			canCreate = canCreate && pluginCreate
			at.X, at.Z = to.border.ClampBlock(at.X, at.Z)
		}
		if spot, ok = s.findPortalNear(to, at, radius); !ok {
			if !canCreate {
				return nil, world.SpawnPos{}, [2]int32{}, false
			}
			sourceAxis := rules.NetherPortalAxis(from.w.BlockAt(rules.BlockOf(pos.X), rules.BlockOf(pos.Y), rules.BlockOf(pos.Z)))
			if spot, ok = s.buildPortalAt(to, at, createRadius, sourceAxis); !ok {
				slog.Warn("no room for a nether portal at the destination",
					"x", at.X, "z", at.Z, "dimension", to.name)
				return nil, world.SpawnPos{}, [2]int32{}, false
			}
		}
		// A spectator's caller applies the rule itself: with no portal found it
		// still says it cannot, so only a found portal reaches the rule.
		if !spectator && !s.portalEntryAllowed(to) {
			return nil, world.SpawnPos{}, [2]int32{}, false
		}
		// The search releases its chunks; reload the exit before reading its axis.
		chunk := world.ChunkPos{X: world.ChunkOf(float64(spot.X)), Z: world.ChunkOf(float64(spot.Z))}
		to.w.Acquire(chunk)
		axis = rules.NetherPortalAxis(to.w.BlockAt(spot.X, spot.Y, spot.Z))
		to.w.Release(chunk)
		return to, spot, axis, true
	case rules.PortalEnd:
		to, spot = s.endPortalDestination(from)
		return to, spot, [2]int32{}, true
	}
	return nil, world.SpawnPos{}, [2]int32{}, false
}

// crossPortal carries one body the portal pass caught, the one with id at
// pos in w, to where its portal of kind leads. arrive builds its twin there
// and returns what registers it, or nil for a body it cannot carry; the
// arrival's chunk is then held and woken so the twin saves with it, and the
// old body goes from view. With nowhere to arrive or no twin, putBack
// returns the body to its list.
func (s *Server) crossPortal(w *world.World, id int32, pos player.Position, kind rules.PortalKind,
	arrive func(to *dimension, spot world.SpawnPos, exitAxis [2]int32) (register func()), putBack func()) {
	to, spot, exitAxis, ok := s.entityPortalArrival(s.relatedByID(w, id), kind, s.dimensionOfWorld(w), pos, false)
	if ok && kind == rules.PortalEnd {
		moved, at, allowed := s.pluginPortal(s.relatedByID(w, id), pos, to, spot, plugin.TeleportEndPortal)
		if ok = allowed; allowed && (moved != to || at != spot) {
			to, spot = moved, at
			exitAxis = rules.NetherPortalAxis(to.w.BlockAt(spot.X, spot.Y, spot.Z))
		}
	}
	var register func()
	if ok {
		register = arrive(to, spot, exitAxis)
	}
	if register == nil {
		putBack()
		return
	}
	s.holdArrival(to, spot)
	s.wakeArrivalChunk(to, spot)
	register()
	s.teleportSpectatorsOf(w, id, to, standingAt(spot))
	s.broadcastWorld(w, protocol.IDPlayRemoveEntities, removeEntitiesPayload([]int32{id}))
}

// crossPortal carries b, which the portal pass took out of the list, across
// its portal of kind (Server.crossPortal) as the twin twinAt raises from its
// tag at the arrival, facing at.Yaw: the body's yaw turned by the angle
// between the portals. register puts the twin into the world, and a body
// that stays goes back to the list.
// vanilla: Entity.teleportCrossDimension, NetherPortalBlock.getExitPortal
func (k *trackedKind[B, S]) crossPortal(s *Server, b B, kind rules.PortalKind, register func(B),
	twinAt func(to *world.World, at player.Position) (B, bool)) {
	from := k.pose(b)
	pos := player.Position{X: from.x, Y: from.y, Z: from.z, Yaw: from.yaw}
	s.crossPortal(k.world(b), k.id(b), pos, kind,
		func(to *dimension, spot world.SpawnPos, exitAxis [2]int32) func() {
			at := standingAt(spot)
			if kind == rules.PortalNether && k.portalDimensions != nil {
				width, height := k.portalDimensions(b)
				at = netherArrivalPosition(k.world(b), to.w, pos, spot, width, height, k.portalCentered)
			}
			// Only the yaw turns, as an arrow's does.
			at.Yaw = from.yaw + portalTurnAt(k.world(b), kind, pos, exitAxis)
			twin, ok := twinAt(to.w, at)
			if !ok {
				slog.Error("could not carry a body across a portal", "id", k.id(b))
				return nil
			}
			return func() {
				register(twin)
				at := k.pose(twin)
				slog.Info("body travelled through a portal", "to", to.name, "x", at.x, "y", at.y, "z", at.z)
			}
		}, func() {
			mu := k.lock(s)
			mu.Lock()
			*k.list(s) = append(*k.list(s), b)
			mu.Unlock()
		})
}

// netherArrivalPosition maps a body's entry offset and clears exit block collisions.
func netherArrivalPosition(from, to *world.World, pos player.Position, exit world.SpawnPos, width, height float64, centered bool) player.Position {
	entry := world.SpawnPos{X: rules.BlockOf(pos.X), Y: rules.BlockOf(pos.Y), Z: rules.BlockOf(pos.Z)}
	at := entity.PortalArrivalPosition(from, to, entry, exit, [3]float64{pos.X, pos.Y, pos.Z}, width, height, centered)
	return player.Position{X: at[0], Y: at[1], Z: at[2]}
}

// portalTurnAt reads the source axis before the departing body's world changes.
func portalTurnAt(w *world.World, kind rules.PortalKind, pos player.Position, exitAxis [2]int32) float32 {
	return rules.PortalTurn(kind, rules.NetherPortalAxis(w.BlockAt(rules.BlockOf(pos.X), rules.BlockOf(pos.Y), rules.BlockOf(pos.Z))), exitAxis, pos.Yaw)
}

// portalHold keeps the chunks around a crossing's arrival loaded for 300
// ticks in place of vanilla's portal ticket, so a body that arrives ticks and
// saves with them. s.portalHoldsMu guards the list: the tick spends it down,
// and a player's crossing goroutine appends to it.
type portalHold struct {
	w     *world.World
	pos   []world.ChunkPos
	holds int
}

// portalTicketRing is how far around the arrival chunk a crossing holds.
// Vanilla's portal ticket has radius 3, level 33-3 = 30, which is entity
// ticking (level 31 or lower) only within one chunk of the arrival; a held
// chunk here always ticks its entities, so only that ring is held.
// vanilla: Entity.placePortalTicket, ChunkLevel.ENTITY_TICKING_LEVEL
const portalTicketRing = 1

// holdArrival holds the chunks within portalTicketRing of the arrival for
// portalCooldownTicks, generating the missing ones in parallel.
func (s *Server) holdArrival(dim *dimension, spot world.SpawnPos) {
	cx, cz := world.ChunkOf(float64(spot.X)), world.ChunkOf(float64(spot.Z))
	ring := make([]world.ChunkPos, 0, (2*portalTicketRing+1)*(2*portalTicketRing+1))
	for dx := int32(-portalTicketRing); dx <= portalTicketRing; dx++ {
		for dz := int32(-portalTicketRing); dz <= portalTicketRing; dz++ {
			ring = append(ring, world.ChunkPos{X: cx + dx, Z: cz + dz})
		}
	}
	for range dim.w.AcquireMany(ring) {
	}
	s.portalHoldsMu.Lock()
	s.portalHolds = append(s.portalHolds, portalHold{w: dim.w, pos: ring, holds: portalCooldownTicks})
	s.portalHoldsMu.Unlock()
}

// tickPortalHolds counts the holds down and releases expired ones.
func (s *Server) tickPortalHolds() {
	s.portalHoldsMu.Lock()
	defer s.portalHoldsMu.Unlock()
	kept := s.portalHolds[:0]
	for _, h := range s.portalHolds {
		h.holds--
		if h.holds > 0 {
			kept = append(kept, h)
			continue
		}
		for _, pos := range h.pos {
			h.w.Release(pos)
		}
	}
	s.portalHolds = kept
}

// portalPiglinDie is the die a nether portal's random tick rolls against the
// difficulty ID. vanilla: NetherPortalBlock.randomTick
const portalPiglinDie = 2000

// portalSpawnsPiglin is a nether portal block's random tick: in the
// overworld, with monsters spawning and a player in range, a roll of 2000
// below the difficulty ID spawns a zombified piglin on the first solid block
// under the portal column, with its portal cooldown set.
// vanilla: NetherPortalBlock.randomTick
func (s *Server) portalSpawnsPiglin(dim *dimension, t world.PortalTick) {
	w := dim.w
	difficulty := s.world.Difficulty()
	if !s.spawningMonsters() || difficulty == 0 || w.DimensionName() != world.DimensionName {
		return
	}
	if s.levelRng.Intn(portalPiglinDie) >= int(difficulty) {
		return
	}
	s.mu.RLock()
	players := spawnPlayersOf(s.clientsLocked(), w)
	s.mu.RUnlock()
	if !chunkIsSpawning(players, world.ChunkPos{X: world.ChunkOf(float64(t.X)), Z: world.ChunkOf(float64(t.Z))}) {
		return
	}
	y := t.Y
	for registry.BlockOf(w.BlockAt(t.X, y, t.Z)) == registry.BlockNetherPortal {
		y--
	}
	if !entity.SpawnableBelow(w.BlockAt(t.X, y, t.Z), entity.TypeZombifiedPiglin) {
		return
	}
	ent := s.standMobArriving(dim, entity.TypeZombifiedPiglin, float64(t.X)+0.5, float64(y+1), float64(t.Z)+0.5,
		0, nil, false, "", true, nil)
	s.mobsMu.Lock()
	ent.portal.Cooldown = portalCooldownTicks
	if vehicle := ent.mob.Vehicle; vehicle != nil {
		if vehEnt := dim.mobs.book[vehicle]; vehEnt != nil {
			vehEnt.portal.Cooldown = portalCooldownTicks
		}
	}
	s.mobsMu.Unlock()
}
