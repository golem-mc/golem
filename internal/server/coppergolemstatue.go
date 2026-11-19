package server

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/plugin"
	"github.com/golem-mc/golem/registry"
)

// copperStatue is an oxidized copper golem turning into its statue block,
// with the golem's name and where it stood.
type copperStatue struct {
	w       *world.World
	x, y, z int32
	state   uint16
	name    nbt.Tag
	at      [3]float64
}

// placeCopperStatue puts the statue block in the golem's cell and keeps the
// golem's name in its block entity; the body is already gone.
// vanilla: CopperGolem.turnToStatue, CopperGolemStatueBlockEntity.createStatue
func (s *Server) placeCopperStatue(st copperStatue) {
	s.setBlockAndTell(st.w, st.x, st.y, st.z, st.state)
	if be := st.w.EnsureBlockEntity(st.x, st.y, st.z, world.KindCopperGolemStatue); be != nil {
		if text, ok := protocol.StyledTextOf(st.name); ok && st.name != nil {
			be.CustomName = &text
		}
	}
	s.broadcastSoundID(st.w, registry.SoundEntityCopperGolemBecomeStatue, protocol.SoundCategoryNeutral, st.at[0], st.at[1], st.at[2], 1, 1)
}

// useCopperGolemStatue applies the statue block's click that
// rules.CopperStatueClickOf decides.
func (c *client) useCopperGolemStatue(hand, x, y, z int32) bool {
	w := c.theWorld()
	state := w.BlockAt(x, y, z)
	click, next := rules.CopperStatueClickOf(state, registry.Item(c.heldItem(hand).ItemID))
	switch click {
	case rules.CopperStatueRevive:
		c.statueToGolem(w, x, y, z, state)
		c.wearHeld(hand, 1)
	case rules.CopperStatuePose:
		s := c.srv
		s.broadcastSoundID(w, registry.SoundEntityCopperGolemBecomeStatue, protocol.SoundCategoryBlock, float64(x)+0.5, float64(y)+0.5, float64(z)+0.5, 1, 1)
		c.setUsedBlock(w, x, y, z, next)
		s.gameEvent(w, rules.GameEventBlockChange, float64(x)+0.5, float64(y)+0.5, float64(z)+0.5, c.gameEventContext(next))
	default:
		return false
	}
	return true
}

// statueToGolem replaces the statue with a golem facing its way, keeping its
// name.
// vanilla: CopperGolemStatueBlockEntity.removeStatue, initCopperGolem
func (c *client) statueToGolem(w *world.World, x, y, z int32, state uint16) {
	s := c.srv
	var name nbt.Tag
	if be := w.BlockEntity(x, y, z); be != nil && be.CustomName != nil {
		name = protocol.TextTag(*be.CustomName)
	}
	yaw := map[string]float32{"south": 0, "west": 90, "north": 180, "east": 270}[registry.StateValue(state, registry.PropFacing)]
	gx, gy, gz := float64(x)+0.5, float64(y), float64(z)+0.5
	ent := s.standMobArriving(s.dimensionOfWorld(w), entity.TypeCopperGolem, gx, gy, gz, 0, nil, false, "", false, func(ent *mobEntity) {
		ent.name = name
		ent.mob.Yaw, ent.mob.HeadYaw = yaw, yaw
	})
	s.broadcastEntitySoundID(w, nil, ent.mob.ID, registry.SoundEntityCopperGolemSpawn, protocol.SoundCategoryNeutral, gx, gy, gz, 1, 1)
	c.setUsedBlock(w, x, y, z, airBlockState)
}

// turnedToStatue removes the body like a despawn; the block goes in once the
// locks are released.
// vanilla: CopperGolem.turnToStatue
func (t *mobDimTick) turnedToStatue(fx *mobEffects, ev entity.MobEvent) {
	if ent, ok := t.h.book[ev.Mob]; ok {
		t.h.forget(ev.Mob)
		fx.scoreDrops = append(fx.scoreDrops, goneMob{ent.uuid, plugin.RemoveTransform})
		fx.release(ent)
		fx.remove(t.w, ev.Mob.ID)
		fx.statues = append(fx.statues, copperStatue{w: t.w, x: int32(ev.X), y: int32(ev.Y), z: int32(ev.Z), state: ev.BlockState, name: ent.name, at: [3]float64{ev.Mob.X, ev.Mob.Y, ev.Mob.Z}})
	}
}
