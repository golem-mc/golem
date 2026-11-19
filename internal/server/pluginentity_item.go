package server

import (
	"math"

	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/plugin"
	"github.com/golem-mc/golem/registry"
)

func (e pluginItemEntity) Stack() plugin.ItemStack {
	c, ok := nbt.Get[nbt.CompoundTag](e.saved(), "Item")
	if !ok {
		return plugin.ItemStack{}
	}
	st, _ := registry.StoredStack(c)
	return pluginStack(st)
}
func (e pluginItemEntity) SetStack(s plugin.ItemStack) {
	if st, ok := pluginProtocolStack(s); ok && !s.Empty() {
		e.merge(nbt.Compound{"Item": registry.StackTag(st)})
	}
}
func (e pluginItemEntity) PickupDelay() int {
	n, _ := nbt.ShortField(e.saved(), "PickupDelay")
	return int(n)
}
func (e pluginItemEntity) SetPickupDelay(n int) {
	e.merge(nbt.Compound{"PickupDelay": nbt.Short(clampInt16(n))})
}
func (e pluginItemEntity) Age() int { n, _ := nbt.ShortField(e.saved(), "Age"); return int(n) }
func (e pluginItemEntity) SetAge(n int) {
	e.merge(nbt.Compound{"Age": nbt.Short(clampInt16(n))})
}
func (e pluginItemEntity) Owner() (plugin.UUID, bool)   { return e.uuidField("Owner") }
func (e pluginItemEntity) SetOwner(id plugin.UUID)      { e.setUUID("Owner", id) }
func (e pluginItemEntity) Thrower() (plugin.UUID, bool) { return e.uuidField("Thrower") }

// DropItem drops the stack as a broken block's drop does when naturally is
// set: each axis offset up to a quarter block and the entity's half height
// below the point, with the item's own random motion; otherwise it appears
// at rest where asked.
// vanilla: Block.popResource, ItemEntity.<init>
func (w pluginWorld) DropItem(pos plugin.Vec3, s plugin.ItemStack, naturally bool) plugin.ItemEntity {
	st, ok := pluginProtocolStack(s)
	if !ok || s.Empty() || math.IsNaN(pos.X+pos.Y+pos.Z) || math.IsInf(pos.X+pos.Y+pos.Z, 0) {
		return nil
	}
	ws := worldScope{srv: w.srv, in: func() *dimension { return w.dim }, quiet: true}
	tag := nbt.Compound{}
	if naturally {
		rng := w.srv.levelRng
		pos = plugin.Vec3{X: pos.X + (rng.Float64()-.5)/2, Y: pos.Y + (rng.Float64()-.5)/2 - .125, Z: pos.Z + (rng.Float64()-.5)/2}
		tag["Motion"] = nbt.NewList([]nbt.Tag{nbt.Double(rng.Float64()*.2 - .1), nbt.Double(.2), nbt.Double(rng.Float64()*.2 - .1)})
	}
	_, id, err := ws.SummonItem(pos.X, pos.Y, pos.Z, st, 10, false, tag)
	if err != nil {
		return nil
	}
	return pluginItemEntity{host: w.host, id: id}
}
