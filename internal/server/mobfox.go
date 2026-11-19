package server

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// mobBerryPick is berries a fox reached, picked after the mob lock is
// released.
type mobBerryPick struct {
	w       *world.World
	mob     *entity.Mob
	x, y, z int32
}

// foxItemView lists, per world with a fox, dolphin or panda, the items they may walk to:
// non-empty, alive, past pickup delay. vanilla: Fox.ALLOWED_ITEMS
func (s *Server) foxItemView() map[*world.World][]entity.FoxItem {
	var worlds map[*world.World]bool
	s.lockTick(&s.mobsMu)
	for _, dim := range s.allDims() {
		for _, m := range dim.mobs.herd {
			if (m.Type == entity.TypeFox || m.Type == entity.TypeDolphin || m.Type == entity.TypePanda) && m.Alive() {
				if worlds == nil {
					worlds = make(map[*world.World]bool)
				}
				worlds[dim.w] = true
				break
			}
		}
	}
	s.unlockTick(&s.mobsMu)
	if worlds == nil {
		return nil
	}
	view := make(map[*world.World][]entity.FoxItem, len(worlds))
	s.lockTick(&s.itemsMu)
	for _, it := range s.items {
		if worlds[it.w] && !it.item.Dead && !it.item.Stack.Empty() && it.item.PickupDue() {
			view[it.w] = append(view[it.w], entity.FoxItem{X: it.item.X, Y: it.item.Y, Z: it.item.Z, ItemID: it.item.Stack.ItemID})
		}
	}
	s.unlockTick(&s.itemsMu)
	return view
}

// mobPickedBerries is the world half of a fox's berry meal: the plant loses
// its berries, the fox takes one if its mouth is empty, the rest drop.
// mob_griefing is reread as it may have changed since the goal.
// vanilla: Fox.FoxEatBerriesGoal.onReachedTarget, pickSweetBerries, CaveVines.use
func (s *Server) mobPickedBerries(p mobBerryPick) {
	if !p.w.BoolRule(world.RuleMobGriefing) {
		return
	}
	pick, ok := rules.BerryHarvest(p.w.BlockAt(p.x, p.y, p.z), false)
	if !ok {
		return
	}
	var stacks []protocol.ItemStack
	soundX, soundY, soundZ := float64(p.x)+0.5, float64(p.y)+0.5, float64(p.z)+0.5
	soundCategory, silent := int32(protocol.SoundCategoryBlock), false
	if registry.BlockOf(pick.Loot.BlockState) == registry.BlockSweetBerryBush {
		age, _ := registry.StateInt(pick.Loot.BlockState, registry.PropAge)
		count := int32(1 + s.lootRng.Intn(2))
		if age == 3 {
			count++
		}
		s.mobsMu.Lock()
		count = p.mob.FoxPocketBerries(count)
		// The sound is the fox's own, not the bush's.
		// vanilla: Fox.FoxEatBerriesGoal.pickSweetBerries
		soundX, soundY, soundZ = p.mob.X, p.mob.Y, p.mob.Z
		soundCategory, silent = mobSoundCategory(p.mob.Type), p.mob.Silent
		s.mobsMu.Unlock()
		if count > 0 {
			stacks = append(stacks, protocol.ItemStack{ItemID: int32(registry.ItemSweetBerries), Count: count})
		}
	} else {
		pick.Loot.Level = s.lootLevelAtBlock(p.w, p.x, p.y, p.z)
		stacks = rules.RollLoot(pick.Table, &pick.Loot, s.lootRng)
	}
	for _, stack := range stacks {
		s.spawnBlockDrop(p.w, p.x, p.y, p.z, stack)
	}
	if !silent {
		s.broadcastSoundID(p.w, pick.Sound, soundCategory, soundX, soundY, soundZ, 1.0, 1.0)
	}
	s.writeBlock(p.w, p.x, p.y, p.z, pick.After, nil, writeLight|writeLightAfter|writeNeighborShapes)
}
