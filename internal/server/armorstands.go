package server

import (
	"context"
	"github.com/golem-mc/golem/plugin"
	"log/slog"
	"math"
	"slices"
	"sync"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/command"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/pluginruntime"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
)

// The armor stands of the worlds, the patterns the paintings': one lock for
// the whole collection, sends made outside it, the thin adapter over the
// plain value internal/server/entity ticks. What a stand is and what its tick does
// lives there; this file holds the dimension it belongs to, the identity
// the packets and the save address it by, and what each client has been
// told about it. A stand arrives on a placement or a summon and leaves in a
// break — a punch's second, a fire's last bite, a blast — with the item it
// is and everything it was holding laid behind it.

// armorStandEntity is one armor stand in a world.
type armorStandEntity struct {
	stand *entity.ArmorStand
	w     *world.World
	uuid  uuid.UUID
	bodyTrack[*armorStandMeta]
	// metaSeen is the snapshot the last tracking pass saw.
	metaSeen *armorStandMeta
	portal   rules.EntityPortal

	// tags are the scoreboard tags, what the tag selector option matches
	// and /tag edits. Guarded by the stands lock, like the rest of a
	// stand's own fields.
	tags nbt.StringSet

	custom customName
	look   entityLook

	// Entity.load changes Fire and HasVisualFire without changing the shared
	// fire flag; only an entity tick refreshes it.
	// vanilla: Entity.load, Entity.setSharedFlagOnFire
	onFire      bool
	spawnReason uint8 // plugin.SpawnReason+1; 0 means SpawnDefault

	// Immutable metadata snapshots are shared by watchers under the stands lock.
	metadata *armorStandMeta
}

// standTracker tracks the armor stands. A stand is told where it went on a
// movement tick (every entity.ArmorStandInterval ticks; a teleport waits for
// the next one), always by an absolute Teleport Entity (standMove).
var standTracker = trackedKind[*armorStandEntity, *armorStandMeta]{
	steppedPath:      true,
	invalidatePiston: func(dim *dimension) { dim.pistons.armorStands.built = false },

	rangeChunks: entity.ArmorStandTrackingRange,
	lock:        func(s *Server) *sync.Mutex { return &s.armorStandsMu },
	list:        func(s *Server) *[]*armorStandEntity { return &s.armorStands },
	chunks:      func(s *Server) *entity.ChunkIndex[*armorStandEntity] { return &s.standChunks },
	filing:      func(it *armorStandEntity) *entity.Filing { return &it.stand.Filing },
	uuid:        func(it *armorStandEntity) uuid.UUID { return it.uuid },
	track:       func(ent *armorStandEntity) *bodyTrack[*armorStandMeta] { return &ent.bodyTrack },
	world:       func(ent *armorStandEntity) *world.World { return ent.w },
	id:          func(ent *armorStandEntity) int32 { return ent.stand.ID },
	dead:        func(ent *armorStandEntity) bool { return ent.stand.Dead },
	pose: func(ent *armorStandEntity) bodyPose {
		st := ent.stand
		return bodyPose{x: st.X, y: st.Y, z: st.Z, yaw: st.Yaw, onGround: st.OnGround}
	},
	pairing: armorStandPairing,
	own:     armorStandMetaOf,
}

// newArmorStandEntity builds one stand of a world with a fresh identity.
func (s *Server) newArmorStandEntity(w *world.World, x, y, z float64) *armorStandEntity {
	return &armorStandEntity{
		stand: entity.NewArmorStand(s.nextEntity(), x, y, z),
		w:     w,
		uuid:  uuid.New(),
	}
}

// registerArmorStand puts a built stand into the world, the summon's and
// the wake's door, and shows it to every player in range.
func (s *Server) registerArmorStand(ent *armorStandEntity) { standTracker.register(s, ent) }

// removeArmorStandLocked takes a stand out of the list. The stands lock is
// held.
func (s *Server) removeArmorStandLocked(ent *armorStandEntity) {
	standTracker.unfile(ent)
	s.armorStands = slices.DeleteFunc(s.armorStands, func(o *armorStandEntity) bool { return o == ent })
	s.entityDestroyed(ent.uuid)
}

// armorStandByIDLocked finds the stand an entity ID names, nil for one
// gone. The stands lock is held.
func (s *Server) armorStandByIDLocked(w *world.World, id int32) *armorStandEntity {
	for _, ent := range s.armorStands {
		if ent.stand.ID == id && ent.w == w {
			return ent
		}
	}
	return nil
}

// armorStandSnapshots reads every live stand's box under the stands lock,
// before a projectile tick takes its own locks: the stands lock is taken
// beneath none of them. Each world's stands come back as the boxes a
// flight's swept line may meet, the target type what tells a stand from the
// bodies beside it. A marker holds no box to hit.
func (s *Server) armorStandSnapshots() map[*world.World][]entity.ArrowTarget {
	s.armorStandsMu.Lock()
	defer s.armorStandsMu.Unlock()
	if len(s.armorStands) == 0 {
		return nil
	}
	byWorld := make(map[*world.World][]entity.ArrowTarget)
	for _, ent := range s.armorStands {
		if ent.stand.Dead || ent.stand.Marker {
			continue
		}
		minX, minY, minZ, maxX, maxY, maxZ := ent.stand.BlastBox()
		byWorld[ent.w] = append(byWorld[ent.w], entity.ArrowTarget{
			EntityID: ent.stand.ID,
			Type:     protocol.EntityTypeArmorStand,
			MinX:     minX, MinY: minY, MinZ: minZ,
			MaxX: maxX, MaxY: maxY, MaxZ: maxZ,
		})
	}
	return byWorld
}

// armorStandRamTargets reads each world's visible stands under the stands
// lock, before the mob lock, as the targets a goat's ram may pick. A marker,
// invisible or invulnerable stand is no target. Nil when there is none.
// vanilla: LivingEntity.canBeSeenAsEnemy, ArmorStand.canBeSeenByAnyone
func (s *Server) armorStandRamTargets() map[*world.World][]entity.StandTarget {
	s.armorStandsMu.Lock()
	defer s.armorStandsMu.Unlock()
	var byWorld map[*world.World][]entity.StandTarget
	for _, ent := range s.armorStands {
		st := ent.stand
		if st.Dead || st.Marker || st.Invisible || st.Invulnerable {
			continue
		}
		if byWorld == nil {
			byWorld = make(map[*world.World][]entity.StandTarget)
		}
		minX, minY, _, maxX, maxY, _ := st.BlastBox()
		_, ey, _ := st.EyePosition()
		byWorld[ent.w] = append(byWorld[ent.w], entity.StandTarget{
			ID: st.ID, X: st.X, Y: st.Y, Z: st.Z,
			W: maxX - minX, H: maxY - minY, Eye: ey - st.Y,
		})
	}
	return byWorld
}

// standRam is a goat's ram landing on a stand.
type standRam struct {
	w    *world.World
	id   int32
	yaw  float64
	push float64
}

// ramStand shoves the stand a goat rammed; the blow does no damage.
// vanilla: RamTarget.tick, LivingEntity.knockback
func (s *Server) ramStand(r standRam) {
	s.armorStandsMu.Lock()
	defer s.armorStandsMu.Unlock()
	if ent := s.armorStandByIDLocked(r.w, r.id); ent != nil && !ent.stand.Dead {
		ent.stand.KnockbackAlong(r.yaw, r.push)
	}
}

// the two sounds of an armor stand's arrival and its end.
const (
	soundArmorStandPlace = registry.SoundEntityArmorStandPlace
	soundArmorStandBreak = registry.SoundEntityArmorStandBreak
)

// armorStandPairing is what shows a stand: the spawn, then what follows it.
func armorStandPairing(ent *armorStandEntity) []outbound {
	packets := []outbound{
		{id: protocol.IDPlaySpawnEntity, data: spawnArmorStandPayload(ent)},
		{id: protocol.IDPlayEntityMetadata, data: armorStandMetadataPayload(ent, false)},
	}
	if attrs := ent.stand.Attributes().Syncable(); len(attrs) != 0 {
		packets = append(packets, outbound{id: protocol.IDPlayUpdateAttributes, data: attributesPayload(ent.stand.ID, attrs)})
	}
	packets = append(packets, outbound{id: protocol.IDPlayEntityEquipment, data: armorStandEquipment(ent)})
	return bundle(pairing(packets...))
}

// spawnArmorStandPayload builds the Spawn Entity packet for a stand: the
// type, the position of its feet, the yaw it was placed or summoned at,
// and no velocity, of which a stand standing still has none. The head yaw
// is the yaw again, a living body's needing one somewhere.
func spawnArmorStandPayload(ent *armorStandEntity) []byte {
	s := ent.stand
	return spawnEntityPayload(spawnEntity{
		id: s.ID, uuid: ent.uuid, typ: protocol.EntityTypeArmorStand,
		x: s.X, y: s.Y, z: s.Z,
		yaw:     encodeAngle(s.Yaw),
		headYaw: encodeAngle(s.Yaw),
	})
}

// armorStandPoseIndices pairs each pose entry's metadata index with its
// slot in the stand's own Pose array, in the order the metadata sends them.
var armorStandPoseIndices = [][2]uint8{
	{protocol.MetadataIndexArmorStandHead, 0},
	{protocol.MetadataIndexArmorStandBody, 1},
	{protocol.MetadataIndexArmorStandLeftArm, 2},
	{protocol.MetadataIndexArmorStandRightArm, 3},
	{protocol.MetadataIndexArmorStandLeftLeg, 4},
	{protocol.MetadataIndexArmorStandRightLeg, 5},
}

// armorStandPoseParts names the six pose parts in the stand's own Pose
// array's order, the names the Pose compound of a save and of a summon's
// tag carry.
var armorStandPoseParts = [...]string{"Head", "Body", "LeftArm", "RightArm", "LeftLeg", "RightLeg"}

// armorStandFlags is the stand's own flags byte at metadata index 15.
// vanilla: ArmorStand.defineSynchedData
func armorStandFlags(s *entity.ArmorStand) uint8 {
	var flags uint8
	if s.Small {
		flags |= protocol.ArmorStandFlagSmall
	}
	if s.ShowArms {
		flags |= protocol.ArmorStandFlagArms
	}
	if s.NoBasePlate {
		flags |= protocol.ArmorStandFlagNoBasePlate
	}
	if s.Marker {
		flags |= protocol.ArmorStandFlagMarker
	}
	return flags
}

// armorStandEquipment builds the Set Equipment packet carrying all six of
// a stand's slots, held and worn, whether or not they hold anything, so
// taking a piece off reaches the watchers as clearly as putting one on.
func armorStandEquipment(ent *armorStandEntity) []byte {
	s := ent.stand
	stacks := [6]protocol.ItemStack{
		s.MainHand, s.OffHand,
		s.Worn[0], s.Worn[1], s.Worn[2], s.Worn[3],
	}
	var b protocol.Buffer
	b.PutVarInt(s.ID)
	for i := range stacks {
		entry := uint8(i)
		if i < len(stacks)-1 {
			entry |= 0x80
		}
		b.PutU8(entry)
		b.PutSlot(stacks[i])
	}
	return b.Bytes()
}

// armorStandWithinReach reports whether the player's eyes are within the
// attack or the entity-interaction range of the stand's box, the reach
// check the punch and the use both run before they read the click.
func (c *client) armorStandWithinReach(ent *armorStandEntity, attack bool) bool {
	minX, minY, minZ, maxX, maxY, maxZ := ent.stand.BlastBox()
	if attack {
		return c.withinAttackRange(minX, minY, minZ, maxX, maxY, maxZ)
	}
	pos := c.player.Position()
	return rules.WithinEntityInteractionRange(c.entityRange(), pos.X, pos.Y+c.dims(c.player.Appearance().Pose).EyeHeight, pos.Z,
		minX, minY, minZ, maxX, maxY, maxZ)
}

// placeArmorStand handles one use of the armor stand item, vanilla
// ArmorStandItem.useOn: a stand stands where a block would go — the
// clicked cell where it can be replaced, the face's neighbour otherwise —
// facing its placer, where its box is free of blocks and of every body.
// handled is true when the held item was a stand, the use answered either
// way.
func (c *client) placeArmorStand(hand, x, y, z, face int32) (handled bool, err error) {
	c.useSwing = swingNone
	stack := c.heldItem(hand)
	if registry.Item(stack.ItemID) != registry.ItemArmorStand {
		return false, nil
	}
	// The bottom face is no face to stand one on.
	// vanilla: ArmorStandItem.useOn
	if face == protocol.FaceBottom {
		return true, nil
	}
	w := c.theWorld()
	ctx := rules.PlacementContext{Face: face, ReplacingClickedOn: true, CursorX: 0.5, CursorY: 0.5, CursorZ: 0.5}
	nx, ny, nz := x, y, z
	if !rules.CanBeReplaced(w.BlockAt(x, y, z), registry.Block(registry.BlockCount), ctx) {
		off := protocol.FaceOffset[face]
		nx, ny, nz = x+off[0], y+off[1], z+off[2]
		// A neighbour nothing replaces — solid blocks on every side of
		// the click — answers with nothing stood up and the item kept.
		if !rules.CanBeReplaced(w.BlockAt(nx, ny, nz), registry.Block(registry.BlockCount), rules.PlacementContext{Face: face}) {
			return true, nil
		}
	}
	// The box the stand would fill has to be free of blocks and of every
	// body: it cannot be stood into a ceiling or onto a player.
	// vanilla: ArmorStandItem.useOn, CollisionGetter.noCollision, getEntities
	box := entity.ArmorStandPlacementBox(nx, ny, nz)
	if w.BoxCollides(box.Corners()) || c.srv.boxTaken(w, box) {
		return true, nil
	}
	// The stand drops onto the shapes of the cell below, EntityType.create's
	// getYOffset, so one placed on a bottom slab rests on the slab's top.
	// vanilla: ArmorStandItem.useOn, EntityType.getYOffset
	spawnY := float64(ny) + w.SpawnYOffset(nx, ny, nz, true, entity.ArmorStandWidth/2)
	ent := c.srv.newArmorStandEntity(w, float64(nx)+0.5, spawnY, float64(nz)+0.5)
	ent.stand.Yaw = entity.ArmorStandPlacementYaw(c.player.Position().Yaw)
	c.srv.applyArmorStandItemData(ent, stack)
	c.useSwing = swingPredicted
	c.srv.registerArmorStand(ent)
	if !c.srv.bodySpawned(ent.uuid, plugin.SpawnDefault) {
		return true, nil
	}
	c.gameEventAt(rules.GameEventEntityPlace, ent.stand.X, ent.stand.Y, ent.stand.Z, 0)
	// The place sound reaches the players within its earshot, the placer
	// included. vanilla: ArmorStandItem.useOn, SoundEvents.ARMOR_STAND_PLACE
	c.srv.broadcastSoundID(w, soundArmorStandPlace, protocol.SoundCategoryBlock,
		ent.stand.X, ent.stand.Y, ent.stand.Z, 0.75, 0.8)
	c.awardUsedHeld(hand)
	// The stand is spent outside creative. vanilla: ItemStack.consume
	if c.player.GameMode() != protocol.GameModeCreative {
		if err := c.consumeHeld(hand); err != nil {
			return true, err
		}
	}
	return true, nil
}

// boxTaken reports whether any non-spectator body's box overlaps the box a
// placement would fill — vanilla's Level.getEntities(null, box).isEmpty().
// Each entity list is walked under its own lock alone.
func (s *Server) boxTaken(w *world.World, box entity.AABB) bool {
	s.mu.RLock()
	clients := s.clientsLocked()
	s.mu.RUnlock()
	for _, c := range clients {
		if c.theWorld() != w || c.player.GameMode() == protocol.GameModeSpectator {
			continue
		}
		pos := c.player.Position()
		d := c.dims(c.player.Appearance().Pose)
		if box.Crosses(entity.NewAABB(pos.X-d.Width/2, pos.Y, pos.Z-d.Width/2,
			pos.X+d.Width/2, pos.Y+d.Height, pos.Z+d.Width/2)) {
			return true
		}
	}

	s.mobsMu.Lock()
	for _, m := range s.herdOf(w).herd {
		if box.Crosses(entity.NewAABB(m.BlastBox())) {
			s.mobsMu.Unlock()
			return true
		}
	}
	s.mobsMu.Unlock()

	s.armorStandsMu.Lock()
	for _, ent := range s.armorStands {
		if ent.w != w || ent.stand.Dead {
			continue
		}
		if box.Crosses(entity.NewAABB(ent.stand.BlastBox())) {
			s.armorStandsMu.Unlock()
			return true
		}
	}
	s.armorStandsMu.Unlock()

	if s.endCrystalBoxTaken(w, box) {
		return true
	}
	return s.looseBoxTaken(w, box) || s.cloudBoxTaken(w, box)
}

// looseBoxTaken is boxTaken's half over the items, orbs, TNT, falling
// blocks, boats and minecarts.
func (s *Server) looseBoxTaken(w *world.World, box entity.AABB) bool {
	s.itemsMu.Lock()
	for _, it := range s.items {
		if it.w == w && !it.item.Dead && box.Crosses(entity.NewAABB(it.item.BlastBox())) {
			s.itemsMu.Unlock()
			return true
		}
	}
	s.itemsMu.Unlock()
	s.orbsMu.Lock()
	for _, it := range s.orbs {
		if it.w == w && !it.orb.Dead && box.Crosses(entity.NewAABB(it.orb.BlastBox())) {
			s.orbsMu.Unlock()
			return true
		}
	}
	s.orbsMu.Unlock()
	s.tntMu.Lock()
	for _, it := range s.tnts {
		if it.w == w && !it.tnt.Dead && box.Crosses(entity.NewAABB(it.tnt.BlastBox())) {
			s.tntMu.Unlock()
			return true
		}
	}
	s.tntMu.Unlock()
	s.fallingMu.Lock()
	for _, it := range s.fallings {
		if it.w == w && !it.fb.Dead && box.Crosses(entity.NewAABB(it.fb.BlastBox())) {
			s.fallingMu.Unlock()
			return true
		}
	}
	s.fallingMu.Unlock()
	s.boatsMu.Lock()
	for _, it := range s.boats {
		if it.w == w && !it.boat.Dead && box.Crosses(entity.NewAABB(it.boat.BlastBox())) {
			s.boatsMu.Unlock()
			return true
		}
	}
	s.boatsMu.Unlock()
	s.minecartsMu.Lock()
	defer s.minecartsMu.Unlock()
	for _, it := range s.minecarts {
		if it.w == w && !it.cart.Dead && box.Crosses(entity.NewAABB(it.cart.BlastBox())) {
			return true
		}
	}
	return false
}

// applyArmorStandItemData applies an armor stand item's own data to a
// stand being placed: the custom_name component's name first, the
// entity_data compound's fields after, so an entity_data CustomName wins.
// entity_data that names a type other than armor_stand is ignored.
// vanilla: EntityType.createDefaultStackConfig
func (s *Server) applyArmorStandItemData(ent *armorStandEntity, stack protocol.ItemStack) {
	ent.custom = customNameOfStack(stack)
	if compound := itemEntityData(stack, protocol.EntityTypeArmorStand); compound != nil {
		custom := customNameOfTag(compound)
		// Item entity_data is merged over the entity's component-derived name.
		if _, named := compound["CustomName"]; !named {
			custom.wire = ent.custom.wire
		}
		ent.custom = custom
		applyArmorStandFields(ent, compound)
	}
}

// attackArmorStand handles a swing at a stand: a creative hand takes it
// down at once, a survival one shakes it and — punched again within the
// window — takes it down with its drops; the blow's sounds, sweep, wear, use count
// and exhaustion follow as against a mob. An invisible or marker stand ignores the
// hit, an invulnerable one refuses a survival hand's punch whole, and a
// player who may not build cannot break one. handled is true when the
// target was a stand, in reach or not.
// vanilla: Player.attack, ArmorStand.hurtServer
func (c *client) attackArmorStand(target int32) bool {
	s := c.srv
	s.armorStandsMu.Lock()
	ent := s.armorStandByIDLocked(c.theWorld(), target)
	if ent == nil {
		s.armorStandsMu.Unlock()
		return false
	}
	// Out of reach spends nothing; the swing was still spent on the stand
	// and not on some mob behind it.
	mode := c.player.GameMode()
	if ent.stand.IgnoresPunch(mode == protocol.GameModeAdventure) || !c.armorStandWithinReach(ent, true) {
		s.armorStandsMu.Unlock()
		return true
	}
	// vanilla: ArmorStand.skipAttackInteraction
	if !c.mayInteractAt(int32(math.Floor(ent.stand.X)), int32(math.Floor(ent.stand.Z))) {
		s.armorStandsMu.Unlock()
		return true
	}
	creative := mode == protocol.GameModeCreative
	held := c.heldItem(protocol.HandMain)
	if s.entityDamageListened() {
		id := ent.uuid
		s.armorStandsMu.Unlock()
		_, ok := s.askNonlivingDamage(c.theWorld(), id, meleeDamageType(held), &damageCause{entityID: c.player.EntityID}, 1)
		s.armorStandsMu.Lock()
		// The swing is spent on a refused or vanished stand all the same.
		if !ok || s.armorStandByIDLocked(c.theWorld(), target) != ent {
			s.armorStandsMu.Unlock()
			return true
		}
	}
	outcome := ent.stand.Punch(int64(s.ticksRun.Load()), creative)
	victim := smashVictim{id: ent.stand.ID, x: ent.stand.X, y: ent.stand.Y, z: ent.stand.Z,
		onGround: ent.stand.OnGround, loot: snapshotLoot(s.armorStandSnapshot(ent, nil))}
	bx0, by0, bz0, bx1, by1, bz1 := ent.stand.BlastBox()
	victim.halfWidth = (bx1 - bx0) / 2
	var shake []byte
	switch outcome {
	case entity.PunchBroke:
		s.removeArmorStandLocked(ent)
	case entity.PunchShaken:
		shake = entityEventPayload(ent.stand.ID, protocol.EntityStatusArmorStandShake)
	}
	s.armorStandsMu.Unlock()
	if outcome == entity.PunchRefused {
		// The refused hurt lands nothing, and vanilla's Player.attack plays
		// the no-damage sound for a blow that did not land.
		c.attackSound(soundAttackNoDamage)
		return true
	}
	c.standBlowFeedback(held, victim.id, victim.loot, bx0, by0, bz0, bx1, by1, bz1)
	if outcome == entity.PunchBroke {
		// A creative hand's punch leaves the burst and the sound behind
		// but no drops; a survival break lays the item and the equipment.
		brk := armorStandBreak{particles: true}
		if !creative {
			brk.item, brk.equipment = true, true
		}
		s.breakArmorStandTail(ent, brk)
		c.smashStand(victim)
		c.standBlowCosts(held)
		return true
	}
	if shake != nil {
		s.broadcastWorld(c.theWorld(), protocol.IDPlayEntityEvent, shake)
		s.gameEvent(c.theWorld(), rules.GameEventEntityDamage, victim.x, victim.y, victim.z, c.gameEventContext(0))
	}
	c.smashStand(victim)
	c.standBlowCosts(held)
	return true
}

// standBlowFeedback plays a landed blow's knockback, sweep and sounds on a
// stand; the swing packet spends the cooldown, and smashStand the smash.
// vanilla: Player.attack
func (c *client) standBlowFeedback(held protocol.ItemStack, standID int32, loot func() *rules.LootEntity, minX, minY, minZ, maxX, maxY, maxZ float64) {
	strength := rules.AttackStrengthScale(int(c.attackStrengthTicks.Load()), c.attackSpeed())
	ctx := c.attackEnchContext(meleeDamageType(held), protocol.EntityTypeArmorStand, loot)
	attackDamage := c.attackDamage()
	plain := rules.AttackDamage(attackDamage, false, 1)
	magic := (rules.EnchantedDamage(held, &ctx, plain) - plain) * strength
	fx := c.blowFeedback(held, strength, c.canCriticalAttack(true), magic)
	c.endSprintAfterKnockback(rules.AttackKnockback(rules.EnchantedKnockback(held, &ctx, c.attackKnockbackBase()), fx.Knockback))
	if fx.Sweep {
		c.sweepHurt(c.theWorld(), held, attackDamage, standID, minX, minY, minZ, maxX, maxY, maxZ, strength)
	}
	c.landedBlowFeedback(fx, standID)
}

// standBlowCosts charges the weapon's use count, wear and the exhaustion of
// a blow that hurt a stand. vanilla: Player.itemAttackInteraction, causeFoodExhaustion
func (c *client) standBlowCosts(held protocol.ItemStack) {
	c.awardWeaponHit(held)
	c.wearHeld(protocol.HandMain, registry.StackDamagePerAttack(held))
	c.exhaust(rules.AttackExhaustion)
}

// sweepStands punches every stand a sword's sweep reaches: the shake or the
// break a hand's own punch would land, none for a marker, an invisible
// stand or a hand that may not build. reaches decides which stands lie in
// the sweep; a stand that only shakes is shoved along yaw.
// vanilla: Player.doSweepAttack, ArmorStand.hurtServer
func (c *client) sweepStands(yaw float64, reaches func(ent *armorStandEntity) bool) {
	s := c.srv
	w := c.theWorld()
	mode := c.player.GameMode()
	creative := mode == protocol.GameModeCreative
	now := int64(s.ticksRun.Load())
	var broke []*armorStandEntity
	var shaken []int32
	var shakenAt [][3]float64
	s.armorStandsMu.Lock()
	for _, ent := range slices.Clone(s.armorStands) {
		if ent.w != w || ent.stand.Dead || ent.stand.IgnoresPunch(mode == protocol.GameModeAdventure) || !reaches(ent) {
			continue
		}
		switch ent.stand.Punch(now, creative) {
		case entity.PunchBroke:
			s.removeArmorStandLocked(ent)
			broke = append(broke, ent)
		case entity.PunchShaken:
			shaken = append(shaken, ent.stand.ID)
			shakenAt = append(shakenAt, [3]float64{ent.stand.X, ent.stand.Y, ent.stand.Z})
			ent.stand.KnockbackAlong(yaw, rules.SweepShove)
		}
	}
	s.armorStandsMu.Unlock()
	for _, ent := range broke {
		s.breakArmorStandTail(ent, armorStandBreak{particles: true, item: !creative, equipment: !creative})
	}
	for i, id := range shaken {
		s.broadcastWorld(w, protocol.IDPlayEntityEvent, entityEventPayload(id, protocol.EntityStatusArmorStandShake))
		s.gameEvent(w, rules.GameEventEntityDamage, shakenAt[i][0], shakenAt[i][1], shakenAt[i][2], c.gameEventContext(0))
	}
}

// smashStand lands the held mace's smash on a stand a punch hurt: any hurt
// that returns true counts as a hit, the shaking one included.
// vanilla: ArmorStand.hurtServer, MaceItem.hurtEnemy
func (c *client) smashStand(v smashVictim) {
	held := c.heldItem(protocol.HandMain)
	fall, smash := c.smashFall(held)
	if smash {
		c.landSmash(c.theWorld(), fall, v)
	}
	c.finishMeleeItemEffects(held, c.attackEnchContext(meleeDamageType(held), protocol.EntityTypeArmorStand, v.loot), fall, smash)
}

// useOnArmorStand right-clicks a stand, vanilla ArmorStand.interact: a held
// item goes into its own slot and only an empty hand uses the clicked slot;
// a locked slot refuses the click. handled is true when the target was a
// stand and the use was consumed, false where the click falls through to
// the plain entity interact.
// vanilla: ArmorStand.interact, swapItem
func (c *client) useOnArmorStand(target int32, hand int32, ox, oy, oz float64) (handled bool, err error) {
	s := c.srv
	asked := false
again:
	s.armorStandsMu.Lock()
	ent := s.armorStandByIDLocked(c.theWorld(), target)
	if ent == nil {
		s.armorStandsMu.Unlock()
		return false, nil
	}
	// Vanilla's handleInteract verifies the body's box against the entity
	// interaction range before the use is read at all: a use past the
	// reach is no use.
	if !c.armorStandWithinReach(ent, false) {
		s.armorStandsMu.Unlock()
		return true, nil
	}
	held := c.heldItem(hand)
	slot, use := ent.stand.UseSlot(held, ox, oy, oz)
	if use != entity.StandUseSwap {
		s.armorStandsMu.Unlock()
		return use == entity.StandUseRefused, nil
	}
	before := ent.stand.StackOf(slot)
	if !asked && c.pluginListening(pluginruntime.PlayerArmorStandManipulate) {
		// Listeners run with no lock held, so the use is read again after.
		asked = true
		uid := ent.uuid
		s.armorStandsMu.Unlock()
		if !c.pluginStandManipulate(uid, slot, held, before) {
			return true, nil
		}
		goto again
	}
	handStack, swapped := ent.stand.SwapItem(slot, held, c.player.GameMode() == protocol.GameModeCreative)
	if !swapped {
		s.armorStandsMu.Unlock()
		return false, nil
	}
	equipment := armorStandEquipment(ent)
	after := ent.stand.StackOf(slot)
	fx := equipFx{x: ent.stand.X, y: ent.stand.Y, z: ent.stand.Z, category: protocol.SoundCategoryNeutral,
		silent: ent.look.silent}
	s.armorStandsMu.Unlock()

	c.replaceHeldWith(hand, handStack)
	s.broadcastWorld(c.theWorld(), protocol.IDPlayEntityEquipment, equipment)
	s.slotEquipFeedback(c.theWorld(), slot, before, after, fx)
	// vanilla: InteractionResult.SUCCESS_SERVER — the use swings the arm.
	c.useSwing = swingServer
	return true, nil
}

// armorStandBreak says what a stand's end leaves behind.
type armorStandBreak struct {
	// item lays the item the stand is, named with the stand's own.
	item bool
	// equipment lays what the stand held and wore.
	equipment bool
	// particles sends the ten oak-plank block particles.
	particles bool
}

// breakArmorStandTail is the half of a stand's end that runs after the
// lock: the removal out of every view, the break sound — which plays
// whatever the game rules — the plank burst where the caller asks, and,
// while block_drops stands, the drops each about the block its end names:
// the stand item at the stand's own cell, the equipment a block above.
// vanilla: ArmorStand.brokenByPlayer, brokenByAnything, showBreakingParticles
func (s *Server) breakArmorStandTail(ent *armorStandEntity, brk armorStandBreak) {
	s.broadcastWorld(ent.w, protocol.IDPlayRemoveEntities,
		removeEntitiesPayload([]int32{ent.stand.ID}))
	s.broadcastSoundID(ent.w, soundArmorStandBreak, protocol.SoundCategoryNeutral,
		ent.stand.X, ent.stand.Y, ent.stand.Z, 1.0, 1.0)
	if brk.particles {
		s.armorStandBreakingParticles(ent)
	}
	if !ent.w.BoolRule(world.RuleBlockDrops) {
		return
	}
	x, y, z := int32(math.Floor(ent.stand.X)), int32(math.Floor(ent.stand.Y)), int32(math.Floor(ent.stand.Z))
	if brk.item {
		if id, ok := entity.ArmorStandItem(); ok {
			stack := protocol.ItemStack{ItemID: id, Count: 1, CustomName: customNameText(ent.custom.tag())}
			s.spawnBlockDrop(ent.w, x, y, z, stack)
		}
	}
	if brk.equipment {
		for _, stack := range ent.stand.EquipmentDrops() {
			s.spawnBlockDrop(ent.w, x, y+1, z, stack)
		}
	}
}

// arrowStandHit is an armor stand a shaft's sweep met: the world it stands
// in, its entity ID, and the shaft itself, so the caller can spend or
// deflect it once the hurt has answered. damageType, when set, is a thrown
// body's hurt, which has no shaft. dropless is a creative shooter's
// hand, which lays nothing; mob is a mob's shaft, which mob_griefing binds;
// noBuild is a player's shaft whose hand may not build.
type arrowStandHit struct {
	hitSound   registry.Sound
	w          *world.World
	standID    int32
	arrow      *arrowEntity
	damageType registry.DamageType
	dropless   bool
	mob        bool
	noBuild    bool
}

// arrowHitArmorStand runs the always-kills hurt a projectile deals a stand,
// ArmorStand.hurtServer's instant break: the body out of the world with the
// item it is and everything it held laid behind it, a creative shooter's
// hand laying nothing. It reports whether the hurt landed and whether it was
// refused; a stand gone from the world between the sweep and the hit is
// neither, the shaft flying on as if it had never met it.
// vanilla: ArmorStand.hurtServer
func (s *Server) arrowHitArmorStand(hit arrowStandHit) (landed, refused bool) {
	s.armorStandsMu.Lock()
	ent := s.armorStandByIDLocked(hit.w, hit.standID)
	if ent == nil {
		s.armorStandsMu.Unlock()
		return false, false
	}
	// A refused hurt still blocks the shaft, which bounces off the stand.
	damageType := hit.damageType
	switch {
	case damageType != 0:
	case hit.arrow != nil && hit.arrow.arrow.Trident:
		damageType = rules.DamageTypeTrident
	default:
		damageType = rules.DamageTypeArrow
	}
	griefingOff := hit.mob && !hit.w.BoolRule(world.RuleMobGriefing)
	if ent.stand.RefusesProjectile(damageType, hit.dropless, hit.noBuild, griefingOff) {
		s.armorStandsMu.Unlock()
		return false, true
	}
	if s.entityDamageListened() {
		id := ent.uuid
		s.armorStandsMu.Unlock()
		cause := &damageCause{}
		if hit.arrow != nil {
			cause.entityID = hit.arrow.arrow.OwnerID
		}
		_, ok := s.askNonlivingDamage(hit.w, id, damageType, cause, 1)
		s.armorStandsMu.Lock()
		if !ok {
			s.armorStandsMu.Unlock()
			return false, true
		}
		if s.armorStandByIDLocked(hit.w, hit.standID) != ent {
			s.armorStandsMu.Unlock()
			return false, false
		}
	}
	s.removeArmorStandLocked(ent)
	s.armorStandsMu.Unlock()
	brk := armorStandBreak{particles: true}
	if !hit.dropless {
		brk.item, brk.equipment = true, true
	}
	s.breakArmorStandTail(ent, brk)
	return true, false
}

// armorStandBreakingParticles sends the ten oak-plank block particles of a
// stand's end.
// vanilla: ArmorStand.showBreakingParticles
func (s *Server) armorStandBreakingParticles(ent *armorStandEntity) {
	x, y, z, dx, dy, dz := ent.stand.BreakingParticles()
	s.sendBlockParticles(ent.w, protocol.ParticleBlock, registry.BlockOakPlanks.DefaultState(), x, y, z, dx, dy, dz, 0.05, 10)
}

// standSends is what a stand pass gathers: the tracker's pairings and
// removals, then the moves, each dropped for a client that has left the
// stand's world by the time it is sent.
type standSends struct {
	trackSends
	told   []outbound
	toldTo []showTo
}

func (o *standSends) send() {
	o.trackSends.send()
	sendShows(o.told, o.toldTo)
}

// trackStand settles every watcher's view of one stand and tells each one
// already shown where it went and which metadata changed. The stands lock is held.
func trackStand(ent *armorStandEntity, watchers []trackingWatcher, near *nearbyPlayers, out *standSends) {
	body := standTracker.body(ent)
	now := armorStandMetaOf(ent)
	// A changed synched value asks for a movement tick off the period.
	// vanilla: ServerEntity.sendChanges (SynchedEntityData.isDirty)
	if ent.metaSeen != nil && ent.metaSeen != now {
		ent.needsSync = true
	}
	ent.metaSeen = now
	mt := body.t.stepDue(entity.ArmorStandInterval, ent.takeSync())
	var attributes []byte
	if dirty := ent.stand.Attributes().TakeDirty(); len(dirty) != 0 {
		attributes = attributesPayload(body.id, dirty)
	}
	// move is the pass's Teleport Entity, encoded for the first watcher due it.
	var move []byte
	for _, i := range body.watchers(near) {
		wh := watchers[i]
		if wh.c.theWorld() != ent.w {
			continue
		}
		_, shown := ent.tracked.get(wh.c)
		if told, seen := body.settle(wh.c, wh.x, wh.z, wh.chunkRadius, &out.trackSends); seen && shown {
			if mt.gate {
				standMove(&body, told, wh.c, mt, &move, out)
			}
			if now != told.own {
				data := armorStandMetadataTable(body.id, now, told.own, armorStandMetaTick)
				out.told = append(out.told, outbound{id: protocol.IDPlayEntityMetadata, data: data})
				out.toldTo = append(out.toldTo, showTo{wh.c, body.w})
				told.own = now
			}
			if attributes != nil {
				out.told = append(out.told, outbound{id: protocol.IDPlayUpdateAttributes, data: attributes})
				out.toldTo = append(out.toldTo, showTo{wh.c, body.w})
			}
		}
	}
}

// standMove tells one watcher where the stand went, with a Teleport Entity
// when it moved past the movement threshold, turned, or was teleported
// anywhere new, however short the step. The payload is the same for every
// watcher, so it is encoded into *move once per pass.
// vanilla: ServerEntity.sendChanges
func standMove(v *trackedBody[*armorStandEntity, *armorStandMeta], told *toldBody[*armorStandMeta], c *client, mt moveTick, move *[]byte, out *standSends) {
	now := v.position()
	dx, dy, dz := now.x-told.x, now.y-told.y, now.z-told.z
	jumped := mt.teleported && (dx != 0 || dy != 0 || dz != 0)
	if dx*dx+dy*dy+dz*dz < movedThresholdSq && now.yaw == told.yaw && !jumped {
		return
	}
	told.trackedPosition = now
	if *move == nil {
		st := v.b.stand
		var buf protocol.Buffer
		buf.Grow(teleportEntityLen)
		putAbsolute(&buf, v.id, bodyPose{x: st.X, y: st.Y, z: st.Z, yaw: st.Yaw}, mt.stepTicks, st.OnGround)
		*move = buf.Bytes()
	}
	out.told = append(out.told, outbound{id: protocol.IDPlayTeleportEntity, data: *move})
	out.toldTo = append(out.toldTo, showTo{c, v.w})
}

// tickArmorStandTracking settles the view of every stand a frozen world
// holds, teleports included.
// vanilla: ChunkMap.tick
func (s *Server) tickArmorStandTracking(watchers []trackingWatcher) {
	near := nearbyTracking(watchers)
	defer near.release()
	var out standSends
	s.lockTick(&s.armorStandsMu)
	for _, ent := range s.armorStands {
		if !ent.stand.Dead {
			trackStand(ent, watchers, near, &out)
		}
	}
	s.unlockTick(&s.armorStandsMu)
	out.send()
}

// tickArmorStands runs one game tick of every stand: the body steps — the
// water's push, the fire's bite, the fall — under the lock, and the sends
// run after it: the break of a stand whose health ran out, the removal of
// one that fell out of the world, the pairing of a watcher that walked into
// range, the removal of one that walked out, and the teleport of a stand a
// push moved. No send holds a store's lock.
func (s *Server) tickArmorStands() {
	watchers := s.trackingWatchers()
	near := nearbyTracking(watchers)
	defer near.release()

	var (
		broken []*armorStandEntity
		// voided are the stands that fell out of the world: removed with
		// nothing laid down.
		voided []*armorStandEntity
		out    standSends
		gates  []gatewayTouch
		// travels are the stands the portal pass caught.
		travels []armorStandPortalTravel
	)
	var located []standLocated
	var bodyLandings []bodyLanding

	s.lockTick(&s.armorStandsMu)
	alive := s.armorStands[:0]
	for _, ent := range s.armorStands {
		if ent.stand.Dead {
			standTracker.unfile(ent)
			continue // a punch or a blast took it out; its tail ran there
		}
		// The portal pass runs ahead of the step, as for items; only a stand
		// with physics is inside a block. vanilla: ArmorStand.hasPhysics
		state := airBlockState
		if !ent.stand.Marker && !ent.stand.NoGravity {
			state = ent.w.BlockAt(rules.BlockOf(ent.stand.X), rules.BlockOf(ent.stand.Y), rules.BlockOf(ent.stand.Z))
		}
		s.notePortalTouch(nil, ent.uuid, ent.w, ent.stand.X, ent.stand.Y, ent.stand.Z, state)
		if kind, due := ent.portal.Step(state, rules.PortalTransitionTime, portalCooldownTicks); due {
			standTracker.unfile(ent)
			travels = append(travels, armorStandPortalTravel{ent: ent, kind: kind})
			continue
		}
		from := ent.stand.Box()
		if ev := ent.stand.Step(ent.w); len(ev) > 0 {
			torn := false
			for _, item := range ev {
				switch item.Kind {
				case entity.ArmorStandVoided:
					voided = append(voided, ent)
					torn = true
				case entity.ArmorStandBroke:
					// The health the tick took ran out; the tail — the removal,
					// the sound, the drops — runs once the lock is given back.
					broken = append(broken, ent)
					torn = true
				case entity.ArmorStandLocated:
					located = append(located, standLocated{ent: ent, x: item.X, y: item.Y, z: item.Z, changed: item.Level})
				}
			}
			if torn {
				standTracker.unfile(ent)
				continue
			}
		}
		if fall := ent.stand.LandedFall; fall > 0 {
			ent.stand.LandedFall = 0
			bodyLandings = append(bodyLandings, bodyLanding{w: ent.w, x: ent.stand.X, y: ent.stand.Y, z: ent.stand.Z,
				halfWidth: ent.stand.HalfWidth(), stand: true, silent: ent.look.silent, fall: fall})
		}
		ent.onFire = ent.stand.FireTicks > 0 || ent.look.visualFire
		// ArmorStand.hasPhysics gates all inside-block effects.
		if !ent.stand.Marker && !ent.stand.NoGravity {
			if gx, gy, gz, ok := entity.EndGatewayInside(ent.w, from, ent.stand.Box()); ok {
				gates = append(gates, gatewayTouch{
					w: ent.w, x: gx, y: gy, z: gz, mu: &s.armorStandsMu,
					portal: &ent.portal, cooldown: portalCooldownTicks,
					move: func(to player.Position) { s.gatewayMoveArmorStand(ent, to) },
				})
			}
		}
		alive = append(alive, ent)
		trackStand(ent, watchers, near, &out)
	}
	s.armorStands = alive
	s.unlockTick(&s.armorStandsMu)
	s.runBodyLandings(bodyLandings)

	var disks []mobDisk
	for _, l := range located {
		disks = append(disks, s.standLocated(l)...)
	}
	s.applyDisks(disks)

	for _, ent := range broken {
		s.breakArmorStandTail(ent, armorStandBreak{equipment: true})
	}
	for _, ent := range voided {
		s.broadcastWorld(ent.w, protocol.IDPlayRemoveEntities,
			removeEntitiesPayload([]int32{ent.stand.ID}))
	}
	out.send()
	s.touchGateways(gates)
	for _, tr := range travels {
		s.finishArmorStandTravel(tr.ent, tr.kind)
	}
}

// armorStandPortalTravel is one stand the portal pass caught.
type armorStandPortalTravel struct {
	ent  *armorStandEntity
	kind rules.PortalKind
}

// finishArmorStandTravel sends one stand the portal pass caught across its
// portal, equipment and all.
func (s *Server) finishArmorStandTravel(ent *armorStandEntity, kind rules.PortalKind) {
	standTracker.crossPortal(s, ent, kind, s.registerArmorStand, func(to *world.World, at player.Position) (*armorStandEntity, bool) {
		twin := armorStandOfTag(s, to, armorStandTag(ent).Entries())
		if twin == nil {
			return nil, false
		}
		twin.stand.SetPos(at.X, at.Y, at.Z)
		twin.stand.Yaw = at.Yaw
		return twin, true
	})
}

// summonArmorStand stands a /summon's stand up at the position given: a
// tagged one through the same reader a saved stand's wake reads — the
// command's position the one it ends at, the tag's Pos overridden on the
// way — and a bare one at its defaults. A tag whose UUID is already a
// live stand's is refused, the caller answering with vanilla's
// duplicate-UUID message.
func (s *Server) summonArmorStand(w *world.World, x, y, z float64, tag nbt.Compound, reason plugin.SpawnReason) (*armorStandEntity, error) {
	if woken, fresh := s.wakeChunkOnce(s.dimensionOfWorld(w),
		world.ChunkPos{X: world.ChunkOf(x), Z: world.ChunkOf(z)}); fresh {
		s.registerWake(woken)
	}
	compound := summonTag(tag, armorStandEntityID, x, y, z)
	// The chunk the stand stands in is loaded for it, the way every
	// summon's is.
	pos := world.ChunkPos{X: world.ChunkOf(x), Z: world.ChunkOf(z)}
	w.Acquire(pos)
	ent := armorStandOfTag(s, w, compound)
	w.Release(pos)
	if ent == nil {
		return nil, errNotSimulated
	}
	ent.stand.SetPos(x, y, z)
	s.armorStandsMu.Lock()
	dup := false
	for _, live := range s.armorStands {
		if live.uuid == ent.uuid {
			dup = true
			break
		}
	}
	s.armorStandsMu.Unlock()
	if dup {
		return nil, command.ErrDuplicateUUID
	}
	ent.spawnReason = spawnReasonCode(reason)
	s.registerArmorStand(ent)
	s.bodySpawned(ent.uuid, reason)
	return ent, nil
}

// killArmorStand discards a stand the way the /kill command and vanilla's
// Entity.kill do: out of the world with nothing laid down behind it.
func (s *Server) killArmorStand(ent *armorStandEntity) {
	s.armorStandsMu.Lock()
	ent.stand.Dead = true
	s.removeArmorStandLocked(ent)
	s.armorStandsMu.Unlock()
	s.broadcastWorld(ent.w, protocol.IDPlayRemoveEntities,
		removeEntitiesPayload([]int32{ent.stand.ID}))
}

// armorStandTwinAt builds the stand that continues this one in another
// dimension, standing at pos: a new stand — new ID, the same UUID — raised
// from the old one's entity tag, which keeps its flags, pose, equipment,
// health, name and tags. The caller holds the stands lock or has taken the
// stand out of the list.
// vanilla: Entity.teleportCrossDimension
func (s *Server) armorStandTwinAt(ent *armorStandEntity, to *dimension, pos player.Position) *armorStandEntity {
	twin := armorStandOfTag(s, to.w, armorStandTag(ent).Entries())
	if twin == nil {
		return nil
	}
	twin.stand.SetPos(pos.X, pos.Y, pos.Z)
	return twin
}

// teleportArmorStand puts a stand somewhere else, facing to.Yaw, and leaves
// the telling to the tracking pass. A /tp (command) also lands it on the
// ground, gravity or not.
// vanilla: Entity.teleportTo, TeleportCommand.performTeleport, ServerEntity.sendChanges
func (s *Server) teleportArmorStand(ent *armorStandEntity, to player.Position, by string, command bool) {
	standTracker.teleport(s, ent, func() {
		ent.stand.SetPos(to.X, to.Y, to.Z)
		ent.stand.VX, ent.stand.VY, ent.stand.VZ = 0, 0, 0
		ent.stand.Yaw = to.Yaw
		if command {
			ent.stand.OnGround = true
		}
	})
	logTeleported("armor stand", to, by)
}

// logTeleported logs an entity teleport at debug level. It checks the level
// first so the arguments are not boxed when debug logging is off.
func logTeleported(kind string, to player.Position, by string, extra ...any) {
	if !slog.Default().Enabled(context.Background(), slog.LevelDebug) {
		return
	}
	slog.Debug(kind+" teleported", append([]any{"x", to.X, "y", to.Y, "z", to.Z, "by", by}, extra...)...)
}

// nameArmorStand gives a stand in reach the name of a name tag, reporting
// whether it did.
// vanilla: NameTagItem.interactLivingEntity
func (c *client) nameArmorStand(target int32, held protocol.ItemStack) bool {
	s := c.srv
	s.armorStandsMu.Lock()
	ent := s.armorStandByIDLocked(c.theWorld(), target)
	if ent == nil || !c.armorStandWithinReach(ent, false) {
		s.armorStandsMu.Unlock()
		return false
	}
	was := ent.custom
	ent.custom = customNameOfStack(held)
	ent.custom.visible = was.visible
	var b protocol.Buffer
	b.PutVarInt(ent.stand.ID)
	ent.custom.putMetadata(&b, was)
	b.EndMetadata()
	s.armorStandsMu.Unlock()
	s.broadcastWorld(c.theWorld(), protocol.IDPlayEntityMetadata, b.Bytes())
	return true
}
