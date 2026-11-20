package server

import (
	"sync"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/command"
	"github.com/golem-mc/golem/internal/server/effect"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/pluginruntime"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/plugin"
	"github.com/golem-mc/golem/registry"
)

// pluginCarried holds the Entity flags a player carries from its file
// without simulating them (Invulnerable, NoCollide), as a plugin last set
// them. They last the session; the save keeps the loaded values.
type pluginCarried struct {
	mu    sync.Mutex
	flags map[string]bool
}

func (p pluginPlayer) flag(key string) bool {
	if !p.IsValid() {
		return false
	}
	return flagOf(p.c, key)
}

// flagOf reads a carried flag without the validity check, so the damage path
// takes no Server.mu. c.saved is assigned once at login (playerdata.go) before
// the client can be hurt and never mutated afterwards.
func flagOf(c *client, key string) bool {
	c.pluginCarried.mu.Lock()
	v, set := c.pluginCarried.flags[key]
	c.pluginCarried.mu.Unlock()
	return v || !set && nbt.BoolField(c.saved, key)
}

func (p pluginPlayer) setFlag(key string, v bool) {
	if !p.IsValid() {
		return
	}
	p.c.pluginCarried.mu.Lock()
	if p.c.pluginCarried.flags == nil {
		p.c.pluginCarried.flags = map[string]bool{}
	}
	p.c.pluginCarried.flags[key] = v
	p.c.pluginCarried.mu.Unlock()
}

// carriedInvulnerable is the Invulnerable flag, which refuses every damage
// type that does not bypass invulnerability.
// vanilla: Entity.isInvulnerableToBase
func (c *client) carriedInvulnerable() bool { return flagOf(c, "Invulnerable") }

func (p pluginPlayer) NameVisible() bool { return p.look().NameVisible }
func (p pluginPlayer) SetNameVisible(v bool) {
	p.setLook(func(a *player.Appearance) { a.NameVisible = v })
}
func (p pluginPlayer) Glowing() bool       { return p.look().Glowing }
func (p pluginPlayer) SetGlowing(v bool)   { p.setLook(func(a *player.Appearance) { a.Glowing = v }) }
func (p pluginPlayer) Silent() bool        { return p.look().Silent }
func (p pluginPlayer) SetSilent(v bool)    { p.setLook(func(a *player.Appearance) { a.Silent = v }) }
func (p pluginPlayer) NoGravity() bool     { return p.look().NoGravity }
func (p pluginPlayer) SetNoGravity(v bool) { p.setLook(func(a *player.Appearance) { a.NoGravity = v }) }

func (p pluginPlayer) look() player.Appearance {
	if !p.IsValid() {
		return player.Appearance{}
	}
	return p.c.player.Appearance()
}

// setLook changes one carried entity field and tells everyone who sees the
// player, the player included.
func (p pluginPlayer) setLook(change func(*player.Appearance)) {
	if !p.IsValid() {
		return
	}
	look := p.c.player.Appearance()
	was := look
	change(&look)
	if look == was {
		return
	}
	p.c.player.SetAppearance(look)
	p.c.srv.broadcastWorld(p.c.theWorld(), protocol.IDPlayEntityMetadata, playerMetadata(p.c.player, true))
}
func (p pluginPlayer) Invulnerable() bool     { return p.flag("Invulnerable") }
func (p pluginPlayer) SetInvulnerable(v bool) { p.setFlag("Invulnerable", v) }
func (p pluginPlayer) Collidable() bool       { return p.IsValid() && !p.flag("NoCollide") }
func (p pluginPlayer) SetCollidable(v bool)   { p.setFlag("NoCollide", !v) }

func (p pluginPlayer) CustomName() *plugin.Text {
	tag := customName{wire: p.look().Name}.tag()
	if tag == nil {
		return nil
	}
	t, ok := pluginTextOf(tag)
	if !ok {
		return nil
	}
	return &t
}

func (p pluginPlayer) SetCustomName(t *plugin.Text) {
	wire := ""
	if t != nil {
		if data, err := nbt.EncodeNetwork(protocol.TextTag(pluginProtocolText(*t))); err == nil {
			wire = string(data)
		}
	}
	p.setLook(func(a *player.Appearance) { a.Name = wire })
}

// entity is the UUID-bound handle whose methods read and drive the live body
// through the command layer, which serves a player as it serves a mob.
func (p pluginPlayer) entity() pluginEntity {
	return pluginEntity{host: p.hostOrAdapter(), id: p.c.profile.UUID}
}

func (p pluginPlayer) Data() plugin.Data               { return p.entity().Data() }
func (p pluginPlayer) SpawnReason() plugin.SpawnReason { return plugin.SpawnDefault }
func (p pluginPlayer) Origin() (plugin.Location, bool) { return plugin.Location{}, false }

func (p pluginPlayer) Vehicle() (plugin.Entity, bool) {
	if !p.IsValid() {
		return nil, false
	}
	return p.entity().Vehicle()
}
func (p pluginPlayer) Passengers() []plugin.Entity {
	if !p.IsValid() {
		return nil
	}
	return p.entity().Passengers()
}
func (p pluginPlayer) AddPassenger(o plugin.Entity) bool {
	return p.IsValid() && p.entity().AddPassenger(o)
}
func (p pluginPlayer) RemovePassenger(o plugin.Entity) bool {
	return p.IsValid() && p.entity().RemovePassenger(o)
}
func (p pluginPlayer) Dismount() {
	if p.IsValid() {
		p.entity().Dismount()
	}
}
func (p pluginPlayer) EyePos() plugin.Vec3 {
	if !p.IsValid() {
		return plugin.Vec3{}
	}
	return p.entity().EyePos()
}
func (p pluginPlayer) HasLineOfSight(o plugin.Entity) bool {
	return p.IsValid() && p.entity().HasLineOfSight(o)
}
func (p pluginPlayer) RayTrace(max float64, fluids bool) (plugin.RayHit, bool) {
	if !p.IsValid() {
		return plugin.RayHit{}, false
	}
	return p.entity().RayTrace(max, fluids)
}
func (p pluginPlayer) LaunchProjectile(t registry.EntityType, v plugin.Vec3) (plugin.Projectile, error) {
	return p.entity().LaunchProjectile(t, v)
}
func (p pluginPlayer) Swing(h plugin.Hand) {
	if p.IsValid() {
		p.c.Swing(h == plugin.OffHand, registry.DefaultSwingAnimation)
	}
}
func (p pluginPlayer) OnGround() bool { return p.IsValid() && p.c.player.Position().OnGround }

func (p pluginPlayer) Velocity() plugin.Vec3 {
	if !p.IsValid() {
		return plugin.Vec3{}
	}
	v := p.c.player.Velocity()
	return plugin.Vec3{X: v.X, Y: v.Y, Z: v.Z}
}

// SetVelocity replaces the delta movement and sends it to the client.
func (p pluginPlayer) SetVelocity(v plugin.Vec3) {
	if p.IsValid() {
		p.c.setVelocity(p.c.theWorld(), v.X, v.Y, v.Z)
	}
}

// vitalsField reads one field of the vitals, zero for a handle that is not valid.
func vitalsField[T any](p pluginPlayer, read func(*player.Vitals) T) (out T) {
	if v := p.vitals(); p.IsValid() {
		out = read(&v)
	}
	return out
}

func (p pluginPlayer) FireTicks() int {
	return max(vitalsField(p, func(v *player.Vitals) int { return v.FireTicks }), 0)
}

// SetFireTicks sets the burn; zero or less puts it out.
// vanilla: Entity.setRemainingFireTicks
func (p pluginPlayer) SetFireTicks(n int) {
	if p.IsValid() {
		p.c.player.UpdateVitals(func(v *player.Vitals) { v.FireTicks = max(n, 0) })
	}
}
func (p pluginPlayer) FrozenTicks() int {
	return vitalsField(p, func(v *player.Vitals) int { return v.TicksFrozen })
}
func (p pluginPlayer) SetFrozenTicks(n int) {
	if p.IsValid() {
		p.c.player.UpdateVitals(func(v *player.Vitals) { v.TicksFrozen = max(n, 0) })
	}
}
func (p pluginPlayer) Air() int {
	return vitalsField(p, func(v *player.Vitals) int { return v.AirSupply })
}
func (p pluginPlayer) SetAir(n int) {
	if p.IsValid() {
		p.c.player.UpdateVitals(func(v *player.Vitals) { v.AirSupply = int(clampInt16(n)) })
	}
}
func (p pluginPlayer) NoDamageTicks() int {
	return vitalsField(p, func(v *player.Vitals) int { return v.InvulnerableTicks })
}
func (p pluginPlayer) SetNoDamageTicks(n int) {
	if p.IsValid() {
		p.c.player.UpdateVitals(func(v *player.Vitals) { v.InvulnerableTicks = max(n, 0) })
	}
}

func (p pluginPlayer) Health() float32 {
	return float32(vitalsField(p, func(v *player.Vitals) float64 { return v.Health }))
}
func (p pluginPlayer) Absorption() float32 {
	return float32(vitalsField(p, func(v *player.Vitals) float64 { return v.Absorption }))
}
func (p pluginPlayer) SetAbsorption(h float32) {
	p.setVitals(func(v *player.Vitals) { v.Absorption = float64(max(h, 0)) })
}
func (p pluginPlayer) Attribute(id registry.AttributeID) (plugin.AttributeInstance, bool) {
	if !p.IsValid() {
		return nil, false
	}
	return attributeOf(func() (command.Entity, bool) { return p.c, p.IsValid() }, id)
}
func (p pluginPlayer) MaxHealth() float32 {
	if a, ok := p.Attribute(registry.AttributeMaxHealth); ok {
		return float32(a.Value())
	}
	return 0
}

// SetHealth clamps to max health; zero kills as /kill does.
// vanilla: LivingEntity.setHealth
func (p pluginPlayer) SetHealth(h float32) {
	if !p.IsValid() || p.vitals().Dead {
		return
	}
	if h <= 0 {
		p.c.kill(&damageCause{posOnly: true, noEvent: true})
		return
	}
	health := float64(min(h, p.MaxHealth()))
	p.setVitals(func(v *player.Vitals) { v.Health = health })
}

// Heal adds health after EntityRegainHealthEvent.
// vanilla: LivingEntity.heal
func (p pluginPlayer) Heal(amount float32, reason plugin.RegainReason) {
	if !p.IsValid() || amount <= 0 || p.vitals().Dead {
		return
	}
	if r := p.c.srv.plugins; r != nil && r.Has(pluginruntime.EntityRegainHealth) {
		e := &plugin.EntityRegainHealthEvent{Entity: p, Amount: amount, Reason: reason}
		r.Fire(e)
		if e.Cancelled() {
			return
		}
		amount = e.Amount
	}
	p.SetHealth(p.Health() + max(amount, 0))
}

func (p pluginPlayer) Damage(amount float32, src plugin.DamageSource) bool {
	return p.IsValid() && p.c.Damage(amount, src.Type, p.hostOrAdapter().damageOrigin(src))
}
func (p pluginPlayer) Kill() {
	if p.IsValid() {
		p.c.Kill()
	}
}

// Killer is the player credited with the last hurt, while that credit lasts.
// vanilla: LivingEntity.getLastHurtByPlayer
func (p pluginPlayer) Killer() (plugin.Player, bool) {
	v := p.vitals()
	if !p.IsValid() || v.LastHurtByPlayer == [16]byte{} || v.AgeTicks-v.LastHurtByPlayerAge >= 100 {
		return nil, false
	}
	if c := p.c.srv.clientByUUID(v.LastHurtByPlayer); c != nil {
		if k := (pluginPlayer{c: c}); k.IsValid() {
			return k, true
		}
	}
	return nil, false
}

func (p pluginPlayer) Effects() []plugin.EffectInstance {
	if !p.IsValid() {
		return nil
	}
	var out []plugin.EffectInstance
	for _, in := range p.c.player.Effects() {
		out = append(out, pluginEffectInstance(in))
	}
	return out
}

func pluginEffectInstance(in effect.Instance) plugin.EffectInstance {
	return plugin.EffectInstance{Effect: plugin.Effect(in.ID), Duration: int(in.Duration), Amplifier: int(in.Amplifier),
		Ambient: in.Ambient, Particles: in.Visible, Icon: in.ShowIcon}
}

func (p pluginPlayer) Effect(e plugin.Effect) (plugin.EffectInstance, bool) {
	for _, in := range p.Effects() {
		if in.Effect == e {
			return in, true
		}
	}
	return plugin.EffectInstance{}, false
}
func (p pluginPlayer) AddEffect(in plugin.EffectInstance) bool {
	return p.IsValid() && in.Amplifier >= 0 && in.Amplifier <= 255 && p.c.AddEffect(effectInstanceOf(in))
}
func (p pluginPlayer) RemoveEffect(e plugin.Effect) bool {
	return p.IsValid() && p.c.RemoveEffect(int32(e))
}
func (p pluginPlayer) ClearEffects() {
	if p.IsValid() {
		p.c.ClearEffects()
	}
}

func (p pluginPlayer) Gliding() bool { return p.IsValid() && p.c.player.Appearance().FallFlying }

// SetGliding does nothing: the client starts and ends a glide with its own
// command, and the server follows it.
// vanilla: ServerGamePacketListenerImpl.handlePlayerCommand
func (p pluginPlayer) SetGliding(bool) {}

func (p pluginPlayer) Sleeping() bool { return p.IsValid() && p.c.player.Sleep().Lying }

var _ plugin.Player = pluginPlayer{}

var (
	_ plugin.Server = (*pluginHost)(nil)
	_ plugin.World  = pluginWorld{}
	_ plugin.Chunk  = pluginChunk{}
)
