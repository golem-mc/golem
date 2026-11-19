package world

import (
	"slices"

	"github.com/golem-mc/golem/internal/server/world/levelgen"
	"github.com/golem-mc/golem/nbt"
)

// StructureHasPieceAt reports whether a loaded position is inside a piece of
// any named structure, following the chunk's saved references to its starts.
// vanilla: StructureManager.getStructureWithPieceAt, structureHasPieceAt
func (w *World) StructureHasPieceAt(x, y, z int32, ids []string) bool {
	for _, id := range ids {
		if w.StructureContains(x, y, z, id, true) {
			return true
		}
	}
	return false
}

// StructureContains tests saved piece bounds or the full bounds of a structure.
// vanilla: ChunkGenerator.getMobsAt
func (w *World) StructureContains(x, y, z int32, id string, piece bool) bool {
	pos := ChunkPos{X: x >> 4, Z: z >> 4}
	w.mu.RLock()
	loaded, ok := w.loadedAt(pos)
	if ok {
		loaded.users.Add(1)
	}
	w.mu.RUnlock()
	if !ok {
		return false
	}
	defer w.Release(pos)
	for _, b := range w.structureBoxes(loaded, id, piece) {
		if b.box.inside(x, y, z) {
			return true
		}
	}
	return false
}

// StructureReferences returns the ids of the structures the chunk at the block
// column references, nil for an unloaded chunk. Callers must not modify it.
// vanilla: StructureManager.getAllStructuresAt
func (w *World) StructureReferences(x, z int32) []string {
	w.mu.RLock()
	loaded, ok := w.loadedAt(ChunkPos{X: x >> 4, Z: z >> 4})
	if !ok {
		w.mu.RUnlock()
		return nil
	}
	if loaded.structureRefsRead {
		refs := loaded.structureRefs
		w.mu.RUnlock()
		return refs
	}
	var ids []string
	structures, _ := nbt.Get[nbt.Compound](loaded.chunk.carriedRoot, "structures")
	references, _ := nbt.Get[nbt.Compound](structures, "References")
	for id := range references {
		if refs, _ := nbt.Get[nbt.LongArray](references, id); len(refs) > 0 {
			ids = append(ids, id)
		}
	}
	w.mu.RUnlock()
	w.mu.Lock()
	loaded.structureRefs, loaded.structureRefsRead = ids, true
	w.mu.Unlock()
	return ids
}

// structureStartAt returns the first of ids, in order, whose start's full bounds hold the
// position, and the chunk of that start. An unloaded chunk answers false.
// vanilla: StructureManager.getStructureAt
func (w *World) structureStartAt(x, y, z int32, ids []string) (string, ChunkPos, bool) {
	pos := ChunkPos{X: x >> 4, Z: z >> 4}
	w.mu.RLock()
	loaded, ok := w.loadedAt(pos)
	if ok {
		loaded.users.Add(1)
	}
	w.mu.RUnlock()
	if !ok {
		return "", ChunkPos{}, false
	}
	defer w.Release(pos)
	for _, id := range ids {
		for _, b := range w.structureBoxes(loaded, id, false) {
			if b.box.inside(x, y, z) {
				return id, b.start, true
			}
		}
	}
	return "", ChunkPos{}, false
}

// structureBox is a saved bound with the chunk of the start that owns it.
type structureBox struct {
	box   jbox
	start ChunkPos
}

type structureBoundsKey struct {
	id    string
	piece bool
}

// structureBoxes caches membership bounds for this chunk. Starts and
// references are fixed when a chunk finishes generation; block edits do not
// change them. The caller holds the loaded chunk through Release.
func (w *World) structureBoxes(loaded *loadedChunk, id string, piece bool) []structureBox {
	key := structureBoundsKey{id: id, piece: piece}
	w.mu.RLock()
	boxes, cached := loaded.structureBounds[key]
	if cached {
		w.mu.RUnlock()
		return boxes
	}
	structures, _ := nbt.Get[nbt.Compound](loaded.chunk.carriedRoot, "structures")
	references, _ := nbt.Get[nbt.Compound](structures, "References")
	refs, _ := nbt.Get[nbt.LongArray](references, id)
	refs = slices.Clone(refs)
	w.mu.RUnlock()

	for _, ref := range refs {
		pos := ChunkPos{X: int32(ref), Z: int32(ref >> 32)}
		c := w.HoldIfLoaded(pos)
		held := c != nil
		if c == nil {
			c, _ = w.loadChunk(pos)
		}
		if c == nil {
			// Only starts are needed; generating terrain here would stall the tick.
			if gen, ok := w.gen.(interface{ saveStructureStarts(*Chunk) }); ok {
				c = newChunk(pos.X, pos.Z)
				gen.saveStructureStarts(c)
			}
		}
		if c == nil {
			continue
		}
		w.mu.RLock()
		structures, _ := nbt.Get[nbt.Compound](c.savedRoot(), "structures")
		starts, _ := nbt.Get[nbt.Compound](structures, "starts")
		start, _ := nbt.Get[nbt.Compound](starts, id)
		if name, _ := nbt.Get[nbt.String](start, "id"); string(name) == id {
			children, _ := nbt.Get[nbt.List](start, "Children")
			var full jbox
			valid := false
			for _, child := range children.Items {
				tag, _ := nbt.CompoundView(child)
				bb, _ := nbt.Get[nbt.IntArray](tag, "BB")
				if len(bb) != 6 || bb[0] > bb[3] || bb[1] > bb[4] || bb[2] > bb[5] {
					continue
				}
				box := jbox{bb[0], bb[1], bb[2], bb[3], bb[4], bb[5]}
				if !valid {
					full = box
				} else {
					full = full.union(box)
				}
				valid = true
				if piece && bb[3]>>4 >= loaded.pos.X && bb[0]>>4 <= loaded.pos.X &&
					bb[5]>>4 >= loaded.pos.Z && bb[2]>>4 <= loaded.pos.Z {
					boxes = append(boxes, structureBox{box, pos})
				}
			}
			if !piece && valid {
				// vanilla: Structure.adjustBoundingBox
				if def := levelgen.StructureNamed(id); def != nil && def.Adaptation != levelgen.AdjustNone {
					full = jbox{full.minX - 12, full.minY - 12, full.minZ - 12, full.maxX + 12, full.maxY + 12, full.maxZ + 12}
				}
				boxes = append(boxes, structureBox{full, pos})
			}
		}
		w.mu.RUnlock()
		if held {
			w.Release(pos)
		}
	}
	w.mu.Lock()
	if loaded.structureBounds == nil {
		loaded.structureBounds = map[structureBoundsKey][]structureBox{}
	}
	loaded.structureBounds[key] = boxes
	w.mu.Unlock()
	return boxes
}

// saveStructureStarts provides starts and references without terrain or decoration.
func (g *OverworldGenerator) saveStructureStarts(c *Chunk) { g.saveStructures(c) }

func (g *NetherGenerator) saveStructureStarts(c *Chunk) {
	g.saveStructures(c)
	g.saveFossils(c)
}
