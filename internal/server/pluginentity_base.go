package server

import (
	"errors"
	"maps"
	"math/rand/v2"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/command"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/plugin"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
)

// Spawn makes the entity from a saved compound with a fresh UUID, the path a
// structure template's entities take, then runs configure on the handle. The
// call runs on the tick goroutine, so no tick, packet or other plugin sees
// the entity between its creation and the end of configure; the creation
// itself sends nothing the configured state does not replace.
// vanilla: SummonCommand.createEntity
func (w pluginWorld) Spawn(t registry.EntityType, loc plugin.Location, configure func(plugin.Entity)) (plugin.Entity, error) {
	return w.spawnWith(t, loc, nil, configure)
}

// spawnWith is Spawn with saved fields the kind reads only when it is made,
// such as a thrown body's Owner.
func (w pluginWorld) spawnWith(t registry.EntityType, loc plugin.Location, extra nbt.Compound, configure func(plugin.Entity)) (plugin.Entity, error) {
	target := w
	if lw, ok := loc.World.(pluginWorld); ok && lw.srv == w.srv {
		target = lw
	}
	id := uuid.New()
	tag := nbt.Compound{
		"UUID":     nbt.UUIDToIntArray(id),
		"Rotation": nbt.NewList([]nbt.Tag{nbt.Float(loc.Yaw), nbt.Float(loc.Pitch)}),
	}
	maps.Copy(tag, extra)
	if t == registry.EntityTypeFireworkRocket {
		// A saved rocket without LifeTime bursts at once; CraftBukkit builds
		// the rocket with its item constructor, which rolls a life.
		// vanilla: FireworkRocketEntity.<init>(Level, double, double, double, ItemStack)
		if _, ok := tag["LifeTime"]; !ok {
			flight, _ := fireworkFlight(protocol.ItemStack{ItemID: int32(registry.ItemFireworkRocket), Count: 1})
			tag["LifeTime"] = nbt.Int(int32(entity.FireworkLifetime(flight, rand.IntN)))
		}
	}
	ws := worldScope{srv: w.srv, in: func() *dimension { return target.dim }, quiet: true, spawnReason: spawnReasonCode(plugin.SpawnPlugin)}
	command.PlaceTemplateEntities(ws, []world.PlacedEntity{{ID: t.Name(), Pos: [3]float64{loc.Pos.X, loc.Pos.Y, loc.Pos.Z}, Tag: tag}}, func(string, ...any) {})
	body, ok := w.host.entityByUUID(id, target.dim.name)
	if !ok {
		return nil, errors.New("plugin: entity type " + t.Name() + " cannot be spawned")
	}
	h := w.host.pluginEntityOf(body)
	if configure != nil {
		configure(h)
	}
	return h, nil
}

// drop removes keys from the live body by loading the saved compound without
// them; /data merge cannot unset a key.
func (e pluginEntity) drop(key string) {
	saved := e.saved()
	if _, has := saved[key]; !has {
		return
	}
	c := maps.Clone(saved)
	delete(c, key)
	if b, ok := e.live(); ok {
		if w, ok := b.(command.DataWriter); ok {
			w.SetDataTag(c)
		}
	}
}

// Remove discards the entity without a death. A mob leaves its herd as the
// dragon's kill does; the other kinds' /kill already only removes them.
// vanilla: Entity.discard
func (e pluginEntity) Remove() {
	b, ok := e.live()
	if !ok {
		return
	}
	if _, isPlayer := b.(*client); isPlayer {
		return
	}
	if s, isSnapshot := b.(*entitySnapshot); isSnapshot {
		if m, isMob := s.ref.(*mobEntity); isMob {
			s.srv.discardMob(m)
			s.srv.flushDestroyed()
			e.host.srv.dropEntityData(e.id.String())
			return
		}
	}
	b.Kill()
	e.host.srv.dropEntityData(e.id.String())
}

func entityDataID(id uuid.UUID) string { return "plugin:entity/" + id.String() }

// dropEntityData unstores a removed entity's plugin Data.
func (s *Server) dropEntityData(id string) {
	s.entityData.Set("plugin:entity/"+id, nbt.Compound{})
}

// Data is kept beside the level in its own storage keyed by UUID: the kinds'
// save forms have no shared place for it. Entries of a removed entity are
// dropped when the entity is removed from the world.
func (e pluginEntity) Data() plugin.Data {
	s, id := e.host.srv.entityData, entityDataID(e.id)
	return pluginData{
		read: func(key string, read func(nbt.Tag)) {
			if _, ok := e.live(); ok {
				s.Read(id, func(c nbt.CompoundTag) { read(c.Entries()[key]) })
			}
		},
		keys: func(keep func(nbt.Tag) bool) (out []string) {
			s.Read(id, func(c nbt.CompoundTag) {
				for k, v := range c.Entries() {
					if keep(v) {
						out = append(out, k)
					}
				}
			})
			return out
		},
		write: func(key string, value nbt.Tag) {
			if _, ok := e.live(); !ok {
				return
			}
			s.Modify(id, func(c nbt.CompoundTag) bool {
				if value == nil {
					if _, had := c.Entries()[key]; !had {
						return false
					}
					c.Delete(key)
					return true
				}
				c.Set(key, value)
				return true
			})
		},
	}
}

func (e pluginEntity) CustomName() *plugin.Text {
	if tag, ok := e.saved()["CustomName"]; ok {
		if t, ok := pluginTextOf(tag); ok {
			return &t
		}
	}
	return nil
}
func (e pluginEntity) SetCustomName(t *plugin.Text) {
	if t == nil {
		e.drop("CustomName")
		return
	}
	e.merge(nbt.Compound{"CustomName": protocol.TextTag(pluginProtocolText(*t))})
}

func (e pluginEntity) rider() (command.Rider, bool) {
	b, ok := e.live()
	if !ok {
		return nil, false
	}
	r, ok := b.(command.Rider)
	return r, ok
}
func (e pluginEntity) Vehicle() (plugin.Entity, bool) {
	if r, ok := e.rider(); ok {
		if v, ok := r.Vehicle(); ok {
			return e.host.pluginEntityOf(v), true
		}
	}
	return nil, false
}
func (e pluginEntity) Passengers() []plugin.Entity {
	if b, ok := e.live(); ok {
		if r, ok := b.(command.Related); ok {
			return e.host.pluginEntities(r.Relation("passengers"), nil)
		}
	}
	return nil
}
func (e pluginEntity) AddPassenger(p plugin.Entity) bool {
	b, ok := e.live()
	pb, found := e.host.entityOfHandle(p)
	r, rides := pb.(command.Rider)
	return ok && found && rides && r.StartRiding(b)
}
func (e pluginEntity) RemovePassenger(p plugin.Entity) bool {
	pb, found := e.host.entityOfHandle(p)
	r, rides := pb.(command.Rider)
	if !found || !rides {
		return false
	}
	if v, on := r.Vehicle(); !on || v.UUID() != e.id {
		return false
	}
	r.StopRiding()
	return true
}
func (e pluginEntity) Dismount() {
	if r, ok := e.rider(); ok {
		r.StopRiding()
	}
}

func (e pluginEntity) SpawnReason() plugin.SpawnReason {
	b, ok := e.live()
	if !ok {
		return plugin.SpawnDefault
	}
	snap, ok := b.(*entitySnapshot)
	if !ok {
		return plugin.SpawnDefault
	}
	srv := e.host.srv
	var code uint8
	switch ref := snap.ref.(type) {
	case *mobEntity:
		srv.mobsMu.Lock()
		code = ref.spawnReason
		srv.mobsMu.Unlock()
	case *itemEntity:
		srv.itemsMu.Lock()
		code = ref.spawnReason
		srv.itemsMu.Unlock()
	case *boatEntity:
		srv.boatsMu.Lock()
		code = ref.spawnReason
		srv.boatsMu.Unlock()
	case *minecartEntity:
		srv.minecartsMu.Lock()
		code = ref.spawnReason
		srv.minecartsMu.Unlock()
	case *armorStandEntity:
		srv.armorStandsMu.Lock()
		code = ref.spawnReason
		srv.armorStandsMu.Unlock()
	}
	return spawnReasonOf(code)
}

func (e pluginEntity) Origin() (plugin.Location, bool) {
	b, ok := e.live()
	if !ok {
		return plugin.Location{}, false
	}
	snap, ok := b.(*entitySnapshot)
	if !ok || snap.mu == nil {
		return plugin.Location{}, false
	}
	holder, ok := snap.ref.(originHolder)
	if !ok {
		return plugin.Location{}, false
	}
	snap.mu.Lock()
	o := holder.entityOrigin()
	snap.mu.Unlock()
	if !o.set {
		return plugin.Location{}, false
	}
	dim := e.host.srv.dimensionByName(o.dim)
	if dim == nil {
		return plugin.Location{}, false
	}
	return plugin.Location{
		World: pluginWorld{srv: e.host.srv, dim: dim, host: e.host},
		Pos:   plugin.Vec3{X: o.x, Y: o.y, Z: o.z},
	}, true
}
