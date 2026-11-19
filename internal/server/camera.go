package server

import (
	"math"
	"slices"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/command"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
)

// cameraView is a live body a spectator's view follows: its position and
// whether it is still in the world, read each tick so the spectator moves
// with it.
type cameraView interface {
	cameraPosition() player.Position
	cameraAlive() bool
}

// cameraFollow is the body a spectator's client renders from, on the
// player's connection. vanilla: ServerPlayer.camera
type cameraFollow struct {
	view cameraView
}

// SetCamera points the player's view at the entity, storing it, snapping
// the player to it and telling the client. The player itself, or an entity
// gone from the world, clears the camera. vanilla: ServerPlayer.setCamera
func (c *client) SetCamera(target command.Entity) {
	if target == nil || target == command.Entity(c) {
		c.clearCamera()
		return
	}
	view, ok := target.(cameraView)
	if !ok {
		c.clearCamera()
		return
	}
	// The crossing clears any old camera, so it comes before the store.
	// vanilla: ServerPlayer.setCamera teleports into the camera's level
	if d := c.srv.findDimension(target.DimensionName()); d != nil && d != c.dimension() {
		if err := c.enterDimension(d, command.Move{Pos: target.Position()}, respawnNone, false); err != nil {
			c.log.Debug("carrying a spectator into its camera's dimension failed", "error", err)
			return
		}
	}
	c.camera.Store(&cameraFollow{view: view})
	c.snapToCamera()
	c.sendSetCamera(target.EntityID())
	c.resetPosition()
}

// clearCamera returns the view to the player's eyes, telling the client if
// it was following a body.
func (c *client) clearCamera() {
	if c.camera.Swap(nil) != nil {
		c.sendSetCamera(c.player.EntityID)
		c.resetPosition()
	}
}

// resetPosition syncs the client's position with the server's: a camera
// snaps the body without telling the client, whose own moves would
// otherwise drag the view off the camera.
func (c *client) resetPosition() {
	c.teleport(command.Move{Pos: c.player.Position()})
}

// watchersOf returns clients whose camera is on body, read under the player
// list lock. A camera may drop after the lock, so callers recheck.
func (s *Server) watchersOf(body *client) []*client {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var watchers []*client
	for _, other := range s.clients {
		if f := other.camera.Load(); f != nil && f.view == cameraView(body) {
			watchers = append(watchers, other)
		}
	}
	return watchers
}

// teleportSpectatorsWith moves every spectator watching body into the
// dimension it crossed to, at its arrival; the watcher's crossing clears
// its camera, returning the view to its eyes.
// vanilla: Entity.teleportSpectators
func (s *Server) teleportSpectatorsWith(body *client, dim *dimension, move command.Move) {
	for _, other := range s.watchersOf(body) {
		// The camera may have been dropped since the lock was given back.
		if f := other.camera.Load(); f == nil || f.view != cameraView(body) {
			continue
		}
		if err := other.enterDimension(dim, move, respawnNone, false); err != nil {
			other.log.Debug("carrying a spectating player across a dimension change failed", "error", err)
		}
	}
}

// teleportSpectatorsOf moves every spectator whose camera is the non-player
// body oldID in from into dimension to, at where the body arrived.
// vanilla: Entity.teleportSpectators
func (s *Server) teleportSpectatorsOf(from *world.World, oldID int32, to *dimension, at player.Position) {
	dimName := s.dimensionOfWorld(from).name
	watching := func(f *cameraFollow) bool {
		snap, ok := f.view.(*entitySnapshot)
		return ok && snap.id == oldID && snap.dim == dimName
	}
	var watchers []*client
	s.mu.RLock()
	for _, other := range s.clients {
		if f := other.camera.Load(); f != nil && watching(f) {
			watchers = append(watchers, other)
		}
	}
	s.mu.RUnlock()
	for _, other := range watchers {
		// The camera may have been dropped since the lock was given back.
		if f := other.camera.Load(); f == nil || !watching(f) {
			continue
		}
		if err := other.enterDimension(to, command.Move{Pos: at}, respawnNone, false); err != nil {
			other.log.Debug("carrying a spectator along with its camera failed", "error", err)
		}
	}
}

// clearWatchersOf returns every camera on body to its own eyes: a respawn
// replaces the followed body, so each view ends rather than following the
// new body. vanilla: PlayerList.respawn, ServerPlayer.tick
func (s *Server) clearWatchersOf(body *client) {
	for _, other := range s.watchersOf(body) {
		// The camera may have been dropped since the lock was given back.
		if f := other.camera.Load(); f == nil || f.view != cameraView(body) {
			continue
		}
		other.clearCamera()
	}
}

// sendSetCamera sends Set Camera for entityID; the player's own ID returns
// the camera to them.
func (c *client) sendSetCamera(entityID int32) {
	var b protocol.Buffer
	b.PutVarInt(entityID)
	c.send(protocol.IDPlaySetCamera, b.Bytes())
}

// snapToCamera moves the player's body to the camera's position, so chunk
// tracking and others' view follow the spectator.
// vanilla: ServerPlayer.absSnapTo
func (c *client) snapToCamera() {
	f := c.camera.Load()
	if f == nil {
		return
	}
	c.move(f.view.cameraPosition(), false)
	c.sendChunksAsync()
}

// tickCamera moves a spectator with the followed body and drops a camera
// whose body died; sneaking or the body dying returns the view to the
// player's eyes. vanilla: ServerPlayer.tick
func (c *client) tickCamera() {
	f := c.camera.Load()
	if f == nil {
		return
	}
	if c.player.Appearance().Sneaking || !f.view.cameraAlive() {
		c.clearCamera()
		return
	}
	c.snapToCamera()
}

// cameraPosition is where the live entity the snapshot stands for now stands.
func (e *entitySnapshot) cameraPosition() player.Position {
	s := e.srv
	switch ref := e.ref.(type) {
	case *mobEntity:
		s.mobsMu.Lock()
		defer s.mobsMu.Unlock()
		m := ref.mob
		return player.Position{X: m.X, Y: m.Y, Z: m.Z, Yaw: m.Yaw, Pitch: m.Pitch, OnGround: m.OnGround}
	case *itemEntity:
		s.itemsMu.Lock()
		defer s.itemsMu.Unlock()
		x, y, z := ref.item.FeetPosition()
		return player.Position{X: x, Y: y, Z: z}
	case *orbEntity:
		s.orbsMu.Lock()
		defer s.orbsMu.Unlock()
		return player.Position{X: ref.orb.X, Y: ref.orb.Y, Z: ref.orb.Z}
	case *arrowEntity:
		s.arrowsMu.Lock()
		defer s.arrowsMu.Unlock()
		return player.Position{X: ref.arrow.X, Y: ref.arrow.Y, Z: ref.arrow.Z, Yaw: ref.arrow.Yaw, Pitch: ref.arrow.Pitch}
	case *thrownEntity:
		s.arrowsMu.Lock()
		defer s.arrowsMu.Unlock()
		return player.Position{X: ref.throwable.X, Y: ref.throwable.Y, Z: ref.throwable.Z, Yaw: ref.throwable.Yaw, Pitch: ref.throwable.Pitch}
	case *tntEntity:
		s.tntMu.Lock()
		defer s.tntMu.Unlock()
		return player.Position{X: ref.tnt.X, Y: ref.tnt.Y, Z: ref.tnt.Z}
	case *boatEntity:
		s.boatsMu.Lock()
		defer s.boatsMu.Unlock()
		return player.Position{X: ref.boat.X, Y: ref.boat.Y, Z: ref.boat.Z, Yaw: ref.boat.Yaw}
	case *fallingEntity:
		s.fallingMu.Lock()
		defer s.fallingMu.Unlock()
		return player.Position{X: ref.fb.X, Y: ref.fb.Y, Z: ref.fb.Z}
	case *armorStandEntity:
		s.armorStandsMu.Lock()
		defer s.armorStandsMu.Unlock()
		return player.Position{X: ref.stand.X, Y: ref.stand.Y, Z: ref.stand.Z, Yaw: ref.stand.Yaw}
	case *minecartEntity:
		s.minecartsMu.Lock()
		defer s.minecartsMu.Unlock()
		return player.Position{X: ref.cart.X, Y: ref.cart.Y, Z: ref.cart.Z, Yaw: ref.cart.Yaw}
	case *mannequinEntity:
		s.mannequinsMu.Lock()
		defer s.mannequinsMu.Unlock()
		m := ref.mannequin
		return player.Position{X: m.X, Y: m.Y, Z: m.Z, Yaw: m.Yaw, Pitch: m.Pitch}
	case *endCrystalEntity:
		s.endCrystalsMu.Lock()
		defer s.endCrystalsMu.Unlock()
		return player.Position{X: ref.crystal.X, Y: ref.crystal.Y, Z: ref.crystal.Z}
	case *cloudEntity:
		s.arrowsMu.Lock()
		defer s.arrowsMu.Unlock()
		return player.Position{X: ref.cloud.X, Y: ref.cloud.Y, Z: ref.cloud.Z}
	case *fireworkEntity:
		s.fireworksMu.Lock()
		defer s.fireworksMu.Unlock()
		f := ref.rocket
		return player.Position{X: f.X, Y: f.Y, Z: f.Z, Yaw: f.Yaw, Pitch: f.Pitch}
	case *bobberEntity:
		s.bobbersMu.Lock()
		defer s.bobbersMu.Unlock()
		h := ref.hook
		return player.Position{X: h.X, Y: h.Y, Z: h.Z, Yaw: h.Yaw, Pitch: h.Pitch}
	case *paintingEntity:
		s.paintingsMu.Lock()
		defer s.paintingsMu.Unlock()
		return player.Position{X: paintingCentreX(ref), Y: paintingCentreY(ref), Z: paintingCentreZ(ref)}
	case *frameEntity:
		s.framesMu.Lock()
		defer s.framesMu.Unlock()
		return player.Position{X: frameCentreX(ref), Y: frameCentreY(ref), Z: frameCentreZ(ref)}
	case *knotEntity:
		s.knotsMu.Lock()
		defer s.knotsMu.Unlock()
		return player.Position{X: knotX(ref), Y: knotY(ref), Z: knotZ(ref)}
	case *fixtureEntity:
		s.fixturesMu.Lock()
		defer s.fixturesMu.Unlock()
		return player.Position{X: ref.x, Y: ref.y, Z: ref.z, Yaw: ref.yaw, Pitch: ref.pitch}
	}
	return e.pos
}

// cameraAlive reports whether the snapshot's live entity is still in the
// world.
func (e *entitySnapshot) cameraAlive() bool {
	s := e.srv
	switch ref := e.ref.(type) {
	case *mobEntity:
		s.mobsMu.Lock()
		defer s.mobsMu.Unlock()
		return ref.mob.Alive()
	case *itemEntity:
		s.itemsMu.Lock()
		defer s.itemsMu.Unlock()
		return !ref.item.Dead
	case *orbEntity:
		s.orbsMu.Lock()
		defer s.orbsMu.Unlock()
		return !ref.orb.Dead
	case *arrowEntity:
		s.arrowsMu.Lock()
		defer s.arrowsMu.Unlock()
		return !ref.arrow.Dead
	case *thrownEntity:
		s.arrowsMu.Lock()
		defer s.arrowsMu.Unlock()
		return !ref.throwable.Dead
	case *tntEntity:
		s.tntMu.Lock()
		defer s.tntMu.Unlock()
		return !ref.tnt.Dead
	case *boatEntity:
		s.boatsMu.Lock()
		defer s.boatsMu.Unlock()
		return !ref.boat.Dead
	case *fallingEntity:
		s.fallingMu.Lock()
		defer s.fallingMu.Unlock()
		return !ref.fb.Dead
	case *armorStandEntity:
		s.armorStandsMu.Lock()
		defer s.armorStandsMu.Unlock()
		return !ref.stand.Dead
	case *minecartEntity:
		s.minecartsMu.Lock()
		defer s.minecartsMu.Unlock()
		return !ref.cart.Dead
	case *mannequinEntity:
		s.mannequinsMu.Lock()
		defer s.mannequinsMu.Unlock()
		return !ref.mannequin.Dead
	case *endCrystalEntity:
		s.endCrystalsMu.Lock()
		defer s.endCrystalsMu.Unlock()
		return !ref.crystal.Dead
	case *cloudEntity:
		s.arrowsMu.Lock()
		defer s.arrowsMu.Unlock()
		return !ref.cloud.Dead
	case *fireworkEntity:
		s.fireworksMu.Lock()
		defer s.fireworksMu.Unlock()
		return !ref.rocket.Dead
	case *bobberEntity:
		s.bobbersMu.Lock()
		defer s.bobbersMu.Unlock()
		return !ref.hook.Dead
	// The block-attached bodies and fixtures have no dead flag: they live
	// while in their list.
	case *paintingEntity:
		s.paintingsMu.Lock()
		defer s.paintingsMu.Unlock()
		return slices.Contains(s.paintings, ref)
	case *frameEntity:
		s.framesMu.Lock()
		defer s.framesMu.Unlock()
		return slices.Contains(s.frames, ref)
	case *knotEntity:
		s.knotsMu.Lock()
		defer s.knotsMu.Unlock()
		return slices.Contains(s.knots, ref)
	case *fixtureEntity:
		s.fixturesMu.Lock()
		defer s.fixturesMu.Unlock()
		return slices.Contains(s.fixtures, ref)
	}
	return false
}

// cameraPosition and cameraAlive read a spectated player: their position
// and whether they died or left.
func (c *client) cameraPosition() player.Position { return c.player.Position() }

func (c *client) cameraAlive() bool {
	select {
	case <-c.gone:
		return false
	default:
	}
	// A body on the end credits is not watchable; the watcher's view returns
	// before the credits' respawn.
	return !c.wonGame.Load() && !c.player.Vitals().Dead
}

// cameraCommandByID is the command entity with this protocol ID in the
// player's dimension, nil if not there: a Spectator Action's target.
func (c *client) cameraCommandByID(id int32) command.Entity {
	for _, e := range c.Entities(command.EntityQuery{}) {
		if e.EntityID() == id && e.DimensionName() == c.dimension().name {
			return e
		}
	}
	return nil
}

// pickableForCamera reports whether a camera may point at a body: not a
// spectator, and not an unpickable body (item, orb, flying projectile,
// marker armor stand). vanilla: Entity.isPickable
func pickableForCamera(e command.Entity) bool {
	switch v := e.(type) {
	case *client:
		return v.player.GameMode() != protocol.GameModeSpectator
	case *entitySnapshot:
		return v.cameraPickable()
	}
	return false
}

// cameraPickable is Entity.isPickable for a snapshot's live body: living
// mobs and mannequins, boats, minecarts, end crystals, non-marker armor
// stands, primed TNT, falling blocks, block-attached bodies, cushions,
// interactions, shulker bullets and redirectable projectiles; arrows and
// the rest fall through to never. Read under the kind's lock.
// vanilla: Entity.isPickable, LivingEntity.isPickable, ArmorStand.isPickable, PrimedTnt.isPickable, FallingBlockEntity.isPickable, Projectile.isPickable, AbstractMinecart.isPickable, EndCrystal.isPickable, BlockAttachedEntity.isPickable, Interaction.isPickable, ShulkerBullet.isPickable
func (e *entitySnapshot) cameraPickable() bool {
	s := e.srv
	switch ref := e.ref.(type) {
	case *mobEntity:
		s.mobsMu.Lock()
		defer s.mobsMu.Unlock()
		return !ref.mob.Dead
	case *boatEntity:
		s.boatsMu.Lock()
		defer s.boatsMu.Unlock()
		return !ref.boat.Dead
	case *armorStandEntity:
		s.armorStandsMu.Lock()
		defer s.armorStandsMu.Unlock()
		return !ref.stand.Dead && !ref.stand.Marker
	case *tntEntity:
		s.tntMu.Lock()
		defer s.tntMu.Unlock()
		return !ref.tnt.Dead
	case *fallingEntity:
		s.fallingMu.Lock()
		defer s.fallingMu.Unlock()
		return !ref.fb.Dead
	case *minecartEntity:
		s.minecartsMu.Lock()
		defer s.minecartsMu.Unlock()
		return !ref.cart.Dead
	case *mannequinEntity:
		s.mannequinsMu.Lock()
		defer s.mannequinsMu.Unlock()
		return !ref.mannequin.Dead
	case *endCrystalEntity:
		s.endCrystalsMu.Lock()
		defer s.endCrystalsMu.Unlock()
		return !ref.crystal.Dead
	case *paintingEntity, *frameEntity, *knotEntity:
		return true
	case *fixtureEntity:
		return ref.kind == entity.FixtureCushion || ref.kind == entity.FixtureInteraction
	case *thrownEntity:
		s.arrowsMu.Lock()
		defer s.arrowsMu.Unlock()
		t := ref.throwable
		return !t.Dead && (t.Redirectable() || t.Kind == entity.ThrownShulkerBullet)
	}
	return false
}

// handleTeleportToEntity moves a spectator to the player the teleport menu
// names, across dimensions if needed. Only players are looked up; an
// unknown UUID or non-spectator sender is ignored.
// vanilla: ServerGamePacketListenerImpl.handleTeleportToEntityPacket
func (c *client) handleTeleportToEntity(r *protocol.Reader) error {
	id := r.ReadUUID()
	if err := r.Err(); err != nil {
		return c.decodeFailure("teleport_to_entity", err)
	}
	if !c.clientLoaded() || c.player.GameMode() != protocol.GameModeSpectator {
		return nil
	}
	target := c.srv.clientByUUID(id)
	if target == nil {
		return nil
	}
	pos := target.player.Position()
	c.Teleport(command.Move{Pos: pos}, target.dimension().name, "teleport menu")
	return nil
}

// handleSpectatorAction handles a spectator's left-click on a body,
// pointing the camera at it unless it is outside the world border, beyond
// entity interaction reach, or never a camera target.
// vanilla: ServerGamePacketListenerImpl.handleSpectatorAction
func (c *client) handleSpectatorAction(r *protocol.Reader) error {
	id := r.ReadVarInt()
	if err := r.Err(); err != nil {
		return c.decodeFailure("spectator_action", err)
	}
	if !c.clientLoaded() {
		return nil
	}
	if c.player.GameMode() != protocol.GameModeSpectator {
		return nil
	}
	if id <= 0 {
		return nil
	}
	target := c.cameraCommandByID(id - 1)
	if target == nil {
		return nil
	}
	// Reach is from the eyes to the body's box, as for a right-click.
	// vanilla: Player.isWithinEntityInteractionRange
	pos := c.player.Position()
	eyeY := pos.Y + c.dims(c.player.Appearance().Pose).EyeHeight
	minX, minY, minZ, maxX, maxY, maxZ := target.Box()
	if !rules.WithinEntityInteractionRange(c.entityRange(), pos.X, eyeY, pos.Z, minX, minY, minZ, maxX, maxY, maxZ) {
		return nil
	}
	// vanilla: ServerGamePacketListenerImpl.handleSpectatorAction
	if p := target.Position(); !c.dimension().border.ContainsBlock(int32(math.Floor(p.X)), int32(math.Floor(p.Z))) {
		return nil
	}
	if !pickableForCamera(target) {
		return nil
	}
	c.SetCamera(target)
	return nil
}
