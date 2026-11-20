package server

import (
	"errors"
	"maps"
	"math"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/command"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/plugin"
	"github.com/golem-mc/golem/registry"
)

// eyePositionOf adds the eye height the command layer leaves out for bodies
// that are not players: a mob's own, 85% of the box height for the rest.
// vanilla: Entity.getEyeHeight, EntityDimensions.eyeHeight
func eyePositionOf(b command.Entity) player.Position {
	p := b.EyePosition()
	if s, ok := b.(*entitySnapshot); ok {
		if m, isMob := s.ref.(*mobEntity); isMob {
			p.Y += m.mob.EyeHeight()
			return p
		}
	}
	if _, isPlayer := b.(*client); !isPlayer {
		_, y0, _, _, y1, _ := b.Box()
		p.Y += (y1 - y0) * .85
	}
	return p
}

// eyeRay is the segment of length max from the eyes along the look direction.
func (e pluginEntity) eyeRay(max float64) (from, to plugin.Vec3, ok bool) {
	b, live := e.live()
	if !live {
		return from, to, false
	}
	p := eyePositionOf(b)
	d := plugin.Location{Yaw: p.Yaw, Pitch: p.Pitch}.Direction()
	from = plugin.Vec3{X: p.X, Y: p.Y, Z: p.Z}
	return from, plugin.Vec3{X: from.X + d.X*max, Y: from.Y + d.Y*max, Z: from.Z + d.Z*max}, true
}

// RayTrace answers the nearer of the first block outline (and fluid, when
// asked) and the first entity box the eye ray enters; spectators are not hit.
// vanilla: ProjectileUtil.getHitResultOnViewVector, Level.clip
func (e pluginEntity) RayTrace(max float64, fluids bool) (plugin.RayHit, bool) {
	from, to, ok := e.eyeRay(max)
	b, _ := e.live()
	if !ok || max <= 0 || math.IsNaN(max) {
		return plugin.RayHit{}, false
	}
	dim := e.host.srv.dimensionByName(b.DimensionName())
	d := plugin.Vec3{X: to.X - from.X, Y: to.Y - from.Y, Z: to.Z - from.Z}
	var fluid func(uint16) bool
	if fluids {
		fluid = world.HoldsFluidSource
	}
	var out plugin.RayHit
	best, found := 2.0, false
	if hit, ok := dim.w.ClipBlocks(from.X, from.Y, from.Z, d.X, d.Y, d.Z, fluid); ok {
		w := e.host.pluginWorldOf(b)
		out = plugin.RayHit{Pos: plugin.Vec3{X: from.X + d.X*hit.T, Y: from.Y + d.Y*hit.T, Z: from.Z + d.Z*hit.T},
			Block: &plugin.Block{World: w, Pos: plugin.BlockPos{X: int(hit.X), Y: int(hit.Y), Z: int(hit.Z)}}, Face: plugin.BlockFace(hit.Face)}
		best, found = hit.T, true
	}
	box := plugin.Box{Min: plugin.Vec3{X: min(from.X, to.X), Y: min(from.Y, to.Y), Z: min(from.Z, to.Z)},
		Max: plugin.Vec3{X: math.Max(from.X, to.X), Y: math.Max(from.Y, to.Y), Z: math.Max(from.Z, to.Z)}}
	for _, o := range e.host.pluginWorldOf(b).EntitiesIn(box) {
		if o.UUID() == e.UUID() {
			continue
		}
		if p, isPlayer := o.(pluginPlayer); isPlayer && p.GameMode() == plugin.Spectator {
			continue
		}
		bb := o.BoundingBox()
		if t, _, hit := world.SegmentAABB(from.X, from.Y, from.Z, d.X, d.Y, d.Z, bb.Min.X, bb.Min.Y, bb.Min.Z, bb.Max.X, bb.Max.Y, bb.Max.Z); hit && t < best {
			out = plugin.RayHit{Pos: plugin.Vec3{X: from.X + d.X*t, Y: from.Y + d.Y*t, Z: from.Z + d.Z*t}, Entity: o}
			best, found = t, true
		}
	}
	return out, found
}

// HasLineOfSight clears when no block outline lies between the two pairs of
// eyes.
// vanilla: LivingEntity.hasLineOfSight
func (e pluginEntity) HasLineOfSight(other plugin.Entity) bool {
	b, ok := e.live()
	ob, found := e.host.entityOfHandle(other)
	if !ok || !found || b.DimensionName() != ob.DimensionName() {
		return false
	}
	p, q := eyePositionOf(b), eyePositionOf(ob)
	_, blocked := e.host.srv.dimensionByName(b.DimensionName()).w.ClipBlocks(p.X, p.Y, p.Z, q.X-p.X, q.Y-p.Y, q.Z-p.Z, nil)
	return !blocked
}

// LaunchProjectile spawns the projectile at the eyes, owned by the entity and
// moving along its look direction at the kind's launch speed, or v when set.
// vanilla: Projectile.shootFromRotation, ThrowableProjectile
func (e pluginEntity) LaunchProjectile(t registry.EntityType, v plugin.Vec3) (plugin.Projectile, error) {
	return e.launch(t, v, nil)
}

// LaunchTippedArrow launches an arrow whose item is set before the launch
// event fires.
// vanilla: Projectile.spawnProjectile (the item is set in the factory callback)
func (e pluginEntity) LaunchTippedArrow(potion plugin.Key, v plugin.Vec3) (plugin.Arrow, error) {
	id, ok := registry.PotionID(potion.String())
	if !ok {
		return nil, errors.New("plugin: unknown potion " + potion.String())
	}
	stack := protocol.ItemStack{ItemID: int32(registry.ItemTippedArrow), Count: 1}
	stack.SetPotion(id)
	proj, err := e.launch(registry.EntityTypeArrow, v, nbt.Compound{"item": registry.StackTag(stack)})
	if err != nil {
		return nil, err
	}
	return proj.(plugin.Arrow), nil
}

func (e pluginEntity) launch(t registry.EntityType, v plugin.Vec3, tip nbt.Compound) (plugin.Projectile, error) {
	b, ok := e.live()
	if !ok {
		return nil, errors.New("plugin: entity is not valid")
	}
	from, to, _ := e.eyeRay(1)
	if v == (plugin.Vec3{}) {
		speed := 1.5
		switch t {
		case registry.EntityTypeArrow, registry.EntityTypeSpectralArrow:
			speed = 3
		case registry.EntityTypeTrident:
			speed = 2.5
		}
		v = plugin.Vec3{X: (to.X - from.X) * speed, Y: (to.Y - from.Y) * speed, Z: (to.Z - from.Z) * speed}
	}
	loc := e.host.entityLocation(b)
	loc.Pos = plugin.Vec3{X: from.X, Y: from.Y - .1, Z: from.Z}
	extra := nbt.Compound{"Owner": nbt.UUIDToIntArray(e.id), "Motion": nbt.NewList([]nbt.Tag{nbt.Double(v.X), nbt.Double(v.Y), nbt.Double(v.Z)})}
	maps.Copy(extra, tip)
	spawned, err := e.host.pluginWorldOf(b).(pluginWorld).spawnWith(t, loc, extra, nil)
	if err != nil {
		return nil, err
	}
	proj, isProjectile := spawned.(plugin.Projectile)
	if !isProjectile {
		spawned.Remove()
		return nil, errors.New("plugin: entity type " + t.Name() + " is not a projectile")
	}
	if !e.host.srv.projectileLaunched(proj.UUID()) {
		return nil, errors.New("plugin: projectile launch was cancelled")
	}
	return proj, nil
}

func (e pluginEntity) Gliding() bool     { return e.flag("FallFlying") }
func (e pluginEntity) SetGliding(v bool) { e.setFlag("FallFlying", v) }
func (e pluginEntity) Sleeping() bool    { _, ok := e.saved()["sleeping_pos"]; return ok }

// mobBody is the live mob a handle stands for.
func (e pluginEntity) mobBody() (*mobEntity, bool) {
	b, ok := e.live()
	if !ok {
		return nil, false
	}
	snap, ok := b.(*entitySnapshot)
	if !ok {
		return nil, false
	}
	m, ok := snap.ref.(*mobEntity)
	return m, ok
}

// ArrowsInBody counts the arrows stuck in a mob; other bodies show none.
// vanilla: LivingEntity.getArrowCount
func (e livingOps) ArrowsInBody() int {
	m, ok := e.e.mobBody()
	if !ok {
		return 0
	}
	e.e.host.srv.mobsMu.Lock()
	defer e.e.host.srv.mobsMu.Unlock()
	return m.mob.ArrowCount
}

// SetArrowsInBody sets the count; the mob tracker tells the change.
// vanilla: LivingEntity.setArrowCount
func (e livingOps) SetArrowsInBody(n int) {
	if m, ok := e.e.mobBody(); ok {
		e.e.host.srv.mobsMu.Lock()
		m.mob.SetMetadataArrowCount(max(n, 0))
		e.e.host.srv.mobsMu.Unlock()
	}
}

// NoDamageTicks is the remaining invulnerability window of a mob's last hit.
// vanilla: Entity.invulnerableTime
func (e livingOps) NoDamageTicks() int {
	m, ok := e.e.mobBody()
	if !ok {
		return 0
	}
	e.e.host.srv.mobsMu.Lock()
	defer e.e.host.srv.mobsMu.Unlock()
	return m.mob.InvulnTicks
}

func (e livingOps) SetNoDamageTicks(n int) {
	if m, ok := e.e.mobBody(); ok {
		e.e.host.srv.mobsMu.Lock()
		m.mob.InvulnTicks = max(n, 0)
		e.e.host.srv.mobsMu.Unlock()
	}
}

// Collidable is whether a mob pushes and is pushed; bodies other than mobs
// are not tracked as pushable here.
// vanilla: Entity.isPushable
func (e livingOps) Collidable() bool {
	m, ok := e.e.mobBody()
	if !ok {
		return true
	}
	e.e.host.srv.mobsMu.Lock()
	defer e.e.host.srv.mobsMu.Unlock()
	return !m.mob.NoPush
}

func (e livingOps) SetCollidable(v bool) {
	if m, ok := e.e.mobBody(); ok {
		e.e.host.srv.mobsMu.Lock()
		m.mob.NoPush = !v
		e.e.host.srv.mobsMu.Unlock()
	}
}

// ActiveItem is false for a body that is not a player: a mob's draw is not
// part of its saved form.
func (e livingOps) ActiveItem() (plugin.ItemStack, plugin.Hand, bool) {
	return plugin.ItemStack{}, plugin.MainHand, false
}

// Killer is the player in the last_hurt_by_player reference, when it is online.
// vanilla: LivingEntity.getLastHurtByPlayer
func (e pluginEntity) Killer() (plugin.Player, bool) {
	id, ok := e.uuidField("last_hurt_by_player")
	if !ok {
		return nil, false
	}
	b, found := e.host.entityByUUID(uuidOf(id), "")
	if !found {
		return nil, false
	}
	p, isPlayer := e.host.pluginEntityOf(b).(plugin.Player)
	return p, isPlayer
}

// A slot's drop chance is in the drop_chances compound by slot name, 0.085
// when unset.
// vanilla: DropChances
var dropChanceKeys = [...]string{plugin.SlotMainHand: "mainhand", plugin.SlotOffHand: "offhand", plugin.SlotFeet: "feet",
	plugin.SlotLegs: "legs", plugin.SlotChest: "chest", plugin.SlotHead: "head", plugin.SlotBody: "body", plugin.SlotSaddle: "saddle"}

func (q pluginEquipment) DropChance(s plugin.EquipmentSlot) float32 {
	if int(s) >= len(dropChanceKeys) {
		return 0
	}
	c, _ := nbt.CompoundView(q.e.saved()["drop_chances"])
	if f, ok := nbt.FloatField(c, dropChanceKeys[s]); ok {
		return f
	}
	return 0.085
}
func (q pluginEquipment) SetDropChance(s plugin.EquipmentSlot, chance float32) {
	if int(s) < len(dropChanceKeys) && chance >= 0 {
		q.e.mergeInto("drop_chances", dropChanceKeys[s], nbt.Float(chance))
	}
}

// maxLineOfSight caps LineOfSight's distance. vanilla: none; Bukkit's
// CraftLivingEntity.getLineOfSight
const maxLineOfSight = 120

// LineOfSight walks the cells the eye ray enters, nearest first, and stops
// after the first one passes rejects.
func (e pluginEntity) LineOfSight(max int, passes func(plugin.BlockState) bool) []plugin.Block {
	if passes == nil {
		passes = func(s plugin.BlockState) bool { return world.IsAirState(uint16(s)) }
	}
	max = min(max, maxLineOfSight)
	from, to, ok := e.eyeRay(1)
	b, _ := e.live()
	if !ok || max <= 0 {
		return nil
	}
	dim := e.host.srv.dimensionByName(b.DimensionName())
	w := e.host.pluginWorldOf(b)
	dir := [3]float64{to.X - from.X, to.Y - from.Y, to.Z - from.Z}
	pos := [3]float64{from.X, from.Y, from.Z}
	var cell, step [3]int
	var next, delta [3]float64
	for i := range cell {
		cell[i] = int(math.Floor(pos[i]))
		switch {
		case dir[i] > 0:
			step[i], delta[i] = 1, 1/dir[i]
			next[i] = (float64(cell[i]+1) - pos[i]) * delta[i]
		case dir[i] < 0:
			step[i], delta[i] = -1, -1/dir[i]
			next[i] = (pos[i] - float64(cell[i])) * delta[i]
		default:
			next[i], delta[i] = math.Inf(1), math.Inf(1)
		}
	}
	var out []plugin.Block
	for {
		state := dim.w.BlockAt(int32(cell[0]), int32(cell[1]), int32(cell[2]))
		out = append(out, plugin.Block{World: w, Pos: plugin.BlockPos{X: cell[0], Y: cell[1], Z: cell[2]}})
		if !passes(plugin.BlockState(state)) {
			return out
		}
		axis := 0
		if next[1] < next[axis] {
			axis = 1
		}
		if next[2] < next[axis] {
			axis = 2
		}
		if next[axis] > float64(max) {
			return out
		}
		cell[axis] += step[axis]
		next[axis] += delta[axis]
	}
}

func (e pluginEntity) TargetBlockPassing(max int, passes func(plugin.BlockState) bool) plugin.Block {
	line := e.LineOfSight(max, passes)
	if len(line) == 0 {
		return plugin.Block{}
	}
	return line[len(line)-1]
}
