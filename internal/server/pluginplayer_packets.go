package server

import (
	"slices"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/command"
	"github.com/golem-mc/golem/internal/server/resourcepack"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/plugin"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
)

func (p pluginPlayer) Kick(reason plugin.Text) {
	if p.IsValid() {
		p.c.Disconnect(pluginProtocolText(reason))
	}
}
func (p pluginPlayer) Transfer(host string, port int) {
	if p.IsValid() {
		p.c.Transfer(host, int32(port))
	}
}
func (p pluginPlayer) SendActionBar(msg plugin.Text) {
	if p.IsValid() {
		p.c.ShowTitle(command.TitleActionBar, pluginProtocolText(msg))
	}
}
func (p pluginPlayer) SendTitle(title, subtitle plugin.Text, fadeIn, stay, fadeOut int) {
	if !p.IsValid() {
		return
	}
	if fadeIn >= 0 || stay >= 0 || fadeOut >= 0 {
		p.c.SetTitleTimes(int32(fadeIn), int32(stay), int32(fadeOut))
	}
	p.c.ShowTitle(command.TitleSubtitle, pluginProtocolText(subtitle))
	p.c.ShowTitle(command.TitleMain, pluginProtocolText(title))
}
func (p pluginPlayer) ClearTitle() {
	if p.IsValid() {
		p.c.ClearTitles(false)
	}
}
func (p pluginPlayer) ShowDialog(key plugin.Key) {
	if !p.IsValid() {
		return
	}
	if ref, ok := command.DialogOf(nbt.String(key.String())); ok {
		p.c.ShowDialog(ref)
	}
}
func (p pluginPlayer) PlaySound(pos plugin.Vec3, s plugin.Sound, cat plugin.SoundCategory, volume, pitch float32) {
	if p.IsValid() && s != 0 {
		p.c.send(protocol.IDPlaySoundEffect, soundEffectIDAt(s, int32(cat), pos.X, pos.Y, pos.Z, volume, pitch))
	}
}
func (p pluginPlayer) StopSound(s plugin.Sound, cat plugin.SoundCategory) {
	if p.IsValid() {
		p.c.StopSound(int32(cat), s.Name())
	}
}
func (p pluginPlayer) SendBlockChange(pos plugin.BlockPos, s plugin.BlockState) {
	if p.IsValid() {
		p.c.send(protocol.IDPlayBlockUpdate, blockUpdate(int32(pos.X), int32(pos.Y), int32(pos.Z), uint16(s)))
	}
}
func (p pluginPlayer) SendPluginMessage(channel plugin.Key, data []byte) {
	if p.IsValid() {
		p.sendPluginMessage(channel, data)
	}
}
func (p pluginPlayer) sendPluginMessage(channel plugin.Key, data []byte) {
	if len(data) > 1048576 { // vanilla: ClientboundCustomPayloadPacket.MAX_PAYLOAD_SIZE
		return
	}
	var b protocol.Buffer
	b.PutString(channel.String())
	b.PutRaw(data)
	p.c.sendPluginCommon(protocol.IDConfigPluginMessage, protocol.IDPlayPluginMessage, b.Bytes())
}
func (p pluginPlayer) SendResourcePack(id plugin.UUID, url, sha1 string, required bool, prompt *plugin.Text) {
	if p.IsValid() {
		p.sendResourcePack(id, url, sha1, required, prompt)
	}
}
func (p pluginPlayer) sendResourcePack(id plugin.UUID, url, sha1 string, required bool, prompt *plugin.Text) {
	pack := resourcepack.Pack{ID: uuid.UUID(id), URL: url, Hash: sha1, Required: required}
	if pack.HashTooLong() {
		return
	}
	if prompt != nil {
		var text protocol.Buffer
		text.PutText(pluginProtocolText(*prompt))
		pack.Prompt = text.Bytes()
	}
	p.c.pluginConnectionMu.Lock()
	defer p.c.pluginConnectionMu.Unlock()
	if protocol.State(p.c.pluginConnectionPhase.Load()) == protocol.StateConfiguration {
		p.c.configurationPacks.Offer(pack.ID)
	}
	p.c.pluginMu.Lock()
	if p.c.pluginPacks == nil {
		p.c.pluginPacks = make(map[uuid.UUID]bool)
	}
	p.c.pluginPacks[uuid.UUID(id)] = required
	p.c.pluginMu.Unlock()
	var b protocol.Buffer
	pack.Put(&b)
	p.c.sendPluginCommonLocked(protocol.IDConfigResourcePackPush, protocol.IDPlayResourcePackPush, b.Bytes())
}
func (p pluginPlayer) RemoveResourcePack(id plugin.UUID) {
	if p.IsValid() {
		p.removeResourcePack(id)
	}
}
func (p pluginPlayer) removeResourcePack(id plugin.UUID) {
	p.c.pluginConnectionMu.Lock()
	defer p.c.pluginConnectionMu.Unlock()
	p.c.pluginMu.Lock()
	delete(p.c.pluginPacks, uuid.UUID(id))
	p.c.pluginMu.Unlock()
	var b protocol.Buffer
	b.PutBool(true)
	b.PutUUID(uuid.UUID(id))
	p.c.sendPluginCommonLocked(protocol.IDConfigResourcePackPop, protocol.IDPlayResourcePackPop, b.Bytes())
	if err := p.c.resolveConfigurationPackLocked(uuid.UUID(id)); err != nil {
		p.c.conn.Close()
	}
}
func (p pluginPlayer) StoreCookie(key plugin.Key, data []byte) {
	if p.IsValid() {
		p.storeCookie(key, data)
	}
}
func (p pluginPlayer) storeCookie(key plugin.Key, data []byte) {
	if len(data) > 5120 { // vanilla: ClientboundStoreCookiePacket.MAX_PAYLOAD_SIZE
		return
	}
	var b protocol.Buffer
	b.PutString(key.String())
	b.PutBytes(data)
	p.c.sendPluginCommon(protocol.IDConfigStoreCookie, protocol.IDPlayStoreCookie, b.Bytes())
}

type pluginCookieRequest struct {
	host *pluginHost
	fn   func([]byte, bool)
}

func (p pluginPlayer) RequestCookie(key plugin.Key, fn func([]byte, bool)) {
	if p.IsValid() {
		p.requestCookie(key, fn)
	}
}
func (p pluginPlayer) requestCookie(key plugin.Key, fn func([]byte, bool)) {
	if fn == nil {
		return
	}
	c := p.c
	c.pluginMu.Lock()
	if c.pluginCookies == nil {
		c.pluginCookies = make(map[string][]pluginCookieRequest)
	}
	requests := c.pluginCookies[key.String()]
	c.pluginCookies[key.String()] = append(requests, pluginCookieRequest{p.hostOrAdapter(), fn})
	c.pluginMu.Unlock()
	// Coalesce outstanding requests: one response completes all callbacks for its key.
	if len(requests) == 0 {
		var b protocol.Buffer
		b.PutString(key.String())
		c.sendPluginCommon(protocol.IDConfigCookieRequest, protocol.IDPlayCookieRequest, b.Bytes())
	}
}
func (c *client) handlePluginCookie(r *protocol.Reader) error {
	if c.srv.plugins == nil {
		return c.rejectUnexpectedQuery("play")
	}
	key := r.ReadString(32767)
	present := r.ReadBool()
	var data []byte
	if present {
		data = r.ReadBytes(5120)
	}
	if r.Err() != nil {
		return c.decodeFailure("cookie_response", r.Err())
	}
	parsed, err := plugin.ParseKey(key)
	if err != nil {
		return c.decodeFailure("cookie_response", err)
	}
	c.pluginMu.Lock()
	requests := c.pluginCookies[parsed.String()]
	delete(c.pluginCookies, parsed.String())
	c.pluginMu.Unlock()
	if len(requests) == 0 {
		return c.rejectUnexpectedQuery("play")
	}
	for _, request := range requests {
		request.host.Scheduler().RunTask(func() {
			if c.connectionPlayer().IsValid() {
				request.fn(slices.Clone(data), present)
			}
		})
	}
	return nil
}

func (p pluginPlayer) SpawnParticles(particle plugin.Particle, pos plugin.Vec3, count int, offset plugin.Vec3, speed float64) {
	if !p.IsValid() {
		return
	}
	if id, data, ok := pluginParticleOptions(particle); ok {
		p.c.SendParticle(id, data, particle.Long, pos.X, pos.Y, pos.Z, float32(offset.X), float32(offset.Y), float32(offset.Z), float32(speed), int32(count))
	}
}

func pluginParticleOptions(particle plugin.Particle) (int32, []byte, bool) {
	id := int32(particle.Type)
	if id < 0 || int(id) >= len(registry.ParticleTypes) {
		return 0, nil, false
	}
	opts := nbt.Compound{"type": nbt.String(registry.ParticleTypes[id]), "color": nbt.Int(int32(particle.Color)), "from_color": nbt.Int(int32(particle.Color)), "to_color": nbt.Int(int32(particle.To)), "scale": nbt.Float(particle.Size)}
	opts["roll"], opts["delay"] = nbt.Float(particle.Roll), nbt.Int(int32(particle.Delay))
	if particle.Power != nil {
		opts["power"] = nbt.Float(*particle.Power)
	}
	opts["target"] = vec3Tag(particle.Target.X, particle.Target.Y, particle.Target.Z)
	opts["duration"] = nbt.Int(int32(particle.Duration))
	opts["destination"] = nbt.Compound{"type": nbt.String("minecraft:block"), "pos": nbt.IntArray{rules.BlockOf(particle.Target.X), rules.BlockOf(particle.Target.Y), rules.BlockOf(particle.Target.Z)}}
	opts["arrival_in_ticks"] = nbt.Int(int32(particle.ArrivalTicks))
	opts["water_blocks"] = nbt.Int(int32(particle.WaterBlocks))
	opts["burst_impulse_base"] = nbt.Float(particle.BurstImpulse)
	// Reuse the shared codec; a block particle must retain its exact state properties.
	var data []byte
	var ok bool
	switch id {
	case protocol.ParticleBlock, protocol.ParticleBlockMarker, protocol.ParticleFallingDust, protocol.ParticleDustPillar, protocol.ParticleBlockCrumble:
		var b protocol.Buffer
		b.PutVarInt(int32(particle.State))
		data = b.Bytes()
		ok = true
	case protocol.ParticleItem:
		if particle.Item != nil {
			opts["item"], _ = pluginValueTag(map[string]any{"id": particle.Item.Item.Name(), "count": particle.Item.Count, "components": pluginItemComponents(particle.Item.Patch)})
		}
		_, data, ok = command.ParticleOptions(opts)
	default:
		_, data, ok = command.ParticleOptions(opts)
	}
	return id, data, ok
}
