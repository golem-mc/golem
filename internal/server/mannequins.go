package server

import (
	"math/rand/v2"
	"slices"
	"sync"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/advancement"
	"github.com/golem-mc/golem/internal/server/command"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
)

const mannequinEntityID = "minecraft:mannequin"

// mannequinEntity is one mannequin in a world. Guarded by the mannequins
// lock, taken beneath no other lock.
type mannequinEntity struct {
	mannequin *entity.Mannequin
	w         *world.World
	uuid      uuid.UUID
	bodyTrack[struct{}]
	tags   nbt.StringSet
	custom customName
	look   entityLook
	// sync is the state of each mannequinFields entry, by position.
	sync  []mannequinSynched
	shown mannequinShown
}

// mannequinTracker tracks mannequin travel and commanded teleports.
var mannequinTracker = trackedKind[*mannequinEntity, struct{}]{
	invalidatePiston: func(dim *dimension) { dim.pistons.mannequins.built = false },
	pistonBit:        pistonMannequins,
	admitsType:       func(typ string) bool { return typ == mannequinEntityID },
	steppedPath:      true,
	frozenSchedule:   true,
	rangeChunks:      entity.MannequinTrackingRange,
	interval:         entity.MannequinInterval,
	lock:             func(s *Server) *sync.Mutex { return &s.mannequinsMu },
	list:             func(s *Server) *[]*mannequinEntity { return &s.mannequins },
	chunks:           func(s *Server) *entity.ChunkIndex[*mannequinEntity] { return &s.mannequinChunks },
	filing:           func(it *mannequinEntity) *entity.Filing { return &it.mannequin.Filing },
	uuid:             func(it *mannequinEntity) uuid.UUID { return it.uuid },
	track:            func(ent *mannequinEntity) *bodyTrack[struct{}] { return &ent.bodyTrack },
	world:            func(ent *mannequinEntity) *world.World { return ent.w },
	id:               func(ent *mannequinEntity) int32 { return ent.mannequin.ID },
	dead:             func(ent *mannequinEntity) bool { return ent.mannequin.Dead },
	pose: func(ent *mannequinEntity) bodyPose {
		m := ent.mannequin
		return bodyPose{x: m.X, y: m.Y, z: m.Z, yaw: m.Yaw, pitch: m.Pitch, onGround: m.OnGround}
	},
	pairing:         mannequinPairing,
	delta:           mannequinDelta,
	dirtyAttributes: mannequinDirtyAttributes,
	motion: func(ent *mannequinEntity) (float64, float64, float64) {
		return ent.mannequin.VX, ent.mannequin.VY, ent.mannequin.VZ
	},
}

// mannequinDelta collects the changed ticking fields, and asks for a movement
// tick off the period when one changed.
// vanilla: ServerEntity.sendChanges
func mannequinDelta(ent *mannequinEntity) []byte {
	metadata := mannequinTickMetadata(ent)
	if metadata != nil {
		ent.needsSync = true
	}
	return metadata
}

func mannequinDirtyAttributes(ent *mannequinEntity) []byte {
	if attrs := ent.mannequin.Attributes.TakeDirty(); len(attrs) > 0 {
		return attributesPayload(ent.mannequin.ID, attrs)
	}
	return nil
}

// mannequinPairing introduces a mannequin with its metadata and equipment.
func mannequinPairing(ent *mannequinEntity) []outbound {
	m := ent.mannequin
	refreshMannequinSynched(ent)
	spawn := spawnEntityPayload(spawnEntity{
		id: m.ID, uuid: ent.uuid, typ: protocol.EntityTypeMannequin,
		x: m.X, y: m.Y, z: m.Z, vx: ent.sentVX, vy: ent.sentVY, vz: ent.sentVZ,
		pitch: encodeAngle(m.Pitch), yaw: encodeAngle(m.Yaw), headYaw: encodeAngle(m.Yaw),
	})
	packets := spawnAndMetadata(spawn, mannequinMetadataPayload(ent, false))
	packets = append(packets, outbound{id: protocol.IDPlayUpdateAttributes, data: attributesPayload(m.ID, m.Attributes.Syncable())})
	var used [6]int
	n := 0
	for slot, stack := range [...]*protocol.ItemStack{&m.MainHand, &m.OffHand, &m.Worn[0], &m.Worn[1], &m.Worn[2], &m.Worn[3]} {
		if !stack.Empty() {
			used[n] = slot
			n++
		}
	}
	if n > 0 {
		slots := mannequinEquipmentNow(m)
		packets = append(packets, outbound{id: protocol.IDPlayEntityEquipment, data: equipmentEntriesPayload(m.ID, slots[:], used[:n])})
	}
	return packets
}

// registerMannequin puts a built mannequin into the world and shows it to
// every player in range.
func (s *Server) registerMannequin(ent *mannequinEntity) {
	ent.mannequin.Combust = func(ticks int, src entity.CombustSource) int {
		return s.askMannequinCombust(ent, ticks, src)
	}
	// The tracker's baseline starts at the spawn velocity, so a later stop is a change.
	// vanilla: ServerEntity.<init>
	ent.sentVX, ent.sentVY, ent.sentVZ = ent.mannequin.VX, ent.mannequin.VY, ent.mannequin.VZ
	mannequinTickMetadata(ent) // seeds the baseline the tracking pass compares against
	mannequinTracker.register(s, ent)
}

// tickMannequins reads push inputs, steps under the mannequin lock, then applies
// damage, removals and movement broadcasts with the lock released.
func (s *Server) tickMannequins() {
	s.lockTick(&s.mannequinsMu)
	none := len(s.mannequins) == 0
	s.unlockTick(&s.mannequinsMu)
	if none {
		return
	}
	teams := s.pushTeams()
	pushes := s.mannequinPushInputs(teams)
	// Tracking precedes physics, as in ServerChunkCache.tick before the entity tick.
	// vanilla: ServerChunkCache.tick, ChunkMap.tick
	mannequinTracker.pass(s, s.trackingWatchers(), true)
	var effects []mannequinTickEffect
	s.lockTick(&s.mannequinsMu)
	for _, ent := range s.mannequins {
		step := ent.mannequin.Step(ent.w)
		var hurts []mannequinHurt
		drowned, extinguished := ent.mannequin.EnvironmentStep(ent.w, func(amount float64, typ registry.DamageType) {
			if fx, landed := s.hurtMannequinLocked(ent, amount, typ, 0, 0, nil, false, nil); landed {
				hurts = append(hurts, fx)
			}
		})
		if len(hurts) > 0 || drowned || extinguished {
			effects = append(effects, mannequinTickEffect{ent: ent, hurts: hurts, drowned: drowned, extinguished: extinguished, pos: player.Position{X: ent.mannequin.X, Y: ent.mannequin.Y, Z: ent.mannequin.Z}})
		}
		pushes.worlds[ent.w].push(ent, teams)
		if step.Remove || step.Damage > 0 {
			effects = append(effects, mannequinTickEffect{ent: ent, step: step, pos: player.Position{X: ent.mannequin.X, Y: ent.mannequin.Y, Z: ent.mannequin.Z}})
		}
	}
	s.unlockTick(&s.mannequinsMu)
	s.applyMannequinPushes(pushes)
	for _, fx := range effects {
		if fx.drowned {
			s.broadcastWorld(fx.ent.w, protocol.IDPlayEntityEvent, entityEventPayload(fx.ent.mannequin.ID, protocol.EntityEventDrown))
		}
		if fx.extinguished {
			p := fx.pos
			s.broadcastSoundID(fx.ent.w, registry.SoundEntityGenericExtinguishFire, protocol.SoundCategoryNeutral, p.X, p.Y, p.Z, 0.7, 1.6+(rand.Float32()-rand.Float32())*0.4)
		}
		for _, hurt := range fx.hurts {
			s.tellMannequinHurt(hurt)
		}
		if fx.step.Remove {
			s.broadcastWorld(fx.ent.w, protocol.IDPlayEntityEvent,
				entityEventPayload(fx.ent.mannequin.ID, protocol.EntityStatusDeathPoof))
			s.killMannequin(fx.ent)
		} else if fx.step.Damage > 0 {
			if fx.step.DamageType == rules.DamageTypeFall {
				p := fx.pos
				s.broadcastSoundID(fx.ent.w, rules.FallSound(fx.step.Damage, rules.FallGeneric), protocol.SoundCategoryNeutral, p.X, p.Y, p.Z, 1, 1)
				if sound, volume, pitch, ok := blockFallSound(fx.ent.w, p.X, p.Y, p.Z); ok {
					s.broadcastSoundID(fx.ent.w, sound, protocol.SoundCategoryNeutral, p.X, p.Y, p.Z, volume, pitch)
				}
			}
			s.hurtMannequin(fx.ent, fx.step.Damage, fx.step.DamageType, 0, 0, nil, false)
		}
	}
}

type mannequinTickEffect struct {
	drowned, extinguished bool
	hurts                 []mannequinHurt
	ent                   *mannequinEntity
	step                  entity.MannequinStep
	pos                   player.Position
}

// hurtMannequin lands amount of damageType on a mannequin and tells its
// watchers: the health, and for a whole hit the damage event and the hurt
// or death sound; a death also its entity event and dying pose. causeID and
// directID are the damage event's entities as entity ID + 1, zero for none,
// and at its source position, nil for none; creative is the cause being a
// creative player. landed is false when nothing landed.
// vanilla: LivingEntity.hurtServer, die
func (s *Server) hurtMannequin(ent *mannequinEntity, amount float64, damageType registry.DamageType, causeID, directID int32, at *[3]float64, creative bool) (landed bool) {
	return s.hurtMannequinBy(ent, amount, damageType, causeID, directID, at, creative, nil)
}

type mannequinKnockback struct {
	x, z, yaw, extra float64
}

func (s *Server) hurtMannequinBy(ent *mannequinEntity, amount float64, damageType registry.DamageType, causeID, directID int32, at *[3]float64, creative bool, shove *mannequinKnockback) bool {
	_, landed := s.hurtMannequinFx(ent, amount, damageType, causeID, directID, at, creative, shove)
	return landed
}

// hurtMannequinFx is hurtMannequinBy that also returns the hurt it told.
func (s *Server) hurtMannequinFx(ent *mannequinEntity, amount float64, damageType registry.DamageType, causeID, directID int32, at *[3]float64, creative bool, shove *mannequinKnockback) (mannequinHurt, bool) {
	s.mannequinsMu.Lock()
	fx, landed := s.hurtMannequinLocked(ent, amount, damageType, causeID, directID, at, creative, shove)
	s.mannequinsMu.Unlock()
	if landed {
		s.tellMannequinHurt(fx)
	}
	return fx, landed
}

// mannequinVictim is the mannequin as a player_hurt_entity victim.
func (s *Server) mannequinVictim(ent *mannequinEntity, fx mannequinHurt, attacker, direct func() rules.LootEntity, indirect bool) advancement.Entity {
	return advancement.Entity{Type: mannequinEntityID, Loot: func() rules.LootEntity {
		return rules.LootEntity{Type: mannequinEntityID, ScoreHolder: ent.uuid.String()}
	}, Blow: &advancement.Blow{Dealt: fx.dealt, Taken: fx.taken, Type: fx.typ, Attacker: attacker, Direct: direct, Indirect: indirect}}
}

type mannequinHurt struct {
	w                 *world.World
	id, cause, direct int32
	typ               registry.DamageType
	at                *[3]float64
	x, y, z           float64
	softened, died    bool
	// dealt is the blow before armour, taken the health it removed.
	dealt, taken float64
}

// hurtMannequinLocked changes the body; tellMannequinHurt sends after unlocking.
func (s *Server) hurtMannequinLocked(ent *mannequinEntity, amount float64, damageType registry.DamageType, causeID, directID int32, at *[3]float64, creative bool, shove *mannequinKnockback) (mannequinHurt, bool) {
	m := ent.mannequin
	if m.InvulnerableTo(damageType, creative) {
		return mannequinHurt{}, false
	}
	before := m.Health
	landed, softened, died := m.HurtBy(amount, damageType, damageType.InTag(registry.DamageTypeTagIDBypassesCooldown))
	if !landed {
		return mannequinHurt{}, false
	}
	if !softened && shove != nil && !damageType.InTag(registry.DamageTypeTagIDNoKnockback) {
		m.KnockbackFrom(shove.x, shove.z, 0.4)
		m.KnockbackAlong(shove.yaw, shove.extra)
	}
	// The tracking pass sends the health and a dying pose after the damage
	// and death events.
	return mannequinHurt{w: ent.w, id: m.ID, cause: causeID, direct: directID, typ: damageType, at: at, x: m.X, y: m.Y, z: m.Z, softened: softened, died: died, dealt: amount, taken: before - m.Health}, true
}

func (s *Server) tellMannequinHurt(fx mannequinHurt) {
	if !fx.softened {
		s.broadcastWorld(fx.w, protocol.IDPlayDamageEvent, damageEventPayloadAt(fx.id, fx.typ, fx.cause, fx.direct, fx.at))
		sound := registry.SoundEntityGenericHurt
		if fx.died {
			sound = registry.SoundEntityGenericDeath
		}
		// vanilla: LivingEntity.getVoicePitch
		s.broadcastSoundID(fx.w, sound, protocol.SoundCategoryNeutral, fx.x, fx.y, fx.z, 1, (rand.Float32()-rand.Float32())*0.2+1)
	}
	if fx.died {
		s.broadcastWorld(fx.w, protocol.IDPlayEntityEvent, entityEventPayload(fx.id, protocol.EntityStatusDeath))
	}
}

// mannequinByIDLocked is the live mannequin with the entity ID in w. The
// mannequin lock is held.
func (s *Server) mannequinByIDLocked(w *world.World, id int32) *mannequinEntity {
	for _, ent := range s.mannequins {
		if ent.mannequin.ID == id && ent.w == w && !ent.mannequin.Dead {
			return ent
		}
	}
	return nil
}

// attackMannequin handles a player's blow at a mannequin: the hurt lands
// through its window, and the blow's sweep, sounds, wear and exhaustion
// follow as against a mob. handled is true when the target was a mannequin, in reach or not.
// vanilla: Player.attack
func (c *client) attackMannequin(target int32) bool {
	s := c.srv
	s.mannequinsMu.Lock()
	ent := s.mannequinByIDLocked(c.theWorld(), target)
	if ent == nil {
		s.mannequinsMu.Unlock()
		return false
	}
	minX, minY, minZ, maxX, maxY, maxZ := ent.mannequin.BlastBox()
	snap := s.mannequinSnapshot(ent, nil)
	s.mannequinsMu.Unlock()
	// Out of reach spends nothing.
	if !c.withinAttackRange(minX, minY, minZ, maxX, maxY, maxZ) {
		return true
	}
	held := c.attackWeapon()
	blow, ok := c.prepareAttack(held, protocol.EntityTypeMannequin, true, snapshotLoot(snap))
	if !ok {
		return true
	}
	pos := c.player.Position()
	extra := rules.AttackKnockback(rules.EnchantedKnockback(held, &blow.context, c.attackKnockbackBase()), blow.feedback.Knockback)
	fx, landed := s.hurtMannequinFx(ent, blow.damage, blow.context.DamageType, c.player.EntityID+1, c.player.EntityID+1, nil,
		c.player.GameMode() == protocol.GameModeCreative, &mannequinKnockback{x: pos.X, z: pos.Z, yaw: float64(pos.Yaw), extra: extra})
	if !landed {
		c.attackSound(soundAttackNoDamage)
		return true
	}
	c.hurtEntityAdvancements(s.mannequinVictim(ent, fx, c.LootEntity, c.LootEntity, false))
	c.endSprintAfterKnockback(extra)
	w := c.theWorld()
	if blow.feedback.Sweep {
		c.sweepHurt(w, held, blow.baseDamage, target, minX, minY, minZ, maxX, maxY, maxZ, blow.strength)
	}
	c.landedBlowFeedback(blow.feedback, target)
	c.finishMeleeItemEffects(held, blow.context, blow.fall, blow.smash)
	c.awardWeaponHit(held)
	c.wearAttackWeapon(registry.StackDamagePerAttack(held))
	c.exhaust(rules.AttackExhaustion)
	return true
}

// killMannequin discards a mannequin as Entity.kill does, with nothing laid
// behind it.
func (s *Server) killMannequin(ent *mannequinEntity) {
	s.mannequinsMu.Lock()
	ent.mannequin.Dead = true
	mannequinTracker.unfile(ent)
	s.mannequins = slices.DeleteFunc(s.mannequins, func(o *mannequinEntity) bool { return o == ent })
	s.mannequinsMu.Unlock()
	s.entityDestroyed(ent.uuid)
	s.broadcastWorld(ent.w, protocol.IDPlayRemoveEntities,
		removeEntitiesPayload([]int32{ent.mannequin.ID}))
}

func (s *Server) mannequinSnapshot(ent *mannequinEntity, q *entityQuery) *entitySnapshot {
	typ := mannequinEntityID
	if rejectsEntityHead(q, typ, ent.uuid, ent.tags) {
		return nil
	}
	m := ent.mannequin
	if m.Dead || m.Dying() {
		return nil
	}
	pos := player.Position{X: m.X, Y: m.Y, Z: m.Z, Yaw: m.Yaw, Pitch: m.Pitch}
	e := s.snapshot(q, ent.tags, ent, m.ID, typ, "Mannequin", ent.uuid,
		s.dimensionOfWorld(ent.w).name, pos, box6(m.BlastBox()))
	if e == nil {
		return nil
	}
	e.eye = m.EyeHeight()
	e.bind(&s.mannequinsMu, &ent.tags, &mannequinEntityOps)
	e.setCustomName(ent.custom.tag())
	return e
}

// setMannequinData loads a merged compound onto a mannequin and tells its
// watchers the whole metadata and, when it moved, where it now stands.
// vanilla: Entity.load
func (s *Server) setMannequinData(ent *mannequinEntity, data nbt.Compound) {
	s.mannequinsMu.Lock()
	m := ent.mannequin
	x, y, z, yaw, pitch := m.X, m.Y, m.Z, m.Yaw, m.Pitch
	before := mannequinEquipmentNow(m)
	applyMannequinTag(ent, data)
	if x != m.X || y != m.Y || z != m.Z || yaw != m.Yaw || pitch != m.Pitch {
		ent.teleported = true
	}
	payload := mannequinMetadataPayload(ent, true)
	now := mannequinEquipmentNow(m)
	used := equipmentChanged(before[:], now[:])
	packets := []outbound{{id: protocol.IDPlayEntityMetadata, data: payload}}
	if len(used) > 0 {
		packets = append(packets, outbound{id: protocol.IDPlayEntityEquipment, data: equipmentEntriesPayload(m.ID, now[:], used)})
	}
	if encodeAngle(yaw) != encodeAngle(m.Yaw) {
		packets = append(packets, outbound{id: protocol.IDPlayHeadRotation, data: mannequinHeadPayload(m.ID, m.Yaw)})
	}
	watchers := trackedWatchers(ent.tracked)
	w := ent.w
	s.mannequinsMu.Unlock()
	s.tellWatchers(w, watchers, packets)
}

// mannequinTwinAt is a copy of ent, a new entity with the same UUID, at move.Pos
// in another dimension; nil when the round trip fails.
func (s *Server) mannequinTwinAt(ent *mannequinEntity, to *dimension, move command.Move) *mannequinEntity {
	twin := mannequinOfTag(s, to.w, mannequinTag(ent).Entries())
	if twin == nil {
		return nil
	}
	pos := move.Pos
	m := twin.mannequin
	m.SetPos(pos.X, pos.Y, pos.Z)
	m.Yaw = pos.Yaw
	m.Pitch = pos.Pitch
	if move.Command {
		m.VX, m.VZ = mannequinTeleportMotion(ent.mannequin, move)
	}
	return twin
}

// teleportMannequin puts a mannequin at move.Pos, facing its angles, and
// leaves the move to the tracking pass. Motion along an axis without its Delta
// flag stops. The head turns with the body, so a changed yaw also sends a
// Rotate Head, without which the player model keeps looking the old way. A /tp
// (move.Command) also lands it on the ground, gravity or not.
// vanilla: Entity.teleportSetPosition, PositionMoveRotation.calculateAbsolute, Entity.forceSetRotation, TeleportCommand.performTeleport, ServerEntity.sendChanges
func (s *Server) teleportMannequin(ent *mannequinEntity, move command.Move, by string) {
	to := move.Pos
	var watchers []uuid.UUID
	var turned bool
	var id int32
	mannequinTracker.teleport(s, ent, func() {
		m := ent.mannequin
		turned = encodeAngle(m.Yaw) != encodeAngle(to.Yaw)
		m.SetPos(to.X, to.Y, to.Z)
		m.Yaw = to.Yaw
		m.Pitch = to.Pitch
		m.VX, m.VZ = mannequinTeleportMotion(m, move)
		if !move.DeltaY || move.Command {
			m.VY = 0
		}
		if move.Command {
			m.OnGround = true
		}
		id = m.ID
		watchers = trackedWatchers(ent.tracked)
	})
	if turned {
		s.tellWatchers(ent.w, watchers, []outbound{{id: protocol.IDPlayHeadRotation, data: mannequinHeadPayload(id, to.Yaw)}})
	}
	logTeleported("mannequin", to, by)
}

// vanilla: PositionMoveRotation.calculateDelta
func mannequinTeleportMotion(m *entity.Mannequin, move command.Move) (vx, vz float64) {
	if move.DeltaX {
		vx = m.VX
	}
	if move.DeltaZ {
		vz = m.VZ
	}
	return vx, vz
}

// mannequinHeadPayload is a Rotate Head turning the mannequin's head to yaw.
func mannequinHeadPayload(id int32, yaw float32) []byte {
	var b protocol.Buffer
	b.PutVarInt(id)
	b.PutU8(encodeAngle(yaw))
	return b.Bytes()
}

// mannequinEquipmentNow returns held and worn equipment in wire-slot order.
func mannequinEquipmentNow(m *entity.Mannequin) [6]protocol.ItemStack {
	return [6]protocol.ItemStack{m.MainHand, m.OffHand, m.Worn[0], m.Worn[1], m.Worn[2], m.Worn[3]}
}

// nameMannequin gives a mannequin in reach the name of a name tag, reporting
// whether it did.
// vanilla: NameTagItem.interactLivingEntity
func (c *client) nameMannequin(target int32, held protocol.ItemStack) bool {
	s := c.srv
	s.mannequinsMu.Lock()
	ent := s.mannequinByIDLocked(c.theWorld(), target)
	if ent == nil {
		s.mannequinsMu.Unlock()
		return false
	}
	minX, minY, minZ, maxX, maxY, maxZ := ent.mannequin.BlastBox()
	pos := c.player.Position()
	if !rules.WithinEntityInteractionRange(c.entityRange(), pos.X, pos.Y+c.dims(c.player.Appearance().Pose).EyeHeight, pos.Z,
		minX, minY, minZ, maxX, maxY, maxZ) {
		s.mannequinsMu.Unlock()
		return false
	}
	visible := ent.custom.visible
	ent.custom = customNameOfStack(held)
	ent.custom.visible = visible
	payload := mannequinFieldsPayload(ent, true, func(_ int, f mannequinField) bool {
		return f.index == protocol.MetadataIndexCustomName
	})
	s.mannequinsMu.Unlock()
	s.broadcastWorld(c.theWorld(), protocol.IDPlayEntityMetadata, payload)
	return true
}
