package server

import (
	"math"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/plugin"
	"github.com/golem-mc/golem/registry"
)

type pluginContainer struct{ pluginNamedBlockEntity }

func (b pluginContainer) Lock() *plugin.LockPredicate {
	return pluginBlockEntityRead(b.pluginBlockEntity, func(e *world.BlockEntity) *plugin.LockPredicate {
		if e.Lock == nil {
			return nil
		}
		value := plugin.LockPredicate(nbt.Stringify(registry.LockTag(e.Lock)))
		return &value
	})
}

func (b pluginContainer) SetLock(value *plugin.LockPredicate) {
	var lock nbt.CompoundTag
	if value != nil {
		parsed, err := nbt.ParseOrderedSNBT(string(*value))
		if err != nil {
			return
		}
		// Parsing owns all children; LockTag alone defines saved normalization.
		lock = registry.LockTag(parsed)
	}
	b.mutate(func(e *world.BlockEntity) { e.Lock = lock })
}

func (b pluginContainer) randomizable() bool {
	switch b.entity.Kind {
	case world.KindChest, world.KindTrappedChest, world.KindBarrel, world.KindShulkerBox,
		world.KindHopper, world.KindDispenser, world.KindDropper, world.KindCrafter:
		return true
	}
	return false
}

func (b pluginContainer) LootTable() (table plugin.Key, seed int64) {
	if !b.randomizable() {
		return
	}
	type loot struct {
		table plugin.Key
		seed  int64
	}
	value := pluginBlockEntityRead(b.pluginBlockEntity, func(e *world.BlockEntity) loot {
		if e.LootTable == "" {
			return loot{}
		}
		key, _ := plugin.ParseKey(e.LootTable)
		return loot{key, e.LootTableSeed}
	})
	return value.table, value.seed
}

func (b pluginContainer) SetLootTable(table plugin.Key, seed int64) {
	if !b.randomizable() {
		return
	}
	var name string
	if table != (plugin.Key{}) {
		if _, err := plugin.ParseKey(table.String()); err != nil {
			return
		}
		name = table.String()
	} else {
		seed = 0
	}
	b.mutate(func(e *world.BlockEntity) { e.LootTable, e.LootTableSeed = name, seed })
}

type pluginFurnace struct{ pluginContainer }

func (b pluginFurnace) BurnTime() int {
	return pluginBlockEntityRead(b.pluginBlockEntity, func(e *world.BlockEntity) int { return int(e.LitRemaining) })
}
func (b pluginFurnace) SetBurnTime(ticks int) {
	b.setProgress(ticks, protocol.FurnacePropertyFuelLeft, func(e *world.BlockEntity) *int32 { return &e.LitRemaining })
}
func (b pluginFurnace) CookTime() int {
	return pluginBlockEntityRead(b.pluginBlockEntity, func(e *world.BlockEntity) int { return int(e.CookingSpent) })
}
func (b pluginFurnace) SetCookTime(ticks int) {
	b.setProgress(ticks, protocol.FurnacePropertyProgress, func(e *world.BlockEntity) *int32 { return &e.CookingSpent })
}
func (b pluginFurnace) CookTimeTotal() int {
	return pluginBlockEntityRead(b.pluginBlockEntity, func(e *world.BlockEntity) int { return int(e.CookingTotal) })
}

func (b pluginFurnace) setProgress(ticks int, property int32, field func(*world.BlockEntity) *int32) {
	if ticks < math.MinInt32 || ticks > math.MaxInt32 {
		return
	}
	changed := false
	if !b.mutate(func(e *world.BlockEntity) {
		value := field(e)
		changed = *value != int32(ticks)
		*value = int32(ticks)
	}) || !changed {
		return
	}
	// Menu packets acquire connection locks only after releasing the entity.
	b.world.srv.containerViewers(b.world.dim.w, b.entity.X, b.entity.Y, b.entity.Z, nil, func(c *client) {
		if window := c.openContainerWindow(); window != nil && window.kind.Cooking() {
			c.queueContainerProperty(window.id, property, int32(ticks))
		}
	})
}

var (
	_ plugin.Container = pluginContainer{}
	_ plugin.Furnace   = pluginFurnace{}
)
