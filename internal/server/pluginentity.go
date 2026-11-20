package server

import (
	"iter"

	"github.com/golem-mc/golem/internal/server/command"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/plugin"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
)

// pluginEntity binds a handle to an entity's UUID, never to the body it
// found once: every call resolves the live body again, so a handle goes
// inert when the entity is removed or its chunk unloads.
type pluginEntity struct {
	host *pluginHost
	id   uuid.UUID
}

// pluginEntityOf wraps a body the command layer listed: a player keeps its
// connection-bound handle, anything else gets a UUID-bound one.
func (h *pluginHost) pluginEntityOf(e command.Entity) plugin.Entity {
	if c, ok := e.(*client); ok {
		return pluginPlayer{host: h, c: c}
	}
	base := pluginEntity{host: h, id: e.UUID()}
	if pluginEntityType(e) == registry.EntityTypeItem {
		return pluginItemEntity{pluginEntity: base}
	}
	if h := pluginVehicleOf(base, e); h != nil {
		return h
	}
	if h := pluginVillagerOf(base, pluginEntityType(e)); h != nil {
		return h
	}
	if h := pluginTypedHandle(base, pluginEntityType(e), base.saved()); h != nil {
		return h
	}
	if pluginEntityType(e) == registry.EntityTypeExperienceOrb {
		return pluginOrbEntity{pluginEntity: base, orbOps: orbOps{base}}
	}
	if a, ok := e.(command.AttributeTarget); ok && a.HasAttributes() {
		if t, ok := e.(command.DataTarget); ok {
			if tag, ok := t.DataTag(); ok {
				return pluginKindHandle(base, tag.Entries())
			}
		}
		return pluginLivingEntity{pluginEntity: base, livingOps: livingOps{base}}
	}
	return pluginPlainEntity{pluginEntity: base}
}

// livingOps are the LivingEntity methods that a body with no health lacks: a
// separate type, so only the living handles have them.
type livingOps struct{ e pluginEntity }

type (
	pluginPlainEntity  struct{ pluginEntity }
	pluginItemEntity   struct{ pluginEntity }
	pluginLivingEntity struct {
		pluginEntity
		livingOps
	}
)

// entityOfHandle is the live body a plugin handle stands for.
func (h *pluginHost) entityOfHandle(e plugin.Entity) (command.Entity, bool) {
	if p, ok := e.(pluginPlayer); ok {
		return p.c, p.IsValid()
	}
	if v, ok := e.(interface{ live() (command.Entity, bool) }); ok {
		return v.live()
	}
	return nil, false
}

func (h *pluginHost) pluginEntities(list []command.Entity, keep func(command.Entity) bool) []plugin.Entity {
	out := make([]plugin.Entity, 0, len(list))
	for _, e := range list {
		if keep == nil || keep(e) {
			out = append(out, h.pluginEntityOf(e))
		}
	}
	return out
}

// entityByUUID finds the live body with the identity, in any dimension.
func (h *pluginHost) entityByUUID(id uuid.UUID, dim string) (command.Entity, bool) {
	list := (worldScope{srv: h.srv}).Entities(command.EntityQuery{UUID: &id, Dimension: dim})
	if len(list) == 0 {
		return nil, false
	}
	return list[0], true
}

// live is the body the handle stands for; a corpse still in its death
// animation answers reads too, so a death event's entity can be inspected.
func (e pluginEntity) live() (command.Entity, bool) {
	if b, ok := e.host.entityByUUID(e.id, ""); ok {
		return b, true
	}
	list := (worldScope{srv: e.host.srv}).Entities(command.EntityQuery{UUID: &e.id, IncludeDead: true})
	if len(list) == 0 {
		return nil, false
	}
	return list[0], true
}

func (e pluginEntity) IsValid() bool     { _, ok := e.host.entityByUUID(e.id, ""); return ok }
func (e pluginEntity) UUID() plugin.UUID { return plugin.UUID(e.id) }
func (e pluginEntity) ID() int32 {
	if b, ok := e.live(); ok {
		return b.EntityID()
	}
	return 0
}
func (e pluginEntity) Type() registry.EntityType {
	if b, ok := e.live(); ok {
		return pluginEntityType(b)
	}
	return -1
}
func (e pluginEntity) World() plugin.World {
	if b, ok := e.live(); ok {
		return e.host.pluginWorldOf(b)
	}
	return nil
}
func (e pluginEntity) Location() plugin.Location {
	if b, ok := e.live(); ok {
		return e.host.entityLocation(b)
	}
	return plugin.Location{}
}
func (e pluginEntity) Pos() plugin.Vec3 { return e.Location().Pos }
func (e pluginEntity) BoundingBox() plugin.Box {
	if b, ok := e.live(); ok {
		return entityBox(b)
	}
	return plugin.Box{}
}
func (e pluginEntity) Teleport(to plugin.Location, cause plugin.TeleportCause) bool {
	b, ok := e.live()
	return ok && e.host.teleportEntity(b, to, cause)
}
func (e pluginEntity) Tags() []string {
	if b, ok := e.live(); ok {
		return b.Tags()
	}
	return nil
}
func (e pluginEntity) AddTag(tag string) bool {
	b, ok := e.live()
	return ok && b.AddTag(tag)
}
func (e pluginEntity) RemoveTag(tag string) bool {
	b, ok := e.live()
	return ok && b.RemoveTag(tag)
}
func (e pluginEntity) Nearby(r float64) []plugin.Entity {
	if b, ok := e.live(); ok {
		return e.host.nearbyEntities(b, r)
	}
	return nil
}

func pluginEntityType(b command.Entity) registry.EntityType {
	t, ok := registry.EntityTypeByName(b.EntityType())
	if !ok {
		return -1
	}
	return t
}

func (h *pluginHost) pluginWorldOf(b command.Entity) plugin.World {
	return pluginWorld{srv: h.srv, dim: h.srv.dimensionByName(b.DimensionName()), host: h}
}

func (h *pluginHost) entityLocation(b command.Entity) plugin.Location {
	p := b.Position()
	return plugin.Location{World: h.pluginWorldOf(b), Pos: plugin.Vec3{X: p.X, Y: p.Y, Z: p.Z}, Yaw: p.Yaw, Pitch: p.Pitch}
}

func entityBox(b command.Entity) plugin.Box {
	x0, y0, z0, x1, y1, z1 := b.Box()
	return plugin.Box{Min: plugin.Vec3{X: x0, Y: y0, Z: z0}, Max: plugin.Vec3{X: x1, Y: y1, Z: z1}}
}

// teleportEntity moves the body to the location's world and position; false
// for a location with no world of this server or a cancelled
// EntityTeleportEvent.
func (h *pluginHost) teleportEntity(b command.Entity, to plugin.Location, cause plugin.TeleportCause) bool {
	w, ok := to.World.(pluginWorld)
	if !ok || w.srv != h.srv {
		return false
	}
	move, dim, ok := h.srv.pluginTeleportCause(b, command.Move{Pos: player.Position{X: to.Pos.X, Y: to.Pos.Y, Z: to.Pos.Z, Yaw: to.Yaw, Pitch: to.Pitch}}, w.dim.name, cause)
	if !ok {
		return false
	}
	b.Teleport(move, dim, pluginTeleportBy)
	return true
}

func (h *pluginHost) nearbyEntities(b command.Entity, r float64) []plugin.Entity {
	box := entityBox(b)
	q := [6]float64{box.Min.X - r, box.Min.Y - r, box.Min.Z - r, box.Max.X + r, box.Max.Y + r, box.Max.Z + r}
	id := b.UUID()
	list := (worldScope{srv: h.srv}).Entities(command.EntityQuery{Dimension: b.DimensionName(), Box: &q})
	return h.pluginEntities(list, func(o command.Entity) bool { return o.UUID() != id })
}

func (w pluginWorld) Entities() iter.Seq[plugin.Entity] {
	return func(yield func(plugin.Entity) bool) {
		for _, e := range w.host.queryEntities(command.EntityQuery{Dimension: w.dim.name}) {
			if !yield(e) {
				return
			}
		}
	}
}

func (h *pluginHost) queryEntities(q command.EntityQuery) []plugin.Entity {
	return h.pluginEntities((worldScope{srv: h.srv}).Entities(q), nil)
}

func (w pluginWorld) EntitiesIn(b plugin.Box) []plugin.Entity {
	q := [6]float64{b.Min.X, b.Min.Y, b.Min.Z, b.Max.X, b.Max.Y, b.Max.Z}
	return w.host.queryEntities(command.EntityQuery{Dimension: w.dim.name, Box: &q})
}

func (w pluginWorld) Entity(id plugin.UUID) (plugin.Entity, bool) {
	b, ok := w.host.entityByUUID(uuid.UUID(id), w.dim.name)
	if !ok {
		return nil, false
	}
	return w.host.pluginEntityOf(b), true
}

func (c pluginChunk) Entities() []plugin.Entity {
	pos := c.Pos()
	w := c.world
	lo, hi := float64(pos.X*16), float64(pos.X*16+16)
	zlo, zhi := float64(pos.Z*16), float64(pos.Z*16+16)
	// The box query matches by overlap; a body belongs to the chunk its position is in.
	q := [6]float64{lo - 4, float64(w.MinY()) - 4, zlo - 4, hi + 4, float64(w.MinY()+w.Height()) + 4, zhi + 4}
	list := (worldScope{srv: w.srv}).Entities(command.EntityQuery{Dimension: w.dim.name, Box: &q})
	return w.host.pluginEntities(list, func(e command.Entity) bool {
		p := e.Position()
		return p.X >= lo && p.X < hi && p.Z >= zlo && p.Z < zhi
	})
}

// A player is also an Entity; its handle forwards to the same adapter the
// other kinds use, through the connection's own body.
func (p pluginPlayer) ID() int32 {
	if !p.IsValid() {
		return 0
	}
	return p.c.EntityID()
}
func (p pluginPlayer) Type() registry.EntityType { return registry.EntityTypePlayer }
func (p pluginPlayer) World() plugin.World {
	if !p.IsValid() {
		return nil
	}
	return p.hostOrAdapter().pluginWorldOf(p.c)
}
func (p pluginPlayer) Location() plugin.Location {
	if !p.IsValid() {
		return plugin.Location{}
	}
	return p.hostOrAdapter().entityLocation(p.c)
}
func (p pluginPlayer) Pos() plugin.Vec3 { return p.Location().Pos }
func (p pluginPlayer) BoundingBox() plugin.Box {
	if !p.IsValid() {
		return plugin.Box{}
	}
	return entityBox(p.c)
}
func (p pluginPlayer) Teleport(to plugin.Location, cause plugin.TeleportCause) bool {
	return p.IsValid() && p.hostOrAdapter().teleportEntity(p.c, to, cause)
}
func (p pluginPlayer) Tags() []string {
	if !p.IsValid() {
		return nil
	}
	return p.c.Tags()
}
func (p pluginPlayer) AddTag(tag string) bool    { return p.IsValid() && p.c.AddTag(tag) }
func (p pluginPlayer) RemoveTag(tag string) bool { return p.IsValid() && p.c.RemoveTag(tag) }
func (p pluginPlayer) Nearby(r float64) []plugin.Entity {
	if !p.IsValid() {
		return nil
	}
	return p.hostOrAdapter().nearbyEntities(p.c, r)
}
