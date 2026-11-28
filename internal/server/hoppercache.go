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
// stamp is w.ContainerStamp(x, z), which the caller may already hold for the same chunk.
func hopperRefresh(w *world.World, cache *world.HopperTargetCache, x, y, z int32, stamp world.ContainerStamp, tags uint64) {
	if !cache.Known || cache.Position != [3]int32{x, y, z} || cache.Stamp != stamp || cache.Tags != tags ||
		(cache.Paired && cache.PartnerStamp != w.ContainerStamp(cache.Partner[0], cache.Partner[1])) {
		*cache = world.HopperTargetCache{Known: true, Position: [3]int32{x, y, z}, Stamp: stamp, Tags: tags}
		state := w.BlockAt(x, y, z)
		cache.MouthBlocked = world.ContainerMouthBlock(state) || world.HopperBlockedBy(state, registry.CollisionShapeFull(state))
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

// hopperMove builds the refreshed cache's target and runs the move. Failures are
// remembered only for block inventories; vehicles must still consume the random
// selection draw. target is nil when there is no container; skipped reports a known
// failure, where no target is built and the caller must not fall back to loose items.
// A gated move is never skipped: a listener's refusal is not a fact about the inventories.
func (s *Server) hopperMove(w *world.World, cache *world.HopperTargetCache, x, y, z int32, move containerMove, slot *moveGate) (target containerTarget, moved, skipped bool) {
	blockContainer := cache.Entities[0] != nil || cache.Composter
	var versions [2]uint64
	var self uint64
	if blockContainer && slot == nil {
		// Read before the locked attempt: a concurrent write after it must force
		// another attempt rather than be recorded as a failure for the new contents.
		self = move.self.be.InventoryVersion()
		for i, be := range cache.Entities {
			if be != nil {
				versions[i] = be.InventoryVersion()
			}
		}
		if cache.Failed && cache.SelfVersion == self && cache.Versions == versions {
			return nil, false, true
		}
	}
	target = s.hopperTarget(w, cache, x, y, z)
	if target == nil {
		return nil, false, false
	}
	if slot != nil {
		slot.target = target
		move.gate = slot
		return target, target.use(w, move), false
	}
	if !blockContainer {
		move.cache = cache
		return target, target.use(w, move), false
	}
	if target.use(w, move) {
		cache.Failed = false
		return target, true, false
	}
	cache.Failed, cache.SelfVersion, cache.Versions = true, self, versions
	return target, false, false
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

// hopperSelf returns b's own block state, re-read only when its chunk's stamp
// moves, with that stamp. The caller holds b's lock.
func hopperSelf(w *world.World, b *world.BlockEntity) (state uint16, stamp world.ContainerStamp) {
	if b.HopperTargets == nil {
		b.HopperTargets = &world.HopperTargets{}
	}
	c := b.HopperTargets
	stamp = w.ContainerStamp(b.X, b.Z)
	if c.SelfStamp != stamp {
		c.SelfState, c.SelfStamp = w.BlockAt(b.X, b.Y, b.Z), stamp
	}
	return c.SelfState, stamp
}
