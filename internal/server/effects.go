package server

import (
	"slices"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/effect"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
)

// Status effects on the wire. A player's effect packets go to that player
// only (vanilla: ServerPlayer.onEffectAdded and the rest). A mob's go to its
// player passengers only, who drive it from their client (vanilla:
// LivingEntity.sendEffectToPassengers); everyone else sees mob effects as
// metadata particles, sent once a tick on change (vanilla:
// ServerEntity.sendDirtyEntityData).

// bodyShown is what a player's metadata last said about their effects.
type bodyShown struct {
	particles  []effect.Particle
	ambient    bool
	absorption float32
	invisible  bool
	glowing    bool
}

// effectParticleTypes maps the particle types effects give off to their IDs.
var effectParticleTypes = map[string]int32{
	"minecraft:entity_effect": protocol.ParticleEntityEffect,
	"minecraft:infested":      protocol.ParticleInfested,
	"minecraft:item_cobweb":   protocol.ParticleItemCobweb,
	"minecraft:item_slime":    protocol.ParticleItemSlime,
	"minecraft:raid_omen":     protocol.ParticleRaidOmen,
	"minecraft:small_gust":    protocol.ParticleSmallGust,
	"minecraft:trial_omen":    protocol.ParticleTrialOmen,
}

// updateMobEffectPayload builds Update Mob Effect; blend marks a new effect
// (vanilla: ClientboundUpdateMobEffectPacket).
func updateMobEffectPayload(entityID int32, in effect.Instance, blend bool) []byte {
	var flags uint8
	if in.Ambient {
		flags |= 0x01
	}
	if in.Visible {
		flags |= 0x02
	}
	if in.ShowIcon {
		flags |= 0x04
	}
	if blend {
		flags |= 0x08
	}
	var b protocol.Buffer
	b.PutVarInt(entityID)
	b.PutVarInt(in.ID)
	b.PutVarInt(in.Amplifier)
	b.PutVarInt(in.Duration)
	b.PutU8(flags)
	return b.Bytes()
}

// removeMobEffectPayload builds Remove Mob Effect.
func removeMobEffectPayload(entityID, effectID int32) []byte {
	var b protocol.Buffer
	b.PutVarInt(entityID)
	b.PutVarInt(effectID)
	return b.Bytes()
}

// riderEffectPackets are the packets a mob's effect changes send its
// riders; the blend flag is false for an added and an updated effect alike.
// vanilla: LivingEntity.onEffectAdded, onEffectUpdated, onEffectsRemoved
func riderEffectPackets(mobID int32, changes []effect.Change) []outbound {
	out := make([]outbound, 0, len(changes))
	for _, ch := range changes {
		if ch.Kind == effect.Removed {
			out = append(out, outbound{id: protocol.IDPlayRemoveMobEffect, data: removeMobEffectPayload(mobID, ch.Instance.ID)})
			continue
		}
		out = append(out, outbound{id: protocol.IDPlayUpdateMobEffect, data: updateMobEffectPayload(mobID, ch.Instance, false)})
	}
	return out
}

// tellSplashMob sends a mob's effect changes to its watchers and riders.
func (s *Server) tellSplashMob(w *world.World, t splashMobTell) {
	s.tellWatchers(w, t.watchers, t.effects)
	for _, c := range t.riders {
		for _, p := range t.riderEffects {
			c.sendIn(w, p.id, p.data)
		}
	}
}

// tellEffects sends the player their effect changes in order and marks the
// particles for resending.
func (c *client) tellEffects(changes []effect.Change) {
	c.tellEffectsFrom(changes, "")
}

func (c *client) tellEffectsFrom(changes []effect.Change, source string) {
	if len(changes) > 0 {
		c.effectsDirty.Store(true)
		defer c.effectsChangedAdvancements(source)
	}
	const levitation = registry.MobEffectLevitation
	for _, ch := range changes {
		if ch.Instance.ID == levitation && ch.Kind != effect.Updated {
			c.levitationStarted(ch.Kind == effect.Added)
		}
		if ch.Kind == effect.Removed {
			c.send(protocol.IDPlayRemoveMobEffect, removeMobEffectPayload(c.player.EntityID, ch.Instance.ID))
			continue
		}
		c.send(protocol.IDPlayUpdateMobEffect, updateMobEffectPayload(c.player.EntityID, ch.Instance, ch.Kind == effect.Added))
		if ch.Kind != effect.Added {
			continue
		}
		if name := rules.EffectAddedSound(ch.Instance.ID); name != registry.SoundNone {
			pos := c.player.Position()
			c.srv.broadcastSoundID(c.theWorld(), name, protocol.SoundCategoryPlayer, pos.X, pos.Y, pos.Z, 1.0, 1.0)
		}
	}
}

// sendActiveEffects sends every active effect on join or dimension change
// (vanilla: PlayerList.sendActivePlayerEffects).
func (c *client) sendActiveEffects() error {
	for _, in := range c.player.Effects() {
		if err := c.writePacket(protocol.IDPlayUpdateMobEffect, "update mob effect",
			updateMobEffectPayload(c.player.EntityID, in, false)); err != nil {
			return err
		}
	}
	return nil
}

// bodyNow is what the metadata should say about the player's effects. With
// none left vanilla clears the particles but keeps the ambient flag
// (LivingEntity.updateInvisibilityStatus); a spectator shows none
// (ServerPlayer.updateInvisibilityStatus).
func bodyNow(p *player.Player, was bodyShown) bodyShown {
	active := p.Effects()
	v := p.Vitals()
	now := bodyShown{
		absorption: float32(v.Absorption),
		ambient:    was.ambient,
		invisible:  rules.InvisibleToOthers(v.GameMode, active),
		glowing:    rules.Glowing(active),
	}
	if len(active) > 0 && v.GameMode != protocol.GameModeSpectator {
		now.particles, now.ambient = effect.Particles(active)
	}
	return now
}

// mobEffectShown is bodyNow for a mob, including its retained ambience.
func mobEffectShown(m *entity.Mob) bodyShown {
	shown := bodyShown{ambient: m.EffectAmbience()}
	if active := m.AllEffects(); len(active) > 0 {
		shown.particles, _ = effect.Particles(active)
		shown.invisible = rules.Invisible(active)
		shown.glowing = rules.Glowing(active)
	}
	return shown
}

// tellBody sends the world the effect particles and extra hearts when
// changed, and the shared flags when invisibility or glowing changed. Any
// effect change resends the particle list even if equal: vanilla's particle
// options compare by identity, so its recomputed list never equals the old.
func (c *client) tellBody() {
	c.trackMu.Lock()
	was := c.shown
	now := bodyNow(c.player, was)
	c.shown = now
	c.trackMu.Unlock()

	dirty := c.effectsDirty.Swap(false)
	particles := !slices.Equal(now.particles, was.particles) || dirty && len(now.particles) > 0
	ambient := now.ambient != was.ambient
	absorption := now.absorption != was.absorption
	flags := now.invisible != was.invisible || now.glowing != was.glowing
	if !particles && !ambient && !absorption && !flags {
		return
	}
	var b protocol.Buffer
	b.PutVarInt(c.player.EntityID)
	if flags {
		v := c.player.Vitals()
		b.PutMetadata(protocol.MetadataIndexEntityFlags, protocol.MetadataByte)
		b.PutU8(playerFlagsFrom(c.player, rules.OnFire(&v), now.invisible, now.glowing))
	}
	putBodyMetadata(&b, now, particles, ambient, absorption)
	b.EndMetadata()
	c.srv.broadcastWorld(c.theWorld(), protocol.IDPlayEntityMetadata, b.Bytes())
}

// putBodyMetadata writes the chosen effect and absorption entries.
func putBodyMetadata(b *protocol.Buffer, s bodyShown, particles, ambient, absorption bool) {
	if particles {
		b.PutMetadata(protocol.MetadataIndexEffectParticles, protocol.MetadataParticles)
		b.PutVarInt(int32(len(s.particles)))
		for _, p := range s.particles {
			b.PutVarInt(effectParticleTypes[p.Type])
			if p.Type == "minecraft:entity_effect" {
				b.PutI32(p.Color)
			}
		}
	}
	if ambient {
		b.PutMetadata(protocol.MetadataIndexEffectAmbient, protocol.MetadataBoolean)
		b.PutBool(s.ambient)
	}
	if absorption {
		b.PutMetadata(protocol.MetadataIndexAbsorption, protocol.MetadataFloat)
		b.PutF32(s.absorption)
	}
}

// mobEffectTells is the metadata a mob's effect changes produce for its
// viewers: the whole particle list (even when empty) and the shared flags
// byte if invisible or glowing changed. Baselines advance so the metadata
// pass does not repeat them. A new omen effect also plays its sound in the
// mob's category. The mob lock is held; the caller sends.
// vanilla: ServerEntity.sendDirtyEntityData, MobEffect.onEffectAdded
func mobEffectTells(ent *mobEntity, changes []effect.Change) []outbound {
	if len(changes) == 0 {
		return nil
	}
	var sounds []outbound
	speedChanged := false
	for _, ch := range changes {
		speedChanged = speedChanged || ch.Instance.ID == registry.MobEffectSpeed || ch.Instance.ID == registry.MobEffectSlowness
		if ch.Kind != effect.Added {
			continue
		}
		if name := rules.EffectAddedSound(ch.Instance.ID); name != registry.SoundNone {
			m := ent.mob
			sounds = append(sounds, outbound{id: protocol.IDPlaySoundEffect,
				data: soundEffectIDAt(name, mobSoundCategory(m.Type), m.X, m.Y, m.Z, 1.0, 1.0)})
		}
	}
	if speedChanged && ent.mob.Attributes() == nil {
		// The movement_speed modifiers of Speed and Slowness; a body with a
		// map sends them from the dirty scan.
		// vanilla: MobEffect.addAttributeModifiers, ServerEntity.sendDirtyEntityData
		sounds = append(sounds, outbound{id: protocol.IDPlayUpdateAttributes, data: mobAttributesPayload(ent)})
	}
	state := mobMetadataStateOf(ent.mob)
	var particles, ambient, flags bool
	for _, was := range ent.tracked.all() {
		particles = particles || !slices.Equal(was.own.meta.particles, state.particles)
		ambient = ambient || was.own.meta.ambient != state.ambient
		flags = flags || was.own.meta.baseFlags != state.baseFlags
	}
	ent.updateShownMetadata(func(shown *mobMetadataState) {
		shown.particles, shown.ambient = state.particles, state.ambient
		shown.baseFlags = state.baseFlags
	})
	if !particles && !ambient && !flags {
		return sounds
	}
	var b protocol.Buffer
	b.PutVarInt(ent.mob.ID)
	if flags {
		b.PutMetadata(protocol.MetadataIndexEntityFlags, protocol.MetadataByte)
		b.PutU8(state.baseFlags)
	}
	putBodyMetadata(&b, bodyShown{particles: state.particles, ambient: state.ambient}, particles, ambient, false)
	b.EndMetadata()
	return append(sounds, outbound{id: protocol.IDPlayEntityMetadata, data: b.Bytes()})
}

// blowEffect is the effect instance a monster's blow or tipped arrow
// applies, with particles and icon shown.
func blowEffect(id, ticks, amplifier int32) effect.Instance {
	return effect.New(id, ticks, amplifier, false, true, true)
}

// tellWatchers sends the packets to every watcher still in the world.
func (s *Server) tellWatchers(w *world.World, watchers []uuid.UUID, packets []outbound) {
	for _, id := range watchers {
		s.mu.RLock()
		c := s.clients[id]
		s.mu.RUnlock()
		if c == nil || c.theWorld() != w {
			continue
		}
		for _, p := range packets {
			c.sendIn(w, p.id, p.data)
		}
	}
}

// tellPickup sends Take Item Entity to the collected body's trackers and to
// the collector (nil for a mob).
// vanilla: LivingEntity.take, ChunkMap.sendToTrackingPlayers
func (s *Server) tellPickup(w *world.World, watchers []uuid.UUID, collector *client, payload []byte) {
	out := []outbound{{id: protocol.IDPlayPickupItem, data: payload}}
	s.tellWatchers(w, watchers, out)
	if collector != nil && !slices.Contains(watchers, collector.player.UUID) && collector.theWorld() == w {
		collector.sendIn(w, out[0].id, out[0].data)
	}
}

// SetEffectCause names what the next effect change is, for
// EntityPotionEffectEvent.
func (c *client) SetEffectCause(cause effect.Cause) { c.effectCause = cause }

// AddEffect gives the player an effect. vanilla: LivingEntity.addEffect
func (c *client) AddEffect(in effect.Instance) bool {
	in, ok := c.askPlayerEffect(in.ID, &in)
	if !ok {
		return false
	}
	return c.changeEffect(func(v *player.Vitals, s *effect.Set, _ *player.Attributes) (effect.Change, bool) {
		return rules.AddEffect(v, s, in)
	})
}

// extinguishFire puts out a burning player, updating metadata so the
// flames stop, with the fizz. vanilla: Entity.extinguishFire
func (c *client) extinguishFire() {
	wasOnFire := false
	c.player.UpdateVitals(func(v *player.Vitals) {
		if v.FireTicks > 0 {
			wasOnFire = true
			v.FireTicks = 0
		}
	})
	if wasOnFire {
		pos := c.player.Position()
		c.srv.broadcastSoundPacketExcept(c.theWorld(), nil,
			c.srv.extinguishSound(protocol.SoundCategoryPlayer, pos.X, pos.Y, pos.Z))

	}
	c.srv.broadcastWorld(c.theWorld(), protocol.IDPlayEntityMetadata, playerMetadata(c.player, true))
}

// RemoveEffect takes one effect off the player, for /effect clear.
func (c *client) RemoveEffect(id int32) bool {
	if _, ok := c.askPlayerEffect(id, nil); !ok {
		return false
	}
	return c.changeEffect(func(v *player.Vitals, s *effect.Set, a *player.Attributes) (effect.Change, bool) {
		return rules.RemoveEffect(v, s, a, id)
	})
}

// ClearEffects takes every effect off the player, for /effect clear.
func (c *client) ClearEffects() bool {
	if c.srv.effectListened() {
		cause, changed := c.effectCause, false
		for _, in := range c.player.Effects() {
			c.effectCause = cause
			changed = c.RemoveEffect(in.ID) || changed
		}
		c.effectCause = effect.CauseOther
		return changed
	}
	c.effectCause = effect.CauseOther
	return c.changeEffects(rules.ClearEffects)
}

// changeEffects applies an effect change and sends the result, reporting
// whether anything changed.
func (c *client) changeEffects(change func(*player.Vitals, *effect.Set, *player.Attributes) []effect.Change) bool {
	return c.changeEffectsFrom(change, "")
}

func (c *client) changeEffectsFrom(change func(*player.Vitals, *effect.Set, *player.Attributes) []effect.Change, source string) bool {
	var changes []effect.Change
	var before float64
	after := c.player.UpdateBody(func(v *player.Vitals, s *effect.Set, a *player.Attributes) {
		before = v.Health
		changes = change(v, s, a)
		// The modifiers follow at once, so a command later in the same tick
		// reads them. vanilla: LivingEntity.onEffectAdded, onEffectsRemoved
		if len(changes) > 0 {
			rules.UpdateEffectAttributes(a, s.All())
		}
	})
	c.tellEffectsFrom(changes, source)
	// A weaker or ended max_health cap lowers health here, since a command's
	// effect change between ticks escapes the tick's comparison.
	if !after.Dead && after.Health != before {
		c.sendHealth(after)
	}
	return len(changes) > 0
}

// changeEffect is changeEffects for a change that comes to at most one effect.
func (c *client) changeEffect(change func(*player.Vitals, *effect.Set, *player.Attributes) (effect.Change, bool)) bool {
	return c.changeEffects(func(v *player.Vitals, s *effect.Set, a *player.Attributes) []effect.Change {
		ch, ok := change(v, s, a)
		if !ok {
			return nil
		}
		return []effect.Change{ch}
	})
}

// effectChanged tells everyone shown the mob of one of its effects turning.
func (t *mobDimTick) effectChanged(fx *mobEffects, in *mobTickInputs, ev entity.MobEvent) {
	h, w := t.h, t.w
	// One of the mob's effects changed; its viewers get the particle metadata.
	if ent, ok := h.book[ev.Mob]; ok {
		tells := mobEffectTells(ent, []effect.Change{ev.EffectChange})
		for _, c := range in.inWorld[w] {
			if _, shown := ent.tracked.get(c); !shown {
				continue
			}
			for _, tell := range tells {
				fx.sends = append(fx.sends, mobSend{c: c, w: w, id: tell.id, data: tell.data})
			}
		}
		riderTells := riderEffectPackets(ent.mob.ID, []effect.Change{ev.EffectChange})
		for _, c := range ent.playersAboard() {
			for _, p := range riderTells {
				fx.sends = append(fx.sends, mobSend{c: c, w: w, id: p.id, data: p.data})
			}
		}
	}
}
