package server

import (
	"strings"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// vehicleCargo is a vehicle's container: slots and the pending loot table.
// The vehicle's lock guards it; a vehicle holding nothing has no slots.
// vanilla: ContainerEntity
type vehicleCargo struct {
	slots   []protocol.ItemStack
	version uint64 // slot writes, guarded by the vehicle lock
	// lootTable is pending until first contents read or break; lootSeed is its seed, 0
	// for the server's loot source.
	lootTable string
	lootSeed  int64
}

// newVehicleCargo is an empty container of n slots.
func newVehicleCargo(n int) vehicleCargo {
	if n == 0 {
		return vehicleCargo{}
	}
	return vehicleCargo{slots: make([]protocol.ItemStack, n)}
}

// putTag writes the container into a vehicle's compound: the pending table
// instead of contents, else the filled slots.
// vanilla: ContainerEntity.addChestVehicleSaveData
func (v *vehicleCargo) putTag(tag nbt.CompoundTag) {
	if v.lootTable != "" {
		tag.Set("LootTable", nbt.String(v.lootTable))
		if v.lootSeed != 0 {
			tag.Set("LootTableSeed", nbt.Long(v.lootSeed))
		}
		return
	}
	registry.PutSlottedItems(tag, v.slots, len(v.slots) > 0)
}

// readVehicleCargo reads an n-slot container from a vehicle's compound; a
// pending table stands for the contents, so items are not read.
// vanilla: ContainerEntity.readChestVehicleSaveData
func readVehicleCargo(compound nbt.Compound, n int) vehicleCargo {
	v := newVehicleCargo(n)
	if n == 0 {
		return v
	}
	if table, ok := nbt.Get[nbt.String](compound, "LootTable"); ok && table != "" {
		v.lootTable = string(table)
		if !strings.Contains(v.lootTable, ":") {
			v.lootTable = "minecraft:" + v.lootTable
		}
		v.lootSeed, _ = nbt.LongValue(compound["LootTableSeed"])
		return v
	}
	registry.LoadSlottedItems(compound, v.slots)
	return v
}

// unpackVehicleLoot rolls the pending table into the empty slots and clears
// it, returning the table it rolled ("" for none); a table without data here
// stays pending so the tag survives a save. The roll's origin is the
// vehicle's block at x, y, z in w.
// vanilla: ContainerEntity.unpackChestVehicleLootTable
func (s *Server) unpackVehicleLoot(w *world.World, x, y, z float64, v *vehicleCargo, opener lootOpener) string {
	table := v.lootTable
	if table == "" || v.slots == nil {
		return ""
	}
	if !s.fillContainerLoot(w, rules.BlockOf(x), rules.BlockOf(y), rules.BlockOf(z), table, v.lootSeed, v.slots, opener) {
		return ""
	}
	v.version++
	v.lootTable, v.lootSeed = "", 0
	return table
}
