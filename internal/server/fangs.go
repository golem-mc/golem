package server

import (
	"math"
	"slices"
	"sync"

	"github.com/google/uuid"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/pluginruntime"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// Evoker summons: fangs and vexes. Fangs are not saved to entity files.
// Not modelled: the spells' entity_place game event.

// fangsTypeID is minecraft:evoker_fangs in the entity type registry.
var fangsTypeID, _ = registry.EntityTypeID("minecraft:evoker_fangs")

// fangsEntity is one set of fangs in a world.
type fangsEntity struct {
	fangs     *entity.EvokerFangs
	w         *world.World
	uuid      uuid.UUID
	ownerUUID uuid.UUID
	tags      nbt.StringSet
	effectData
	bodyTrack[struct{}]
}

// fangSet is every dimension's fangs; registration takes mu inside s.mu,
// never inside another entity lock.
type fangSet struct {
	mu     sync.Mutex
	list   []*fangsEntity
	chunks entity.ChunkIndex[*fangsEntity]
	// effects is scratch owned by the tick goroutine; records are cleared after applying.
	effects fangsEffects
}

// fangsCast is one fang a spell raised, handed out of the mob pass.
type fangsCast struct {
	w       *world.World
	owner   int32
	x, y, z float64
	radians float64
	warmup  int
}

// vexSummon is one vex a summon spell called.
type vexSummon struct {
	w       *world.World
	owner   int32
	x, y, z float64
	life    int
}

// fangsSpawnPayload is the Spawn Entity packet of fangs.
func fangsSpawnPayload(it *fangsEntity) []byte {
	f := it.fangs
	return spawnEntityPayload(spawnEntity{
		id: f.ID, uuid: it.uuid, typ: fangsTypeID,
		x: f.X, y: f.Y, z: f.Z,
		yaw: encodeAngle(it.yaw), pitch: encodeAngle(it.pitch),
		vx: it.vx, vy: it.vy, vz: it.vz,
	})
}

// fangsShowRange is the tracking range of the fangs type in blocks.
const fangsShowRange = entity.FangsTracking * 16

// spawnFangs spawns fangs and shows them to players in range.
// vanilla: Evoker.EvokerAttackSpellGoal.createSpellEntity
func (s *Server) spawnFangs(cast fangsCast) {
	it := &fangsEntity{
		fangs: entity.NewEvokerFangs(s.nextEntity(), cast.x, cast.y, cast.z, cast.radians, cast.warmup, cast.owner),
		w:     cast.w,
		uuid:  uuid.New(),
	}
	s.mobsMu.Lock()
	owner, _ := s.mobByEntityIDAnyDimLocked(cast.owner)
	s.mobsMu.Unlock()
	if owner != nil {
		it.ownerUUID = owner.uuid
	}
	it.yaw = it.fangs.Yaw
	s.registerFangs(it)
	if owner != nil {
		s.gameEvent(cast.w, rules.GameEventEntityPlace, cast.x, cast.y, cast.z, rules.GameEventContext{Mob: owner.uuid})
	}
}

// registerFangs tracks fangs and shows them to players in range.
func (s *Server) registerFangs(it *fangsEntity) {
	fangsTracker.register(s, it)
}

// fangsEffect holds only the inputs a spike or removal needs after the fangs lock is released.
type fangsEffect struct {
	w         *world.World
	id, owner int32
	identity  uuid.UUID
	x, y, z   float64
}

// fangsBiteEffect is a fangsEffect with the owner the bite resolves.
type fangsBiteEffect struct {
	fangsEffect
	ownerUUID uuid.UUID
}

// fangsEffects keeps spike, bite and removal ordering after the lock is released.
type fangsEffects struct {
	spikes, gone []fangsEffect
	bites        []fangsBiteEffect
}

// tickFangs ticks every fang set: the spike event, the bite, the removal.
// vanilla: EvokerFangs.tick
func (s *Server) tickFangs() {
	s.fangs.mu.Lock()
	if len(s.fangs.list) == 0 {
		s.fangs.mu.Unlock()
		return
	}
	fx := s.fangs.effects
	alive := s.fangs.list[:0]
	for _, it := range s.fangs.list {
		spike, bite := it.fangs.Tick()
		if spike || bite {
			// The effects must not read a command's concurrent position or owner write.
			f := it.fangs
			at := fangsEffect{w: it.w, id: f.ID, owner: f.OwnerID, identity: it.uuid, x: f.X, y: f.Y, z: f.Z}
			if spike {
				fx.spikes = append(fx.spikes, at)
			}
			if bite {
				fx.bites = append(fx.bites, fangsBiteEffect{at, it.ownerUUID})
			}
		}
		if it.fangs.Dead {
			f := it.fangs
			fx.gone = append(fx.gone, fangsEffect{w: it.w, id: f.ID, x: f.X, y: f.Y, z: f.Z})
			fangsTracker.unfile(it)
			continue
		}
		alive = append(alive, it)
	}
	s.fangs.list = alive
	s.fangs.mu.Unlock()
	fx.apply(s)
	clear(fx.spikes)
	clear(fx.bites)
	clear(fx.gone)
	s.fangs.effects = fangsEffects{spikes: fx.spikes[:0], bites: fx.bites[:0], gone: fx.gone[:0]}
}

// apply sends spikes before bites and removals, with no entity lock held.
func (fx fangsEffects) apply(s *Server) {
	for _, it := range fx.spikes {
		s.broadcastNear(it.w, it.x, it.y, it.z, fangsShowRange, protocol.IDPlayEntityEvent,
			entityEventPayload(it.id, protocol.EntityStatusEvokerFangsBite))
	}
	for _, it := range fx.bites {
		s.fangsBite(it)
	}
	for _, it := range fx.gone {
		s.broadcastNear(it.w, it.x, it.y, it.z, fangsShowRange, protocol.IDPlayRemoveEntities,
			removeEntitiesPayload([]int32{it.id}))
	}
}

// fangsBite hurts every living body in reach except the owner and its
// allies. The owner is a mob in the herd (found by identity, in any
// dimension) or an online player. Ownerless fangs hurt everything with plain
// magic damage, unscaled and without knockback.
// vanilla: EvokerFangs.dealDamageTo
func (s *Server) fangsBite(it fangsBiteEffect) {
	w := it.w
	body := entity.EvokerFangs{X: it.x, Y: it.y, Z: it.z}
	minX, minY, minZ, maxX, maxY, maxZ := body.BiteBox()
	// The fang may be discarded before the damage lands, so a listener's
	// event carries its identity; no handle is built when nobody listens.
	var fang *removedBody
	if r := s.plugins; r != nil && (r.Has(pluginruntime.EntityDamage) || r.Has(pluginruntime.PlayerDeath)) {
		fang = &removedBody{id: it.identity, typ: registry.EntityTypeEvokerFangs}
	}

	s.mobsMu.Lock()
	dim := s.dimensionOfWorld(w)
	h := dim.mobs
	owner := entity.AllianceMember{}
	var ownerUUID uuid.UUID
	var ownerID int32
	ownerW := w
	var ownerClient *client
	// The owner is found by identity: a dimension transfer gives it a new entity ID,
	// and a corpse stays the owner until it is removed.
	// vanilla: EntityReference.getEntity
	if ent, at := s.mobOrCorpseByUUIDLocked(it.ownerUUID, dim); ent != nil {
		ownerW = at.w
		owner = entity.AllianceMember{Mob: ent.mob, Team: ent.mob.Team}
		ownerUUID = ent.uuid
		ownerID = ent.mob.ID
	} else if c := s.mobInputs.clientByEntityID[it.owner]; c != nil && it.owner != 0 {
		ownerClient = c
		ownerID = it.owner
		ownerUUID = c.player.UUID
		owner = entity.AllianceMember{Player: ownerUUID, Team: s.mobInputs.playerTeams[ownerUUID]}
	}
	owned := owner != (entity.AllianceMember{})
	lookup := s.fangsAllyByUUIDLocked
	var victims []*entity.Mob
	// A body is found only through the sections its feet are filed in, however
	// wide its box. vanilla: EntitySectionStorage.forEachAccessibleNonEmptySection
	window := entity.NewSectionWindow(minX, maxX, minY, maxY, minZ, maxZ)
	consider := func(m *entity.Mob) bool {
		if m.Dead || m == owner.Mob || !window.HoldsAt(m.X, m.Y, m.Z) {
			return true
		}
		if !m.BlastBoxHits(minX, minY, minZ, maxX, maxY, maxZ) {
			return true
		}
		if owner.Mob != nil && owner.Mob.EvokerFangsDirectAlly(m, ownerUUID) {
			return true
		}
		if owned && entity.FangsAllied(owner, entity.AllianceMember{Mob: m, Team: m.Team}, lookup) {
			return true
		}
		victims = append(victims, m)
		return true
	}
	indexed := false
	if len(h.book) != 0 {
		// smallChunkArea adds the same two-block margin as the window.
		var area chunkArea
		area, indexed = smallChunkArea(&[6]float64{minX, minY, minZ, maxX, maxY, maxZ})
		h.mobsInArea(area, indexed, consider)
	}
	if indexed && len(victims) > 1 {
		// The index visits by chunk; damage goes in herd order.
		hit := make(map[*entity.Mob]struct{}, len(victims))
		for _, m := range victims {
			hit[m] = struct{}{}
		}
		victims = victims[:0]
		for _, m := range h.herd {
			if _, ok := hit[m]; ok {
				victims = append(victims, m)
				if len(victims) == len(hit) {
					break
				}
			}
		}
	}
	s.mobsMu.Unlock()

	s.mu.RLock()
	var clients []*client
	for _, c := range s.clients {
		if c.theWorld() == w {
			clients = append(clients, c)
		}
	}
	s.mu.RUnlock()
	for _, c := range clients {
		// One position read for the box and the section, which the connection goroutine may move between reads.
		pos := c.player.Position()
		cb := c.bodyBoxAt(pos)
		if !boxHits(minX, minY, minZ, maxX, maxY, maxZ, cb[0], cb[1], cb[2], cb[3], cb[4], cb[5]) {
			continue
		}
		// A scaled player's box can reach past the sections its feet are filed in.
		// vanilla: EntitySectionStorage.forEachAccessibleNonEmptySection
		if !window.HoldsAt(pos.X, pos.Y, pos.Z) {
			continue
		}
		if !owned {
			c.hurt(entity.FangsDamage, rules.DamageTypeMagic, &damageCause{x: it.x, y: it.y, z: it.z})
			continue
		}
		s.mobsMu.Lock()
		allied := entity.FangsAllied(owner, lookup(c.player.UUID), lookup)
		s.mobsMu.Unlock()
		if allied {
			continue
		}
		// Indirect magic scales only for a living non-player cause.
		// vanilla: DamageSource.scalesWithDifficulty
		damage := entity.FangsDamage
		if rules.DamageScales(rules.DamageTypeIndirectMagic, owner.Mob != nil) {
			damage = rules.ScaledWithDifficulty(damage, w.Difficulty())
		}
		var killer *rules.Killer
		if ownerClient != nil {
			killer = &rules.Killer{Name: ownerClient.DisplayName(), Weapon: ownerClient.heldItem(protocol.HandMain)}
		} else {
			killer = s.entityKiller(ownerW, ownerID)
		}
		cause := &damageCause{entityID: ownerID, directID: it.id + 1, directType: "minecraft:evoker_fangs", directUUID: it.identity, x: it.x, y: it.y, z: it.z, killer: killer, exploder: fang}
		if landed, softened := c.hurt(damage, rules.DamageTypeIndirectMagic, cause); landed {
			if !softened {
				c.knockbackFromPoint(w, it.x, it.y, it.z, syncVelocityAfterTravel)
			}
			if ownerClient != nil {
				// vanilla: EnchantmentHelper.doPostAttackEffects reads the owner's weapon
				s.thornsOnPlayerOwner(w, c, ownerClient, ownerClient.heldItem(protocol.HandMain), rules.DamageTypeIndirectMagic, fangsTypeID)
			} else if owner.Mob != nil {
				s.fangsThorns(w, c, owner.Mob, ownerW)
			}
		}
	}
	for _, m := range victims {
		switch {
		case ownerClient != nil:
			s.hurtMobByPlayerFangs(w, m, it.fangsEffect, ownerClient, fang)
		case owner.Mob != nil:
			s.hurtMobVia(w, m, owner.Mob, &directBody{id: it.id, identity: it.identity, x: it.x, z: it.z, removed: fang}, entity.FangsDamage, rules.DamageTypeIndirectMagic, nil)
		default:
			s.hurtMobPlain(w, m, entity.FangsDamage, rules.DamageTypeMagic)
		}
	}
}

// fangsThorns hits back the fangs' owner, living in ownerW, for a bite on victim.
func (s *Server) fangsThorns(w *world.World, victim *client, owner *entity.Mob, ownerW *world.World) {
	s.thornsOnMobOwner(w, victim, owner, ownerW, &rules.EnchContext{
		DamageType: rules.DamageTypeIndirectMagic, Victim: protocol.EntityTypePlayer,
		Attacker: -1, DirectAttacker: -1,
	})
}

// hurtMobByPlayerFangs bites m as indirect magic caused by the owner player,
// who gets the kill credit, tells its watchers and runs the post-attack
// effects of m's equipment and the owner's weapon. An invulnerable mob is
// spared even by a creative owner.
// vanilla: EvokerFangs.dealDamageTo, EnchantmentHelper.doPostAttackEffects
func (s *Server) hurtMobByPlayerFangs(w *world.World, m *entity.Mob, it fangsEffect, owner *client, fang *removedBody) {
	cause := &damageCause{entityID: it.owner, directUUID: it.identity, exploder: fang, player: true,
		creative: owner.player.GameMode() == protocol.GameModeCreative, x: it.x, z: it.z}
	if !s.hurtMobTyped(w, m, entity.FangsDamage, rules.DamageTypeIndirectMagic, cause, it.id+1, "minecraft:evoker_fangs", true, nil) {
		return
	}
	// Only enchantments can act, so an unenchanted weapon and mob skip the sky
	// and block lookups and the loot binding.
	weapon := owner.heldItem(protocol.HandMain)
	enchanted := weapon.Enchanted()
	if !enchanted {
		s.mobsMu.Lock()
		for _, p := range m.EnchEquipment() {
			if p.Stack.Enchanted() {
				enchanted = true
				break
			}
		}
		s.mobsMu.Unlock()
	}
	if !enchanted {
		return
	}
	s.postAttack(w, attackBody{player: owner}, attackBody{mob: m}, weapon, &rules.EnchContext{
		DamageType: rules.DamageTypeIndirectMagic,
		Victim:     mobEntityType(m.Type), Attacker: protocol.EntityTypePlayer, DirectAttacker: fangsTypeID,
	}, syncVelocityAfterTravel)
}

// vanilla: EntityReference.getEntity
func (s *Server) fangsAllyByUUIDLocked(id uuid.UUID) entity.AllianceMember {
	if ent, _ := s.mobByUUIDLocked(id, nil); ent != nil {
		return entity.AllianceMember{Mob: ent.mob, Team: ent.mob.Team}
	}
	// The mob pass snapshots online players' teams before fangs tick.
	if team, online := s.mobInputs.playerTeams[id]; online {
		return entity.AllianceMember{Player: id, Team: team}
	}
	return entity.AllianceMember{}
}

// hurtMobPlain hurts m with no source and tells its watchers.
func (s *Server) hurtMobPlain(w *world.World, m *entity.Mob, amount float64, damageType registry.DamageType) {
	s.mobsMu.Lock()
	ent := s.herdOf(w).book[m]
	if ent == nil || m.Dead {
		s.mobsMu.Unlock()
		return
	}
	landed, softened := m.HurtByWorld(amount, damageType)
	var health []byte
	var sends []mobSend
	if landed {
		health = ent.syncHealth()
		if !softened {
			sends = mobHurtTold(w, m, damageType)
		}
	}
	s.mobsMu.Unlock()
	if !landed {
		return
	}
	s.broadcastWorld(w, protocol.IDPlayEntityMetadata, health)
	s.sendMobSends(sends)
}

// summonVex spawns a vex bound to the spell's chosen block, owned by the
// evoker by ID and identity, so a save keeps the owner. The identity is read
// here, in the effects phase, where the summoner is live and no lock held.
// vanilla: Evoker.EvokerSummonSpellGoal.performSpellCasting, Vex.setOwner
func (s *Server) summonVex(call vexSummon) {
	dim := s.dimensionOfWorld(call.w)
	identity := s.entityIdentityOf(call.owner)
	s.spawnMobArriving(dim, entity.TypeVex, call.x+0.5, call.y, call.z+0.5, 0, nil, false, "", true, func(ent *mobEntity) {
		ent.mob.SetVexOwner(call.owner)
		ent.mob.SetVexOwnerUUID(identity)
		ent.mob.SetVexBound(int32(call.x), int32(call.y), int32(call.z))
		ent.mob.SetVexLife(call.life)
	})
	s.gameEvent(call.w, rules.GameEventEntityPlace, math.Floor(call.x)+0.5, math.Floor(call.y)+0.5, math.Floor(call.z)+0.5,
		rules.GameEventContext{Mob: identity})
}

// fangsTag is EvokerFangs.addAdditionalSaveData with the base entity fields.
func fangsTag(it *fangsEntity) *nbt.OrderedCompound {
	return fangsTagAs(it, "minecraft:evoker_fangs")
}

func fangsTagAs(it *fangsEntity, id string) *nbt.OrderedCompound {
	f := it.fangs
	tag := orderedEntityBaseTag(id, f.X, f.Y, f.Z, it.vx, it.vy, it.vz,
		it.yaw, it.pitch, it.uuid, it.onGround, it.cooldown, it.fire, 0)
	it.put(tag)
	putTags(tag, it.tags.Values())
	tag.Set("Warmup", nbt.Int(f.Warmup()))
	if it.ownerUUID != uuid.Nil {
		tag.Set("Owner", nbt.UUIDToIntArray(it.ownerUUID))
	}
	return tag
}

func (s *Server) fangsSnapshot(it *fangsEntity, q *entityQuery) *entitySnapshot {
	typ := "minecraft:evoker_fangs"
	if rejectsEntityHead(q, typ, it.uuid, it.tags) {
		return nil
	}
	f := it.fangs
	if f.Dead {
		return nil
	}
	pos := player.Position{X: f.X, Y: f.Y, Z: f.Z, Yaw: it.yaw, Pitch: it.pitch}
	half := entity.FangsWidth / 2
	// vanilla: EntityTypes.EVOKER_FANGS stores dimensions as floats.
	height := float64(float32(entity.FangsHeight))
	e := s.snapshot(q, it.tags, it, f.ID, typ, "Evoker Fangs", it.uuid,
		s.dimensionOfWorld(it.w).name, pos,
		box6(f.X-half, f.Y, f.Z-half, f.X+half, f.Y+height, f.Z+half))
	if e != nil {
		e.bind(&s.fangs.mu, &it.tags, &fangsEntityOps)
		e.setCustomName(it.custom.tag())
	}
	return e
}

// killFangs discards the fangs without their bite. vanilla: Entity.kill
func (s *Server) killFangs(it *fangsEntity) {
	s.fangs.mu.Lock()
	it.fangs.Dead = true
	fangsTracker.unfile(it)
	s.fangs.list = slices.DeleteFunc(s.fangs.list, func(o *fangsEntity) bool { return o == it })
	s.fangs.mu.Unlock()
	s.entityDestroyed(it.uuid)
	s.broadcastWorld(it.w, protocol.IDPlayRemoveEntities, removeEntitiesPayload([]int32{it.fangs.ID}))
}
