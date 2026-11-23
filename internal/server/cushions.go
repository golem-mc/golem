package server

import (
	"maps"
	"math"
	"slices"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
)

// The cushion: a fixture put down from a cushion item on a block's top,
// broken by a blow, a projectile, a blast, lightning, fire or lava, a piston
// move, or by losing what it rests on. A player sits on one by using it.

// cushionPollTicks is the survives poll's interval: the counter's
// post-increment fires it on every 101st tick.
// vanilla: BlockAttachedEntity.tick, CHECK_INTERVAL
const cushionPollTicks = 100

const (
	cushionSoundPlace = registry.SoundEntityCushionPlace
	cushionSoundBreak = registry.SoundEntityCushionBreak
	cushionSoundSit   = registry.SoundEntityCushionSit
	cushionSoundGetUp = registry.SoundEntityCushionGetUp
)

// cushionColorOfItem is the DyeColor ID of a cushion item, false for any
// other item. The sixteen cushion items are numbered in DyeColor order.
// vanilla: Items.CUSHION, the cushion/color component
func cushionColorOfItem(item registry.Item) (int32, bool) {
	if item < registry.ItemWhiteCushion || item > registry.ItemBlackCushion {
		return 0, false
	}
	return int32(item - registry.ItemWhiteCushion), true
}

// isCushionItem matches the cushion items' stacks.
func isCushionItem(s protocol.ItemStack) bool {
	_, ok := cushionColorOfItem(registry.Item(s.ItemID))
	return ok
}

// placeCushion handles one use of a cushion item on a block: a cushion goes
// down centred on the clicked column at the click's height, facing the
// placer's snapped yaw, where it rests on something, is not buried and
// overlaps no other cushion. Any face but the top refuses the use.
// vanilla: CushionItem.useOn
func (c *client) placeCushion(hand, x, y, z int32, ctx rules.PlacementContext) (handled bool, err error) {
	c.useSwing = swingNone
	stack := c.heldItem(hand)
	color, ok := cushionColorOfItem(registry.Item(stack.ItemID))
	if !ok {
		return false, nil
	}
	s, w := c.srv, c.theWorld()
	pos := c.player.Position()
	face := ctx.Face
	clickY := float64(y) + float64(ctx.CursorY)
	if f, hitY, ok := rules.CushionClickOnCollision(w.BlockAt(x, y, z), x, y, z,
		pos.X, pos.Y+c.dims(c.player.Appearance().Pose).EyeHeight, pos.Z,
		float64(x)+float64(ctx.CursorX), clickY, float64(z)+float64(ctx.CursorZ)); ok {
		face, clickY = f, hitY
	}
	if face != protocol.FaceTop {
		return true, nil
	}
	ex, ez := float64(x)+0.5, float64(z)+0.5
	minX, minY, minZ, maxX, maxY, maxZ := entity.FixtureBox(entity.FixtureCushion, nil, ex, clickY, ez)
	if !rules.CushionCanBePlacedAt(w, minX, minY, minZ, maxX, maxY, maxZ) ||
		s.cushionOverlaps(w, minX, minY, minZ, maxX, maxY, maxZ) {
		return true, nil
	}
	if stack.Removes(protocol.DataComponentCushionColor) {
		color = 0 // vanilla: Cushion.DEFAULT_COLOR
	}
	tag := nbt.Compound{"color": nbt.String(registry.DyeNames[color])}
	cushionItemData(tag, stack)
	ent := fixtureOfTag(s, w, summonTag(tag, entity.FixtureCushion, ex, clickY, ez))
	if ent == nil {
		return true, nil
	}
	ent.yaw, ent.pitch = rules.CushionYaw(pos.Yaw), 0
	c.useSwing = swingPredicted
	s.registerFixture(ent)
	// Lava waits for the first poll. vanilla: CushionItem.useOn, Cushion.destroyIfInFire
	if rules.CushionInFire(w, minX, minY, minZ, maxX, maxY, maxZ) && !s.cushionInvulnerable(ent) {
		s.breakCushion(ent, cushionDrops)
	}
	s.broadcastSoundID(w, cushionSoundPlace, protocol.SoundCategoryBlock, ex, clickY, ez, 0.75, 0.8)
	c.gameEventAt(rules.GameEventEntityPlace, ex, clickY, ez, 0)
	c.awardUsedHeld(hand)
	if c.player.GameMode() != protocol.GameModeCreative {
		return true, c.consumeHeld(hand)
	}
	return true, nil
}

// cushionItemData applies the placing stack's custom name, then the fields
// of an entity_data naming the cushion type, to the new cushion's tag.
// vanilla: EntityType.createDefaultStackConfig
func cushionItemData(tag nbt.Compound, stack protocol.ItemStack) {
	if stack.CustomName != nil {
		tag["CustomName"] = protocol.TextTag(*stack.CustomName)
	}
	for _, raw := range stack.Raw {
		if raw.Type != protocol.DataComponentEntityData {
			continue
		}
		r := protocol.NewReader(raw.Data)
		if typ := r.ReadVarInt(); r.Err() != nil || typ != int32(registry.EntityTypeCushion) {
			return
		}
		if decoded, _, err := nbt.DecodeNetwork(r.Rest()); err == nil {
			if compound, ok := decoded.(nbt.Compound); ok {
				maps.Copy(tag, compound)
			}
		}
		return
	}
}

// cushionOverlaps reports whether a live cushion of w overlaps the box.
// vanilla: CushionItem.useOn, ServerLevel.getEntitiesOfClass
func (s *Server) cushionOverlaps(w *world.World, minX, minY, minZ, maxX, maxY, maxZ float64) bool {
	s.fixturesMu.Lock()
	defer s.fixturesMu.Unlock()
	for _, ent := range s.fixtures {
		if ent.dead || ent.w != w || ent.kind != entity.FixtureCushion {
			continue
		}
		x0, y0, z0, x1, y1, z1 := entity.FixtureBox(ent.kind, ent.tag, ent.x, ent.y, ent.z)
		if x0 < maxX && x1 > minX && y0 < maxY && y1 > minY && z0 < maxZ && z1 > minZ {
			return true
		}
	}
	return false
}

// attackCushion handles a blow at a cushion, true when the target is one.
// A player who may not build, or not at the cushion's cell, leaves it, as
// a survival one does an invulnerable cushion; a creative one breaks it
// without the drop.
// vanilla: BlockAttachedEntity.skipAttackInteraction, Cushion.hurtServer
func (c *client) attackCushion(target int32) bool {
	s := c.srv
	ent := s.fixtureByEntityID(c.theWorld(), target, entity.FixtureCushion)
	if ent == nil {
		return false
	}
	s.fixturesMu.Lock()
	minX, minY, minZ, maxX, maxY, maxZ := entity.FixtureBox(ent.kind, ent.tag, ent.x, ent.y, ent.z)
	cx, cz := int32(math.Floor(ent.x)), int32(math.Floor(ent.z))
	s.fixturesMu.Unlock()
	creative := c.player.GameMode() == protocol.GameModeCreative
	if !c.withinAttackRange(minX, minY, minZ, maxX, maxY, maxZ) {
		return true
	}
	if !rules.ModeMayBuild(c.player.GameMode()) || !c.mayInteractAt(cx, cz) {
		return true
	}
	if !creative && s.cushionInvulnerable(ent) {
		// Player.attack plays the no-damage sound for a refused hurt.
		c.attackSound(soundAttackNoDamage)
		return true
	}
	s.breakCushion(ent, cushionBreakBy(creative))
	return true
}

// cushionInvulnerable reports whether the cushion's Invulnerable tag turns
// away a hurt that is not a creative player's.
// vanilla: Entity.isInvulnerableToBase
func (s *Server) cushionInvulnerable(ent *fixtureEntity) bool {
	s.fixturesMu.Lock()
	defer s.fixturesMu.Unlock()
	return nbt.BoolField(ent.tag, "Invulnerable")
}

// cushionBreak is what took a cushion down, which shapes its drop.
type cushionBreak uint8

const (
	cushionDrops    cushionBreak = iota
	cushionDropless              // a creative breaker
	// cushionStruck is a lightning bolt, whose drop is invulnerable for a
	// while so the bolt's fire does not burn it at once.
	cushionStruck
)

// cushionStruckInvulnerableTicks is the struck cushion's drop's
// invulnerable time. vanilla: Cushion.dropItem
const cushionStruckInvulnerableTicks = 20

// cushionBreakBy is the break a blow or blast deals, dropless when its
// cause is a creative player.
func cushionBreakBy(creative bool) cushionBreak {
	if creative {
		return cushionDropless
	}
	return cushionDrops
}

// breakCushion takes a cushion down: the break sound and the wool's
// particles, then its item, which keeps the cushion's custom name, unless
// the break is dropless or entity_drops is off. false when the cushion was
// already gone.
// vanilla: BlockAttachedEntity.kill, Cushion.dropItem
func (s *Server) breakCushion(ent *fixtureEntity, cause cushionBreak) bool {
	s.fixturesMu.Lock()
	if ent.dead {
		s.fixturesMu.Unlock()
		return false
	}
	s.removeFixtureLocked(ent)
	w, x, y, z := ent.w, ent.x, ent.y, ent.z
	color := entity.CushionColor(ent.tag)
	name, named := protocol.StyledTextOf(ent.tag["CustomName"])
	riders := slices.Clone(ent.riders)
	s.fixturesMu.Unlock()

	ejectCushionRiders(riders)
	s.broadcastWorld(w, protocol.IDPlayRemoveEntities, removeEntitiesPayload([]int32{ent.id}))
	s.gameEvent(w, rules.GameEventEntityDie, x, y, z, rules.GameEventContext{})
	s.broadcastSoundID(w, cushionSoundBreak, protocol.SoundCategoryNeutral, x, y, z, 1, 1)
	// vanilla: Cushion.showBreakingParticles
	s.sendBlockParticles(w, protocol.ParticleBlock, (registry.BlockWhiteWool + registry.Block(color)).DefaultState(),
		x, y+entity.CushionHeight*2/3, z, entity.CushionWidth/4, entity.CushionHeight/4, entity.CushionWidth/4, 0.05, 10)
	if cause == cushionDropless || !w.BoolRule(world.RuleEntityDrops) {
		return true
	}
	stack := protocol.ItemStack{ItemID: int32(registry.ItemWhiteCushion) + color, Count: 1}
	if named {
		stack.CustomName = &name
	}
	drop := s.newItemAtFeet(w, stack, x, y, z)
	if cause == cushionStruck {
		drop.item.InvulnerableTime = cushionStruckInvulnerableTicks
	}
	s.registerFreshItem(drop)
	return true
}

// cushionInHarm reports whether fire or lava reaches the cushion's box,
// which breaks it with its drop.
// vanilla: Cushion.destroyIfInFire, Cushion.tickAtCheckInterval
func (s *Server) cushionInHarm(ent *fixtureEntity) bool {
	s.fixturesMu.Lock()
	minX, minY, minZ, maxX, maxY, maxZ := entity.FixtureBox(ent.kind, ent.tag, ent.x, ent.y, ent.z)
	cx, cy, cz := int32(math.Floor(ent.x)), int32(math.Floor(ent.y)), int32(math.Floor(ent.z))
	w := ent.w
	s.fixturesMu.Unlock()
	return rules.CushionInFire(w, minX, minY, minZ, maxX, maxY, maxZ) ||
		registry.BlockOf(w.BlockAt(cx, cy, cz)) == registry.BlockLava
}

// tickCushions runs each cushion's check: fire and lava, then the survives
// poll, which breaks one left resting on nothing.
// vanilla: BlockAttachedEntity.tick
func (s *Server) tickCushions() {
	s.tickCushionRiders()
	s.lockTick(&s.fixturesMu)
	var due []*fixtureEntity
	for _, ent := range s.fixtures {
		if ent.dead || ent.kind != entity.FixtureCushion {
			continue
		}
		ent.age++
		if ent.age%(cushionPollTicks+1) == 0 {
			due = append(due, ent)
		}
	}
	s.unlockTick(&s.fixturesMu)
	for _, ent := range due {
		if s.cushionInHarm(ent) {
			// The fire's and lava's hurts are refused by an invulnerable
			// cushion, which stays to take the survives poll.
			if !s.cushionInvulnerable(ent) {
				s.breakCushion(ent, cushionDrops)
				continue
			}
		}
		minX, minY, minZ, maxX, maxY, maxZ := s.fixtureBox(ent)
		if !rules.CushionSurvives(ent.w, minX, minY, minZ, maxX, maxY, maxZ) {
			s.breakCushion(ent, cushionDrops)
		}
	}
}

// cushionsWhere lists the live cushions of w the test admits by box and
// position.
func (s *Server) cushionsWhere(w *world.World, hit func(minX, minY, minZ, maxX, maxY, maxZ, x, y, z float64) bool) []*fixtureEntity {
	s.fixturesMu.Lock()
	defer s.fixturesMu.Unlock()
	var out []*fixtureEntity
	for _, ent := range s.fixtures {
		if ent.dead || ent.w != w || ent.kind != entity.FixtureCushion {
			continue
		}
		minX, minY, minZ, maxX, maxY, maxZ := entity.FixtureBox(ent.kind, ent.tag, ent.x, ent.y, ent.z)
		if hit(minX, minY, minZ, maxX, maxY, maxZ, ent.x, ent.y, ent.z) {
			out = append(out, ent)
		}
	}
	return out
}

// cushionSnapshots is each world's live cushions as projectile targets.
func (s *Server) cushionSnapshots() map[*world.World][]entity.ArrowTarget {
	s.fixturesMu.Lock()
	defer s.fixturesMu.Unlock()
	var byWorld map[*world.World][]entity.ArrowTarget
	for _, ent := range s.fixtures {
		if ent.dead || ent.kind != entity.FixtureCushion {
			continue
		}
		if byWorld == nil {
			byWorld = make(map[*world.World][]entity.ArrowTarget)
		}
		minX, minY, minZ, maxX, maxY, maxZ := entity.FixtureBox(ent.kind, ent.tag, ent.x, ent.y, ent.z)
		byWorld[ent.w] = append(byWorld[ent.w], entity.ArrowTarget{
			EntityID: ent.id, Type: protocol.EntityTypeCushion,
			MinX: minX, MinY: minY, MinZ: minZ, MaxX: maxX, MaxY: maxY, MaxZ: maxZ,
		})
	}
	return byWorld
}

// cushionProjectileHit is a cushion a projectile's sweep met. shooter is the
// owning player in the cushion's world, nil for any other owner.
type cushionProjectileHit struct {
	hitSound registry.Sound
	w        *world.World
	id       int32
	shooter  *client
	mob      bool
	arrow    *arrowEntity
}

// projectileHitCushion lands a projectile's hurt on a cushion, which breaks
// it with its drop. false is a refused hurt: a mob's with mob_griefing off,
// a player's who may not build, or not at the cushion's cell, or any but a
// creative player's on an invulnerable cushion. A cushion
// already gone takes the hit all the same.
// vanilla: Cushion.hurtServer, BlockAttachedEntity.hurtServer
func (s *Server) projectileHitCushion(hit cushionProjectileHit) bool {
	ent := s.fixtureByEntityID(hit.w, hit.id, entity.FixtureCushion)
	if ent == nil {
		return true
	}
	if hit.mob && !hit.w.BoolRule(world.RuleMobGriefing) {
		return false
	}
	if c := hit.shooter; c != nil {
		s.fixturesMu.Lock()
		cx, cz := int32(math.Floor(ent.x)), int32(math.Floor(ent.z))
		s.fixturesMu.Unlock()
		if !rules.ModeMayBuild(c.player.GameMode()) || !c.mayInteractAt(cx, cz) {
			return false
		}
	}
	creative := creativeClient(hit.shooter)
	if !creative && s.cushionInvulnerable(ent) {
		return false
	}
	s.breakCushion(ent, cushionBreakBy(creative))
	return true
}

// useOnCushion is a use on a cushion: a click not sneaking seats the player
// on an empty one and plays the sit; any other click does nothing.
// vanilla: Cushion.interact
func (c *client) useOnCushion(ent *fixtureEntity, sneaking bool) error {
	s := c.srv
	pos := c.player.Position()
	minX, minY, minZ, maxX, maxY, maxZ := s.fixtureBox(ent)
	if !rules.WithinEntityInteractionRange(c.entityRange(),
		pos.X, pos.Y+c.dims(c.player.Appearance().Pose).EyeHeight, pos.Z,
		minX, minY, minZ, maxX, maxY, maxZ) {
		return nil
	}
	s.fixturesMu.Lock()
	occupied := len(ent.riders) > 0
	s.fixturesMu.Unlock()
	if sneaking || occupied || !s.mountCushion(c, ent, false) {
		return nil
	}
	s.fixturesMu.Lock()
	w, x, y, z := ent.w, ent.x, ent.y, ent.z
	s.fixturesMu.Unlock()
	c.useSwing = swingServer
	s.broadcastSoundID(w, cushionSoundSit, protocol.SoundCategoryNeutral, x, y, z, 1, 1)
	return nil
}

// mountCushion seats a player on a cushion: they leave any other vehicle,
// every viewer is told the cushion's riders, and the body moves to the seat.
// force skips the empty-seat check, as /ride mount does. It reports whether
// the rider was seated.
// vanilla: Entity.startRiding
func (s *Server) mountCushion(rider *client, ent *fixtureEntity, force bool) bool {
	if rider.cushion.Load() == ent || rider.player.Vitals().Dead || !force && !rider.mountAllowed(ent.uuid, false) {
		return false
	}
	s.fixturesMu.Lock()
	ok := !ent.dead && (force || len(ent.riders) == 0)
	s.fixturesMu.Unlock()
	if !ok {
		return false
	}
	if rider.isPassenger() {
		rider.dismountBoat(false)
		rider.dismountMinecart(false)
		rider.dismountCushion(false)
	}
	s.fixturesMu.Lock()
	if ent.dead || rider.isPassenger() || (!force && len(ent.riders) > 0) {
		s.fixturesMu.Unlock()
		return false
	}
	ent.riders = append(ent.riders, rider)
	rider.cushion.Store(ent)
	w, id, ids := ent.w, ent.id, cushionRiderIDs(ent)
	bx, by, bz := ent.x, ent.y, ent.z
	x, y, z := bx, entity.CushionSeatY(by), bz
	s.fixturesMu.Unlock()

	s.gameEvent(w, rules.GameEventEntityMount, bx, by, bz, rider.gameEventContext(0))
	s.broadcastWorld(w, protocol.IDPlaySetPassengers, setPassengersPayload(id, ids))
	pos := rider.player.Position()
	pos.X, pos.Y, pos.Z = x, y, z
	rider.move(pos, false)
	startedRidingAdvancements([]*client{rider})
	return true
}

// cushionRiderIDs lists the cushion's riders' entity IDs; the fixtures lock
// is held.
func cushionRiderIDs(ent *fixtureEntity) []int32 {
	ids := make([]int32, len(ent.riders))
	for i, r := range ent.riders {
		ids[i] = r.player.EntityID
	}
	return ids
}

// ridingCushion reports whether the player sits on a cushion.
func (c *client) ridingCushion() bool { return c.cushion.Load() != nil }

// dismountCushion takes the player off the cushion they sit on: every viewer
// is told the riders, then, when step is set, the body stands on the
// cushion's top, or beside a removed cushion where its box fits. A cushion
// still standing plays the get-up.
// vanilla: LivingEntity.dismountVehicle, Entity.getDismountLocationForPassenger,
// Cushion.removePassenger
func (c *client) dismountCushion(step bool) {
	ent := c.cushion.Load()
	if ent == nil {
		return
	}
	s := c.srv
	s.fixturesMu.Lock()
	i := slices.Index(ent.riders, c)
	if i < 0 {
		s.fixturesMu.Unlock()
		return
	}
	ent.riders = slices.Delete(ent.riders, i, i+1)
	c.cushion.Store(nil)
	w, id, ids, removed := ent.w, ent.id, cushionRiderIDs(ent), ent.dead
	x, y, z := ent.x, ent.y, ent.z
	s.fixturesMu.Unlock()

	s.gameEvent(w, rules.GameEventEntityDismount, x, y, z, c.gameEventContext(0))
	s.broadcastWorld(w, protocol.IDPlaySetPassengers, setPassengersPayload(id, ids))
	if step && !c.player.Vitals().Dead {
		pos := c.player.Position()
		spot := player.Position{X: x, Y: y + entity.CushionHeight, Z: z, Yaw: pos.Yaw, Pitch: pos.Pitch}
		if removed {
			d := c.dims(c.player.Appearance().Pose)
			spot.X, spot.Y, spot.Z = entity.OffRemovedVehicle(c.theWorld(), pos.X, pos.Y, pos.Z, y, d.Width, d.Height)
		}
		c.move(spot, false)
		c.dismountTo(spot)
		c.updateAppearance(func(*player.Appearance) {})
	}
	if !removed {
		s.broadcastSoundID(w, cushionSoundGetUp, protocol.SoundCategoryNeutral, x, y, z, 1, 1)
	}
}

// ejectCushionRiders stands up the riders of a cushion just removed.
// vanilla: Entity.setRemoved, ejectPassengers
func ejectCushionRiders(riders []*client) {
	for _, r := range riders {
		r.dismountCushion(true)
	}
}

// tickCushionRiders stands up anyone left on a cushion that is gone or in
// another world, as a cross-dimension teleport of the cushion leaves them.
func (s *Server) tickCushionRiders() {
	s.mu.RLock()
	var gone []*client
	for _, c := range s.clients {
		ent := c.cushion.Load()
		if ent == nil {
			continue
		}
		s.lockTick(&s.fixturesMu)
		if ent.dead || ent.w != c.theWorld() {
			gone = append(gone, c)
		}
		s.unlockTick(&s.fixturesMu)
	}
	s.mu.RUnlock()
	for _, c := range gone {
		c.dismountCushion(true)
	}
}

// isCushionTag matches a saved entity's id naming a cushion.
func isCushionTag(id nbt.String) bool { return string(id) == entity.FixtureCushion }

// savedCushion is the RootVehicle compound of a player seated on a cushion,
// nil when another player shares it. With take, as on leaving, the player
// gets up and the cushion leaves the world without its drop, so the file's
// copy is the only one a rejoin can raise. The get-up plays: vanilla stops
// the riding before it removes the cushion.
// vanilla: ServerPlayer.saveParentVehicle, PlayerList.remove
func (c *client) savedCushion(ent *fixtureEntity, take bool) nbt.Compound {
	s := c.srv
	s.fixturesMu.Lock()
	if ent.dead || len(ent.riders) != 1 {
		s.fixturesMu.Unlock()
		return nil
	}
	tag := fixtureTag(ent)
	s.fixturesMu.Unlock()
	if take {
		c.dismountCushion(false)
		s.killFixture(ent)
	}
	return nbt.Compound{"Attach": nbt.UUIDToIntArray(ent.uuid), "Entity": tag}
}

// remountSavedCushion seats a joining player on the cushion of their file's
// RootVehicle: the one of w with its identity, else the file's copy stood
// up and joined to its Team.
// vanilla: ServerPlayer.loadAndSpawnParentVehicle
func (s *Server) remountSavedCushion(c *client, w *world.World, tag nbt.Compound, identity uuid.UUID) {
	s.fixturesMu.Lock()
	var standing *fixtureEntity
	for _, ent := range s.fixtures {
		if !ent.dead && ent.w == w && ent.uuid == identity {
			standing = ent
			break
		}
	}
	s.fixturesMu.Unlock()
	if standing == nil {
		if standing = fixtureOfTag(s, w, tag); standing == nil {
			return
		}
		s.registerFixture(standing)
		s.joinTagTeam(tagTeam{identity: identity, tag: tag})
	}
	s.mountCushion(c, standing, false)
}
