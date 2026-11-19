package server

import (
	"maps"

	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/plugin"
	"github.com/golem-mc/golem/registry"
)

type pluginTrialSpawner struct{ pluginCarriedBlockEntity }

func (b pluginTrialSpawner) EntityType() (kind registry.EntityType) {
	kind = -1
	b.entity.Read(func(tag nbt.Compound) {
		data, _ := nbt.Get[nbt.Compound](tag, "spawn_data")
		spawn, _ := nbt.Get[nbt.Compound](data, "entity")
		id, _ := nbt.Get[nbt.String](spawn, "id")
		if value, ok := registry.EntityTypeByName(string(id)); ok {
			kind = value
		}
	})
	return kind
}

func (b pluginTrialSpawner) SetEntityType(kind registry.EntityType) {
	if kind < 0 || int(kind) >= len(registry.EntityTypes) {
		return
	}
	if !b.entity.Edit(func(tag nbt.Compound) {
		out := entity.OverrideTrialEntity(tag, kind, nil)
		clear(tag)
		maps.Copy(tag, out)
	}) {
		return
	}
	// Release the carried edit lock before touching the world. The shared flush
	// sends the inactive block before its (empty) update tag.
	p := b.entity.Pos()
	w := b.world.dim.w
	if next, ok := registry.WithProp(w.BlockAt(p[0], p[1], p[2]), registry.PropTrialSpawnerState, "inactive"); ok {
		w.SetBlock(p[0], p[1], p[2], next)
	}
	b.world.srv.markChanged(w, p[0], p[1], p[2])
}

func (b pluginTrialSpawner) NextSpawnAt() (at int64) {
	b.entity.Read(func(tag nbt.Compound) { at, _ = nbt.LongValue(tag["next_mob_spawns_at"]) })
	return at
}
func (b pluginTrialSpawner) SetNextSpawnAt(at int64) {
	b.write(func(tag nbt.Compound) { tag["next_mob_spawns_at"] = nbt.Long(at) })
}
func (b pluginTrialSpawner) RequiredPlayerRange() (distance int) {
	b.entity.Read(func(tag nbt.Compound) {
		distance = 14
		if value, ok := nbt.IntValue(tag["required_player_range"]); ok && value >= 1 && value <= 128 {
			distance = int(value)
		}
	})
	return distance
}
func (b pluginTrialSpawner) SetRequiredPlayerRange(distance int) {
	if distance < 1 || distance > 128 {
		return
	}
	b.write(func(tag nbt.Compound) { tag["required_player_range"] = nbt.Int(distance) })
}
func (b pluginTrialSpawner) Config(ominous bool) (config plugin.TrialSpawnerConfig) {
	b.entity.Read(func(tag nbt.Compound) {
		config = plugin.TrialSpawnerConfig(entity.ReadTrialSpawnerSettings(tag, ominous))
	})
	return config
}
func (b pluginTrialSpawner) SetConfig(ominous bool, config plugin.TrialSpawnerConfig) {
	settings := entity.TrialSpawnerSettings(config)
	if !settings.Valid() {
		return
	}
	b.write(func(tag nbt.Compound) { entity.UpdateTrialSpawnerSettings(tag, ominous, settings) })
}

var _ plugin.TrialSpawner = pluginTrialSpawner{}
