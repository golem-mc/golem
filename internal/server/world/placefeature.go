package world

import (
	"sync"

	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// liveTerrain is the terrainSource of a loaded world: the terrain a feature
// sees is a copy of the loaded chunks, so the feature's writes stay in its
// region's protos until the caller diffs them.
type liveTerrain struct {
	w *World
	decorState
	mu     sync.Mutex
	chunks map[ChunkPos]*shapedTerrain
}

func (t *liveTerrain) cachedTerrain(cx, cz int32) *shapedTerrain { return t.terrainOf(cx, cz, nil) }

// terrainOf copies a loaded chunk; an unloaded one reads as empty.
func (t *liveTerrain) terrainOf(cx, cz int32, _ *Chunk) *shapedTerrain {
	pos := ChunkPos{X: cx, Z: cz}
	t.mu.Lock()
	defer t.mu.Unlock()
	if s := t.chunks[pos]; s != nil {
		return s
	}
	s := &shapedTerrain{}
	if c := t.w.chunkIfLoaded(pos); c != nil {
		s.keep(c, true)
	}
	t.chunks[pos] = s
	return s
}

func (t *liveTerrain) decor() *decorState                                { return &t.decorState }
func (t *liveTerrain) isAir(state uint16) bool                           { return overworldAirBlocks[registry.BlockOf(state)] }
func (t *liveTerrain) biomeIndex(*decorRegion, int32, int32, int32) uint { return 0 }

// chunkIfLoaded returns the loaded chunk at pos, nil if none.
func (w *World) chunkIfLoaded(pos ChunkPos) *Chunk {
	w.mu.RLock()
	defer w.mu.RUnlock()
	if l, ok := w.loadedAt(pos); ok {
		return l.chunk
	}
	return nil
}

// PlacementExtras are the block entities and entities a placement leaves
// besides its blocks.
type PlacementExtras struct {
	// BlockEntities carry their id and absolute x, y and z.
	BlockEntities []nbt.Compound
	// Entities carry no UUID, so a repeated placement does not collide.
	Entities []PlacedEntity
}

// PlaceConfiguredFeature runs a configured feature at (x, y, z) with a random
// stream seeded by seed and returns the blocks it changed and their extras,
// without writing them. ok is false for a feature without a generator here, in an unloaded
// chunk, or one that placed nothing.
// vanilla: ConfiguredFeature.place
func (w *World) PlaceConfiguredFeature(key string, seed int64, x, y, z int32) (changes []BlockChange, extras PlacementExtras, ok bool) {
	return w.placeConfiguredFeature(key, seed, x, y, z, nil)
}

// placeConfiguredFeature stages replacements in the private region so a failed
// grower leaves its saplings and water untouched.
func (w *World) placeConfiguredFeature(key string, seed int64, x, y, z int32, replacements []BlockChange) (changes []BlockChange, extras PlacementExtras, ok bool) {
	center := w.chunkIfLoaded(ChunkPos{X: x >> 4, Z: z >> 4})
	if center == nil {
		return nil, extras, false
	}
	defer func() {
		if e := recover(); e != nil {
			if _, isUnported := e.(unported); !isUnported {
				panic(e)
			}
			changes, extras, ok = nil, PlacementExtras{}, false
		}
	}()
	var place func(*decorRegion, treePos) bool
	if key == "minecraft:chorus_plant" {
		place = func(r *decorRegion, p treePos) bool { return r.canWrite(p) && placeLiveChorus(r, p) }
	} else {
		place = compileConfigured(key)
	}
	r := newDecorRegion(&liveTerrain{w: w, chunks: map[ChunkPos]*shapedTerrain{}}, center, w.MinY(), w.Height())
	r.nx, r.nz = x>>4, z>>4
	r.rng.SetSeed(seed)
	for _, c := range replacements {
		r.setKnownShape(c.X, c.Y, c.Z, c.State)
	}
	if !place(r, treePos{x, y, z}) {
		return nil, extras, false
	}
	changes = r.changesFrom(w, center, &extras)
	return changes, extras, len(changes) > 0
}

// changesFrom lists the blocks the region's protos hold that differ from the
// loaded world and adds the protos' block entities and entities to extras;
// center is the region's middle chunk.
func (r *decorRegion) changesFrom(w *World, center *Chunk, extras *PlacementExtras) (changes []BlockChange) {
	for i, p := range r.protos {
		if p == nil {
			continue
		}
		cx := center.x + int32(i%decorRegionSpan) - decorRegionSpan/2
		cz := center.z + int32(i/decorRegionSpan) - decorRegionSpan/2
		for _, e := range p.entities {
			be, ent := e.resolve(cx, cz, r.minY, w.SeedValue())
			if be != nil {
				extras.BlockEntities = append(extras.BlockEntities, be)
			} else if placed, ok := placedFrom(ent); ok {
				extras.Entities = append(extras.Entities, placed)
			}
		}
		for sec, blocks := range p.blocks {
			if blocks == nil {
				continue
			}
			for idx, v := range blocks {
				if v == 0 {
					continue
				}
				bx, by, bz := cx*sectionSize+int32(idx&15), r.minY+int32(sec)*16+int32(idx>>8), cz*sectionSize+int32(idx>>4&15)
				if v-1 != w.BlockAt(bx, by, bz) {
					changes = append(changes, BlockChange{X: bx, Y: by, Z: bz, State: v - 1})
				}
			}
		}
	}
	return changes
}

// placedFrom is a generated entity compound as a placement stands it up,
// without its UUID: a generated UUID derives from the entity's place, so a
// repeated placement would collide. false for a compound without an id or Pos.
// vanilla: StructureTemplate.placeEntities
func placedFrom(tag nbt.Compound) (PlacedEntity, bool) {
	id, _ := nbt.Get[nbt.String](tag, "id")
	pos, _ := nbt.Get[nbt.List](tag, "Pos")
	if id == "" || len(pos.Items) != 3 {
		return PlacedEntity{}, false
	}
	var at [3]float64
	for i, v := range pos.Items {
		d, ok := v.(nbt.Double)
		if !ok {
			return PlacedEntity{}, false
		}
		at[i] = float64(d)
	}
	delete(tag, "UUID")
	return PlacedEntity{ID: string(id), Pos: at, Tag: tag}, true
}
