package server

import (
	"github.com/golem-mc/golem/internal/server/pluginruntime"
	"math"
	"math/rand"
	"slices"
	"time"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/plugin"
	"github.com/golem-mc/golem/registry"

	"github.com/google/uuid"
)

// boltEntity is one lightning bolt standing in a world.
type boltEntity struct {
	bolt *entity.Bolt
	w    *world.World
	uuid uuid.UUID
	tags nbt.StringSet
	effectData
	bodyTrack[struct{}]
	// cause is the player credited by a channeling trident, or uuid.Nil.
	cause uuid.UUID
	// fires counts the blocks the bolt lit; hit holds the entity IDs its
	// box hurt, which are not bystanders.
	fires int
	hit   map[int32]bool
}

// boltSpawnPayload builds a bolt's Spawn Entity.
func boltSpawnPayload(it *boltEntity) []byte {
	typeID, _ := registry.EntityTypeID("minecraft:lightning_bolt")
	return spawnEntityPayload(spawnEntity{
		id: it.bolt.ID, uuid: it.uuid, typ: typeID,
		x: it.bolt.X, y: it.bolt.Y, z: it.bolt.Z,
		yaw: encodeAngle(it.yaw), pitch: encodeAngle(it.pitch),
		vx: it.vx, vy: it.vy, vz: it.vz,
	})
}

// rollThunder rolls each chunk's thunderstorm strike and strikes where it
// hits and rain falls on the target. vanilla: ServerLevel.tickThunder
func (s *Server) rollThunder(dim *dimension, chunks []world.ChunkPos) {
	w := dim.w
	if !w.IsRaining() || !w.IsThundering() {
		return
	}
	s.mu.RLock()
	clients := s.clientsLocked()
	s.mu.RUnlock()
	players := spawnPlayersOf(clients, w)
	for _, pos := range chunks {
		// vanilla: ServerChunkCache.tickChunks rolls thunder only in chunks a
		// player is close to; forced chunks with nobody near skip it.
		if !chunkIsSpawning(players, pos) || !rules.LightningStrikes(s.levelRng, s.lightningOdds) {
			continue
		}
		x := pos.X*16 + int32(s.levelRng.Intn(16))
		z := pos.Z*16 + int32(s.levelRng.Intn(16))
		x, y, z := s.lightningTargetAround(w, x, z)
		if !w.RainingAt(x, y, z) {
			continue
		}
		trap := s.rollTrapHorse(dim, x, y, z)
		s.spawnBolt(w, float64(x)+0.5, float64(y), float64(z)+0.5, trap, plugin.LightningWeather)
	}
}

// lightningTargetAround is where a strike over the column lands: a
// lightning rod in range, else a living body under open sky near the top,
// else the top. vanilla: ServerLevel.findLightningTargetAround
func (s *Server) lightningTargetAround(w *world.World, x, z int32) (int32, int32, int32) {
	y := w.MotionBlockingHeight(x, z) + 1
	if rx, ry, rz, ok := w.ClosestLightningRod(x, y, z); ok {
		return rx, ry + 1, rz
	}
	search := rules.LightningSearchBox(x, y, z)
	var found [][3]int32

	s.mu.RLock()
	clients := slices.Collect(func(yield func(*client) bool) {
		for _, c := range s.clients {
			if c.theWorld() == w && !yield(c) {
				return
			}
		}
	})
	s.mu.RUnlock()
	for _, c := range clients {
		if !c.Alive() || !search.Hits(boxOf(c.Box())) {
			continue
		}
		pos := c.player.Position()
		cell := [3]int32{rules.BlockOf(pos.X), rules.BlockOf(pos.Y), rules.BlockOf(pos.Z)}
		if w.SkyLightAt(cell[0], cell[1], cell[2]) == 15 {
			found = append(found, cell)
		}
	}
	s.mobsMu.Lock()
	for _, m := range s.herdOf(w).herd {
		if !m.Alive() || !search.Hits(boxOf(m.BlastBox())) {
			continue
		}
		cell := [3]int32{rules.BlockOf(m.X), rules.BlockOf(m.Y), rules.BlockOf(m.Z)}
		if w.SkyLightAt(cell[0], cell[1], cell[2]) == 15 {
			found = append(found, cell)
		}
	}
	s.mobsMu.Unlock()
	s.armorStandsMu.Lock()
	for _, ent := range s.armorStands {
		m := ent.stand
		if ent.w != w || m.Dead || m.Health <= 0 || !search.Hits(boxOf(m.BlastBox())) {
			continue
		}
		cell := [3]int32{rules.BlockOf(m.X), rules.BlockOf(m.Y), rules.BlockOf(m.Z)}
		if w.SkyLightAt(cell[0], cell[1], cell[2]) == 15 {
			found = append(found, cell)
		}
	}
	s.armorStandsMu.Unlock()
	s.mannequinsMu.Lock()
	for _, ent := range s.mannequins {
		m := ent.mannequin
		if ent.w != w || m.Dead || m.Dying() || !search.Hits(boxOf(m.BlastBox())) {
			continue
		}
		cell := [3]int32{rules.BlockOf(m.X), rules.BlockOf(m.Y), rules.BlockOf(m.Z)}
		if w.SkyLightAt(cell[0], cell[1], cell[2]) == 15 {
			found = append(found, cell)
		}
	}
	s.mannequinsMu.Unlock()
	if px, py, pz, ok := rules.LightningPickTarget(s.levelRng, found); ok {
		return px, py, pz
	}
	return x, y, z
}

// boxOf gathers a six-number box into a rules.LightningBox.
func boxOf(minX, minY, minZ, maxX, maxY, maxZ float64) rules.LightningBox {
	return rules.LightningBox{MinX: minX, MinY: minY, MinZ: minZ, MaxX: maxX, MaxY: maxY, MaxZ: maxZ}
}

// spawnBolt spawns a bolt and shows it to players in tracking range; it
// returns nil when a plugin vetoes the strike.
func (s *Server) spawnBolt(w *world.World, x, y, z float64, visualOnly bool, cause plugin.LightningCause) *boltEntity {
	id := s.nextEntity()
	it := &boltEntity{
		bolt: entity.NewBolt(id, x, y, z, visualOnly, rand.New(rand.NewSource(s.entitySeed(id)))),
		w:    w,
		uuid: uuid.New(),
	}
	if !s.lightningAllowed(w, it.uuid, cause) {
		return nil
	}
	s.registerBolt(it)
	return it
}

// registerBolt shows a built bolt to players in tracking range.
func (s *Server) registerBolt(it *boltEntity) {
	boltTracker.register(s, it)
}

// boltEffect snapshots one due pass without copying tracking or saved state.
type boltEffect struct {
	live        *boltEntity
	step        entity.BoltStep
	w           *world.World
	uuid, cause uuid.UUID
	x, y, z     float64
	id          int32
	visualOnly  bool
	fires       int
	hit         map[int32]bool
}

// boltEffects are collected under boltMu and applied with no entity lock held.
type boltEffects []boltEffect

// tickBolts ticks every bolt: fire, hurt and end. vanilla: LightningBolt.tick
func (s *Server) tickBolts() {
	s.boltMu.Lock()
	fx := s.boltTickEffects[:0]
	alive := s.bolts[:0]
	for _, it := range s.bolts {
		step := it.bolt.Step()
		if step != (entity.BoltStep{}) {
			// Commands share this lock: one strike keeps one position and cause.
			b := it.bolt
			fx = append(fx, boltEffect{live: it, step: step, w: it.w, uuid: it.uuid, cause: it.cause,
				x: b.X, y: b.Y, z: b.Z, id: b.ID, visualOnly: b.VisualOnly, fires: it.fires, hit: it.hit})
		}
		if !step.Done {
			alive = append(alive, it)
		}
	}
	s.bolts = alive
	s.boltMu.Unlock()
	fx.apply(s)
	clear(fx)
	s.boltTickEffects = fx[:0]
}

// apply preserves each bolt's fire/block/hurt/advancement order, then tells removals.
func (fx boltEffects) apply(s *Server) {
	for i := range fx {
		it := &fx[i]
		step := it.step
		if step.Strike && rules.LightningFiresAtStrike(s.world.Difficulty()) {
			s.boltFire(it, 4)
		}
		if step.Strike {
			s.boltStrikeBlocks(it)
			s.gameEvent(it.w, rules.GameEventLightningStrike, it.x, it.y, it.z, rules.GameEventContext{})
		}
		if step.Flash {
			s.boltFire(it, 0)
		}
		if step.Harm {
			s.boltHurt(it)
		}
		if step.Done {
			s.boltAdvancements(it)
		}
	}
	if len(fx) == 0 {
		return
	}
	s.boltMu.Lock()
	for _, it := range fx {
		it.live.fires, it.live.hit = it.fires, it.hit
	}
	s.boltMu.Unlock()
	for _, it := range fx {
		if it.step.Done {
			s.broadcastWorld(it.w, protocol.IDPlayRemoveEntities, removeEntitiesPayload([]int32{it.id}))
		}
	}
}

// boltStrikeBlocks powers the struck rod and scrapes nearby copper.
// vanilla: LightningBolt.powerLightningRod, LightningBolt.clearCopperOnLightningStrike
func (s *Server) boltStrikeBlocks(it *boltEffect) {
	// The strike cell is the one under the bolt.
	x, y, z := rules.BlockOf(it.x), rules.BlockOf(it.y-1e-6), rules.BlockOf(it.z)
	s.afterLecternWake(it.w, func(p *world.Physics) ([]world.BlockChange, []world.Effect, []world.TNTPrime) {
		return p.LightningStruck(x, y, z)
	})
}

// boltFire lights the strike cell and extra cells beside it where air and
// fire can survive. vanilla: LightningBolt.spawnFire
func (s *Server) boltFire(it *boltEffect, extra int) {
	b, w := it, it.w
	x, y, z := rules.BlockOf(b.x), rules.BlockOf(b.y), rules.BlockOf(b.z)
	if b.visualOnly || !(fireSpreadChecker{s: s, dim: s.dimensionOfWorld(w)}).FireCanSpread(x, y, z) {
		return
	}
	light := func(x, y, z int32) {
		if rules.LightningLightsFire(w, x, y, z) && !s.pluginIgniteByLightning(w, x, y, z) {
			s.setBlockAndTell(w, x, y, z, rules.FireStateForPlacement(w, x, y, z))
			it.fires++
		}
	}
	light(x, y, z)
	for range extra {
		dx, dy, dz := rules.LightningFireOffset(s.levelRng)
		light(x+dx, y+dy, z+dz)
	}
}

func (it *boltEffect) markHit(id int32) {
	if it.hit == nil {
		it.hit = map[int32]bool{}
	}
	it.hit[id] = true
}

// boltAdvancements fires lightning_strike for every player within 256 blocks
// of a spent bolt, with the living entities within 15 blocks it did not hurt
// as bystanders. vanilla: LightningBolt.tick
func (s *Server) boltAdvancements(it *boltEffect) {
	b, w := it, it.w
	view := boxOf(b.x-15, b.y-15, b.z-15, b.x+15, b.y+6+15, b.z+15)
	s.mu.RLock()
	var clients []*client
	watched := false
	for _, c := range s.clients {
		if c.theWorld() == w {
			clients = append(clients, c)
			watched = watched || c.advancements.Load() != nil
		}
	}
	s.mu.RUnlock()
	if !watched {
		return
	}
	var bystanders []string
	for _, c := range clients {
		if c.Alive() && !it.hit[c.player.EntityID] && view.Hits(boxOf(c.Box())) {
			bystanders = append(bystanders, "minecraft:player")
		}
	}
	s.mobsMu.Lock()
	for _, m := range s.herdOf(w).herd {
		if m.Alive() && !it.hit[m.ID] && view.Hits(boxOf(m.BlastBox())) {
			bystanders = append(bystanders, m.Type.Name())
		}
	}
	s.mobsMu.Unlock()
	now := time.Now()
	for _, c := range clients {
		pos := c.player.Position()
		dx, dy, dz := pos.X-b.x, pos.Y-b.y, pos.Z-b.z
		if float32(math.Sqrt(dx*dx+dy*dy+dz*dz)) >= 256 {
			continue
		}
		if progress := c.advancements.Load(); progress != nil {
			progress.LightningStrike(point(pos), [3]float64{b.x, b.y, b.z}, it.fires, bystanders, now)
		}
	}
}

// boltHurt hits every body in the bolt's box for one tick.
// vanilla: LightningBolt.tick, Entity.thunderHit
func (s *Server) boltHurt(it *boltEffect) {
	b, w := it, it.w
	box := rules.LightningHurtBox(b.x, b.y, b.z)

	s.mu.RLock()
	var clients []*client
	for _, c := range s.clients {
		if c.theWorld() == w {
			clients = append(clients, c)
		}
	}
	s.mu.RUnlock()
	// struck is the type of every body this tick's box holds, for the cause's
	// channeled_lightning; taken before a conversion replaces one.
	var struck []string
	for _, c := range clients {
		if !c.Alive() || !box.Hits(boxOf(c.Box())) {
			continue
		}
		it.markHit(c.player.EntityID)
		struck = append(struck, "minecraft:player")
		var ignite bool
		c.player.UpdateVitals(func(v *player.Vitals) {
			v.FireTicks, ignite = rules.LightningThunderHitFire(v.FireTicks)
		})
		if ignite {
			c.igniteForTicks(rules.LightningIgniteTicks)
		}
		c.hurt(rules.LightningDamage, rules.DamageTypeLightningBolt, nil)
	}

	s.mobsMu.Lock()
	var hits []*entity.Mob
	var converts, flipped, charges []*mobEntity
	listening := s.plugins != nil && s.plugins.Has(pluginruntime.CreeperPower)
	h := s.herdOf(w)
	for _, m := range h.herd {
		if !m.Alive() || !box.Hits(boxOf(m.BlastBox())) {
			continue
		}
		it.markHit(m.ID)
		struck = append(struck, m.Type.Name())
		// vanilla: Pig.thunderHit, Villager.thunderHit, which skip
		// Entity.thunderHit when they convert
		if (m.Type == entity.TypePig || m.Type == entity.TypeVillager) && s.world.Difficulty() != 0 {
			converts = append(converts, h.book[m])
			continue
		}
		// vanilla: MushroomCow.thunderHit, which never burns or hurts
		if m.Type == entity.TypeMooshroom {
			if ent := h.book[m]; ent.lastBolt != it.uuid {
				ent.lastBolt = it.uuid
				if m.Mushroom == "brown" {
					m.SetMetadataMushroom("red")
				} else {
					m.SetMetadataMushroom("brown")
				}
				if !m.Silent {
					flipped = append(flipped, ent)
				}
			}
			continue
		}
		holdCharge := listening && entity.Family(m.Type) == entity.TypeCreeper
		landed, softened := m.ThunderHit(holdCharge)
		if holdCharge {
			charges = append(charges, h.book[m])
		}
		if m.Type == entity.TypeCopperGolem {
			// vanilla: CopperGolem.thunderHit, after Entity.thunderHit's burn and hurt
			m.CopperStruck(it.uuid)
		}
		// A hit the damage window softened sends no damage event.
		if landed && !softened {
			hits = append(hits, m)
		}
	}
	s.mobsMu.Unlock()
	s.chargeCreepers(w, charges)
	var burned []*itemEntity
	s.itemsMu.Lock()
	for _, ent := range s.items {
		if ent.item.Dead || ent.w != w || !box.Hits(boxOf(ent.item.BlastBox())) {
			continue
		}
		it.markHit(ent.item.ID)
		struck = append(struck, "minecraft:item")
		if ent.item.ThunderHit() {
			burned = append(burned, ent)
		}
	}
	s.itemsMu.Unlock()
	var orbsGone []int32
	s.orbsMu.Lock()
	for _, ent := range s.orbs {
		if ent.orb.Dead || ent.w != w || !box.Hits(boxOf(ent.orb.BlastBox())) {
			continue
		}
		it.markHit(ent.orb.ID)
		struck = append(struck, "minecraft:experience_orb")
		if ent.orb.HurtDead(rules.LightningDamage) {
			orbsGone = append(orbsGone, ent.orb.ID)
		}
	}
	s.orbsMu.Unlock()
	// Boats and minecarts alike, read as a projectile's targets.
	vehicles, _, _ := s.boatSnapshots()
	var vehicleHits []int32
	for _, t := range vehicles[w] {
		if box.Hits(boxOf(t.MinX, t.MinY, t.MinZ, t.MaxX, t.MaxY, t.MaxZ)) {
			it.markHit(t.EntityID)
			struck = append(struck, registry.EntityType(t.Type).Name())
			vehicleHits = append(vehicleHits, t.EntityID)
		}
	}
	// Every crystal in the box is struck, an invulnerable one too: its
	// hurtServer refusing the hit happens after the bolt listed it.
	var crystalHits []int32
	for _, t := range s.endCrystalSnapshots()[w] {
		if box.Hits(boxOf(t.MinX, t.MinY, t.MinZ, t.MaxX, t.MaxY, t.MaxZ)) {
			it.markHit(t.EntityID)
			struck = append(struck, "minecraft:end_crystal")
			crystalHits = append(crystalHits, t.EntityID)
		}
	}
	var mannequinHits []*mannequinEntity
	s.mannequinsMu.Lock()
	for _, ent := range s.mannequins {
		m := ent.mannequin
		if ent.w != w || m.Dead || m.Dying() || !box.Hits(boxOf(m.BlastBox())) {
			continue
		}
		it.markHit(m.ID)
		struck = append(struck, mannequinEntityID)
		mannequinHits = append(mannequinHits, ent)
	}
	s.mannequinsMu.Unlock()
	cause := it.cause
	if cause != uuid.Nil && len(struck) > 0 {
		if c := s.clientByUUID(cause); c != nil {
			if progress := c.advancements.Load(); progress != nil {
				progress.ChanneledLightning(struck, time.Now())
			}
		}
	}
	for _, ent := range converts {
		s.convertStruck(ent)
	}
	for _, ent := range flipped {
		s.broadcastSoundID(w, registry.SoundEntityMooshroomConvert, protocol.SoundCategoryNeutral,
			ent.mob.X, ent.mob.Y, ent.mob.Z, 2.0, 1.0)
	}
	for _, m := range hits {
		s.broadcastWorld(w, protocol.IDPlayDamageEvent,
			damageEventPayload(m.ID, rules.DamageTypeLightningBolt, 0, 0))
		s.broadcastWorld(w, protocol.IDPlayHurtAnimation, hurtAnimationPayload(m.ID, 0))
	}
	if len(burned) > 0 {
		ids := make([]int32, len(burned))
		spills := make([]itemSpill, len(burned))
		for i, ent := range burned {
			ids[i], spills[i] = ent.item.ID, spillOf(ent)
		}
		s.broadcastWorld(w, protocol.IDPlayRemoveEntities, removeEntitiesPayload(ids))
		s.spillContents(spills)
	}
	for _, ent := range mannequinHits {
		s.hurtMannequin(ent, rules.LightningDamage, rules.DamageTypeLightningBolt, 0, 0, nil, false)
	}
	// vanilla: Cushion.thunderHit
	for _, ent := range s.cushionsWhere(w, func(minX, minY, minZ, maxX, maxY, maxZ, _, _, _ float64) bool {
		return box.Hits(boxOf(minX, minY, minZ, maxX, maxY, maxZ))
	}) {
		it.markHit(ent.id)
		s.breakCushion(ent, cushionStruck)
	}
	if len(orbsGone) > 0 {
		s.broadcastWorld(w, protocol.IDPlayRemoveEntities, removeEntitiesPayload(orbsGone))
	}
	// A struck vehicle's hurt is fifty, past the forty that breaks it: the
	// boat or cart tick's next pass takes it out with its item.
	// vanilla: VehicleEntity.hurtServer
	for _, id := range vehicleHits {
		s.boatProjectileHit(boatProjectileBlow{w: w, boatID: id, damage: rules.LightningDamage})
	}
	// A bolt has no source entity, so the crystal's blast has none either;
	// lightning_bolt is not is_explosion, so the blast runs.
	// vanilla: EndCrystal.hurtServer
	for _, id := range crystalHits {
		s.projectileHitEndCrystal(crystalProjectileHit{w: w, crystalID: id})
	}
}

// convertStruck replaces a struck pig with a zombified piglin, or a struck
// villager with a witch, as a SINGLE conversion that drops the old gear.
// The alive check under the mob lock stops a mob that died since the bolt
// from converting.
// vanilla: Pig.thunderHit, ConversionParams.single(this, false, true);
// Villager.thunderHit, ConversionParams.single(this, false, false)
func (s *Server) convertStruck(ent *mobEntity) {
	into := entity.TypeWitch
	if ent.mob.Type == entity.TypePig {
		into = entity.TypeZombifiedPiglin
	}
	if !s.transformAllowed(ent, into, plugin.TransformLightning) {
		return
	}
	s.mobsMu.Lock()
	h := s.herdOf(ent.w)
	if h.book[ent.mob] != ent || ent.mob.Dead {
		s.mobsMu.Unlock()
		return
	}
	h.remove(ent.mob)
	h.forget(ent.mob)
	holder := ent.mob.LeashHolderID
	s.mobsMu.Unlock()
	// Read off the board before the removal takes the old body off its team:
	// the tick's copy may be a tick old.
	team, _ := s.scoreboard.PlayerTeam(ent.uuid.String())
	s.scoreboard.EntityRemoved(ent.uuid.String())

	s.broadcastWorld(ent.w, protocol.IDPlayRemoveEntities, removeEntitiesPayload([]int32{ent.mob.ID}))
	pig := ent.mob.Type == entity.TypePig
	kind := entity.TypeWitch
	if pig {
		kind = entity.TypeZombifiedPiglin
	}
	id := s.nextEntity()
	m := entity.NewMob(kind, id, ent.mob.X, ent.mob.Y, ent.mob.Z, s.entitySeed(id))
	m.ConvertedFrom(ent.mob)
	for slot := range 6 {
		m.SetEquipment(slot, protocol.ItemStack{})
	}
	m.ResetDropChances()
	m.LeashHolderID = holder
	if pig {
		m.EquipPiglinHand()
	} else {
		// A witch is never a baby (Mob.setBaby is a no-op), and finalizeSpawn
		// runs after the conversion, so its left-handed roll stands.
		m.SetMetadataBabyAge(0)
		effective := s.effectiveDifficultyAt(ent.w, m.X, m.Z)
		local := rules.NewLocalDifficulty(s.world.Difficulty(), effective)
		m.FinalizeSpawn(m.SpawnDice(), ent.w.Biome(rules.BlockOf(m.X), rules.BlockOf(m.Y), rules.BlockOf(m.Z)), nil, local)
	}
	converted := convertedEntity(ent, m, ent.w, pig)
	converted.convertTeam = team
	m.Persistence = true // convertedEntity copies the old mob's flag
	if !pig {
		// vanilla: Raider.finalizeSpawn, a witch not spawned NATURAL can join raids
		converted.raider = &raiderRef{ent: converted, canJoin: true}
	}
	s.addRiddenConversion(ent, converted)
}

// boltTag is LightningBolt.addAdditionalSaveData with the base entity fields.
func boltTag(it *boltEntity) *nbt.OrderedCompound {
	return boltTagAs(it, "minecraft:lightning_bolt")
}

func boltTagAs(it *boltEntity, id string) *nbt.OrderedCompound {
	b := it.bolt
	tag := orderedEntityBaseTag(id, b.X, b.Y, b.Z, it.vx, it.vy, it.vz,
		it.yaw, it.pitch, it.uuid, it.onGround, it.cooldown, it.fire, 0)
	it.put(tag)
	putTags(tag, it.tags.Values())
	return tag
}

func (s *Server) boltSnapshot(it *boltEntity, q *entityQuery) *entitySnapshot {
	typ := "minecraft:lightning_bolt"
	if rejectsEntityHead(q, typ, it.uuid, it.tags) {
		return nil
	}
	b := it.bolt
	pos := player.Position{X: b.X, Y: b.Y, Z: b.Z, Yaw: it.yaw, Pitch: it.pitch}
	e := s.snapshot(q, it.tags, it, b.ID, typ, "Lightning Bolt", it.uuid,
		s.dimensionOfWorld(it.w).name, pos, box6(b.X, b.Y, b.Z, b.X, b.Y, b.Z))
	if e != nil {
		e.bind(&s.boltMu, &it.tags, &boltEntityOps)
		e.setCustomName(it.custom.tag())
	}
	return e
}

// killBolt removes a bolt immediately, including while the game is frozen.
// vanilla: Entity.kill
func (s *Server) killBolt(it *boltEntity) {
	s.boltMu.Lock()
	s.bolts = slices.DeleteFunc(s.bolts, func(o *boltEntity) bool { return o == it })
	s.boltMu.Unlock()
	s.entityDestroyed(it.uuid)
	s.broadcastWorld(it.w, protocol.IDPlayRemoveEntities, removeEntitiesPayload([]int32{it.bolt.ID}))
}

// chargeCreepers asks CreeperPowerEvent for each creeper the bolt struck and
// charges those not refused, with no lock held while the listeners run.
// vanilla: Creeper.thunderHit
func (s *Server) chargeCreepers(w *world.World, creepers []*mobEntity) {
	if len(creepers) == 0 {
		return
	}
	refused := make([]bool, len(creepers))
	for i, ent := range creepers {
		e := &plugin.CreeperPowerEvent{Creeper: pendingMob{id: ent.uuid}}
		s.plugins.Fire(e)
		refused[i] = e.Cancelled()
	}
	s.mobsMu.Lock()
	defer s.mobsMu.Unlock()
	h := s.herdOf(w)
	for i, ent := range creepers {
		if !refused[i] && h.book[ent.mob] == ent {
			ent.mob.SetMetadataPowered(true)
		}
	}
}
