package server

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/inventory"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// runCrafter is one crafter visit: the grid is crafted and the result and
// remainders go into the container in front or out of the face, one item
// spent per cell. As with a dispense, the decision is made under the
// crafter's lock and the act runs after, since the push locks the target.
// vanilla: CrafterBlock.dispenseFrom
func (s *Server) runCrafter(w *world.World, d world.Dispense) {
	be := w.BlockEntity(d.X, d.Y, d.Z)
	if be == nil {
		return
	}
	var items []protocol.ItemStack
	var crafted, gone bool
	var recipe inventory.RecipeCraft
	be.Use(func(b *world.BlockEntity) {
		if !w.TouchEntity(b) {
			gone = true
			return
		}
		recipe, items, crafted = inventory.CrafterCraft(b.Slots)
		if crafted {
			crafted = player.MapCraftable(items[0], s.maps.Scalable)
		}
		if crafted {
			items[0] = player.PostProcessMap(items[0], s.maps.Scaled, s.maps.Locked)
		}
		if crafted {
			b.CrafterStartAnimation()
			inventory.CrafterSpend(containerSlots(b, w))
		}
	})
	if gone {
		return
	}
	if !crafted {
		s.broadcastLevelEvent(w,
			protocol.WorldEventCrafterFail, d.X, d.Y, d.Z, 0)

		return
	}
	s.setCrafterCrafting(w, d.X, d.Y, d.Z, true)
	for _, item := range items {
		if s.crafterEject(w, be, item, d) {
			s.crafterRecipeAdvancements(w, d.X, d.Y, d.Z, recipe)
		}
	}
	s.syncContainerViewers(w, d.X, d.Y, d.Z)
}

// crafterEject puts a product into the container the face opens onto and
// drops what it did not take, reporting whether it dropped; a craft that found room makes no sound or
// smoke. vanilla: CrafterBlock.dispenseItem
func (s *Server) crafterEject(w *world.World, be *world.BlockEntity, stack protocol.ItemStack, d world.Dispense) bool {
	face := int32(d.Dir)
	tx, ty, tz := faceOffset(d.X, d.Y, d.Z, face)
	if target := s.containerAt(w, tx, ty, tz); target != nil {
		target.use(w, containerMove{fn: func(dest inventory.Block) {
			var intoEmpty bool
			stack, intoEmpty = inventory.CrafterPour(dest, stack, oppositeFace(face))
			if intoEmpty && target.entity() != nil {
				target.entity().HopperReceived(be)
			}
		}})
		target.sync(s, w)
		target.settle(s, w)
	}
	if stack.Empty() {
		return false
	}
	px, py, pz, vx, vy, vz := inventory.DispenseDropMotion(s.dispenseRng, d.X, d.Y, d.Z, face)
	s.spawnItem(w, stack, px, py, pz, vx, vy, vz, 0)
	s.broadcastLevelEvent(w,
		protocol.WorldEventCrafterCraft, d.X, d.Y, d.Z, 0)

	s.broadcastLevelEvent(w,
		protocol.WorldEventCrafterAnim, d.X, d.Y, d.Z, face)
	return true
}

// setCrafterCrafting writes a crafter's crafting flag: raising it is
// vanilla's flag-2 write (no neighbour updates), clearing it flag 3.
// vanilla: CrafterBlock.dispenseFrom, CrafterBlockEntity.serverTick
func (s *Server) setCrafterCrafting(w *world.World, x, y, z int32, crafting bool) {
	state := w.BlockAt(x, y, z)
	if registry.BlockOf(state) != registry.BlockCrafter {
		return
	}
	state, ok := registry.WithBool(state, registry.PropCrafting, crafting)
	if !ok {
		return
	}
	if !crafting {
		s.setBlockAndTell(w, x, y, z, state)
		return
	}
	s.writeBlock(w, x, y, z, state, nil, writeNeighborShapes)
}

// tickCrafter counts down the crafting animation of one crafter.
// vanilla: CrafterBlockEntity.serverTick
func (s *Server) tickCrafter(w *world.World, be *world.BlockEntity) {
	var done bool
	be.Tick(func(b *world.BlockEntity) bool {
		done = b.CrafterTick()
		return b.CraftingTicks <= 0
	})
	if done {
		s.setCrafterCrafting(w, be.X, be.Y, be.Z, false)
	}
}
