package server

import (
	"math"
	"math/rand"
	"slices"
	"sync"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
)

const endCrystalEntityID = "minecraft:end_crystal"

// endCrystalEntity is one end crystal in a world. Guarded by the end
// crystals lock, taken beneath no other lock.
type endCrystalEntity struct {
	crystal *entity.EndCrystal
	w       *world.World
	fight   *dragonFight
	uuid    uuid.UUID
	// tags are the scoreboard tags, guarded by the end crystals lock.
	tags   nbt.StringSet
	custom customName
	look   entityLook
	bodyTrack[struct{}]
}

// endCrystalTracker tracks end crystals without periodic movement updates.
// vanilla: EntityTypes.END_CRYSTAL
var endCrystalTracker = trackedKind[*endCrystalEntity, struct{}]{
	invalidatePiston: func(dim *dimension) { dim.pistons.endCrystals.built = false },

	rangeChunks: entity.EndCrystalTrackingRange,
	lock:        func(s *Server) *sync.Mutex { return &s.endCrystalsMu },
	list:        func(s *Server) *[]*endCrystalEntity { return &s.endCrystals },
	chunks:      func(s *Server) *entity.ChunkIndex[*endCrystalEntity] { return &s.crystalChunks },
	filing:      func(it *endCrystalEntity) *entity.Filing { return &it.crystal.Filing },
	track:       func(ent *endCrystalEntity) *bodyTrack[struct{}] { return &ent.bodyTrack },
	world:       func(ent *endCrystalEntity) *world.World { return ent.w },
	id:          func(ent *endCrystalEntity) int32 { return ent.crystal.ID },
	dead:        func(ent *endCrystalEntity) bool { return ent.crystal.Dead },
	pose: func(ent *endCrystalEntity) bodyPose {
		c := ent.crystal
		return bodyPose{x: c.X, y: c.Y, z: c.Z, vx: c.VX, vy: c.VY, vz: c.VZ}
	},
	pairing: endCrystalPairing,
}

func (s *Server) newEndCrystalEntity(w *world.World, x, y, z float64) *endCrystalEntity {
	return &endCrystalEntity{
		crystal: entity.NewEndCrystal(s.nextEntity(), x, y, z),
		w:       w,
		uuid:    uuid.New(),
	}
}

// endCrystalPairing is what shows a crystal: the spawn, then its metadata.
func endCrystalPairing(ent *endCrystalEntity) []outbound {
	return spawnAndMetadata(spawnEndCrystalPayload(ent), endCrystalMetadataPayload(ent))
}

func spawnEndCrystalPayload(ent *endCrystalEntity) []byte {
	c := ent.crystal
	return spawnEntityPayload(spawnEntity{
		id: c.ID, uuid: ent.uuid, typ: protocol.EntityTypeEndCrystal,
		x: c.X, y: c.Y, z: c.Z, vx: c.VX, vy: c.VY, vz: c.VZ,
	})
}

// endCrystalMetadataPayload sends the fields that differ from their
// defaults, nil when none does.
// vanilla: ServerEntity.sendPairingData, EndCrystal.defineSynchedData
func endCrystalMetadataPayload(ent *endCrystalEntity) []byte { return endCrystalMetadata(ent, false) }

func endCrystalMetadata(ent *endCrystalEntity, all bool) []byte {
	c := ent.crystal
	if !all && !c.HasBeam() && !c.HidesBottom() && !c.Silent && ent.custom == (customName{}) {
		return nil
	}
	var b protocol.Buffer
	b.PutVarInt(c.ID)
	if all {
		b.PutMetadata(protocol.MetadataIndexCustomName, protocol.MetadataOptionalText)
		ent.custom.putName(&b)
		b.PutMetadata(protocol.MetadataIndexCustomNameVisible, protocol.MetadataBoolean)
		b.PutBool(ent.custom.visible)
	} else {
		ent.custom.putMetadata(&b, customName{})
	}
	if all || c.Silent {
		b.PutMetadata(protocol.MetadataIndexSilent, protocol.MetadataBoolean)
		b.PutBool(c.Silent)
	}
	if all || c.HasBeam() {
		b.PutMetadata(protocol.MetadataIndexEndCrystalBeam, protocol.MetadataOptionalPosition)
		b.PutBool(c.HasBeam())
		if c.HasBeam() {
			b.PutPosition(c.Beam[0], c.Beam[1], c.Beam[2])
		}
	}
	if all || c.HidesBottom() {
		b.PutMetadata(protocol.MetadataIndexEndCrystalShowBottom, protocol.MetadataBoolean)
		b.PutBool(c.ShowBottom)
	}
	b.EndMetadata()
	return b.Bytes()
}

// registerEndCrystal puts a built crystal into the world and shows it to
// every player in range.
func (s *Server) registerEndCrystal(ent *endCrystalEntity) {
	ent.fight = s.dragonFightOf(ent.w)
	endCrystalTracker.register(s, ent)
}

func (s *Server) endCrystalByIDLocked(w *world.World, id int32) *endCrystalEntity {
	for _, ent := range s.endCrystals {
		if ent.crystal.ID == id && ent.w == w && !ent.crystal.Dead {
			return ent
		}
	}
	return nil
}

func (s *Server) removeEndCrystalLocked(ent *endCrystalEntity) {
	ent.crystal.Dead = true
	endCrystalTracker.unfile(ent)
	s.endCrystals = slices.DeleteFunc(s.endCrystals, func(o *endCrystalEntity) bool { return o == ent })
	s.entityDestroyed(ent.uuid)
}

// tickEndCrystals lights the fire a crystal stands in, then settles who sees
// which crystal with the same pass a frozen world runs.
// vanilla: EndCrystal.tick
func (s *Server) tickEndCrystals() {
	watchers := s.trackingWatchers()
	type spot struct {
		w       *world.World
		x, y, z int32
	}
	var fires []spot
	s.lockTick(&s.endCrystalsMu)
	for _, ent := range s.endCrystals {
		if ent.fight == nil {
			continue
		}
		x, y, z := ent.crystal.FireCell()
		fires = append(fires, spot{ent.w, x, y, z})
	}
	s.unlockTick(&s.endCrystalsMu)
	for _, f := range fires {
		if f.w.ChunkLoadedAt(f.x, f.z) && rules.IsAir(f.w.BlockAt(f.x, f.y, f.z)) {
			s.setBlockAndTell(f.w, f.x, f.y, f.z, rules.FireStateForPlacement(f.w, f.x, f.y, f.z))
		}
	}
	endCrystalTracker.frozenPass(s, watchers)
}

// destroyEndCrystal takes a crystal out of the world and tells its watchers;
// it is the shared tail of a hurt and of a blast that reached it.
func (s *Server) destroyEndCrystal(ent *endCrystalEntity, sourceID int32) {
	// vanilla: EnderDragonFight.onCrystalDestroyed
	if f := ent.fight; f != nil {
		f.mu.Lock()
		if f.ritual.Stage != "" && slices.Contains(f.ritual.Crystals, ent.uuid) {
			s.abortDragonRespawn(ent.w, f)
		} else {
			s.refreshDragonCrystalCount(ent.w, f)
		}
		f.mu.Unlock()
	}
	if h := s.herdOf(ent.w); h.dragons.Load() != 0 {
		var source *entity.Player
		nearest := math.Inf(1)
		s.mu.RLock()
		for _, c := range s.clients {
			if c.theWorld() != ent.w {
				continue
			}
			pos := c.player.Position()
			p := c.sized(entity.Player{EntityID: c.player.EntityID, X: pos.X, Y: pos.Y, Z: pos.Z, Creative: c.player.GameMode() == protocol.GameModeCreative})
			if p.EntityID == sourceID {
				source = &p
				break
			}
			// With a null targeter, TargetingConditions applies neither its range
			// nor line of sight. vanilla: ServerEntityGetter.getNearestPlayer
			if !c.Alive() || p.Creative || c.player.GameMode() == protocol.GameModeSpectator || ent.w.Difficulty() == rules.DifficultyPeaceful {
				continue
			}
			dist := math.Pow(pos.X-math.Floor(ent.crystal.X), 2) + math.Pow(pos.Y-math.Floor(ent.crystal.Y), 2) + math.Pow(pos.Z-math.Floor(ent.crystal.Z), 2)
			if dist < nearest {
				nearest, source = dist, &p
			}
		}
		s.mu.RUnlock()
		damageType := rules.DamageTypeExplosion
		var causeID int32
		if source != nil {
			damageType = rules.DamageTypePlayerExplosion
			causeID = source.EntityID + 1
		}
		var sends []mobSend
		s.mobsMu.Lock()
		for _, m := range h.herd {
			if m.Type == entity.TypeEnderDragon {
				landed, softened := m.DragonCrystalDestroyed(ent.crystal.ID, ent.crystal.X, ent.crystal.Z, source)
				if landed {
					if ref := h.book[m]; ref != nil {
						sends = append(sends, mobSend{w: ent.w, id: protocol.IDPlayEntityMetadata, data: ref.syncHealth()})
					}
					if !softened {
						sends = append(sends, mobHurtToldBy(ent.w, m, damageType, causeID, ent.crystal.ID+1, nil)...)
					}
				}
			}
		}
		s.mobsMu.Unlock()
		s.sendMobSends(sends)
	}
	s.broadcastWorld(ent.w, protocol.IDPlayRemoveEntities, removeEntitiesPayload([]int32{ent.crystal.ID}))
}

// explodeEndCrystal runs a removed crystal's blast with the source entity
// behind it. vanilla: EndCrystal.hurtServer
func (s *Server) explodeEndCrystal(ent *endCrystalEntity, causeID int32, causeName string, causeUUID uuid.UUID) {
	killer := s.entityKiller(ent.w, causeID)
	sourceTeam, _ := s.scoreboard.PlayerTeam(ent.uuid.String())
	s.endCrystalsMu.Lock()
	carrier := &rules.LootEntity{Type: endCrystalEntityID, Tags: ent.tags.Values(), Team: sourceTeam}
	s.endCrystalsMu.Unlock()
	// vanilla: DamageSources.explosion picks player_explosion when both entities exist
	damageType := rules.DamageTypeExplosion
	if killer != nil {
		damageType = rules.DamageTypePlayerExplosion
	}
	s.runExplosion(blast{
		w: ent.w, x: ent.crystal.X, y: ent.crystal.Y, z: ent.crystal.Z,
		radius:      entity.EndCrystalBlastPower,
		causeID:     causeID,
		causeName:   causeName,
		causeKiller: killer,
		causeUUID:   causeUUID,
		directID:    ent.crystal.ID,
		directUUID:  ent.uuid,
		directName:  s.directNameOf(killer, protocol.EntityTypeEndCrystal, ent.uuid, customName{}),
		silent:      ent.crystal.Silent,
		directType:  endCrystalEntityID,
		directLoot:  carrier,
		damageType:  damageType,
		sourceTeam:  sourceTeam,
		rng:         rand.New(rand.NewSource(s.entitySeed(ent.crystal.ID))),
		// vanilla: Level.ExplosionInteraction.BLOCK
		interaction: rules.BlockExplosionInteraction(s.world),
	})
}

// endCrystalSnapshots reads every live crystal's box under the crystals
// lock, before a projectile tick takes its own locks.
func (s *Server) endCrystalSnapshots() map[*world.World][]entity.ArrowTarget {
	s.endCrystalsMu.Lock()
	defer s.endCrystalsMu.Unlock()
	if len(s.endCrystals) == 0 {
		return nil
	}
	byWorld := make(map[*world.World][]entity.ArrowTarget)
	for _, ent := range s.endCrystals {
		if ent.crystal.Dead {
			continue
		}
		minX, minY, minZ, maxX, maxY, maxZ := ent.crystal.BlastBox()
		byWorld[ent.w] = append(byWorld[ent.w], entity.ArrowTarget{
			EntityID: ent.crystal.ID,
			Type:     protocol.EntityTypeEndCrystal,
			MinX:     minX, MinY: minY, MinZ: minZ,
			MaxX: maxX, MaxY: maxY, MaxZ: maxZ,
		})
	}
	return byWorld
}

// crystalProjectileHit is a crystal a projectile's sweep met.
type crystalProjectileHit struct {
	w         *world.World
	crystalID int32
	ownerID   int32
	ownerUUID uuid.UUID
}

// projectileHitEndCrystal removes the crystal and runs its blast, whatever
// the damage. It reports landed once the blast ran, and refused when an
// invulnerable crystal turned the hurt away; neither means the crystal is gone.
// vanilla: EndCrystal.hurtServer
func (s *Server) projectileHitEndCrystal(hit crystalProjectileHit) (landed, refused bool) {
	if s.entityDamageListened() {
		s.endCrystalsMu.Lock()
		ent := s.endCrystalByIDLocked(hit.w, hit.crystalID)
		var id uuid.UUID
		if ent != nil {
			id = ent.uuid
		}
		s.endCrystalsMu.Unlock()
		if ent == nil {
			return false, false
		}
		if _, ok := s.askNonlivingDamage(hit.w, id, rules.DamageTypeGeneric, &damageCause{entityID: hit.ownerID}, 1); !ok {
			return false, true
		}
	}
	s.endCrystalsMu.Lock()
	ent := s.endCrystalByIDLocked(hit.w, hit.crystalID)
	if ent == nil {
		s.endCrystalsMu.Unlock()
		return false, false
	}
	if ent.crystal.Invulnerable {
		s.endCrystalsMu.Unlock()
		return false, true
	}
	s.removeEndCrystalLocked(ent)
	s.endCrystalsMu.Unlock()
	s.destroyEndCrystal(ent, hit.ownerID)
	s.explodeEndCrystal(ent, hit.ownerID, "", hit.ownerUUID)
	return true, false
}

// attackEndCrystal answers a player's blow on a crystal: out of reach it
// spends nothing, otherwise the crystal is removed and its blast runs, the
// player the indirect source. It reports whether the target was a crystal.
// vanilla: EndCrystal.hurtServer
func (c *client) attackEndCrystal(target int32) bool {
	s := c.srv
	s.endCrystalsMu.Lock()
	ent := s.endCrystalByIDLocked(c.theWorld(), target)
	if ent == nil {
		s.endCrystalsMu.Unlock()
		return false
	}
	creative := c.player.GameMode() == protocol.GameModeCreative
	if ent.crystal.Invulnerable && !creative {
		s.endCrystalsMu.Unlock()
		return true
	}
	minX, minY, minZ, maxX, maxY, maxZ := ent.crystal.BlastBox()
	if !c.withinAttackRange(minX, minY, minZ, maxX, maxY, maxZ) {
		s.endCrystalsMu.Unlock()
		return true
	}
	// The blow's cooldown, sweep, sound and exhaustion run as against any
	// body. vanilla: Player.attack
	held := c.heldItem(protocol.HandMain)
	attackDamage := c.attackDamage()
	strength := rules.AttackStrengthScale(int(c.attackStrengthTicks.Load()), c.attackSpeed())
	c.attackStrengthTicks.Store(0)
	// The conditions read the crystal's body, which takes the lock, so they
	// run with it released and the crystal is found again after.
	snap := s.endCrystalSnapshot(ent, nil)
	s.endCrystalsMu.Unlock()
	ctx := c.attackEnchContext(meleeDamageType(held), protocol.EntityTypeEndCrystal, snapshotLoot(snap))
	plain := rules.AttackDamage(attackDamage, false, 1)
	magic := (rules.EnchantedDamage(held, &ctx, plain) - plain) * strength
	falling := c.canCriticalAttack(false)
	s.endCrystalsMu.Lock()
	if ent = s.endCrystalByIDLocked(c.theWorld(), target); ent == nil {
		s.endCrystalsMu.Unlock()
		return true
	}
	if rules.AttackDamage(attackDamage, falling, strength) <= 0 && magic <= 0 {
		s.endCrystalsMu.Unlock()
		return true
	}
	if s.entityDamageListened() {
		id := ent.uuid
		s.endCrystalsMu.Unlock()
		if _, ok := s.askNonlivingDamage(c.theWorld(), id, meleeDamageType(held), &damageCause{entityID: c.player.EntityID}, float32(attackDamage)); !ok {
			return true
		}
		s.endCrystalsMu.Lock()
		if ent = s.endCrystalByIDLocked(c.theWorld(), target); ent == nil {
			s.endCrystalsMu.Unlock()
			return true
		}
	}
	s.removeEndCrystalLocked(ent)
	s.endCrystalsMu.Unlock()
	// Feedback broadcasts under s.mu, so it runs after endCrystalsMu is released.
	fx := c.blowFeedback(held, strength, falling, magic)
	s.destroyEndCrystal(ent, c.player.EntityID)
	s.explodeEndCrystal(ent, c.player.EntityID, c.player.Name, c.player.UUID)
	if fx.Sweep {
		c.sweepHurt(ent.w, held, attackDamage, ent.crystal.ID, minX, minY, minZ, maxX, maxY, maxZ, strength)
	}
	c.landedBlowFeedback(fx, ent.crystal.ID)
	c.finishMeleeItemEffects(held, ctx, c.player.Vitals().FallDistance, false)
	// A non-living target wears nothing. vanilla: Player.itemAttackInteraction
	c.exhaust(rules.AttackExhaustion)
	return true
}

// placeEndCrystal handles one use of the end crystal item.
// handled is true when the held item was a crystal, the use answered either
// way.
// vanilla: EndCrystalItem.useOn
func (c *client) placeEndCrystal(hand, x, y, z int32) (handled bool, err error) {
	c.useSwing = swingNone
	stack := c.heldItem(hand)
	if registry.Item(stack.ItemID) != registry.ItemEndCrystal {
		return false, nil
	}
	w := c.theWorld()
	if !entity.EndCrystalCanStandOn(w.BlockAt(x, y, z), w.BlockAt(x, y+1, z)) {
		return true, nil
	}
	if c.srv.boxTaken(w, entity.EndCrystalPlacementBox(x, y+1, z)) {
		return true, nil
	}
	ent := c.srv.newEndCrystalEntity(w, float64(x)+0.5, float64(y+1), float64(z)+0.5)
	ent.crystal.ShowBottom = false
	c.srv.registerEndCrystal(ent)
	c.gameEventAtCell(rules.GameEventEntityPlace, x, y+1, z)
	c.srv.tryRespawnDragon(w)
	c.useSwing = swingPredicted
	c.awardUsedHeld(hand)
	if c.player.GameMode() != protocol.GameModeCreative {
		if err := c.consumeHeld(hand); err != nil {
			return true, err
		}
	}
	return true, nil
}

// endCrystalBoxTaken reports whether a crystal's box overlaps the box.
func (s *Server) endCrystalBoxTaken(w *world.World, box entity.AABB) bool {
	s.endCrystalsMu.Lock()
	defer s.endCrystalsMu.Unlock()
	for _, ent := range s.endCrystals {
		if ent.w == w && !ent.crystal.Dead && ent.crystal.Overlaps(box) {
			return true
		}
	}
	return false
}

// blastEndCrystals takes out the crystals a blast reaches, none when the
// blast may not hurt entities or spares their team, and returns them for destroyEndCrystal once
// the lock is released. An explosion's own hurt does not set off another.
// vanilla: EndCrystal.hurtServer
func (s *Server) blastEndCrystals(b blast, in *blastInputs) []*endCrystalEntity {
	s.endCrystalsMu.Lock()
	var gone []*endCrystalEntity
	for _, ent := range s.endCrystals {
		c := ent.crystal
		if ent.w == b.w && c.ReachedByBlast(b.x, b.y, b.z, b.radius, b.harmless) && !in.spares(ent.uuid) {
			gone = append(gone, ent)
		}
	}
	if !s.entityDamageListened() {
		for _, ent := range gone {
			s.removeEndCrystalLocked(ent)
		}
		s.endCrystalsMu.Unlock()
		return gone
	}
	s.endCrystalsMu.Unlock()
	gone = askBlastBodies(s, b, gone, func(e *endCrystalEntity) uuid.UUID { return e.uuid })
	s.endCrystalsMu.Lock()
	defer s.endCrystalsMu.Unlock()
	gone = slices.DeleteFunc(gone, func(ent *endCrystalEntity) bool { return ent.crystal.Dead })
	for _, ent := range gone {
		s.removeEndCrystalLocked(ent)
	}
	return gone
}

func endCrystalTag(ent *endCrystalEntity) *nbt.OrderedCompound {
	return endCrystalTagAs(ent, endCrystalEntityID)
}

func endCrystalTagAs(ent *endCrystalEntity, id string) *nbt.OrderedCompound {
	c := ent.crystal
	tag := orderedEntityBaseTag(id, c.X, c.Y, c.Z, c.VX, c.VY, c.VZ, 0, ent.look.pitch, ent.uuid, false, 0, 0, 0)
	putEntityOptions(tag, ent.custom, c.Silent, false, ent.look.dataOnly(), ent.tags)
	tag.Set("fall_distance", nbt.Double(c.FallDistance))
	tag.Set("Invulnerable", boolTag(c.Invulnerable))
	if c.Beam != nil {
		tag.Set("beam_target", nbt.IntArray{c.Beam[0], c.Beam[1], c.Beam[2]})
	}
	tag.Set("ShowBottom", boolTag(c.ShowBottom))
	return tag
}

// endCrystalOfTag builds a crystal from its saved compound, nil for one that
// is not a crystal or has no position.
// vanilla: EndCrystal.readAdditionalSaveData
func endCrystalOfTag(s *Server, w *world.World, compound nbt.Compound) *endCrystalEntity {
	if id, _ := nbt.Get[nbt.String](compound, "id"); string(id) != endCrystalEntityID {
		return nil
	}
	x, y, z, ok := entityPositionOfTag(compound)
	if !ok {
		return nil
	}
	identity, has := entityUUIDOfTag(compound)
	if !has {
		identity = uuid.New()
	}
	ent := &endCrystalEntity{
		crystal: entity.NewEndCrystal(s.nextEntity(), x, y, z),
		w:       w,
		uuid:    identity,
	}
	applyEndCrystalTag(ent, compound)
	return ent
}

// applyEndCrystalTag loads the fields a crystal saves from compound; an absent
// beam_target clears the beam.
// vanilla: Entity.load, EndCrystal.readAdditionalSaveData
func applyEndCrystalTag(ent *endCrystalEntity, compound nbt.Compound) {
	if x, y, z, ok := entityPositionOfTag(compound); ok {
		ent.crystal.SetPos(x, y, z)
	}
	ent.crystal.VX, ent.crystal.VY, ent.crystal.VZ = entityMotionOfTag(compound)
	ent.crystal.FallDistance = nbt.DoubleFieldOr(compound, "fall_distance", 0)
	ent.tags.Reset(tagsOfTag(compound))
	ent.crystal.Invulnerable = nbt.BoolField(compound, "Invulnerable")
	ent.crystal.Silent = nbt.BoolField(compound, "Silent")
	ent.custom = customNameOfTag(compound)
	ent.look = entityLookOf(compound)
	if _, named := compound["ShowBottom"]; named {
		ent.crystal.ShowBottom = nbt.BoolField(compound, "ShowBottom")
	}
	ent.crystal.Beam = nil
	if beam, ok := nbt.Get[nbt.IntArray](compound, "beam_target"); ok && len(beam) == 3 {
		ent.crystal.Beam = &[3]int32{beam[0], beam[1], beam[2]}
	}
}

// killEndCrystal removes a crystal and tells the dragon fight, with no
// explosion.
// vanilla: EndCrystal.kill
func (s *Server) killEndCrystal(ent *endCrystalEntity) {
	s.endCrystalsMu.Lock()
	if ent.crystal.Dead {
		s.endCrystalsMu.Unlock()
		return
	}
	s.removeEndCrystalLocked(ent)
	s.endCrystalsMu.Unlock()
	s.destroyEndCrystal(ent, 0)
}

// teleportEndCrystal moves a crystal and marks it for the next tracking pass.
// vanilla: Entity.teleportTo
func (s *Server) teleportEndCrystal(ent *endCrystalEntity, to player.Position, by string) {
	endCrystalTracker.teleport(s, ent, func() { ent.crystal.SetPos(to.X, to.Y, to.Z) })
}

func (s *Server) endCrystalSnapshot(ent *endCrystalEntity, q *entityQuery) *entitySnapshot {
	typ := endCrystalEntityID
	if rejectsEntityHead(q, typ, ent.uuid, ent.tags) {
		return nil
	}
	c := ent.crystal
	if c.Dead {
		return nil
	}
	e := s.snapshot(q, ent.tags, ent, c.ID, typ, "End Crystal", ent.uuid,
		s.dimensionOfWorld(ent.w).name, player.Position{X: c.X, Y: c.Y, Z: c.Z}, box6(c.BlastBox()))
	if e == nil {
		return nil
	}
	e.bind(&s.endCrystalsMu, &ent.tags, &endCrystalEntityOps)
	return e
}
