package world

import (
	"maps"
	"slices"

	"github.com/golem-mc/golem/internal/server/world/levelgen"
	"github.com/golem-mc/golem/nbt"
)

// FindMapStructure finds a destination, optionally reserving its saved reference ticket.
// Radius counts placement cells; concentric-ring placements ignore it.
// vanilla: ServerLevel.findNearestMapStructure, ChunkGenerator.tryAddReference
func (w *World) FindMapStructure(ids []string, x, y, z int32, radius int, skipKnown bool) (int32, int32, string, bool) {
	locator, ok := w.gen.(interface {
		locateStructure([]string, int32, int32, int32, int, func(string, int32, int32) bool, startLookup) (int32, int32, string, bool)
	})
	if !ok {
		return 0, 0, "", false
	}
	var accept func(string, int32, int32) bool
	if skipKnown {
		// vanilla: ExplorationMapFunction.claimStructureAt
		if id, start, ok := w.structureStartAt(x, y, z, ids); ok {
			w.referenceMapStructure(id, start.X, start.Z)
		}
		accept = w.referenceMapStructure
	}
	return locator.locateStructure(ids, x, y, z, radius, accept, w.savedStart)
}

// referenceMapStructure claims the start under the chunk lock, including concurrent searches.
func (w *World) referenceMapStructure(id string, cx, cz int32) bool {
	pos := ChunkPos{cx, cz}
	c, _ := w.Acquire(pos)
	defer w.Release(pos)
	w.mu.Lock()
	defer w.mu.Unlock()
	structures, _ := nbt.Get[nbt.Compound](c.savedRoot(), "structures")
	starts, _ := nbt.Get[nbt.Compound](structures, "starts")
	start, ok := nbt.Get[nbt.Compound](starts, id)
	if !ok {
		return false
	}
	savedID, _ := nbt.Get[nbt.String](start, "id")
	children, _ := nbt.Get[nbt.List](start, "Children")
	if string(savedID) != id || len(children.Items) == 0 {
		return false
	}
	refs, _ := nbt.IntField(start, "references")
	// vanilla: StructureStart.getMaxReferences
	if refs >= 1 {
		return false
	}
	start = maps.Clone(start)
	start["references"] = nbt.Int(refs + 1)
	if live := c.liveStarts[id]; live != nil {
		// Moving structure pieces must keep saving their live positions as well as the ticket.
		c.setLiveStructureStart(id, func() nbt.Compound {
			current := maps.Clone(live())
			current["references"] = nbt.Int(refs + 1)
			return current
		})
	} else {
		c.setStructureStart(id, start)
	}
	w.chunks[pos].markDirty()
	return true
}

// NearestStructure finds a structure without consuming its explorer-map ticket.
func (g *OverworldGenerator) NearestStructure(ids []string, x, y, z int32, radius int) (int32, int32, string, bool) {
	return g.locateStructure(ids, x, y, z, radius, nil, nil)
}

// locateStructure compares the first hit from every placement at each search radius.
// A reference is consumed when a placement finds a hit, even if another placement wins.
// vanilla: ChunkGenerator.findNearestMapStructure
func (g *OverworldGenerator) locateStructure(ids []string, x, y, z int32, radius int, accept func(string, int32, int32) bool, saved startLookup) (int32, int32, string, bool) {
	if g.noStructures {
		return 0, 0, "", false
	}
	if len(ids) == 1 && ids[0] == strongholdID {
		return g.nearestStronghold(ids, x, y, z, accept, saved)
	}
	var bx, bz int32
	var bestID string
	var best int64
	offer := func(px, pz int32, id string) {
		dx, dz := int64(px)-int64(x), int64(pz)-int64(z)
		if d := dx*dx + dz*dz; bestID == "" || d < best {
			bx, bz, bestID, best = px, pz, id, d
		}
	}
	if containsID(ids, strongholdID) {
		if px, pz, id, ok := g.nearestStronghold(ids, x, y, z, accept, saved); ok {
			offer(px, pz, id)
		}
	}
	var sets []*levelgen.StructureSet
	for _, set := range g.structureSets() {
		if set.Placement.Spacing > 0 && slices.ContainsFunc(set.Entries, func(e levelgen.StructureSetEntry) bool { return slices.Contains(ids, e.Structure) }) {
			sets = append(sets, set)
		}
	}
	for r := int32(0); r <= int32(radius) && len(sets) > 0; r++ {
		found := false
		for _, set := range sets {
			px, pz, id, ok := g.randomSpreadStructure(set, ids, x>>4, z>>4, r, accept, saved)
			if ok {
				found = true
				offer(px, pz, id)
			}
		}
		if found {
			break
		}
	}
	return bx, bz, bestID, bestID != ""
}

// randomSpreadStructure keeps the placement's x-major perimeter scan order.
// vanilla: ChunkGenerator.getNearestGeneratedStructure (random spread)
func (g *OverworldGenerator) randomSpreadStructure(set *levelgen.StructureSet, ids []string, ox, oz, r int32, accept func(string, int32, int32) bool, saved startLookup) (int32, int32, string, bool) {
	p := &set.Placement
	for x := -r; x <= r; x++ {
		for z := -r; z <= r; z++ {
			if x != -r && x != r && z != -r && z != r {
				continue
			}
			cx, cz := p.PotentialChunk(g.seed, ox+p.Spacing*x, oz+p.Spacing*z)
			if !set.IsStructureChunk(g.seed, cx, cz, g.inStrongholdRing) {
				continue
			}
			var generated []*structureStart
			generatedRead := false
			for _, e := range set.Entries {
				id := e.Structure
				if !slices.Contains(ids, id) {
					continue
				}
				state := startUnknown
				if saved != nil {
					state = saved(id, cx, cz)
				}
				if state == startUnknown {
					if !generatedRead {
						generated, generatedRead = g.structureStartsIn(cx, cz), true
					}
					if slices.ContainsFunc(generated, func(s *structureStart) bool { return s.def.Name == id }) {
						state = startPresent
					}
				}
				if state != startPresent || accept != nil && !accept(id, cx, cz) {
					continue
				}
				return cx*16 + p.LocateOffset[0], cz*16 + p.LocateOffset[2], id, true
			}
		}
	}
	return 0, 0, "", false
}
