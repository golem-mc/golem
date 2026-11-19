package server

import (
	"maps"

	"github.com/golem-mc/golem/internal/server/command"
	"github.com/golem-mc/golem/internal/server/effect"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/plugin"
	"github.com/golem-mc/golem/registry"
)

// saved reads the body's saved compound, the form /data shows; nil for a
// stale handle or a kind with no save form.
func (e pluginEntity) saved() nbt.Compound {
	b, ok := e.live()
	if !ok {
		return nil
	}
	t, ok := b.(command.DataTarget)
	if !ok {
		return nil
	}
	tag, ok := t.DataTag()
	if !ok {
		return nil
	}
	return tag.Entries()
}

// merge loads fields over the saved compound onto the live body as /data
// merge does: a load resets every field its compound lacks.
// vanilla: DataCommands.MERGE
func (e pluginEntity) merge(fields nbt.Compound) bool {
	b, ok := e.live()
	if !ok {
		return false
	}
	w, ok := b.(command.DataWriter)
	if !ok {
		return false
	}
	merged := maps.Clone(e.saved())
	if merged == nil {
		return false
	}
	maps.Copy(merged, fields)
	return w.SetDataTag(merged)
}

func (e pluginEntity) flag(key string) bool { return nbt.BoolField(e.saved(), key) }
func (e pluginEntity) setFlag(key string, v bool) {
	e.merge(nbt.Compound{key: nbt.Byte(boolByte(v))})
}

func (e pluginEntity) Velocity() plugin.Vec3 {
	list, _ := nbt.Get[nbt.List](e.saved(), "Motion")
	var v [3]float64
	for i := 0; i < len(list.Items) && i < 3; i++ {
		if d, ok := list.Items[i].(nbt.Double); ok {
			v[i] = float64(d)
		}
	}
	return plugin.Vec3{X: v[0], Y: v[1], Z: v[2]}
}
func (e pluginEntity) SetVelocity(v plugin.Vec3) {
	e.merge(nbt.Compound{"Motion": nbt.NewList([]nbt.Tag{nbt.Double(v.X), nbt.Double(v.Y), nbt.Double(v.Z)})})
}
func (e pluginEntity) OnGround() bool {
	b, ok := e.live()
	return ok && b.Position().OnGround
}
func (e pluginEntity) NameVisible() bool      { return e.flag("CustomNameVisible") }
func (e pluginEntity) SetNameVisible(v bool)  { e.setFlag("CustomNameVisible", v) }
func (e pluginEntity) Glowing() bool          { return e.flag("Glowing") }
func (e pluginEntity) SetGlowing(v bool)      { e.setFlag("Glowing", v) }
func (e pluginEntity) Silent() bool           { return e.flag("Silent") }
func (e pluginEntity) SetSilent(v bool)       { e.setFlag("Silent", v) }
func (e pluginEntity) NoGravity() bool        { return e.flag("NoGravity") }
func (e pluginEntity) SetNoGravity(v bool)    { e.setFlag("NoGravity", v) }
func (e pluginEntity) Invulnerable() bool     { return e.flag("Invulnerable") }
func (e pluginEntity) SetInvulnerable(v bool) { e.setFlag("Invulnerable", v) }
func (e pluginEntity) FireTicks() int         { n, _ := nbt.ShortField(e.saved(), "Fire"); return int(n) }
func (e pluginEntity) SetFireTicks(n int)     { e.merge(nbt.Compound{"Fire": nbt.Short(clampInt16(n))}) }
func (e pluginEntity) FrozenTicks() int {
	n, _ := nbt.IntField(e.saved(), "TicksFrozen")
	return int(n)
}
func (e pluginEntity) SetFrozenTicks(n int) {
	e.merge(nbt.Compound{"TicksFrozen": nbt.Int(int32(max(n, 0)))})
}
func (e pluginEntity) Air() int        { n, _ := nbt.ShortField(e.saved(), "Air"); return int(n) }
func (e pluginEntity) SetAir(n int)    { e.merge(nbt.Compound{"Air": nbt.Short(clampInt16(n))}) }
func (e pluginEntity) Health() float32 { h, _ := nbt.FloatField(e.saved(), "Health"); return h }
func (e pluginEntity) Absorption() float32 {
	h, _ := nbt.FloatField(e.saved(), "AbsorptionAmount")
	return h
}
func (e pluginEntity) SetAbsorption(h float32) {
	e.merge(nbt.Compound{"AbsorptionAmount": nbt.Float(max(h, 0))})
}
func (e pluginEntity) EyePos() plugin.Vec3 {
	if b, ok := e.live(); ok {
		p := eyePositionOf(b)
		return plugin.Vec3{X: p.X, Y: p.Y, Z: p.Z}
	}
	return plugin.Vec3{}
}

func clampInt16(n int) int16 { return int16(min(max(n, -1<<15), 1<<15-1)) }

// SetHealth clamps to the max_health attribute as LivingEntity.setHealth does.
// vanilla: LivingEntity.setHealth
func (e pluginEntity) SetHealth(h float32) {
	e.merge(nbt.Compound{"Health": nbt.Float(min(max(h, 0), e.MaxHealth()))})
}
func (e pluginEntity) MaxHealth() float32 {
	if a, ok := e.Attribute(registry.AttributeMaxHealth); ok {
		return float32(a.Value())
	}
	return 0
}
func (e pluginEntity) Heal(amount float32, _ plugin.RegainReason) {
	if amount > 0 {
		e.SetHealth(e.Health() + amount)
	}
}
func (e pluginEntity) Kill() {
	if b, ok := e.live(); ok {
		b.Kill()
	}
}
func (e pluginEntity) Swing(h plugin.Hand) {
	if b, ok := e.live(); ok {
		b.Swing(h == plugin.OffHand, registry.DefaultSwingAnimation)
	}
}

// Damage hurts through the body's own hurt path, as /damage does.
// vanilla: LivingEntity.hurtServer
func (e pluginEntity) Damage(amount float32, src plugin.DamageSource) bool {
	b, ok := e.live()
	t, hurts := b.(command.DamageTarget)
	if !ok || !hurts {
		return false
	}
	return t.Damage(amount, src.Type, e.host.damageOrigin(src))
}

func (h *pluginHost) damageOrigin(src plugin.DamageSource) (origin command.DamageOrigin) {
	if src.Direct != nil {
		origin.Direct, _ = h.entityOfHandle(src.Direct)
	}
	if src.Causing != nil {
		origin.Cause, _ = h.entityOfHandle(src.Causing)
	}
	if src.Pos != nil {
		origin.At = &player.Position{X: src.Pos.X, Y: src.Pos.Y, Z: src.Pos.Z}
	}
	return origin
}

var equipmentSlots = [...]command.ItemSlot{
	plugin.SlotMainHand: command.ItemSlotMainHand, plugin.SlotOffHand: command.ItemSlotOffHand,
	plugin.SlotFeet: command.ItemSlotFeet, plugin.SlotLegs: command.ItemSlotLegs,
	plugin.SlotChest: command.ItemSlotChest, plugin.SlotHead: command.ItemSlotHead,
	plugin.SlotBody: command.ItemSlotBody, plugin.SlotSaddle: command.ItemSlotSaddle,
}

// pluginEquipment reads and writes a body's slots and drop chances.
type pluginEquipment struct{ e pluginEntity }

func (e pluginEntity) Equipment() plugin.Equipment { return pluginEquipment{e: e} }
func (q pluginEquipment) Get(s plugin.EquipmentSlot) plugin.ItemStack {
	if b, ok := q.e.live(); ok && int(s) < len(equipmentSlots) {
		if st, ok := b.ItemIn(equipmentSlots[s]); ok {
			return pluginStack(st)
		}
	}
	return plugin.ItemStack{}
}
func (q pluginEquipment) Set(s plugin.EquipmentSlot, item plugin.ItemStack) {
	b, ok := q.e.live()
	st, valid := pluginProtocolStack(item)
	if ok && valid && int(s) < len(equipmentSlots) {
		b.SetItemIn(equipmentSlots[s], st)
	}
}

// Effects are read from the saved compound and written through the /effect path.
func (e pluginEntity) Effects() []plugin.EffectInstance {
	list, _ := nbt.Get[nbt.List](e.saved(), "active_effects")
	var out []plugin.EffectInstance
	for _, item := range list.Items {
		c, ok := item.(nbt.CompoundTag)
		if !ok {
			continue
		}
		if in, ok := pluginEffectOf(c.Entries()); ok {
			out = append(out, in)
		}
	}
	return out
}
func pluginEffectOf(c nbt.Compound) (plugin.EffectInstance, bool) {
	name, _ := c["id"].(nbt.String)
	id, ok := registry.MobEffectID(string(name))
	if !ok {
		return plugin.EffectInstance{}, false
	}
	amp, _ := nbt.IntField(c, "amplifier")
	dur, _ := nbt.IntField(c, "duration")
	particles, icon := true, true
	if _, set := c["show_particles"]; set {
		particles = nbt.BoolField(c, "show_particles")
	}
	if _, set := c["show_icon"]; set {
		icon = nbt.BoolField(c, "show_icon")
	}
	return plugin.EffectInstance{Effect: plugin.Effect(id), Duration: int(dur), Amplifier: int(amp),
		Ambient: nbt.BoolField(c, "ambient"), Particles: particles, Icon: icon}, true
}
func (e pluginEntity) Effect(id plugin.Effect) (plugin.EffectInstance, bool) {
	for _, in := range e.Effects() {
		if in.Effect == id {
			return in, true
		}
	}
	return plugin.EffectInstance{}, false
}
func (e pluginEntity) effectTarget() (command.EffectTarget, bool) {
	b, ok := e.live()
	if !ok {
		return nil, false
	}
	t, ok := b.(command.EffectTarget)
	return t, ok
}
func (e pluginEntity) AddEffect(in plugin.EffectInstance) bool {
	t, ok := e.effectTarget()
	if !ok || in.Amplifier < 0 || in.Amplifier > 255 {
		return false
	}
	setEffectCause(t, effect.CausePlugin)
	return t.AddEffect(effectInstanceOf(in))
}
func effectInstanceOf(in plugin.EffectInstance) effect.Instance {
	return effect.Instance{ID: int32(in.Effect), Amplifier: int32(in.Amplifier), Duration: int32(in.Duration),
		Ambient: in.Ambient, Visible: in.Particles, ShowIcon: in.Icon}
}
func (e pluginEntity) RemoveEffect(id plugin.Effect) bool {
	t, ok := e.effectTarget()
	if ok {
		setEffectCause(t, effect.CausePlugin)
	}
	return ok && t.RemoveEffect(int32(id))
}
func (e pluginEntity) ClearEffects() {
	if t, ok := e.effectTarget(); ok {
		setEffectCause(t, effect.CausePlugin)
		t.ClearEffects()
	}
}

// pluginAttribute reads and edits one attribute under the body's lock on every call.
type pluginAttribute struct {
	live func() (command.Entity, bool)
	id   registry.AttributeID
}

func (e pluginEntity) Attribute(id registry.AttributeID) (plugin.AttributeInstance, bool) {
	return attributeOf(e.live, id)
}

// attributeOf is the handle to one of the live body's attributes, false when
// the body has none.
func attributeOf(live func() (command.Entity, bool), id registry.AttributeID) (plugin.AttributeInstance, bool) {
	b, ok := live()
	if !ok {
		return nil, false
	}
	t, ok := b.(command.AttributeTarget)
	if !ok || !t.HasAttributes() {
		return nil, false
	}
	has := false
	t.WithAttributes(func(a *player.Attributes) { has = a.Has(id) })
	if !has {
		return nil, false
	}
	return pluginAttribute{live: live, id: id}, true
}
func (a pluginAttribute) with(fn func(*player.AttributeInstance)) {
	if b, ok := a.live(); ok {
		if t, ok := b.(command.AttributeTarget); ok {
			t.WithAttributes(func(attrs *player.Attributes) { fn(attrs.Instance(a.id)) })
		}
	}
}
func (a pluginAttribute) Base() (v float64) {
	a.with(func(in *player.AttributeInstance) { v = in.Base })
	return
}
func (a pluginAttribute) Value() (v float64) {
	a.with(func(in *player.AttributeInstance) { v = in.Value() })
	return
}
func (a pluginAttribute) SetBase(v float64) {
	a.with(func(in *player.AttributeInstance) { in.Base = a.id.Attribute().Sanitize(v) })
}
func (a pluginAttribute) Modifiers() (out []plugin.AttributeModifier) {
	a.with(func(in *player.AttributeInstance) {
		for _, m := range in.Modifiers {
			k, err := plugin.ParseKey(m.ID)
			if err == nil {
				out = append(out, plugin.AttributeModifier{ID: k, Amount: m.Amount, Operation: m.Operation})
			}
		}
	})
	return out
}
func (a pluginAttribute) AddModifier(m plugin.AttributeModifier) {
	if b, ok := a.live(); ok {
		if t, ok := b.(command.AttributeTarget); ok {
			t.WithAttributes(func(attrs *player.Attributes) {
				attrs.AddModifier(a.id, player.Modifier{ID: m.ID.String(), Amount: m.Amount, Operation: m.Operation, Persistent: true})
			})
		}
	}
}
func (a pluginAttribute) RemoveModifier(id plugin.Key) {
	if b, ok := a.live(); ok {
		if t, ok := b.(command.AttributeTarget); ok {
			t.WithAttributes(func(attrs *player.Attributes) { attrs.RemoveModifier(a.id, id.String()) })
		}
	}
}

func boolByte(v bool) int8 {
	if v {
		return 1
	}
	return 0
}

// setEffectCause names what the target's next effect change is.
func setEffectCause(t any, c effect.Cause) {
	if t, ok := t.(interface{ SetEffectCause(effect.Cause) }); ok {
		t.SetEffectCause(c)
	}
}
