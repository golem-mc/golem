package world

import (
	"maps"
	"sync"

	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// BeehiveAt is a carried beehive or bee nest with at least one stored bee.
type BeehiveAt struct {
	X, Y, Z int32
	// Tag is the carried compound. A change replaces it (see ReplaceCarried);
	// only the bees' ticks_in_hive move in place (see AdvanceHiveClocks).
	Tag nbt.Compound
}

// BeesStayInHive reads the gameplay attribute, including weather's layer.
// vanilla: WeatherAttributes.addLayer
func (w *World) BeesStayInHive() bool {
	return w.environmentGlobal(envBeesStayInHive) != 0
}

// CatGiftChance reads gameplay/cat_waking_up_gift_chance.
func (w *World) CatGiftChance() float32 {
	return w.environmentGlobal(envCatGiftChance)
}

// isHiveEntry reports whether the carried compound is a beehive block entity.
func isHiveEntry(entry nbt.Compound) bool {
	id, _ := nbt.Get[nbt.String](entry, "id")
	return id == "minecraft:beehive"
}

// hiveStates holds every state of the beehive and bee nest blocks.
var hiveStates = sync.OnceValue(func() map[uint16]bool {
	out := make(map[uint16]bool)
	for _, name := range []string{"minecraft:beehive", "minecraft:bee_nest"} {
		for _, entry := range registry.AllStates[name] {
			out[entry.ID] = true
		}
	}
	return out
})

// BeehiveOccupants is the number of bees the beehive or bee nest at the cell
// stores, false where no such block stands or its chunk is not loaded.
func (w *World) BeehiveOccupants(x, y, z int32) (int, bool) {
	pos := ChunkPos{X: ChunkOf(float64(x)), Z: ChunkOf(float64(z))}
	w.mu.RLock()
	_, loaded := w.chunks[pos]
	w.mu.RUnlock()
	if !loaded || !hiveStates()[w.BlockAt(x, y, z)] {
		return 0, false
	}
	tag, _ := w.BlockEntityTag(x, y, z)
	list, _ := nbt.Get[nbt.List](tag, "bees")
	return len(list.Items), true
}

// AdvanceHiveClocks increments ticks_in_hive of every stored bee without
// marking the chunk (the next real change saves them). It swaps in a copy of
// the entry because connection goroutines read the old compounds without the
// lock.
func (w *World) AdvanceHiveClocks(x, y, z int32) {
	pos := ChunkPos{X: ChunkOf(float64(x)), Z: ChunkOf(float64(z))}
	w.mu.Lock()
	defer w.mu.Unlock()
	loaded, ok := w.chunks[pos]
	if !ok {
		return
	}
	items := loaded.chunk.carriedBlockEntities.Items
	for i, item := range items {
		entry, ok := nbt.CompoundView(item)
		if !ok {
			continue
		}
		if cx, cy, cz := carriedPos(entry); cx != x || cy != y || cz != z {
			continue
		}
		list, _ := nbt.Get[nbt.List](entry, "bees")
		bees := make([]nbt.Tag, len(list.Items))
		for j, bee := range list.Items {
			occupant, ok := nbt.CompoundView(bee)
			if !ok {
				bees[j] = bee
				continue
			}
			next := nbt.ShallowCopyCompound(bee.(nbt.CompoundTag))
			ticks, _ := nbt.IntValue(occupant["ticks_in_hive"])
			next.Set("ticks_in_hive", nbt.Int(ticks+1))
			bees[j] = next
		}
		entryCopy := make(nbt.Compound, len(entry))
		maps.Copy(entryCopy, entry)
		list.Items = bees
		entryCopy["bees"] = list
		swapped := make([]nbt.Tag, len(items))
		copy(swapped, items)
		swapped[i] = entryCopy
		loaded.chunk.carriedBlockEntities.Items = swapped
		return
	}
}

// Beehives lists the loaded hives that store a bee. It walks only the chunks
// that carry a ticking block entity.
func (w *World) Beehives() []BeehiveAt {
	w.mu.RLock()
	defer w.mu.RUnlock()
	var out []BeehiveAt
	for pos := range w.tickingChunks {
		for _, item := range w.chunks[pos].chunk.carriedBlockEntities.Items {
			compound, ok := nbt.CompoundView(item)
			if !ok || !isHiveEntry(compound) {
				continue
			}
			if list, _ := nbt.Get[nbt.List](compound, "bees"); len(list.Items) == 0 {
				continue
			}
			if x, y, z, ok := carriedPosInTag(compound); ok {
				out = append(out, BeehiveAt{X: x, Y: y, Z: z, Tag: compound})
			}
		}
	}
	return out
}

// BeehiveCells lists the beehive and bee nest blocks within r blocks of the
// cell, empty ones included.
// vanilla: PoiManager.getInRange with the bee_home tag
func (w *World) BeehiveCells(x, y, z, r int32) [][3]int32 {
	blocks := w.BlocksIn(x-r, y-r, z-r, x+r, y+r, z+r, nil)
	hives := hiveStates()
	n := int(2*r + 1)
	var out [][3]int32
	for i, state := range blocks {
		if !hives[state] {
			continue
		}
		dx, dy, dz := int32(i/(n*n))-r, int32(i/n%n)-r, int32(i%n)-r
		if dx*dx+dy*dy+dz*dz <= r*r {
			out = append(out, [3]int32{x + dx, y + dy, z + dz})
		}
	}
	return out
}
