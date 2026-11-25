package server

import (
	"log/slog"
	"sync"
	"time"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/advancement"
	"github.com/golem-mc/golem/internal/server/command"
	"github.com/golem-mc/golem/internal/server/effect"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/pluginruntime"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/plugin"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
)

// The arrow lock sits beneath the player list's and above the mobs' (the
// tick reads both in that order); sends, hurts and pickups run after the
// locks are down.

// arrowEntity is one arrow or thrown trident in a world. The arrow itself is
// entity.Arrow; the server holds the dimension, UUID and tracking.
// ownerUUID is the shooter's identity as the entity file keeps it.
// entityType is the arrow's entity type on the wire: the spectral one for a
// dispensed spectral arrow, whose flight and metadata are the plain arrow's.
type arrowEntity struct {
	arrow *entity.Arrow
	w     *world.World
	uuid  uuid.UUID

	bodyTrack[arrowMetadataState]
	shotMark

	entityType int32

	ownerUUID uuid.UUID

	// piercingHit preserves the body's hit semantics before Step clears the
	// pierce level on a later block hit in the same tick.
	// vanilla: AbstractArrow.stepMoveAndHit
	piercingHit bool

	// pierced is the types of the bodies a piercing shaft has killed, which
	// killed_by_arrow reads whole on each hit. Guarded by the arrow lock.
	pierced []string

	// portal is the arrow's portal state, shared with every other body of
	// vanilla's Entity.
	portal rules.EntityPortal

	// tags are the scoreboard tags, what the tag selector option matches
	// and /tag edits. Guarded by the arrow lock, like the rest of an
	// arrow's own fields.
	tags nbt.StringSet
	// custom is the custom name, guarded like tags.
	custom customName
	look   entityLook

	// spawning holds the arrow out of its tick while its spawn effects run.
	// Guarded by the arrow lock once registered.
	spawning bool
}

// arrowTracker tracks the arrows and the tridents; a watcher's own state is
// the metadata it was told. An arrow is never told on the ground, and its
// Teleport Entity carries no velocity: the motion packets do.
var arrowTracker = trackedKind[*arrowEntity, arrowMetadataState]{
	invalidatePiston: func(dim *dimension) { dim.pistons.arrows.built = false },
	pistonBit:        pistonArrows,

	rangeChunks:    entity.ArrowTracking,
	interval:       entity.ArrowInterval,
	alwaysPosition: true,
	lock:           func(s *Server) *sync.Mutex { return &s.arrowsMu },
	list:           func(s *Server) *[]*arrowEntity { return &s.arrows },
	chunks:         func(s *Server) *entity.ChunkIndex[*arrowEntity] { return &s.arrowChunks },
	filing:         func(it *arrowEntity) *entity.Filing { return &it.arrow.Filing },
	uuid:           func(it *arrowEntity) uuid.UUID { return it.uuid },
	track:          func(it *arrowEntity) *bodyTrack[arrowMetadataState] { return &it.bodyTrack },
	world:          func(it *arrowEntity) *world.World { return it.w },
	id:             func(it *arrowEntity) int32 { return it.arrow.ID },
	dead:           func(it *arrowEntity) bool { return it.arrow.Dead },
	pose: func(it *arrowEntity) bodyPose {
		a := it.arrow
		return bodyPose{x: a.X, y: a.Y, z: a.Z, yaw: a.Yaw, pitch: a.Pitch}
	},
	pairing: arrowPairing,
	own:     func(it *arrowEntity) arrowMetadataState { return arrowMetadataStateOf(it) },
}

// arrowMetadataState is the metadata-bearing slice of an arrow's state:
// the critical bit, the in-ground flag, the on-fire bit of the shared
// flags byte, the noclip of a loyalty trident's return, and the colour of
// the tip a tipped arrow carries.
type arrowMetadataState struct {
	crit     bool
	inGround bool
	flags    uint8
	noclip   bool
	color    int32
	pierce   int32
	// noGravity, loyalty and foil change only by a /data write or at the
	// spawn; loyalty is the trident's, 0 to 127.
	noGravity bool
	silent    bool
	foil      bool
	loyalty   int32
	name      customName
}

// arrowMetadataStateOf reads the metadata-bearing state of an arrow as it
// stands now.
func arrowMetadataStateOf(it *arrowEntity) arrowMetadataState {
	a := it.arrow
	return arrowMetadataState{
		crit: a.Crit, inGround: a.InGround, flags: it.look.flags(a.FireTicks > 0),
		noclip: a.NoPhysics,
		color:  arrowColor(a.Effect),
		pierce: a.PierceLevel,
		silent: it.look.silent, noGravity: a.NoGravity, loyalty: a.Loyalty, foil: a.Foil,
		name: it.custom,
	}
}

// potionBaseColor is PotionContents.BASE_POTION_COLOR, the colour of
// contents with no visible effect.
const potionBaseColor = -13083194

// arrowColor is the Arrow's effect colour metadata: -1 for a plain arrow,
// else the effect's colour, opaque. vanilla: PotionContents.getColorOptional
func arrowColor(e entity.ArrowEffect) int32 {
	if e.HasColor {
		return e.Color
	}
	if e.Name == "" {
		if e.Tipped {
			return potionBaseColor
		}
		return -1
	}
	id, ok := registry.MobEffectID(e.Name)
	if !ok {
		return -1
	}
	return int32(uint32(0xFF000000) | uint32(registry.MobEffectOf(id).Color))
}

// pickupEffect reads the effects an arrow's pickup stack carries: the
// potion's, then the custom ones. No potion_contents, or one without effects, is
// the flight of a plain arrow. A tipped arrow scales the duration by its
// POTION_DURATION_SCALE (only a tipped_arrow item sets it, to 0.125) and
// keeps each effect's amplifier. A custom_color replaces the effect colour.
// vanilla: Arrow.updateColor, PotionContents.forEachEffect
func pickupEffect(stack protocol.ItemStack) entity.ArrowEffect {
	contents, ok := stack.PotionContentsOf()
	if !ok {
		return entity.ArrowEffect{}
	}
	scale := float32(1)
	if rawScale, ok := stack.PotionDurationScale(); ok {
		scale = rawScale
	} else if registry.Item(stack.ItemID) == registry.ItemTippedArrow && !stack.Removes(protocol.DataComponentPotionDurationScale) {
		scale = tippedArrowScale
	}
	out := entity.ArrowEffect{
		Tipped: contents.Potion != protocol.NoPotion || len(contents.CustomEffects) > 0 ||
			contents.CustomColor != nil || contents.CustomName != nil,
	}
	if contents.CustomColor != nil {
		out.HasColor, out.Color = true, *contents.CustomColor
	}
	add := func(name string, ticks, amplifier int32) {
		if out.Name == "" {
			out.Name, out.Ticks, out.Amplifier = name, ticks, amplifier
			return
		}
		out.More = append(out.More, entity.ArrowStatus{Name: name, Ticks: ticks, Amplifier: amplifier})
	}
	if contents.Potion != protocol.NoPotion {
		for _, e := range registry.PotionEffects(contents.Potion) {
			add(e.Effect, scaledArrowTicks(e.Duration, scale), e.Amplifier)
		}
	}
	for _, e := range contents.CustomEffects {
		add(registry.MobEffectName(e.ID), scaledArrowTicks(e.Duration, scale), e.Amplifier)
	}
	return out
}

// tippedArrowScale is Items.TIPPED_ARROW's POTION_DURATION_SCALE.
const tippedArrowScale = 0.125

// scaledArrowTicks is the effect duration an arrow carries: the effect's
// own times the pickup's POTION_DURATION_SCALE, floored as vanilla's int
// cast is. A zero or infinite (-1) duration is
// left alone, and a shorter one keeps at least a tick.
// vanilla: MobEffectInstance.withScaledDuration
func scaledArrowTicks(duration int32, scale float32) int32 {
	if duration == 0 || duration == -1 {
		return duration
	}
	if scaled := int32(float32(duration) * scale); scaled > 1 {
		return scaled
	}
	return 1
}

// spectralArrowGlowTicks is SpectralArrow.duration's constructor default,
// and so the glowing SpectralArrow.doPostHurtEffects gives when no tag
// names another duration.
// vanilla: SpectralArrow.<init>, doPostHurtEffects
const spectralArrowGlowTicks = 200

// arrowPostEffects are the effects one shaft's landed hit leaves in the
// body: the tip the arrow carries at its amplifier, and the glowing a
// spectral shaft gives for its fixed 200 ticks.
// vanilla: Arrow.doPostHurtEffects, SpectralArrow.doPostHurtEffects
func arrowPostEffects(it *arrowEntity) []effect.Instance {
	var out []effect.Instance
	if e := it.arrow.Effect; e.Name != "" {
		out = append(out, blowEffect(registry.MustMobEffect(e.Name), e.Ticks, e.Amplifier))
		for _, m := range e.More {
			out = append(out, blowEffect(registry.MustMobEffect(m.Name), m.Ticks, m.Amplifier))
		}
	}
	if it.entityType == protocol.EntityTypeSpectralArrow {
		out = append(out, blowEffect(registry.MobEffectGlowing, it.arrow.GlowTicks, 0))
	}
	return out
}

// arrowHitSound builds the impact sound at the shaft's position.
// vanilla: AbstractArrow.onHitEntity, onHitBlock
func arrowHitSound(w *world.World, a *entity.Arrow, sound registry.Sound) pendingSound {
	// The conversion prevents a fused multiply-add from skipping a rounding.
	return pendingSound{w: w, sound: sound.Holder(), category: protocol.SoundCategoryNeutral,
		x: a.X, y: a.Y, z: a.Z, volume: 1.0, pitch: 1.2 / (float32(a.Rand.Float32()*0.2) + 0.9)}
}

// tickArrowPickups runs one pass of the arrow pickups alone, the part of
// tickArrows a frozen world still owes its players: vanilla's player keeps
// ticking and touching nearby entities while the shaft's own tick is held.
// vanilla: Player.aiStep, AbstractArrow.playerTouch
func (s *Server) tickArrowPickups() {
	s.mu.RLock()
	clients := s.clientsLocked()
	s.mu.RUnlock()

	type watcher struct {
		c        *client
		pos      player.Position
		dims     rules.PlayerDimensions
		creative bool
	}
	watchers := make(map[*world.World][]watcher)
	for _, c := range clients {
		if c.player == nil {
			continue
		}
		watchers[c.theWorld()] = append(watchers[c.theWorld()], watcher{
			c:        c,
			pos:      c.player.Position(),
			dims:     c.dims(c.player.Appearance().Pose),
			creative: c.player.GameMode() == protocol.GameModeCreative,
		})
	}

	var pickups []arrowPickup
	s.lockTick(&s.arrowsMu)
	for _, it := range s.arrows {
		if it.arrow.Dead {
			continue
		}
		for _, w := range watchers[it.w] {
			if w.c.theWorld() != it.w {
				continue // crossed to another dimension since the snapshot
			}
			if it.arrow.CanPickUp(w.creative) && arrowWithinTouch(w.pos, w.dims, it.arrow) {
				pickups = append(pickups, arrowPickup{arrow: it, collector: w.c})
				break
			}
		}
	}
	s.unlockTick(&s.arrowsMu)
	for _, p := range pickups {
		s.arrowPickup(p)
	}
}

// arrowPickup is one pickup waiting to be given: the arrow and the player
// who walked over it.
type arrowPickup struct {
	arrow     *arrowEntity
	collector *client
}

// arrowPlayerHit is one player an arrow is about to hurt, carried out of
// the locks: the arrow, the world the shaft flew, the victim, the raw
// damage, the burn, and where the arrow stood.
type arrowPlayerHit struct {
	hitSound    registry.Sound
	arrow       *arrowEntity
	w           *world.World
	victim      *client
	damage      float64
	igniteTicks int32
	// effects are the instances the shaft's landed hit leaves, read under
	// the arrow lock with the rest of its state.
	effects []effect.Instance
	// custom is the arrow's custom name, read under the arrow lock.
	custom     customName
	x, y, z    float64
	vx, vy, vz float64
}

// shooterOf returns what there is to name of the shooter of the hit: the
// entity ID the damage event carries, the killer the death message names —
// a player's own name and held item, or a mob's — and the client itself
// when the shooter is a player still in the world, the dinger of a hit that
// landed. A shooter gone from the world is nobody.
func (h *arrowPlayerHit) shooterOf() (int32, *rules.Killer, *client) {
	id := h.arrow.arrow.OwnerID
	c := h.victim.srv.entityClient(id)
	return id, h.victim.srv.entityKiller(h.victim.theWorld(), id), c
}

// arrowMobHit is one mob an arrow hurt, carried out of the locks with the
// telling the dimension is to hear: where the hurt was taken, whose shot
// was behind it, the knockback velocity, the health left.
type arrowMobHit struct {
	// dodged is an enderman's teleport away from the hit, which leaves the
	// shaft flying.
	dodged   bool
	w        *world.World
	victimID int32
	kind     entity.MobType
	sound    protocol.SoundHolder
	volume   float32
	pitch    float32
	silent   bool
	causeID  int32
	directID int32
	// damageType is the blow's own — the arrow's, or the trident's for a
	// trident's hit — which the damage event names the hurt by.
	damageType registry.DamageType
	// dealt is the shaft's damage before armor and taken the health the
	// body lost to it.
	dealt, taken float64
	x, y, z      float64
	yaw          float32
	vx, vy, vz   float64
	kb           *mobKnockback
	health       []byte
	// softened is a blow that landed only its excess through the mob's
	// invulnerability window: its health alone is told of, none of a whole
	// hurt's damage event, flinch, shove or hurt sound.
	softened bool
	// shaftSound is the plain arrow's hit voice at the body, nil for a
	// trident, whose deflection plays its own.
	shaftSound *pendingSound
	// shooter is the owner's entity ID and direct the entity type name of
	// the shaft, set for a landed hit; killed lists the victims of a
	// killed_by_arrow trigger, nil when it has none to fire.
	shooter int32
	direct  string
	killed  []string
	// weapon is the item the shaft was fired from.
	weapon protocol.ItemStack
	// thorns is what the body's pieces hit a player shooter back with.
	thorns mobThorns
	// ink is the ink cloud a struck squid's landed blow squeezed out, nil
	// for every other body and every softened blow.
	ink *inkCloud
	// effects tell the watchers and riders of the body it landed in what the
	// shaft's tip left behind; watchers are the uuids of the players shown
	// it, who alone are told of the hit, looked up once the mob lock is given
	// back.
	effects  []splashMobTell
	watchers []uuid.UUID
}

// shooterIsPlayer reports whether the entity ID an arrow names as its
// owner is one of the players of the world — the difference between a
// shot whose kill is a player's and one whose kill is nobody's.
func shooterIsPlayer(ownerID int32, clientsByID map[int32]*client) bool {
	_, ok := clientsByID[ownerID]
	return ok && ownerID != 0
}

// shooterCreative reports whether the entity ID an arrow names as its owner
// is a creative player, whose own shot reaches an invulnerable body where a
// survival shot is turned aside.
// vanilla: DamageSource.isCreativePlayer
func shooterCreative(ownerID int32, clientsByID map[int32]*client) bool {
	return creativeClient(clientsByID[ownerID])
}

// arrowHurtMob runs one arrow's hurt against a mob, the mob lock held by
// the caller. The health the body is left with is put into every watcher's
// record, so the tick's own metadata pass does not repeat it. A hurt that
// landed ends the flight, as vanilla's onHitEntity discards the shaft — a
// trident's never does, its hit leaving the body to the deflection the
// caller has queued — and a refused one bounces off, as vanilla's deflect
// answers a hurt that does not land, a trident's standing for the same
// deflection instead. What comes back is the telling the dimension is to
// hear.
func (s *Server) arrowHurtMob(it *arrowEntity, dim *dimension, m *entity.Mob, ev entity.ArrowEvent, shooterWasPlayer, shooterWasCreative bool, from *entity.HitSource) arrowMobHit {
	if m.Type == entity.TypeEnderDragon {
		m.AimDragonPart(int(ev.Target.EntityID - m.ID - 1))
		defer m.AimDragonPart(entity.DragonBody)
	}
	if m.DragonSitting() {
		it.arrow.FireTicks = 20
	}
	damageType := rules.DamageTypeArrow
	directType := int32(protocol.EntityTypeArrow)
	if it.arrow.Trident {
		damageType = rules.DamageTypeTrident
		directType = protocol.EntityTypeTrident
	}
	punch := rules.EnchantedKnockback(it.arrow.Weapon, &rules.EnchContext{
		DamageType:     damageType,
		Victim:         mobEntityType(m.Type),
		DirectAttacker: directType,
	}, 0)
	var landed, softened bool
	healthBefore := m.Health
	if it.arrow.FireTicks > 0 {
		m.PrimeSulfurByFireArrow()
	}
	oldVX, oldVY, oldVZ, oldImpulse := m.VX, m.VY, m.VZ, m.Impulse
	if it.arrow.Trident {
		landed, softened = m.HurtByTridentFrom(ev.Damage, ev.VX, ev.VZ, punch, it.arrow.OwnerID, shooterWasPlayer, shooterWasCreative, from)
	} else {
		landed, softened = m.HurtByArrowFrom(ev.Damage, ev.VX, ev.VZ, punch, it.arrow.OwnerID, shooterWasPlayer, shooterWasCreative, from)
	}
	if m.TookDodge() {
		// An enderman teleported away from the hit: the shaft flies on
		// with no sound or effect, a trident spent but not deflected.
		// vanilla: AbstractArrow.onHitEntity, ThrownTrident.onHitEntity
		if it.arrow.Trident {
			it.arrow.RestoreDealtDamage(true)
		}
		return arrowMobHit{damageType: damageType, dodged: true}
	}
	if !landed {
		// The hurt was refused — the invulnerability window of an earlier
		// blow, or an invulnerable body the shot may not hurt — and the
		// arrow bounces off; a trident stands for the deflection the
		// caller runs instead of the bounce of a shaft.
		if !it.arrow.Trident {
			it.arrow.Bounce(it.w)
		}
		return arrowMobHit{damageType: damageType}
	}
	m.NoteBlowDirect(arrowTypeName(it))
	if !it.arrow.Trident && !it.piercingHit {
		it.arrow.Dead = true
		// vanilla: AbstractArrow.onHitEntity
		m.SetMetadataArrowCount(m.ArrowCount + 1)
	}
	var shaftSound *pendingSound
	if !it.arrow.Trident {
		snd := arrowHitSound(dim.w, it.arrow, ev.HitSound)
		snd.x, snd.y, snd.z = ev.X, ev.Y, ev.Z
		shaftSound = &snd
	}
	ent := dim.mobs.book[m]
	hit := arrowMobHit{
		w:          dim.w,
		victimID:   m.ID,
		kind:       m.Type,
		sound:      mobSound(m, true),
		volume:     m.SoundVolume(),
		pitch:      m.VoicePitch(),
		silent:     m.Silent,
		causeID:    causeEntityID(&damageCause{entityID: it.arrow.OwnerID}),
		directID:   it.arrow.ID + 1,
		damageType: damageType,
		dealt:      ev.Damage, taken: healthBefore - m.Health,
		x: ev.X, y: ev.Y, z: ev.Z,
		yaw: mobHurtYaw(entity.Player{X: ev.X, Y: ev.Y, Z: ev.Z}, m),
		vx:  m.VX, vy: m.VY, vz: m.VZ,
		kb:         s.holdMobKnockback(ent, it.uuid, oldVX, oldVY, oldVZ, oldImpulse),
		softened:   softened,
		shaftSound: shaftSound,
		shooter:    it.arrow.OwnerID,
		direct:     arrowTypeName(it),
		weapon:     it.arrow.Weapon,
	}
	if shooterWasPlayer && ent != nil {
		// vanilla: AbstractArrow.onHitEntity, ThrownTrident.onHitEntity
		hit.thorns = mobRetaliations(ent, &rules.EnchContext{
			DamageType: damageType,
			Victim:     mobEntityType(m.Type), Attacker: protocol.EntityTypePlayer, DirectAttacker: directType,
		}, protocol.ItemStack{})
	}
	hit.killed = it.killedByArrow(!m.Alive(), m.Type.Name())
	if ent != nil {
		hit.health = ent.syncHealth()
		hit.watchers = trackedWatchers(ent.tracked)
	}
	// The squid's ink, squeezed where the blow landed: read under the lock,
	// and told even for a softened blow, which still landed in vanilla.
	hit.ink = takeInk(m)
	// The effects the shaft's landed hit leaves bite the body, and the
	// packets they come to ride the hit out of the locks, the way the
	// hurt's own telling does.
	for _, in := range arrowPostEffects(it) {
		m.EffectCause = effect.CauseArrow
		if ch, ok := m.AddEffect(in); ok && ent != nil {
			hit.effects = append(hit.effects, mobEffectTellOf(ent, []effect.Change{ch}))
		}
	}
	return hit
}

// tridentCanTake is a trident's answer to the touch the arrows'
// playerTouch asks, the ThrownTrident's own: the thrower alone may pick it
// up — or anyone at all, where a body of the world kept no owner — with
// the arrows' ground-and-shake rules underneath; a returning trident —
// noclip, the owner's — is the owner's wherever it flies, its pickup
// rule waived for the take the return ends in.
// vanilla: ThrownTrident.playerTouch, tryPickup
func tridentCanTake(a *entity.Arrow, collector int32, creative bool) bool {
	if a.Dead || a.ShakeTicks > 0 {
		return false
	}
	stranger := a.OwnerID != 0 && collector != a.OwnerID
	if a.NoPhysics {
		return !stranger
	}
	if stranger || !a.InGround {
		return false
	}
	switch a.Pickup {
	case entity.PickupAllowed:
		return true
	case entity.PickupCreativeOnly:
		return creative
	}
	return false
}

// arrowWithinTouch reports whether a player's body stands close enough to
// an arrow for the touch the pickup runs on: the player's box grown a
// block sideways and half a block each way up meeting the arrow's
// half-block one.
// vanilla: Player.aiStep (getBoundingBox().inflate(1.0, 0.5, 1.0))
func arrowWithinTouch(p player.Position, dims rules.PlayerDimensions, a *entity.Arrow) bool {
	half := dims.Width / 2
	growX, growY := 1.0, 0.5
	px0, px1 := p.X-half-growX, p.X+half+growX
	py0, py1 := p.Y-growY, p.Y+dims.Height+growY
	pz0, pz1 := p.Z-half-growX, p.Z+half+growX
	ax0, ax1 := a.X-entity.ArrowHalfSize, a.X+entity.ArrowHalfSize
	ay0, ay1 := a.Y, a.Y+entity.ArrowHeight
	az0, az1 := a.Z-entity.ArrowHalfSize, a.Z+entity.ArrowHalfSize
	return px0 <= ax1 && px1 >= ax0 && py0 <= ay1 && py1 >= ay0 && pz0 <= az1 && pz1 >= az0
}

// spawnArrow lays one arrow in the world at the position and velocity
// given, pointed the yaw and pitch given — the bow's shot names the heading
// its bow velocity had before the shooter's movement joined it, vanilla
// Projectile.shoot setting yRot and xRot from the movement it was given and
// shootFromRotation adding getKnownMovement after — owned by the entity ID
// given, whose identity is the one the entity file will keep, pickup-ruled
// as given, and shows it to every player within its tracking range,
// returning the entity ID the packets address it by.
func (s *Server) spawnArrow(w *world.World, x, y, z, vx, vy, vz float64,
	yaw, pitch float32, crit bool, baseDamage float64, pickup entity.ArrowPickup, ownerID int32, ownerUUID uuid.UUID,
	weapon protocol.ItemStack, fireTicks int32) int32 {
	return s.spawnArrowFrom(w, x, y, z, vx, vy, vz, yaw, pitch, crit, baseDamage, pickup, ownerID, ownerUUID,
		weapon, fireTicks, protocol.ItemStack{})
}

// spawnArrowFrom is spawnArrow for a shot loaded with the ammo stack given:
// a spectral or tipped stack makes the matching arrow, and the stack is
// what a pickup gives back.
func (s *Server) spawnArrowFrom(w *world.World, x, y, z, vx, vy, vz float64,
	yaw, pitch float32, crit bool, baseDamage float64, pickup entity.ArrowPickup, ownerID int32, ownerUUID uuid.UUID,
	weapon protocol.ItemStack, fireTicks int32, ammo protocol.ItemStack) int32 {
	it := newArrowEntity(s, w, x, y, z, vx, vy, vz, yaw, pitch, crit, baseDamage, pickup, ownerID, ownerUUID, weapon, fireTicks)
	armArrow(it, ammo)
	s.launchArrowFrom(it, ammo, weapon)
	return it.arrow.ID
}

// launchArrowFrom puts the arrow in the world, then runs the fired
// ammunition's projectile_spawned effects at it, then the weapon's when the
// two are different items. The arrow's tick holds it still until they have
// run: a player's shot runs on its connection, which vanilla runs between
// ticks.
// vanilla: Projectile.spawnProjectile, Projectile.applyOnProjectileSpawned
func (s *Server) launchArrowFrom(it *arrowEntity, ammo, weapon protocol.ItemStack) {
	if !ammo.Empty() && weapon.ItemID == ammo.ItemID {
		weapon = protocol.ItemStack{}
	}
	effects := len(ammo.Enchantments) > 0 || len(weapon.Enchantments) > 0
	it.spawning = effects
	if !s.launchArrow(it) || !effects {
		return
	}
	s.spawnEffects(spawnedProjectile{w: it.w, typ: it.entityType, body: it}, ammo, weapon)
}

// spawnEffects runs the projectile_spawned effects of ammo, then weapon, at
// p, then lets p's tick step it.
func (s *Server) spawnEffects(p spawnedProjectile, ammo, weapon protocol.ItemStack) {
	defer p.body.spawned(s)
	s.projectileSpawned(ammo, p)
	s.projectileSpawned(weapon, p)
}

// spawnedProjectile is the projectile a projectile_spawned walk runs at.
type spawnedProjectile struct {
	w    *world.World
	typ  int32
	body spawnedBody
}

// spawnedBody reads a projectile under its kind's lock, which the caller
// must not hold.
type spawnedBody interface {
	// spawnedTarget is the body as it stands now.
	spawnedTarget(s *Server) enchantTarget
	// spawnedSnapshot freezes the body for a requirement; nil once it is gone.
	spawnedSnapshot(s *Server) *entitySnapshot
	// spawned lets the body's tick step it.
	spawned(s *Server)
}

// spawnedIgniter is a spawnedBody an ignite effect sets burning.
type spawnedIgniter interface {
	ignite(s *Server, ticks int32)
}

func (it *arrowEntity) spawnedTarget(s *Server) enchantTarget {
	s.arrowsMu.Lock()
	defer s.arrowsMu.Unlock()
	a := it.arrow
	return enchantTarget{uuid: it.uuid, x: a.X, y: a.Y, z: a.Z, width: .5, height: .5, entityID: a.ID, causeID: a.OwnerID}
}

func (it *arrowEntity) spawnedSnapshot(s *Server) *entitySnapshot {
	s.arrowsMu.Lock()
	defer s.arrowsMu.Unlock()
	return s.arrowSnapshot(it, nil)
}

func (it *arrowEntity) spawned(s *Server) {
	s.arrowsMu.Lock()
	it.spawning = false
	s.arrowsMu.Unlock()
}

func (it *arrowEntity) ignite(s *Server, ticks int32) {
	s.arrowsMu.Lock()
	it.arrow.FireTicks = max(it.arrow.FireTicks, int(ticks))
	s.arrowsMu.Unlock()
}

// spawnedContext is the context a projectile_spawned requirement reads: the
// projectile as "this" and where it stands now as the origin.
// vanilla: Enchantment.entityContext
func (s *Server) spawnedContext(p spawnedProjectile) *rules.EnchContext {
	t := p.body.spawnedTarget(s)
	origin := [3]float64{t.x, t.y, t.z}
	return &rules.EnchContext{
		Victim: p.typ,
		Rand:   s.levelRng,
		BodyOf: func() *rules.LootEntity {
			if load := snapshotLoot(p.body.spawnedSnapshot(s)); load != nil {
				return load()
			}
			return &rules.LootEntity{Type: registry.EntityType(p.typ).Name()}
		},
		Place: func(this bool) rules.EnchPlace {
			at := origin
			if this {
				t := p.body.spawnedTarget(s)
				at = [3]float64{t.x, t.y, t.z}
			}
			return enchPlaceAt(p.w, at[0], at[1], at[2])
		},
		LootWorld: func(loot *rules.LootContext) {
			s.fillLootOrigin(p.w, loot, origin)
		},
	}
}

// projectileSpawned runs the item's projectile_spawned effects at the
// projectile in order, each where the projectile stands when it runs.
// vanilla: EnchantmentHelper.onProjectileSpawned
func (s *Server) projectileSpawned(item protocol.ItemStack, p spawnedProjectile) {
	if len(item.Enchantments) == 0 {
		return
	}
	rules.VisitProjectileSpawned(item, func() *rules.EnchContext { return s.spawnedContext(p) }, func(e rules.LeveledEffect) {
		if ig, ok := p.body.(spawnedIgniter); ok && e.Effect.Duration != nil {
			ig.ignite(s, rules.IgniteTicks(float64(rules.EnchLevelValueAt(e.Effect.Duration, e.Level))))
		}
		s.applyPackEffects(p.w, e.Effect, e.Level, p.body.spawnedTarget(s))
	})
}

// armArrow makes the arrow the ammo stack's kind: the spectral entity type,
// or the tipped arrow's effects. The one-item stack, components included,
// is kept as the pickup.
// vanilla: ArrowItem.createArrow, SpectralArrowItem.createArrow, Arrow.getPotionContents
func armArrow(it *arrowEntity, ammo protocol.ItemStack) {
	if ammo.Empty() {
		return
	}
	if registry.Item(ammo.ItemID) == registry.ItemSpectralArrow {
		it.entityType = protocol.EntityTypeSpectralArrow
		it.arrow.GlowTicks = spectralArrowGlowTicks
	}
	if it.entityType == protocol.EntityTypeArrow {
		it.arrow.Effect = pickupEffect(ammo)
	}
	ammo.Count = 1
	it.arrow.PickupStack = ammo
}

// spawnTrident lays one thrown trident in the world the way spawnArrow
// lays a bow's shot — the same pairing, the same tracking — the stack the
// throw spent carried as the pickup's own return and the hit's
// enchantments' weapon, and shows it to every player within the arrow
// type's tracking range, returning the entity ID the packets address it
// by.
func (s *Server) spawnTrident(w *world.World, x, y, z, vx, vy, vz float64,
	yaw, pitch float32, pickup entity.ArrowPickup, ownerID int32, ownerUUID uuid.UUID,
	stack protocol.ItemStack) int32 {
	id := s.nextEntity()
	it := &arrowEntity{
		arrow:      entity.NewTrident(id, x, y, z, vx, vy, vz, pickup, ownerID, stack, s.entitySeed(id), entityItemEnchContext(w, x, y, z, protocol.EntityTypeTrident)),
		uuid:       uuid.New(),
		w:          w,
		entityType: protocol.EntityTypeTrident,
		sentVX:     vx, sentVY: vy, sentVZ: vz,
		ownerUUID: ownerUUID,
	}
	it.arrow.Yaw, it.arrow.Pitch = yaw, pitch
	it.arrow.Foil = tridentFoil(&stack)
	s.launchArrowFrom(it, protocol.ItemStack{}, stack)
	return it.arrow.ID
}

// newArrowEntity builds the arrow spawnArrow lays, not yet in the world.
func newArrowEntity(s *Server, w *world.World, x, y, z, vx, vy, vz float64,
	yaw, pitch float32, crit bool, baseDamage float64, pickup entity.ArrowPickup, ownerID int32, ownerUUID uuid.UUID,
	weapon protocol.ItemStack, fireTicks int32) *arrowEntity {
	identity := uuid.New()
	id := s.nextEntity()
	it := &arrowEntity{
		arrow: entity.NewArrow(id, x, y, z, vx, vy, vz,
			crit, baseDamage, pickup, ownerID, s.entitySeed(id)),
		uuid:       identity,
		w:          w,
		entityType: protocol.EntityTypeArrow,
		sentVX:     vx, sentVY: vy, sentVZ: vz,
		ownerUUID: ownerUUID,
	}
	it.arrow.Yaw, it.arrow.Pitch = yaw, pitch
	it.arrow.Weapon = weapon
	it.arrow.FireTicks = int(fireTicks)
	return it
}

// summonArrow stands a /summon's arrow up at the position given: read
// through the same loader a saved shaft's wake reads — the motion its tag
// named, the rotation, the pickup rule, the crit flag, the weapon — with the
// command's position the one it flies from, ownerless where the tag named
// no Owner. A load that comes to nothing and a tag whose UUID is already a
// live body's are the two refusals, the notifier the caller's.
// vanilla: SummonCommand.createEntity
func (s *Server) summonArrow(w *world.World, name string, x, y, z float64, tag nbt.Compound) (*arrowEntity, error) {
	compound := summonTag(tag, name, x, y, z)
	if woken, fresh := s.wakeChunkOnce(s.dimensionOfWorld(w),
		world.ChunkPos{X: world.ChunkOf(x), Z: world.ChunkOf(z)}); fresh {
		s.registerWake(woken)
	}
	pos := world.ChunkPos{X: world.ChunkOf(x), Z: world.ChunkOf(z)}
	w.Acquire(pos)
	it := arrowOfTag(s, w, compound)
	w.Release(pos)
	if it == nil {
		return nil, errNotSimulated
	}
	if s.summonDuplicate(it.uuid) {
		return nil, command.ErrDuplicateUUID
	}
	it.arrow.OwnerID = s.resolveOwner(it.ownerUUID)
	it.arrow.SetPos(x, y, z)
	s.registerArrow(it)
	return it, nil
}

// registerArrow puts a built arrow into the world and shows it to every
// player in range: the one door every arrival but a wake walks.
func (s *Server) registerArrow(it *arrowEntity) {
	// Pairing accounts for the initial tracking update; the first periodic
	// correction is after twenty flight ticks, not twenty-one.
	// vanilla: ServerEntity.sendChanges
	it.tickCount = 1
	arrowTracker.register(s, it)
}

// arrowPairing is what shows an arrow: the spawn, then the metadata a crit,
// a stick or a fire gives it.
func arrowPairing(it *arrowEntity) []outbound {
	return spawnAndMetadata(arrowSpawnPayload(it), arrowMetadataPayload(it))
}

// arrowSpawnPayload builds the Spawn Entity packet that puts an arrow in
// the world for a client: entity ID and UUID, the entity type the arrow is
// — the plain minecraft:arrow, the spectral one whose flight it shares, or
// the minecraft:trident of a thrown trident — position, velocity and
// heading. The data field is the shooter's entity ID, nought for an arrow
// nobody owns, which the client resolves the owner from — vanilla
// Projectile.getAddEntityPacket hands the owner's own ID to the packet.
func arrowSpawnPayload(it *arrowEntity) []byte {
	a := it.arrow
	return spawnEntityPayload(spawnEntity{
		id: a.ID, uuid: it.uuid, typ: it.entityType,
		x: a.X, y: a.Y, z: a.Z,
		vx: a.VX, vy: a.VY, vz: a.VZ,
		pitch: encodeAngle(a.Pitch),
		yaw:   encodeAngle(a.Yaw),
		data:  a.OwnerID,
	})
}

// arrowPickup gives a picked-up arrow to its collector and tells the
// world: the slot updates, the Take Item Entity packet that plays the
// arrow flying to its taker, and the body going out of every view. The
// tick's pickups run one after another, so the first toucher's discard
// leaves nothing for the next; an add the inventory cannot hold leaves the
// shaft where it stuck.
func (s *Server) arrowPickup(p arrowPickup) {
	c := p.collector
	it := p.arrow
	a := it.arrow

	// vanilla: AbstractArrow.playerTouch
	s.arrowsMu.Lock()
	gone := a.Dead
	s.arrowsMu.Unlock()
	if gone {
		return
	}
	if !s.mayPickUpArrow(c, it) {
		return
	}

	// vanilla: AbstractArrow.getPickupItem — the pickup stack the arrow
	// was born holding; the creative-only kind gives nothing. A trident's
	// own stack is the one the throw spent, wear and enchantments whole.
	// vanilla: ThrownTrident.getPickupItem, tryPickup
	var slots [][]byte
	var visible bool
	creative := c.player.GameMode() == protocol.GameModeCreative
	// The rule answers first: an allowed stack is added below, a
	// creative-only one a creative hand takes without adding it.
	addsStack := a.Pickup == entity.PickupAllowed
	taken := a.Pickup != entity.PickupDisallowed
	if a.Pickup == entity.PickupCreativeOnly {
		taken = creative
	}
	// ThrownTrident.tryPickup's return: a returning trident the collector
	// owns is taken where the rule refused it, its stack added.
	if !taken && a.Trident && a.NoPhysics && a.OwnerID != 0 && c.player.EntityID == a.OwnerID {
		addsStack = true
	}
	if addsStack {
		stack := protocol.ItemStack{}
		if a.Trident {
			stack = a.Weapon
		} else if !a.PickupStack.Empty() {
			stack = a.PickupStack
		} else {
			item := registry.ItemArrow
			if it.entityType == protocol.EntityTypeSpectralArrow {
				item = registry.ItemSpectralArrow
			}
			stack = protocol.ItemStack{ItemID: int32(item), Count: 1}
		}
		if !stack.Empty() {
			c.player.Inventory(func(inv *player.Inventory) {
				left, changed := inv.Add(stack)
				// A creative add always succeeds, room or not.
				// vanilla: Inventory.add
				taken = left == 0 || creative
				for _, slot := range changed {
					slots = append(slots, playerWindowSlotOf(inv, slot))
					visible = visible || player.VisibleSlot(inv, slot)
				}
			})
			if taken {
				for _, slot := range slots {
					c.send(protocol.IDPlayContainerSlot, slot)
				}
				if visible {
					c.broadcastEquipment()
				}
			}
		}
	}

	// A survival add with no room fails the pickup, and the arrow stays
	// stuck for a later touch. vanilla: AbstractArrow.tryPickup
	s.arrowsMu.Lock()
	if taken {
		a.Dead = true
	}
	s.arrowsMu.Unlock()
	if !taken {
		return
	}

	s.arrowsMu.Lock()
	watchers := trackedWatchers(it.tracked)
	s.arrowsMu.Unlock()
	s.tellPickup(it.w, watchers, c, pickupItemPayload(a.ID, c.player.EntityID, 1))
	s.entityDestroyed(it.uuid)
	s.broadcastWorld(it.w, protocol.IDPlayRemoveEntities,
		removeEntitiesPayload([]int32{a.ID}))
}

// projectileUnhittable reports whether this player's body is one no
// projectile can meet at all — vanilla's canBeHitByProjectile across the
// game modes, true of a spectator alone. A creative player's body is met
// like any other's: the hurt is what their mode refuses, and the shaft
// deflects off the refusal.
func projectileUnhittable(c *client) bool {
	return c.player.GameMode() == protocol.GameModeSpectator
}

// killArrow discards an arrow the way the /kill command and vanilla's
// Entity.kill do: out of the world flying or stuck, nothing laid down
// behind it.
func (s *Server) killArrow(it *arrowEntity) {
	s.arrowsMu.Lock()
	it.arrow.Dead = true
	s.arrowsMu.Unlock()
	s.entityDestroyed(it.uuid)
	s.broadcastWorld(it.w, protocol.IDPlayRemoveEntities,
		removeEntitiesPayload([]int32{it.arrow.ID}))
}

// spendArrow discards an arrow that landed on a body; one with a pierce level
// flies on, the sweep having counted the body.
// vanilla: AbstractArrow.onHitEntity
func (s *Server) spendArrow(it *arrowEntity) {
	if !it.piercingHit {
		s.killArrow(it)
	}
}

// teleportArrow moves an arrow to the position given and stops it.
// vanilla: Entity.teleportTo
func (s *Server) teleportArrow(it *arrowEntity, to player.Position, by string) {
	arrowTracker.teleport(s, it, func() {
		it.arrow.SetPos(to.X, to.Y, to.Z)
		it.arrow.VX, it.arrow.VY, it.arrow.VZ = 0, 0, 0
	})
	logTeleported("arrow", to, by)
}

// arrowTwinAt builds the arrow that continues this one's flight in another
// dimension, standing at pos: a new shaft, new ID but the same UUID, raised
// from the old one's entity tag, which keeps stick state, life, arrival
// shake, pickup rule, damage, critical bit, velocity. The caller holds the
// arrow lock or has taken the arrow out of the list.
// vanilla: Entity.teleportCrossDimension
func (s *Server) arrowTwinAt(it *arrowEntity, to *dimension, pos player.Position) *arrowEntity {
	twin := arrowOfTag(s, to.w, arrowTag(it).Entries())
	if twin == nil {
		return nil
	}
	twin.arrow.SetPos(pos.X, pos.Y, pos.Z)
	return twin
}

// portalTwinOfArrow is arrowTwinAt at a portal's arrival, the yaw turned
// by the angle between the two portals.
func (s *Server) portalTwinOfArrow(it *arrowEntity, to *dimension, arrival world.SpawnPos, kind rules.PortalKind, exitAxis [2]int32) *arrowEntity {
	twin := s.arrowTwinAt(it, to, standingAt(arrival))
	if twin == nil {
		return nil
	}
	if turn := portalTurnAt(it.w, kind, player.Position{X: it.arrow.X, Y: it.arrow.Y, Z: it.arrow.Z, Yaw: twin.arrow.Yaw}, exitAxis); turn != 0 {
		// The turn is the yaw's alone: vanilla's transition carries the
		// rotation relative and the movement without ROTATE_DELTA, so the
		// flight keeps the direction it had however the portals run.
		twin.arrow.Yaw += turn
	}
	return twin
}

// finishArrowTravel sends one arrow the portal pass caught across its
// portal (crossPortal), the shooter's reference resolved in the target world.
func (s *Server) finishArrowTravel(it *arrowEntity, kind rules.PortalKind) {
	pos := player.Position{X: it.arrow.X, Y: it.arrow.Y, Z: it.arrow.Z}
	s.crossPortal(it.w, it.arrow.ID, pos, kind, func(to *dimension, spot world.SpawnPos, exitAxis [2]int32) func() {
		twin := s.portalTwinOfArrow(it, to, spot, kind, exitAxis)
		if twin == nil {
			slog.Error("could not carry an arrow across a portal")
			return nil
		}
		return func() {
			twin.arrow.OwnerID = s.resolveOwner(twin.ownerUUID)
			s.registerArrow(twin)
			slog.Debug("arrow travelled through a portal",
				"to", to.name, "x", twin.arrow.X, "y", twin.arrow.Y, "z", twin.arrow.Z)
		}
	}, func() {
		s.arrowsMu.Lock()
		s.arrows = append(s.arrows, it)
		arrowTracker.file(s, it)
		s.arrowsMu.Unlock()
	})
}

// arrowCauldron is a layered cauldron a burning arrow was put out in.
type arrowCauldron struct {
	w       *world.World
	x, y, z int32
}

// arrowTypeName is the entity type name of a shaft.
func arrowTypeName(it *arrowEntity) string {
	switch {
	case it.arrow.Trident:
		return "minecraft:trident"
	case it.entityType == protocol.EntityTypeSpectralArrow:
		return "minecraft:spectral_arrow"
	}
	return "minecraft:arrow"
}

// killedByArrow returns the victims killed_by_arrow reads for a hit on a
// body of type victim, or nil when it does not fire. The caller holds the
// arrow lock. vanilla: AbstractArrow.onHitEntity
func (it *arrowEntity) killedByArrow(died bool, victim string) []string {
	// A trident's own hit has no such trigger.
	if it.arrow.Trident {
		return nil
	}
	switch {
	case it.piercingHit:
		if died {
			it.pierced = append(it.pierced, victim)
		}
		// Non-nil even when empty: vanilla fires for every piercing hit.
		return append([]string{}, it.pierced...)
	case died:
		return []string{victim}
	}
	return nil
}

// arrowHitAdvancements fires player_hurt_entity and, for non-nil killed,
// killed_by_arrow for a player's shaft that hurt a body; shooter may be nil.
// vanilla: LivingEntity.hurtServer, AbstractArrow.onHitEntity
func (s *Server) arrowHitAdvancements(shooter *client, direct string, killed []string, weapon protocol.ItemStack, victim *advancement.Entity) {
	if shooter == nil {
		return
	}
	if progress := shooter.advancements.Load(); progress != nil {
		now := time.Now()
		progress.PlayerHurtProjectile(direct, now)
		if victim != nil {
			progress.PlayerHurtEntity(*victim, now)
		}
		if killed != nil {
			progress.KilledByArrow(killed, weapon, &advancement.Subjects{Victims: advancement.TypeOnly(killed), Distinct: true}, now)
		}
	}
}

// shotArrow queues a mob's arrow or thrown trident.
func (t *mobDimTick) shotArrow(fx *mobEffects, ev entity.MobEvent) {
	h, w := t.h, t.w
	// A skeleton's bow let go: the arrow is stood in the
	// world outside the locks, owned by the skeleton and
	// pickup-ruled to nobody — vanilla's DISALLOWED.
	shot := mobShot{
		w: w, shooter: ev.Mob.ID,
		x: ev.X, y: ev.Y, z: ev.Z,
		vx: ev.VX, vy: ev.VY, vz: ev.VZ,
		damage:   ev.Damage,
		effect:   ev.Effect,
		weapon:   ev.Mob.MainHand,
		crossbow: ev.Crossbow,
		trident:  ev.Kind == entity.MobThrewTrident,
	}
	if shot.trident && registry.Item(shot.weapon.ItemID) != registry.ItemTrident {
		// vanilla: Drowned.performRangedAttack can finish a hunt after the weapon changes.
		shot.weapon = protocol.ItemStack{ItemID: int32(registry.ItemTrident), Count: 1}
	}
	if shot.crossbow {
		shot.weapon = ev.Stack
		shot.ammo = ev.Ammo
	} else if !shot.trident && registry.Item(shot.weapon.ItemID) != registry.ItemBow {
		// vanilla: ProjectileUtil.getWeaponHoldingHand falls back to the off hand
		shot.weapon = ev.Mob.OffHand
	}
	if ev.Mob.Type == entity.TypeWitherSkeleton {
		// vanilla: WitherSkeleton.getArrow, igniteForSeconds(100)
		shot.fireTicks = rules.IgniteTicks(100)
	}
	if ent, ok := h.book[ev.Mob]; ok {
		shot.shooterUUID = ent.uuid
	}
	fx.shots = append(fx.shots, shot)
}

// spawnMobShot stands a mob's arrow or thrown trident in the world.
func (s *Server) spawnMobShot(shot mobShot) {
	if shot.trident {
		yaw, pitch := entity.ArrowAngles(shot.vx, shot.vy, shot.vz)
		// AbstractArrow.setOwner only allows pickup for a player owner.
		s.spawnTrident(shot.w, shot.x, shot.y, shot.z, shot.vx, shot.vy, shot.vz, yaw, pitch,
			entity.PickupDisallowed, shot.shooter, shot.shooterUUID, shot.weapon)
		return
	}
	var ammo protocol.ItemStack
	if shot.ammo != nil {
		ammo = *shot.ammo
	}
	if isFireworkRocket(ammo) {
		s.spawnCrossbowRocket(shot.w, ammo, shot.x, shot.y, shot.z, shot.vx, shot.vy, shot.vz,
			shot.shooter, shot.shooterUUID, true)
		return
	}
	yaw, pitch := entity.ArrowAngles(shot.vx, shot.vy, shot.vz)
	pickup := entity.PickupDisallowed
	if ammo.Intangible {
		// vanilla: AbstractArrow.setPickup via the intangible projectile
		pickup = entity.PickupCreativeOnly
	}
	it := newArrowEntity(s, shot.w, shot.x, shot.y, shot.z, shot.vx, shot.vy, shot.vz, yaw, pitch,
		false, shot.damage, pickup, shot.shooter, shot.shooterUUID,
		shot.weapon, shot.fireTicks)
	it.arrow.Effect = shot.effect
	armArrow(it, ammo)
	if shot.crossbow {
		// vanilla: CrossbowItem.createProjectile
		it.arrow.HitSound = crossbowHitSound
		it.arrow.PierceLevel = rules.PiercingLevel(shot.weapon, ammo, &rules.EnchContext{Rand: s.levelRng})
	}
	s.launchArrowFrom(it, ammo, shot.weapon)
}

// mayPickUpArrow puts a player's touch of a shaft they may take to
// PlayerPickupArrowEvent listeners and reports whether the pickup goes on.
// vanilla: AbstractArrow.playerTouch
func (s *Server) mayPickUpArrow(c *client, it *arrowEntity) bool {
	if s.plugins == nil || !s.plugins.Has(pluginruntime.PlayerPickupArrow) {
		return true
	}
	a := it.arrow
	creative := c.player.GameMode() == protocol.GameModeCreative
	if !a.CanPickUp(creative) && (!a.Trident || !a.NoPhysics || a.OwnerID == 0 || c.player.EntityID != a.OwnerID) {
		return true
	}
	e := &plugin.PlayerPickupArrowEvent{Player: c.pluginSelf(), Arrow: pluginEntity{id: it.uuid}}
	s.firePluginEvent(e)
	return !e.Cancelled() && !a.Dead
}
