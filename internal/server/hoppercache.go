package server

import (
	"github.com/golem-mc/golem/internal/server/inventory"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// hopperRefresh revalidates the cached block container at the position until a block or
// container is replaced. Chest halves also watch the partner's chunk, even when that half
// has no entity yet.
// vanilla: HopperBlockEntity.getContainerAt; Lithium: cacheInsertBlockInventory
func hopperRefresh(w *world.World, cache *world.HopperTargetCache, x, y, z int32) {
	stamp := w.ContainerStamp(x, z)
	tags := registry.TagsGeneration()
	if !cache.Known || cache.Position != [3]int32{x, y, z} || cache.Stamp != stamp || cache.Tags != tags ||
		(cache.Paired && cache.PartnerStamp != w.ContainerStamp(cache.Partner[0], cache.Partner[1])) {
		*cache = world.HopperTargetCache{Known: true, Position: [3]int32{x, y, z}, Stamp: stamp, Tags: tags}
		state := w.BlockAt(x, y, z)
		if world.IsChestBlock(registry.BlockOf(state)) && registry.StateValue(state, registry.PropType) != "single" {
			dx, dz := world.ChestPartnerStep(state)
			cache.Paired = true
			cache.Partner = [2]int32{x + dx, z + dz}
			cache.PartnerStamp = w.ContainerStamp(x+dx, z+dz)
		}
		switch target := blockContainerAt(w, x, y, z).(type) {
		case singleTarget:
			cache.Entities[0] = target.be
		case *pairTarget:
			cache.Entities = target.bes
		case *composterTarget:
			cache.Composter = true
		}
	}
}

// hopperKnownFailure reports that the refreshed block container's last move failed and
// neither inventory changed since, so no target needs building. A gated move is never
// skipped: a listener's refusal is not a fact about the inventories.
func hopperKnownFailure(cache *world.HopperTargetCache, self *world.BlockEntity, gated bool) bool {
	if gated || !cache.Failed || (cache.Entities[0] == nil && !cache.Composter) || cache.SelfVersion != self.InventoryVersion() {
		return false
	}
	for i, be := range cache.Entities {
		if be != nil && cache.Versions[i] != be.InventoryVersion() {
			return false
		}
		if be == nil && cache.Versions[i] != 0 {
			return false
		}
	}
	return true
}

// hopperTarget materializes the container of a refreshed cache.
func (s *Server) hopperTarget(w *world.World, cache *world.HopperTargetCache, x, y, z int32) containerTarget {
	if cache.Composter {
		// Each view carries the effects of exactly one attempt.
		return &composterTarget{x: x, y: y, z: z}
	}
	if cache.Entities[1] != nil {
		return &pairTarget{bes: cache.Entities}
	}
	if cache.Entities[0] != nil {
		return singleTarget{cache.Entities[0]}
	}
	return s.entityContainerAt(w, float64(x)+0.5, float64(y)+0.5, float64(z)+0.5, nil)
}

// hopperMove remembers failures only for block inventories. Vehicles must still
// consume the random selection draw.
func hopperMove(w *world.World, cache *world.HopperTargetCache, target containerTarget, move containerMove) bool {
	if move.gate != nil {
		// A refusal by a listener is not a fact about the inventories.
		return target.use(w, move)
	}
	if cache.Entities[0] == nil && !cache.Composter {
		move.cache = cache
		return target.use(w, move)
	}
	self := move.self.be.InventoryVersion()
	var versions [2]uint64
	for i, be := range cache.Entities {
		if be != nil {
			versions[i] = be.InventoryVersion()
		}
	}
	if cache.Failed && cache.SelfVersion == self && cache.Versions == versions {
		return false
	}
	if target.use(w, move) {
		cache.Failed = false
		return true
	}
	// Read before the locked attempt: a concurrent write after it must force
	// another attempt rather than be recorded as a failure for the new contents.
	cache.Failed, cache.SelfVersion, cache.Versions = true, self, versions
	return false
}

// cachedVehicleMove runs after selection, under the vehicle lock: random draws
// and membership checks still happen even when the transfer itself can sleep.
func cachedVehicleMove[B inventory.Block](move containerMove, slots B, version uint64) bool {
	cache := move.cache
	if cache == nil || slots.Len() == 0 {
		return applyContainerMove(move, slots, nil)
	}
	key := &slots.Stacks()[0]
	self := move.self.be.InventoryVersion()
	if cache.Failed && cache.VehicleSlots == key && cache.SelfVersion == self && cache.Versions[0] == version {
		return false
	}
	moved := applyContainerMove(move, slots, nil)
	cache.Failed = !moved
	cache.VehicleSlots, cache.SelfVersion, cache.Versions[0] = key, self, version
	return moved
}
