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

// foxViewMob is a fox, dolphin or panda and the chunk rectangle its item
// search touches.
type foxViewMob struct {
	m                      *entity.Mob
	w                      *world.World
	minX, minZ, maxX, maxZ int32
	from, to               int
}

// foxItemView lists, per fox, dolphin and panda, the items in the chunks its
// search box touches: non-empty, alive, past pickup delay. The goals filter by
// distance. The lists are slices of one buffer reused next tick.
// vanilla: Fox.ALLOWED_ITEMS, Fox.FoxSearchForItemsGoal
func (s *Server) foxItemView() map[*entity.Mob][]entity.FoxItem {
	mobs := s.foxViewMobs[:0]
	s.lockTick(&s.mobsMu)
	for _, dim := range s.allDims() {
		for _, m := range dim.mobs.herd {
			reach := 8.0 // vanilla: inflate(8, 8, 8); a panda's is 6
			switch m.Type {
			case entity.TypePanda:
				reach = 6
			case entity.TypeFox, entity.TypeDolphin:
			default:
				continue
			}
			if !m.Alive() {
				continue
			}
			// The mob's width and an item's half size fit in the extra block.
			reach++
			mobs = append(mobs, foxViewMob{m: m, w: dim.w,
				minX: world.ChunkOf(m.X - reach), minZ: world.ChunkOf(m.Z - reach),
				maxX: world.ChunkOf(m.X + reach), maxZ: world.ChunkOf(m.Z + reach)})
		}
	}
	s.unlockTick(&s.mobsMu)
	s.foxViewMobs = mobs
	if len(mobs) == 0 {
		return nil
	}
	buf := s.foxViewBuf[:0]
	s.lockTick(&s.itemsMu)
	for i := range mobs {
		v := &mobs[i]
		v.from = len(buf)
		for x := v.minX; x <= v.maxX; x++ {
			for z := v.minZ; z <= v.maxZ; z++ {
				for it := range s.itemChunks.In(world.ChunkPos{X: x, Z: z}) {
					if it.w == v.w && !it.item.Dead && !it.item.Stack.Empty() && it.item.PickupDue() {
						buf = append(buf, entity.FoxItem{X: it.item.X, Y: it.item.Y, Z: it.item.Z, ItemID: it.item.Stack.ItemID})
					}
				}
			}
		}
		v.to = len(buf)
	}
	s.unlockTick(&s.itemsMu)
	s.foxViewBuf = buf
	if s.foxView == nil {
		s.foxView = make(map[*entity.Mob][]entity.FoxItem)
	}
	clear(s.foxView)
	for _, v := range mobs {
		s.foxView[v.m] = buf[v.from:v.to:v.to]
	}
	return s.foxView
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
