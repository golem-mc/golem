package server

import (
	"github.com/golem-mc/golem/plugin"
	"math/rand"
	"slices"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
)

// The server side of an explosion. The decisions (which blocks go, each
// body's exposure, damage and push, what a removed block leaves) are in
// internal/server/rules/explosion.go; this file reads the bodies, applies the
// decisions and tells the clients.
//
// Order matches vanilla: blocks are ray-cast and entities measured against
// the intact world, then the blocks are removed.
// vanilla: ServerExplosion.explode (calculateExplodedPositions,
// hurtEntities, interactWithBlocks)

// blast is one queued explosion. causeID/causeName are the indirect source
// (damage event cause, death message killer); directID is the carrier, e.g.
// the primed TNT. damageType is player_explosion when both sources are
// entities, else explosion (vanilla: Explosion.getDefaultDamageSource); a bed
// passes bad_respawn_point. rng is the exploding entity's own, or the loot
// dice for a bed. A blast runs outside every lock.
type blast struct {
	w         *world.World
	x, y, z   float64
	radius    float64
	causeID   int32
	causeName string
	// causeKiller is the name the blast's death message carries — a mob's
	// translated kind for a creeper — or nil for a blast with nobody behind
	// it. causeName is only the plain word the logging and a primed TNT's
	// owner record keep.
	causeKiller *rules.Killer
	// causeUUID is the source's persistent identity: a TNT block the blast
	// primes is saved as owned by it.
	causeUUID uuid.UUID
	directID  int32
	// directUUID is the exploding entity, asked of the plugin listeners; the
	// zero UUID for a blast without one.
	directUUID uuid.UUID
	// directName names the carrier for a death message when no living entity
	// caused the blast.
	directName *protocol.Text
	// directType is the carrier's entity type, the THIS_ENTITY of the
	// destroyed blocks' loot rolls. vanilla: BlockBehaviour.onExplosionHit
	directType string
	// directLoot is the carrier's snapshot when it has one, frozen at the
	// blast; nil rolls with the type alone.
	directLoot *rules.LootEntity
	// directInWater is whether the carrier (the primed TNT, the creeper)
	// stood in water, which spares a block-attached body.
	// vanilla: BlockAttachedEntity.ignoreExplosion
	directInWater bool
	// directLiving is whether the carrier is a living entity (the creeper),
	// which a blocking shield shoves the holder away from.
	directLiving bool
	damageType   registry.DamageType
	// silent is the source entity's Silent flag, which mutes the blast's
	// sound. vanilla: ServerLevel.explode
	silent bool
	rng    *rand.Rand
	// chargedHeads is set on a charged creeper's blast: the first kill that
	// has a head to drop drops it; headless kills leave it set.
	// vanilla: Creeper.killedEntity
	chargedHeads bool
	// blockResist overrides a cell's blast resistance, including water-braced
	// anchors and portal cells; nil uses the block's default resistance.
	blockResist func(bx, by, bz int32) (float64, bool)
	// spared reports a cell the blast leaves standing though its rays reach
	// it; nil for none. vanilla: Entity.shouldBlockExplode
	spared func(bx, by, bz int32) bool
	// interaction is the ExplosionInteraction the caller chose, resolved by
	// game rules. vanilla: ServerLevel.explode
	interaction rules.ExplosionInteraction
	// harmless is a damage calculator that hurts no entity: the blast only
	// pushes. knockbackScale is its knockback multiplier, 0 meaning 1.
	// vanilla: SimpleExplosionDamageCalculator
	harmless       bool
	knockbackScale float64
	// burst replaces the packet's particle and sound with the wind charge
	// burst's own. vanilla: ServerLevel.explode
	burst       bool
	breezeBurst bool
	// look replaces the packet's particles and sound with a data pack
	// explode effect's. vanilla: ExplodeEffect.apply
	look *registry.EnchExplosionLook
	// gust is the burst of a wind_charged body's death: the large emitter
	// and the breeze's sound. vanilla: WindChargedMobEffect.onMobRemoved
	gust bool
	// fire is whether the blast leaves fires on the cleared ground, the
	// bed's and the respawn anchor's. vanilla: ServerExplosion.fire
	fire bool
	// reflectedFireball marks a large fireball a player owns, which kills
	// a ghast it blasts.
	reflectedFireball bool
	// sourceTeam is the scoreboard team of the exploding entity itself (the
	// creeper, TNT minecart, end crystal or fireball), "" for none.
	// vanilla: ServerExplosion.source
	sourceTeam string
	// source is the block that exploded (a bed or an anchor), asked of the
	// plugin listeners; nil for any other blast.
	source *blastSourceBlock
	// yield replaces the share of blocks that drop when a listener changed
	// it; nil keeps the interaction's own.
	yield *float32
}

// blastSourceBlock is the block a block explosion comes from, with the state
// it had before it was removed.
type blastSourceBlock struct {
	x, y, z int32
	state   uint16
}

// playerBlast is one player's share of a blast, one per player of its
// world. pushable is false for one out of reach, and for a spectator or a
// flying creative player, whom vanilla leaves out of the hit-players map the
// packet's knockback comes from.
type playerBlast struct {
	c          *client
	kx, ky, kz float64
	pushable   bool
	// reached is whether the blast hit the player at all. vanilla: Entity.onExplosionHit
	reached bool
}

// pushedMotion is a velocity packet for a body the blast pushed, sent the
// same tick as vanilla's tracking pass would, to the clients already
// tracking it. Destroyed bodies get a removal instead.
type pushedMotion struct {
	data     []byte
	watchers []uuid.UUID
}

// blastInputs is what a blast reads before it hurts anything.
type blastInputs struct {
	cancelled bool
	at        rules.Blast
	blocks    *world.BlockSnapshot
	// destroyed is the ray-cast against the intact world, less the cells
	// the blast spares.
	destroyed rules.BlastBlocks
	// items and orbs are read before any hurt lands — vanilla's
	// hurtEntities collects its bodies first — so what a death during the
	// pass drops, the victim's inventory and experience, is not among them
	// and survives the blast that killed them.
	// vanilla: ServerExplosion.hurtEntities
	items []*itemEntity
	orbs  []*orbEntity
	// clients are the players in the blast's world.
	clients []*client
	// sparedTeam is the source's team when its friendly fire is off, whose
	// members the blast pushes but does not hurt; teams maps each member to
	// its team then. vanilla: Entity.doTeamsAllowDamage
	sparedTeam string
	teams      map[string]string
	// causeCreative is whether the indirect source — the player who lit the
	// TNT, looked up by the identity the blast carries — is a creative
	// player.
	causeCreative bool
	causeIsPlayer bool
}

// blastEffects is what the steps under the bodies' locks leave for after
// them. applyBlastEffects applies the first group in field order, the kinds'
// order; sendBlast sends the second with the Explosion packet.
type blastEffects struct {
	heads     []mobDrops
	paintings []paintingTell
	frames    []frameTell
	knots     []knotTell
	cushions  []*fixtureEntity
	stands    []*armorStandEntity
	crystals  []*endCrystalEntity
	// removed are the IDs of the items and orbs the hurt killed.
	removed []int32
	// spills are the stacks of the items the hurt killed.
	spills  []itemSpill
	bullets []arrowBulletHit

	players   []playerBlast
	pushed    []pushedMotion
	destroyed int
	// cancelled is a listener's cancelled explode event: the blocks stay and
	// no packet or sound goes out, the entities are still hurt.
	cancelled bool
}

// runExplosion applies one blast and sends the packets it calls for.
func (s *Server) runExplosion(blast blast) {
	// vanilla: ServerExplosion.explode
	var ok bool
	if blast, ok = s.explosionPrime(blast); !ok {
		return
	}
	s.gameEvent(blast.w, rules.GameEventExplode, blast.x, blast.y, blast.z, rules.GameEventContext{})
	s.sendBlast(blast, s.applyBlast(blast))
}

// applyBlast decides a blast and applies it, in phases: the inputs are read
// with no lock held; each kind's bodies are stepped under that kind's lock
// alone, the hurts and pushes landing on the bodies and the rest collected;
// the collected effects are applied once the locks are released; then the
// blocks go. No mob lock is held while a player lock is taken.
func (s *Server) applyBlast(b blast) blastEffects {
	in := s.blastInputs(b)
	if b.source != nil {
		b = s.blockExplodeEvent(b, &in)
	} else if b.directUUID != (uuid.UUID{}) {
		b = s.entityExplodeEvent(b, &in)
	}
	defer func() {
		if in.blocks != nil {
			in.blocks.Release()
		}
	}()
	fx := blastEffects{destroyed: len(in.destroyed), cancelled: in.cancelled}
	s.blastPlayers(b, &in, &fx)
	s.blastMobs(b, &in, &fx)
	s.blastBoats(b, &in)
	s.blastMinecarts(b, &in, &fx)
	s.blastHanging(b, &in, &fx)
	s.blastStands(b, &in, &fx)
	fx.crystals = s.blastEndCrystals(b, &in)
	s.blastPushed(b, &in, &fx)
	in.blocks.Release()
	in.blocks = nil
	s.applyBlastEffects(b.w, &fx, in.causeCreative, b.causeID, in.causeIsPlayer)
	s.blastBlocks(b, in.destroyed)
	return fx
}

// blastInputs reads the blast's destroyed cells, the items and orbs it may
// hurt, the players of its world and its source's mode.
func (s *Server) blastInputs(b blast) blastInputs {
	// Each blast starts after the preceding blast has applied its blocks.
	in := blastInputs{
		at:     rules.Blast{X: b.x, Y: b.y, Z: b.z, Radius: b.radius, KnockbackScale: b.knockbackScale},
		blocks: b.w.SnapshotBlocks(),
	}
	in.destroyed = rules.ExplosionBlocks(in.blocks, b.rng, b.x, b.y, b.z, b.radius, b.blockResist)
	if b.spared != nil {
		in.destroyed = slices.DeleteFunc(in.destroyed, func(c [3]int32) bool { return b.spared(c[0], c[1], c[2]) })
	}
	s.itemsMu.Lock()
	in.items = slices.Clone(s.items)
	s.itemsMu.Unlock()
	s.orbsMu.Lock()
	in.orbs = slices.Clone(s.orbs)
	s.orbsMu.Unlock()
	s.mu.RLock()
	in.clients = make([]*client, 0, len(s.clients))
	for _, c := range s.clients {
		if c.theWorld() == b.w {
			in.clients = append(in.clients, c)
		}
	}
	in.causeCreative = creativeClient(s.clients[b.causeUUID])
	in.causeIsPlayer = s.clients[b.causeUUID] != nil
	s.mu.RUnlock()
	if b.sourceTeam != "" {
		if t, ok := s.scoreboard.Team(b.sourceTeam); ok && !t.FriendlyFire {
			in.sparedTeam, in.teams = b.sourceTeam, s.scoreboard.MemberTeams()
		}
	}
	return in
}

// spares reports whether the blast pushes the body held by id without
// hurting it: it is on the source's team, whose friendly fire is off.
// vanilla: Entity.doTeamsAllowDamage
func (in *blastInputs) spares(id uuid.UUID) bool {
	return in.sparedTeam != "" && in.teams[id.String()] == in.sparedTeam
}

// sparesHanging reports whether both the hurt and the push leave a hanging
// body standing: invulnerability and friendly fire only refuse the hurt.
// vanilla: ServerExplosion.hurtEntities, BlockAttachedEntity.push
func (in *blastInputs) sparesHanging(w rules.BlockReader, id uuid.UUID, invulnerable bool, minX, minY, minZ, maxX, maxY, maxZ float64) bool {
	return (invulnerable && !in.causeCreative || in.spares(id)) &&
		rules.ExplosionExposure(w, minX, minY, minZ, maxX, maxY, maxZ, in.at.X, in.at.Y, in.at.Z) == 0
}

// blastPlayers hurts the players in reach through the normal hurt pipeline
// (invulnerability is its call) as they are found, holding no lock, and
// keeps each one's push for the Explosion packet.
func (s *Server) blastPlayers(b blast, in *blastInputs, fx *blastEffects) {
	difficulty := b.w.Difficulty()
	fx.players = make([]playerBlast, len(in.clients))
	for i, c := range in.clients {
		fx.players[i].c = c
		pos := c.player.Position()
		dist, ok := in.at.Reach(pos.X, pos.Y, pos.Z)
		if !ok {
			continue
		}
		var resist float64
		c.player.Attributes(func(a *player.Attributes) {
			resist = a.Value(rules.ExplosionKnockbackResistanceAttribute)
		})
		// vanilla: ServerExplosion.hurtEntities aims from getEyePosition, which follows the pose
		eyeY := pos.Y + c.dims(c.player.Appearance().Pose).EyeHeight
		hit := in.at.Hit(in.blocks, dist, c.Box, func() (x, y, z float64) { return pos.X, eyeY, pos.Z }).Resisted(resist)
		fx.players[i] = playerBlast{c: c, kx: hit.KX, ky: hit.KY, kz: hit.KZ,
			pushable: rules.ExplosionPushesPlayer(c.player.GameMode(), c.flying.Load()), reached: true}
		if p := &fx.players[i]; p.pushable {
			pusher := b.directUUID
			if pusher == uuid.Nil {
				pusher = b.causeUUID
			}
			p.kx, p.ky, p.kz, p.pushable = s.knockbackEvent(c.pluginSelf(), pusher, p.kx, p.ky, p.kz)
		}
		if in.sparedTeam != "" && in.teams[c.player.Name] == in.sparedTeam {
			continue
		}
		if damage := rules.ExplosionAfterDifficulty(hit.Damage, difficulty); damage > 0 && !b.harmless {
			c.hurt(damage, b.damageType, blastCause(b))
		}
	}
}

// blastMobs hurts and pushes the mobs in reach: raw damage, difficulty
// scaling being player-only. The exploding creeper was already removed from
// the herd. A charged creeper's blast keeps the first head a kill drops.
func (s *Server) blastMobs(b blast, in *blastInputs, fx *blastEffects) {
	w := b.w
	s.mobsMu.Lock()
	defer s.mobsMu.Unlock()
	for _, m := range s.herdOf(w).herd {
		// vanilla: ServerExplosion.hurtEntities leaves the source entity out
		if b.directLiving && m.ID == b.directID {
			continue
		}
		if m.Type == entity.TypeEnderDragon {
			s.blastDragon(b, in, m)
			continue
		}
		dist, ok := in.at.Reach(m.X, m.Y, m.Z)
		if !ok {
			continue
		}
		hit := in.at.Hit(in.blocks, dist, m.BlastBox, m.EyePosition).
			Resisted(m.ExplosionKnockbackResistance())
		if b.harmless || (in.sparedTeam != "" && m.Team == in.sparedTeam) {
			m.PushByBlast(hit.KX, hit.KY, hit.KZ)
			continue
		}
		wasAlive := m.Alive()
		damage := hit.Damage
		if b.reflectedFireball && m.ReflectedFireballKills() {
			damage = entity.ReflectedFireballDamage
		}
		s.hurtByBlast(b, m, damage, hit.KX, hit.KY, hit.KZ, in.causeCreative)
		if b.chargedHeads && wasAlive && m.Dead && w.BoolRule(world.RuleMobDrops) {
			if stacks := m.RollChargedHead(s.lootLevelAt(w, m.X, m.Y, m.Z)); len(stacks) > 0 {
				fx.heads = append(fx.heads, mobDrops{w: w, stacks: stacks, x: m.X, y: m.Y, z: m.Z})
				b.chargedHeads = false
			}
		}
	}
}

// blastBoats hurts and pushes the boats in reach. The blast's hurt lays the
// tenfold a blow's own does, the total past forty destroying the hull,
// which the next boat tick takes out with the item and cargo its break
// leaves. The push goes on whatever the hurt did. Only a mob's blast with
// mob_griefing off spares a boat, the hurt and the push both.
// vanilla: ServerExplosion.hurtEntities, VehicleEntity.hurtServer,
// VehicleEntity.ignoreExplosion, Entity.push
func (s *Server) blastBoats(b blast, in *blastInputs) {
	if b.interaction == rules.ExplosionKeep {
		return
	}
	s.boatsMu.Lock()
	defer s.boatsMu.Unlock()
	for _, it := range s.boats {
		if it.boat.Dead || it.w != b.w {
			continue
		}
		if dist, ok := in.at.Reach(it.boat.X, it.boat.Y, it.boat.Z); ok {
			hit := in.at.Hit(in.blocks, dist, it.boat.BlastBox, it.boat.EyePosition)
			if !b.harmless && !in.spares(it.uuid) {
				it.boat.Hurt(hit.Damage, b.damageType, in.causeCreative)
			}
			it.boat.Push(hit.KX, hit.KY, hit.KZ)
		}
	}
}

// blastMinecarts pushes the carts in reach and hurts them, the damage landing
// once the cart lock is released: a hurt takes the lock itself, and a TNT
// cart's explosion source ignites it.
// vanilla: ServerExplosion.hurtEntities, VehicleEntity.ignoreExplosion,
// MinecartTNT.damageSourceIgnitesTnt
func (s *Server) blastMinecarts(b blast, in *blastInputs, fx *blastEffects) {
	if b.interaction == rules.ExplosionKeep {
		return
	}
	type hurt struct {
		it     *minecartEntity
		damage float64
	}
	var hurts []hurt
	s.minecartsMu.Lock()
	for _, it := range s.minecarts {
		if it.cart.Dead || it.w != b.w {
			continue
		}
		dist, ok := in.at.Reach(it.cart.X, it.cart.Y, it.cart.Z)
		if !ok {
			continue
		}
		hit := in.at.Hit(in.blocks, dist, it.cart.BlastBox, it.cart.EyePosition)
		it.cart.Push(hit.KX, hit.KY, hit.KZ)
		queueMotion(fx, &it.bodyTrack, it.cart.ID, it.cart.VX, it.cart.VY, it.cart.VZ)
		if !b.harmless && !in.spares(it.uuid) {
			hurts = append(hurts, hurt{it, hit.Damage})
		}
	}
	s.minecartsMu.Unlock()
	for _, h := range hurts {
		s.minecartHurtBy(h.it, b.w, h.damage, in.causeCreative, entity.TNTCartHit{DamageType: b.damageType, OwnerID: b.causeID, Ignites: true})
	}
}

// blastHanging takes down the paintings, frames, knots and cushions in
// reach. A refused hurt still leaves a push that breaks an exposed body.
// Fixed frames ignore the explosion, and knots drop no item.
// vanilla: ServerExplosion.hurtEntities, BlockAttachedEntity.hurtServer,
// BlockAttachedEntity.push, ItemFrame.ignoreExplosion
func (s *Server) blastHanging(b blast, in *blastInputs, fx *blastEffects) {
	if b.interaction.SparesHanging(b.directInWater) {
		return
	}
	s.paintingsMu.Lock()
	var gone []*paintingEntity
	for _, p := range s.paintings {
		if _, ok := in.at.Reach(paintingCentreX(p), paintingCentreY(p), paintingCentreZ(p)); ok && p.w == b.w &&
			!in.sparesHanging(in.blocks, p.uuid, p.invulnerable, p.minX, p.minY, p.minZ, p.maxX, p.maxY, p.maxZ) {
			gone = append(gone, p)
		}
	}
	if s.hangingBreakListened() {
		s.paintingsMu.Unlock()
		by := removedByBlast(b)
		gone = slices.DeleteFunc(gone, func(p *paintingEntity) bool { return !s.askHangingBreak(p.uuid, by) })
		s.paintingsMu.Lock()
		gone = slices.DeleteFunc(gone, func(p *paintingEntity) bool { return s.paintingByIDLocked(b.w, p.id) != p })
	}
	for _, p := range gone {
		fx.paintings = append(fx.paintings, paintingTellOf(p))
		s.removePaintingLocked(p)
	}
	s.paintingsMu.Unlock()

	s.framesMu.Lock()
	var goneFrames []*frameEntity
	for _, f := range s.frames {
		if _, ok := in.at.Reach(frameCentreX(f), frameCentreY(f), frameCentreZ(f)); ok && f.w == b.w && !f.fixed &&
			!in.sparesHanging(in.blocks, f.uuid, f.invulnerable, f.minX, f.minY, f.minZ, f.maxX, f.maxY, f.maxZ) {
			goneFrames = append(goneFrames, f)
		}
	}
	if s.entityDamageListened() || s.hangingBreakListened() {
		s.framesMu.Unlock()
		goneFrames = askBlastBodies(s, b, goneFrames, func(f *frameEntity) uuid.UUID { return f.uuid })
		by := removedByBlast(b)
		goneFrames = slices.DeleteFunc(goneFrames, func(f *frameEntity) bool { return !s.askHangingBreak(f.uuid, by) })
		s.framesMu.Lock()
		goneFrames = slices.DeleteFunc(goneFrames, func(f *frameEntity) bool { return s.frameByIDLocked(b.w, f.id) != f })
	}
	for _, f := range goneFrames {
		fx.frames = append(fx.frames, frameTellOf(f))
		s.removeFrameLocked(f)
	}
	s.framesMu.Unlock()

	s.knotsMu.Lock()
	for _, k := range s.knots {
		minX, minY, minZ, maxX, maxY, maxZ := knotBox(k)
		if _, ok := in.at.Reach((minX+maxX)/2, (minY+maxY)/2, (minZ+maxZ)/2); ok && k.w == b.w &&
			!in.sparesHanging(in.blocks, k.uuid, k.invulnerable, minX, minY, minZ, maxX, maxY, maxZ) {
			fx.knots = append(fx.knots, knotTellOf(k))
		}
	}
	s.knotsMu.Unlock()

	fx.cushions = s.cushionsWhere(b.w, func(_, _, _, _, _, _, x, y, z float64) bool {
		_, ok := in.at.Reach(x, y, z)
		return ok
	})
	fx.cushions = slices.DeleteFunc(fx.cushions, func(c *fixtureEntity) bool {
		minX, minY, minZ, maxX, maxY, maxZ := entity.FixtureBox(c.kind, c.tag, c.x, c.y, c.z)
		return in.sparesHanging(in.blocks, c.uuid, s.cushionInvulnerable(c), minX, minY, minZ, maxX, maxY, maxZ)
	})
}

// blastStands takes out the visible stands in reach, killed outright by the
// explosion damage whatever its amount, their equipment laid behind them
// and the stand item not. An invisible stand, like a marker, is untouched.
// One invulnerable to the blast — any blast but a creative player's own —
// or on the team it spares is pushed instead, its own tick carrying the
// velocity to its watchers.
// vanilla: ArmorStand.hurtServer, ignoreExplosion, Entity.push
func (s *Server) blastStands(b blast, in *blastInputs, fx *blastEffects) {
	if !b.interaction.RemovesBlocks() {
		return
	}
	s.armorStandsMu.Lock()
	defer s.armorStandsMu.Unlock()
	for _, ent := range s.armorStands {
		if ent.w != b.w || ent.stand.Dead || ent.stand.Invisible || ent.stand.Marker {
			continue
		}
		dist, ok := in.at.Reach(ent.stand.X, ent.stand.Y, ent.stand.Z)
		if !ok {
			continue
		}
		if ent.stand.InvulnerableTo(b.damageType, in.causeCreative) || in.spares(ent.uuid) {
			hit := in.at.Hit(in.blocks, dist, ent.stand.BlastBox, ent.stand.EyePosition)
			ent.stand.Push(hit.KX, hit.KY, hit.KZ)
			continue
		}
		fx.stands = append(fx.stands, ent)
	}
	if s.entityDamageListened() {
		s.armorStandsMu.Unlock()
		fx.stands = askBlastBodies(s, b, fx.stands, func(e *armorStandEntity) uuid.UUID { return e.uuid })
		s.armorStandsMu.Lock()
		fx.stands = slices.DeleteFunc(fx.stands, func(e *armorStandEntity) bool { return s.armorStandByIDLocked(b.w, e.stand.ID) != e })
	}
	// Removing shifts the tail down, so every removal waits until the range
	// above is done; doing it in place would skip a stand and read the
	// zeroed last slot.
	for _, ent := range fx.stands {
		s.removeArmorStandLocked(ent)
	}
}

// sparesItems reports whether the blast ignores item entities: no hurt, no push.
// vanilla: ItemEntity.ignoreExplosion, ServerExplosion.shouldAffectBlocklikeEntities
func (b blast) sparesItems() bool {
	if b.directType == "minecraft:wind_charge" || b.directType == "minecraft:breeze_wind_charge" {
		return true
	}
	return !b.w.BoolRule(world.RuleMobGriefing) && !b.interaction.RemovesBlocks()
}

// blastPushed hurts and pushes the items and orbs in reach, and pushes the
// arrows, thrown bodies, primed TNT and falling blocks; a hurt does nothing
// else to those.
func (s *Server) blastPushed(b blast, in *blastInputs, fx *blastEffects) {
	// A stack's damage_resistant component (the nether star's is_explosion)
	// can protect it; an orb has none. A body the hurt kills is gone before
	// the blocks go, as vanilla discards it.
	var hurtItems [][3]float64
	verdicts := s.askBlastItems(b, in)
	s.itemsMu.Lock()
	for _, it := range in.items {
		if b.sparesItems() {
			break
		}
		if it.item.Dead || it.w != b.w {
			continue
		}
		if dist, ok := in.at.Reach(it.item.FeetPosition()); ok {
			hit := in.at.Hit(in.blocks, dist, it.item.BlastBox, it.item.EyePosition)
			damage := float32(hit.Damage)
			if v, asked := verdicts[it]; asked {
				damage = v
			}
			landed := !b.harmless && !in.spares(it.uuid) && (verdicts == nil || damage > 0) && it.item.Hurt(damage, b.damageType, in.causeCreative)
			if landed {
				x, y, z := it.item.FeetPosition()
				hurtItems = append(hurtItems, [3]float64{x, y, z})
			}
			if landed && it.item.Dead {
				fx.removed = append(fx.removed, it.item.ID)
				fx.spills = append(fx.spills, spillOf(it))
				s.entityDestroyed(it.uuid)
				continue
			}
			it.item.Push(hit.KX, hit.KY, hit.KZ)
			queueMotion(fx, &it.bodyTrack, it.item.ID, it.item.VX, it.item.VY, it.item.VZ)
		}
	}
	s.itemsMu.Unlock()
	for _, p := range hurtItems {
		s.gameEvent(b.w, rules.GameEventEntityDamage, p[0], p[1], p[2], s.attackerEventContext(b.causeID))
	}
	s.orbsMu.Lock()
	for _, it := range in.orbs {
		if it.orb.Dead || it.w != b.w {
			continue
		}
		if dist, ok := in.at.Reach(it.orb.X, it.orb.Y, it.orb.Z); ok {
			hit := in.at.Hit(in.blocks, dist, it.orb.BlastBox, it.orb.EyePosition)
			if !b.harmless && !in.spares(it.uuid) && it.orb.HurtDead(hit.Damage) {
				fx.removed = append(fx.removed, it.orb.ID)
				s.entityDestroyed(it.uuid)
				continue
			}
			it.orb.Push(hit.KX, hit.KY, hit.KZ)
			queueMotion(fx, &it.bodyTrack, it.orb.ID, it.orb.VX, it.orb.VY, it.orb.VZ)
		}
	}
	s.orbsMu.Unlock()

	// A stuck arrow carries the push until its block lets go. A thrown body
	// sends no motion: its next periodic teleport carries the result. A wind
	// charge ignores the push. A redirectable body becomes the blast's
	// cause's, nobody's without one.
	// vanilla: AbstractWindCharge.push, ServerExplosion.hurtEntities
	s.arrowsMu.Lock()
	for _, it := range s.arrows {
		if it.arrow.Dead || it.w != b.w {
			continue
		}
		if dist, ok := in.at.Reach(it.arrow.X, it.arrow.Y, it.arrow.Z); ok {
			hit := in.at.Hit(in.blocks, dist, it.arrow.BlastBox, it.arrow.EyePosition)
			it.arrow.Push(hit.KX, hit.KY, hit.KZ)
			queueMotion(fx, &it.bodyTrack, it.arrow.ID, it.arrow.VX, it.arrow.VY, it.arrow.VZ)
		}
	}
	for _, it := range s.thrown {
		if it.throwable.Dead || it.w != b.w {
			continue
		}
		if dist, ok := in.at.Reach(it.throwable.X, it.throwable.Y, it.throwable.Z); ok {
			if it.throwable.Redirectable() {
				it.throwable.OwnerID, it.ownerUUID = b.causeID, b.causeUUID
			}
			if it.throwable.Kind.IsWindCharge() {
				continue
			}
			hit := in.at.Hit(in.blocks, dist, it.throwable.BlastBox, it.throwable.EyePosition)
			// Any hurt destroys a bullet.
			// vanilla: ShulkerBullet.hurtServer
			if it.throwable.Kind == entity.ThrownShulkerBullet && !b.harmless && !in.spares(it.uuid) {
				it.throwable.Dead = true
				fx.bullets = append(fx.bullets, arrowBulletHit{w: b.w, id: it.throwable.ID,
					x: it.throwable.X, y: it.throwable.Y, z: it.throwable.Z})
				continue
			}
			it.throwable.Push(hit.KX, hit.KY, hit.KZ)
		}
	}
	s.arrowsMu.Unlock()

	// A primed TNT's hurt returns false: the push is the chain reaction's
	// shove. A falling block sends no motion: its next position packet
	// carries it.
	s.tntMu.Lock()
	for _, it := range s.tnts {
		if it.tnt.Dead || it.w != b.w {
			continue
		}
		if dist, ok := in.at.Reach(it.tnt.X, it.tnt.Y, it.tnt.Z); ok {
			hit := in.at.Hit(in.blocks, dist, it.tnt.BlastBox, it.tnt.BlastOrigin)
			it.tnt.Push(hit.KX, hit.KY, hit.KZ)
			queueMotion(fx, &it.bodyTrack, it.tnt.ID, it.tnt.VX, it.tnt.VY, it.tnt.VZ)
		}
	}
	s.tntMu.Unlock()
	s.fallingMu.Lock()
	for _, it := range s.fallings {
		if it.fb.Dead || it.w != b.w {
			continue
		}
		if dist, ok := in.at.Reach(it.fb.X, it.fb.Y, it.fb.Z); ok {
			hit := in.at.Hit(in.blocks, dist, it.fb.BlastBox, it.fb.BlastOrigin)
			it.fb.Push(hit.KX, hit.KY, hit.KZ)
		}
	}
	s.fallingMu.Unlock()
}

// queueMotion queues the motion packet of a pushed body whose velocity
// moved past the tracker's threshold, for sendBlast to send this tick to
// the body's watchers. The body's lock is held.
func queueMotion[S any](fx *blastEffects, t *bodyTrack[S], id int32, vx, vy, vz float64) {
	if t.motionDue(vx, vy, vz, false) {
		fx.pushed = append(fx.pushed, pushedMotion{
			data:     entityVelocityPayload(id, vx, vy, vz),
			watchers: trackedWatchers(t.tracked)})
	}
}

// applyBlastEffects applies what the bodies' steps collected, in the kinds'
// order. The indirect source's mode decides the hanging bodies' drops:
// hurtServer hands dropItem source.getEntity(), which skips the item for a
// creative player. vanilla: BlockAttachedEntity.hurtServer, Painting.dropItem
func (s *Server) applyBlastEffects(w *world.World, fx *blastEffects, causeCreative bool, causeID int32, causeIsPlayer bool) {
	for _, drops := range fx.heads {
		s.scatterDrops(drops)
	}
	for _, tell := range fx.paintings {
		s.breakPaintingTail(tell, causeCreative)
	}
	for _, tell := range fx.frames {
		s.breakFrameTail(tell, causeCreative)
	}
	for _, tell := range fx.knots {
		s.downKnot(tell)
	}
	for _, ent := range fx.cushions {
		s.breakCushion(ent, cushionBreakBy(causeCreative))
	}
	for _, ent := range fx.stands {
		s.breakArmorStandTail(ent, armorStandBreak{equipment: true})
	}
	for _, ent := range fx.crystals {
		sourceID := int32(0)
		if causeIsPlayer {
			sourceID = causeID
		}
		s.destroyEndCrystal(ent, sourceID)
	}
	for _, id := range fx.removed {
		s.broadcastWorld(w, protocol.IDPlayRemoveEntities, removeEntitiesPayload([]int32{id}))
	}
	s.spillContents(fx.spills)
	for _, hit := range fx.bullets {
		s.bulletStruck(w, hit.id, hit.x, hit.y, hit.z)
	}
}

// blastBlocks applies the blast to its cells: the removal, what each
// removed block leaves, the toggles of a trigger blast, the wakes, the
// fires and the relight.
func (s *Server) blastBlocks(b blast, destroyed rules.BlastBlocks) {
	w := b.w
	var removed []world.BlockChange
	if b.interaction.RemovesBlocks() {
		// One batched write is this server's own design, not vanilla's, whose
		// ServerExplosion.interactWithBlocks shuffles its target blocks and
		// setBlock(pos, AIR, 3)s them one at a time, with neighbour updates in
		// between: one hold of the world lock and one chunk re-encode serve the
		// whole crater, and the wakes, the packets and the relight follow it
		// whole. The cells are written in sorted order, a deliberate departure
		// from vanilla's shuffle, so the fuse dice of a chain a TNT wall lights
		// read the same cells in the same order every run.
		dug := make([]world.BlockChange, 0, len(destroyed))
		for _, pos := range destroyed {
			dug = append(dug, world.BlockChange{X: pos[0], Y: pos[1], Z: pos[2], State: airBlockState})
		}
		removed = w.SetBlocks(dug)
		// The clients hear the crater as one packet per section rather than
		// one per cell, straight after the write and before the drops and
		// wakes, /fill's order: nothing the removals cause can write a cell
		// the batch already told air.
		updates := make([]world.BlockChange, len(removed))
		for i, old := range removed {
			updates[i] = world.BlockChange{X: old.X, Y: old.Y, Z: old.Z, State: airBlockState}
		}
		s.tellSectionBlocks(w, updates)
	}
	var drops rules.BlastDrops
	for _, old := range removed {
		s.explodedBlock(b, old, &drops)
	}
	// Spawned after every block has rolled, so the loots have merged.
	// vanilla: ServerExplosion.interactWithBlocks
	for _, d := range drops.Drops() {
		s.spawnBlockDrop(w, d.Pos[0], d.Pos[1], d.Pos[2], d.Stack)
	}
	// A wind charge's rays toggle what they reach instead of breaking it, in
	// sorted order for the same reason the crater is.
	// vanilla: ServerExplosion.interactWithBlocks, BlockBehaviour.onExplosionHit
	// A breeze's charge only triggers with mob_griefing on.
	// vanilla: ServerExplosion.canTriggerBlocks
	if b.interaction == rules.ExplosionTrigger && (!b.breezeBurst || w.BoolRule(world.RuleMobGriefing)) {
		for _, pos := range destroyed {
			changes, effects, primes := s.physicsFor(w).ExplosionTrigger(pos[0], pos[1], pos[2])
			for _, e := range effects {
				s.tellEffect(w, e)
			}
			s.tellBlockChanges(w, changes)
			for _, t := range primes {
				s.primeTNT(w, t)
			}
		}
	}
	// The wake per removed cell, the flags-3 treatment any block change
	// gets: state before removal, neighbours read after every cell is air.
	// A stair, fence or wall left standing beside the crater reshapes first.
	for _, old := range removed {
		s.updateNeighborShapes(w, old.X, old.Y, old.Z)
		s.blockChanged(w, old.X, old.Y, old.Z, old.State)
	}
	if b.fire {
		for _, c := range rules.ExplosionFireCells(w, s.levelRng, destroyed) {
			s.setBlockAndTell(w, c[0], c[1], c[2], rules.FireStateForPlacement(w, c[0], c[1], c[2]))
		}
	}
	// The crater's relight, one box per chunk it spans.
	s.relightChanges(w, removed)
}

// explodedBlock applies what one removed block leaves: its block entity's
// spill, then a primed TNT, or the infestation and the loot, which goes to
// drops rather than the world. The spill is not collected: a container's
// contents spawn where it stood.
func (s *Server) explodedBlock(b blast, old world.BlockChange, drops *rules.BlastDrops) {
	w := b.w
	// A skull's carried compound holds its profile and name; the context
	// reads them before the break takes the compound out of the world.
	ctx := s.brokenBlockLootContext(w, old.X, old.Y, old.Z, old.State, nil)
	if registry.BlockOf(old.State) == registry.BlockCreakingHeart {
		s.creakingHeartExploded(b, old.X, old.Y, old.Z, old.State)
	}
	if isHiveState(old.State) {
		// vanilla: BeehiveBlock.onExplosionHit angers for every blast; getDrops releases.
		if hiveBlasters[b.directType] {
			s.releaseHiveBees(w, old.X, old.Y, old.Z, old.State, nil, false, true)
		}
		s.angerBeesAround(w, old.X, old.Y, old.Z)
	}
	// The entity leaves the chunk before its stacks are read: an off-tick
	// bed blast races a hopper, so the container must be gone before the
	// spill or a hopper move duplicates what it drops.
	container, customName := s.breakBlockEntity(w, old.X, old.Y, old.Z, old.State, true,
		func(be *world.BlockEntity, spills []protocol.ItemStack) {
			if s.blastSpillSeam != nil {
				s.blastSpillSeam(be)
			}
			for _, stack := range spills {
				s.spawnBlockDrop(w, old.X, old.Y, old.Z, stack)
			}
		})
	ctx.Container = container
	if customName != nil {
		ctx.CustomName = customName
	}
	switch rules.ExplodedBlockOf(registry.BlockOf(old.State), w) {
	case rules.ExplodedNothing:
		return
	case rules.ExplodedPrimed:
		// Owned by the blast's indirect source.
		s.spawnTNT(w,
			float64(old.X)+0.5, float64(old.Y), float64(old.Z)+0.5,
			rules.TNTShortFuse(b.rng, entity.TNTDefaultFuse),
			b.causeID, b.causeName, b.causeUUID, true)
		return
	case rules.ExplodedInfested:
		s.spawnInfestation(w, old.X, old.Y, old.Z)
	}
	// survives_explosion and explosion_decay roll against the radius, set
	// only under decay; match_tool fails with no tool, so silk touch never
	// applies. Every destroyed block rolls whatever the blast's radius.
	// This is the carrier (TNT, creeper), which a chorus flower's table
	// checks. vanilla: BlockBehaviour.onExplosionHit
	ctx.Radius = b.interaction.LootRadius(b.radius)
	if b.yield != nil {
		ctx.Radius = yieldLootRadius(*b.yield)
	}
	ctx.This = rules.LootEntity{Type: b.directType}
	if b.directLoot != nil {
		ctx.This = *b.directLoot
	}
	for _, stack := range rules.RollLoot(rules.BlockLootTable(registry.BlockOf(old.State)), &ctx, b.rng) {
		drops.Add(stack, [3]int32{old.X, old.Y, old.Z})
	}
}

// sendBlast queues a blast's packets the same tick its damage landed: the
// Explosion packet — whose Player Delta Velocity pushes the client's own
// player — and the pushed bodies' motions. Each packet is about the world
// the blast was decided in, so sendIn drops it for a player who crossed
// dimensions while the blast ran rather than pushing them in the one they
// arrived at.
func (s *Server) sendBlast(b blast, fx blastEffects) {
	// Players strictly within 64 blocks get the packet (vanilla:
	// ServerLevel.explode), with their own knockback if they were pushed.
	for _, p := range fx.players {
		pos := p.c.player.Position()
		if entity.Dist2(pos.X, pos.Y, pos.Z, b.x, b.y, b.z) >= 64*64 {
			continue
		}
		if p.pushable {
			p.c.player.AddVelocity(p.kx, p.ky, p.kz)
		}
		if p.reached {
			impact := [3]float64{pos.X, pos.Y, pos.Z}
			p.c.player.UpdateVitals(func(v *player.Vitals) {
				v.ExplosionImpact, v.ExplosionCause = &impact, b.directType
				// vanilla: ServerPlayer.onExplosionHit; the else branch keeps the position.
				if b.directType == "minecraft:wind_charge" {
					rules.SetImpulseImpact(v, impact)
				} else {
					v.ImpulseGraceTicks = 0
				}
			})
		}
		if fx.cancelled {
			continue
		}
		p.c.sendIn(b.w, protocol.IDPlayExplosion, b.payload(int32(fx.destroyed), p.kx, p.ky, p.kz, p.pushable))
	}
	// Send the pushes now, as vanilla's tracking pass does in the blast's
	// tick. The sent baseline moved under the lock, so the body's next tick
	// only reports the physics after the push.
	for _, m := range fx.pushed {
		s.tellWatchers(b.w, m.watchers, []outbound{{id: protocol.IDPlaySetEntityMotion, data: m.data}})
	}
}

// hurtByBlast hurts m by the blast, naming the removed exploder to the
// damage listeners. The caller holds mobsMu, which guards pendingExploder.
func (s *Server) hurtByBlast(b blast, m *entity.Mob, damage, kx, ky, kz float64, creative bool) {
	s.pendingExploder = blastCause(b).exploder
	defer func() { s.pendingExploder = nil }()
	m.HurtByCausedBlast(damage, kx, ky, kz, b.damageType, creative, b.causeID != 0, b.causeID)
}

// blastCause is the damage cause for a blast's hurt. No indirect source is
// encoded as ID -1, which the wire's +1 encoding turns into 0 (nobody); the
// carrier's ID is encoded +1, 0 when there is no carrier.
func blastCause(b blast) *damageCause {
	cause, direct := b.causeID, b.directID
	if cause == 0 {
		cause = -1
	}
	if direct != 0 {
		direct++
	}
	c := &damageCause{entityID: cause, directID: direct, x: b.x, y: b.y, z: b.z, killer: b.causeKiller, directName: b.directName}
	if b.directUUID != uuid.Nil {
		c.exploder = &removedBody{id: b.directUUID, typ: blastEntityType(b), isCause: b.causeID != 0 && b.causeID == b.directID}
	}
	return c
}

// payload builds the blast's Explosion packet (vanilla:
// ClientboundExplodePacket), carrying (kx, ky, kz) when pushed. Particle is
// explosion below radius 2, else the emitter. The block particles are
// vanilla's default weighted list: poof at scale 0.5 and smoke at 1.0, both
// speed 1, weight 1.
func (b blast) payload(blockCount int32, kx, ky, kz float64, pushed bool) []byte {
	var p protocol.Buffer
	p.PutF64(b.x)
	p.PutF64(b.y)
	p.PutF64(b.z)
	p.PutF32(float32(b.radius))
	p.PutI32(blockCount)
	p.PutBool(pushed)
	if pushed {
		p.PutF64(kx)
		p.PutF64(ky)
		p.PutF64(kz)
	}
	switch {
	case b.gust:
		// vanilla: WindChargedMobEffect.onMobRemoved, ServerLevel.explode
		// picks the emitter by radius
		if rules.ExplosionSmall(b.radius) {
			p.PutVarInt(protocol.ParticleGustEmitterSmall)
		} else {
			p.PutVarInt(protocol.ParticleGustEmitterLarge)
		}
		p.PutVarInt(protocol.SoundEventBreezeWindBurst)
		p.PutVarInt(0)
		p.PutBool(!b.silent)
		return p.Bytes()
	case b.burst:
		// vanilla: WindCharge.explode
		if rules.ExplosionSmall(b.radius) {
			p.PutVarInt(protocol.ParticleGustEmitterSmall)
		} else {
			p.PutVarInt(protocol.ParticleGustEmitterLarge)
		}
		if b.breezeBurst {
			p.PutVarInt(protocol.SoundEventBreezeWindBurst)
		} else {
			p.PutVarInt(protocol.SoundEventWindChargeBurst)
		}
		p.PutVarInt(0) // no block particle alternatives
		p.PutBool(!b.silent)
		return p.Bytes()
	}
	if l := b.look; l != nil {
		if rules.ExplosionSmall(b.radius) {
			p.PutVarInt(l.Small)
		} else {
			p.PutVarInt(l.Large)
		}
		protocol.PutSoundHolder(&p, l.Sound)
		p.PutVarInt(int32(len(l.Blocks)))
		for _, e := range l.Blocks {
			p.PutVarInt(e.Particle)
			p.PutF32(e.Scaling)
			p.PutF32(e.Speed)
			p.PutVarInt(e.Weight)
		}
		p.PutBool(!b.silent)
		return p.Bytes()
	}
	if rules.ExplosionSmall(b.radius) {
		p.PutVarInt(protocol.ParticleExplosion)
	} else {
		p.PutVarInt(protocol.ParticleExplosionEmitter)
	}
	p.PutVarInt(protocol.SoundEventGenericExplode)
	p.PutVarInt(2) // the block particle alternatives
	p.PutVarInt(protocol.ParticlePoof)
	p.PutF32(0.5)
	p.PutF32(1.0)
	p.PutVarInt(1)
	p.PutVarInt(protocol.ParticleSmoke)
	p.PutF32(1.0)
	p.PutF32(1.0)
	p.PutVarInt(1)
	p.PutBool(!b.silent)
	return p.Bytes()
}

// creeperExploded takes a creeper out of the world and queues its blast,
// and the death effects its removal fires.
func (t *mobDimTick) creeperExploded(fx *mobEffects, ev entity.MobEvent) {
	h, w := t.h, t.w
	// A creeper went up: out of the world like a despawn —
	// no corpse, no death sound, no drops — and the blast,
	// whose own sound is its telling, is the tick's to run.
	var explodedUUID uuid.UUID
	portalCooldown := 0
	causeName := mobDisplayName(ev.Mob.Type)
	causeKiller := &rules.Killer{Name: mobTypeName(ev.Mob)}
	if ent, ok := h.book[ev.Mob]; ok {
		explodedUUID = ent.uuid
		portalCooldown = ent.portal.Cooldown
		causeName = mobName(ent)
		causeKiller = t.s.mobKiller(ent)
		h.forget(ev.Mob)
		fx.scoreDrops = append(fx.scoreDrops, goneMob{ent.uuid, plugin.RemoveDiscard})
		fx.remove(w, ev.Mob.ID)
		// The creeper is gone with the blast to come, its
		// dice and effects read here under the lock, the
		// death's own fire waiting outside it.
		// vanilla: LivingEntity.remove's
		// triggerOnDeathMobEffects
		m := ev.Mob
		if m.Type == entity.TypeCreeper {
			if active := m.AllEffects(); len(active) > 0 {
				fx.creeperClouds = append(fx.creeperClouds, creeperCloud{w: w, x: m.X, y: m.Y, z: m.Z, effects: active})
			}
		}
		minX, minY, minZ, maxX, maxY, maxZ := m.BlastBox()
		fx.triggers = append(fx.triggers, mobEffectTrigger{
			w: w, body: m,
			box: entity.NewAABB(minX, minY, minZ, maxX, maxY, maxZ).Inflate(2),
			x:   m.X, y: m.Y, z: m.Z,
			dice: m.Rand, effects: m.AllEffects(),
			killed: true, height: maxY - minY,
		})
	}
	interaction := rules.MobExplosionInteraction(w)
	if ev.Mob.Type == entity.TypeSulfurCube {
		// vanilla: SulfurCube.tickFuse, TNT interaction unless mob_griefing is off
		interaction = rules.TNTExplosionInteraction(w)
		if !w.BoolRule(world.RuleMobGriefing) {
			interaction = rules.ExplosionKeep
		}
	}
	var blockResist func(int32, int32, int32) (float64, bool)
	var spared func(int32, int32, int32) bool
	if ev.Mob.Type == entity.TypeSulfurCube && (portalCooldown > 0 || t.sulfurPortalCooling[ev.Mob]) {
		spared, blockResist = portalSparing(w)
	}
	fx.blasts = append(fx.blasts, blast{
		blockResist: blockResist, spared: spared,
		w: w,
		x: ev.X, y: ev.Y, z: ev.Z,
		radius:       ev.Radius,
		fire:         ev.Fire,
		causeID:      ev.Mob.ID,
		causeName:    causeName,
		causeKiller:  causeKiller,
		directID:     ev.Mob.ID,
		directUUID:   explodedUUID,
		directType:   ev.Mob.Type.Name(),
		directLoot:   frozenMobLoot(ev.Mob),
		directLiving: true,
		silent:       ev.Mob.Silent,
		// The creeper is the direct source; a wet one spares
		// the paintings its blast reaches.
		directInWater: ev.Mob.InWater(),
		chargedHeads:  ev.Mob.Powered,
		// The creeper is both sources of its own blast: the
		// direct one, and the indirect one
		// Explosion.getIndirectSourceEntity finds in any
		// living body, so DamageSources.explosion picks
		// player_explosion and the death message names it.
		causeUUID:  explodedUUID,
		damageType: rules.DamageTypePlayerExplosion,
		rng:        ev.Mob.Rand,
		sourceTeam: ev.Mob.Team,
		// A creeper's blast is a mob's: the mob_griefing
		// rule vetoes the blocks whole, and the mob decay
		// rule scales what the survivors drop.
		interaction: interaction,
	})
}

// blastDragon measures each part independently, including parts beyond the
// parent's reach. Part pushes belong to the parts, not their parent.
// vanilla: ServerExplosion.hurtEntities, Level.getEntities
func (s *Server) blastDragon(b blast, in *blastInputs, m *entity.Mob) {
	defer m.AimDragonPart(entity.DragonBody)
	typ := int32(m.Type)
	for part := -1; part < 8; part++ {
		x, y, z := m.X, m.Y, m.Z
		box, aim := m.BlastBox, m.EyePosition
		if part >= 0 {
			p := entity.DragonPartTarget(m, typ, part)
			x, y, z = (p.MinX+p.MaxX)/2, p.MinY, (p.MinZ+p.MaxZ)/2
			box = func() (float64, float64, float64, float64, float64, float64) {
				return p.MinX, p.MinY, p.MinZ, p.MaxX, p.MaxY, p.MaxZ
			}
			aim = func() (float64, float64, float64) { return x, p.EyeY, z }
		}
		dist, ok := in.at.Reach(x, y, z)
		if !ok {
			continue
		}
		hit := in.at.Hit(in.blocks, dist, box, aim)
		if part == -1 {
			hit = hit.Resisted(m.ExplosionKnockbackResistance())
			m.PushByBlast(hit.KX, hit.KY, hit.KZ)
		}
		if !b.harmless {
			m.AimDragonPart(part)
			s.hurtByBlast(b, m, hit.Damage, 0, 0, 0, in.causeCreative)
		}
	}
}

// portalSparing is the blast's spared / blockResist pair for a body that
// came through a portal: nether portal blocks neither break nor slow a ray.
// vanilla: PrimedTnt.USED_PORTAL_DAMAGE_CALCULATOR
func portalSparing(w *world.World) (spared func(int32, int32, int32) bool, blockResist func(int32, int32, int32) (float64, bool)) {
	spared = func(x, y, z int32) bool { return registry.BlockOf(w.BlockAt(x, y, z)) == registry.BlockNetherPortal }
	blockResist = func(x, y, z int32) (float64, bool) {
		if spared(x, y, z) {
			// Cancel the resistance-step loss: Optional.empty costs a ray nothing.
			return -0.3, true
		}
		return 0, false
	}
	return spared, blockResist
}
