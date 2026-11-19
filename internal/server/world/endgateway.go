package world

import (
	"maps"

	"github.com/golem-mc/golem/nbt"
)

// TickEndGateways advances carried ages and returns positions due for a beam.
// vanilla: TheEndGatewayBlockEntity.portalTick
func (w *World) TickEndGateways(beamInterval int64) [][3]int32 {
	return w.TickEndGatewaysIn(beamInterval, nil)
}

// TickEndGatewaysIn is TickEndGateways over the chunks ticks accepts (nil: all).
func (w *World) TickEndGatewaysIn(beamInterval int64, ticks func(ChunkPos) bool) [][3]int32 {
	// ticks takes w.mu itself, so it is asked before the write lock: a reader
	// queued behind a pending writer would deadlock a nested RLock.
	var skip map[ChunkPos]bool
	if ticks != nil {
		skip = map[ChunkPos]bool{}
		for _, g := range w.TickingCarriedOfKind("minecraft:end_gateway") {
			pos := ChunkPos{X: g.Pos[0] >> 4, Z: g.Pos[2] >> 4}
			if _, done := skip[pos]; !done {
				skip[pos] = !ticks(pos)
			}
		}
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	var beams [][3]int32
	w.forTickingCarriedOfKind("minecraft:end_gateway", func(loaded *loadedChunk, i int, at CarriedAt) {
		if skip[ChunkPos{X: at.Pos[0] >> 4, Z: at.Pos[2] >> 4}] {
			return
		}
		age, _ := nbt.LongValue(at.Tag["Age"])
		age++
		// Readers and pending saves may still hold the previous compound.
		entry := maps.Clone(at.Tag)
		entry["Age"] = nbt.Long(age)
		loaded.chunk.carriedBlockEntities.Items[i] = entry
		loaded.markDirty()
		if age%beamInterval == 0 {
			beams = append(beams, at.Pos)
		}
	})
	return beams
}
