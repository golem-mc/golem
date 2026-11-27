package world

import (
	"cmp"
	"maps"
	"slices"

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

var hiveBlocks = blockSet(registry.BlockBeehive, registry.BlockBeeNest)

// BeehiveOccupants is the number of bees the beehive or bee nest at the cell
// stores, false where no such block stands or its chunk is not loaded.
func (w *World) BeehiveOccupants(x, y, z int32) (int, bool) {
	pos := ChunkPos{X: ChunkOf(float64(x)), Z: ChunkOf(float64(z))}
	w.mu.RLock()
	_, loaded := w.chunks[pos]
	w.mu.RUnlock()
	if !loaded || !hiveBlocks[registry.BlockOf(w.BlockAt(x, y, z))] {
		return 0, false
	}
	tag, _ := w.BlockEntityTag(x, y, z)
	list, _ := nbt.Get[nbt.List](tag, "bees")
	return len(list.Items), true
}

// AdvanceHiveClocks increments ticks_in_hive of every stored bee of the given
// hives without marking the chunk (the next real change saves them). It swaps
// in copies of the entries because connection goroutines read the old
// compounds without the lock.
func (w *World) AdvanceHiveClocks(hives []BeehiveAt) {
	if len(hives) == 0 {
		return
	}
	type cell [3]int32
	byChunk := make(map[ChunkPos]map[cell]struct{})
	for _, h := range hives {
		pos := ChunkPos{X: ChunkOf(float64(h.X)), Z: ChunkOf(float64(h.Z))}
		due := byChunk[pos]
		if due == nil {
			due = make(map[cell]struct{})
			byChunk[pos] = due
		}
		due[cell{h.X, h.Y, h.Z}] = struct{}{}
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	for pos, due := range byChunk {
		loaded, ok := w.chunks[pos]
		if !ok {
			continue
		}
		items := loaded.chunk.carriedBlockEntities.Items
		var swapped []nbt.Tag
		for i, item := range items {
			entry, ok := nbt.CompoundView(item)
			if !ok {
				continue
			}
			if x, y, z, ok := carriedPosInTag(entry); !ok {
				continue
			} else if _, hit := due[cell{x, y, z}]; !hit {
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
				var next nbt.CompoundTag
				if ordered, ok := bee.(*nbt.OrderedCompound); ok {
					next = ordered.Clone()
				} else {
					next = nbt.ShallowCopyCompound(bee.(nbt.CompoundTag))
				}
				ticks, _ := nbt.IntValue(occupant["ticks_in_hive"])
				next.Set("ticks_in_hive", nbt.Int(ticks+1))
				bees[j] = next
			}
			entryCopy := make(nbt.Compound, len(entry))
			maps.Copy(entryCopy, entry)
			list.Items = bees
			entryCopy["bees"] = list
			if swapped == nil {
				swapped = slices.Clone(items)
			}
			swapped[i] = entryCopy
		}
		if swapped != nil {
			loaded.chunk.carriedBlockEntities.Items = swapped
		}
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
	x0, x1 := x-r, x+r
	y0, y1 := max(y-r, worldMinY), min(y+r, worldMinY+worldHeight-1)
	z0, z1 := z-r, z+r
	if x0 > x1 || y0 > y1 || z0 > z1 {
		return nil
	}
	minSec, maxSec := (y0-worldMinY)>>4, (y1-worldMinY)>>4
	radiusSquared := r * r
	var out [][3]int32
	w.mu.RLock()
	for cx := x0 >> 4; cx <= x1>>4; cx++ {
		baseX := cx << 4
		loX, hiX := max(x0, baseX)-baseX, min(x1, baseX+15)-baseX
		for cz := z0 >> 4; cz <= z1>>4; cz++ {
			loaded, ok := w.loadedAt(ChunkPos{X: cx, Z: cz})
			if !ok {
				continue
			}
			baseZ := cz << 4
			loZ, hiZ := max(z0, baseZ)-baseZ, min(z1, baseZ+15)-baseZ
			for sec := minSec; sec <= maxSec; sec++ {
				s := &loaded.chunk.sections[sec]
				remaining := s.pois
				if remaining == 0 {
					continue
				}
				baseY := worldMinY + sec*sectionSize
				loY, hiY := max(y0, baseY)-baseY, min(y1, baseY+15)-baseY
			scan:
				for ly := loY; ly <= hiY; ly++ {
					for lz := loZ; lz <= hiZ; lz++ {
						for lx := loX; lx <= hiX; lx++ {
							if !hiveBlocks[registry.BlockOf(s.at(blockIndex(lx, ly, lz)))] {
								continue
							}
							px, py, pz := baseX+lx, baseY+ly, baseZ+lz
							dx, dy, dz := px-x, py-y, pz-z
							if dx*dx+dy*dy+dz*dz <= radiusSquared {
								out = append(out, [3]int32{px, py, pz})
							}
							// Each hive accounts for one POI, even outside the sphere.
							remaining--
							if remaining == 0 {
								break scan
							}
						}
					}
				}
			}
		}
	}
	w.mu.RUnlock()
	// The bee's stable distance sort uses the cube's coordinate order for ties.
	slices.SortFunc(out, func(a, b [3]int32) int {
		return cmp.Or(cmp.Compare(a[0], b[0]), cmp.Compare(a[1], b[1]), cmp.Compare(a[2], b[2]))
	})
	return out
}
