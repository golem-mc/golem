package world

import (
	"cmp"
	"errors"
	"log/slog"
	"slices"

	"github.com/golem-mc/golem/nbt"
)

// chunkTicketsFile is where vanilla keeps a dimension's chunk tickets, under its
// storage folder. vanilla: TicketStorage.TYPE
const chunkTicketsFile = "data/minecraft/chunk_tickets.dat"

const (
	// forcedTicketType is the ticket type /forceload adds. vanilla: TicketType.FORCED
	forcedTicketType = "minecraft:forced"
	// forcedTicketLevel is the level of a forced chunk's ticket: entity
	// ticking. vanilla: ChunkMap.FORCED_TICKET_LEVEL, ChunkLevel.ENTITY_TICKING_LEVEL
	forcedTicketLevel = 31
	// MaxForceLoadChunks caps the chunks one /forceload add or remove names.
	// vanilla: ForceLoadCommand.MAX_CHUNK_LIMIT
	MaxForceLoadChunks = 256
)

// forcedReach is how far from a forced chunk the chunks stay loaded: the
// 8 neighbours block-tick and the ring beyond them is the loaded border.
// vanilla: ChunkLevel.FULL_CHUNK_LEVEL (33) minus ChunkMap.FORCED_TICKET_LEVEL (31)
const forcedReach = 2

// forcedChunks is the chunks /forceload keeps loaded in one dimension; each
// holds its whole forcedFootprint with one Acquire per chunk while forced.
type forcedChunks struct {
	set map[ChunkPos]struct{}
	// others are saved tickets of other types, carried to the next write
	// untouched.
	others []nbt.Tag
	dirty  bool
}

// loadForcedChunks reads the dimension's ticket file; a missing or unusable
// one reads as no tickets.
func (w *World) loadForcedChunks() {
	w.forced = &forcedChunks{set: map[ChunkPos]struct{}{}}
	data, err := readSavedData(w.storage, chunkTicketsFile)
	switch {
	case errors.Is(err, errNoSavedData):
		return
	case err != nil:
		slog.Warn("could not read the chunk tickets", "dimension", w.dimName, "error", err)
		return
	}
	list, _ := nbt.Get[nbt.List](data, "tickets")
	for _, tag := range list.Items {
		entry, ok := tag.(nbt.Compound)
		if !ok {
			continue
		}
		typ, _ := nbt.Get[nbt.String](entry, "type")
		pos, _ := nbt.Get[nbt.IntArray](entry, "chunk_pos")
		if typ != forcedTicketType || len(pos) != 2 {
			w.forced.others = append(w.forced.others, entry)
			continue
		}
		w.forced.set[ChunkPos{X: pos[0], Z: pos[1]}] = struct{}{}
	}
}

// forcedFootprint is the chunks a forced chunk at pos keeps loaded.
func forcedFootprint(pos ChunkPos) []ChunkPos {
	out := make([]ChunkPos, 0, (2*forcedReach+1)*(2*forcedReach+1))
	for dz := -forcedReach; dz <= forcedReach; dz++ {
		for dx := -forcedReach; dx <= forcedReach; dx++ {
			out = append(out, ChunkPos{X: pos.X + int32(dx), Z: pos.Z + int32(dz)})
		}
	}
	return out
}

// HoldForcedChunks loads every forced chunk with its footprint and keeps
// them held, which the server runs once at startup. It returns how many
// distinct chunks that is.
// vanilla: MinecraftServer.prepareLevels
func (w *World) HoldForcedChunks() int {
	var held []ChunkPos
	distinct := map[ChunkPos]struct{}{}
	for _, pos := range w.ForcedChunks() {
		for _, c := range forcedFootprint(pos) {
			held = append(held, c)
			distinct[c] = struct{}{}
		}
	}
	for range w.AcquireMany(held) {
	}
	return len(distinct)
}

// SetChunkForced forces or releases the chunk at pos, reporting whether that
// changed anything. A forced chunk is loaded at once, with its footprint, and
// stays loaded.
// vanilla: ServerLevel.setChunkForced
func (w *World) SetChunkForced(pos ChunkPos, forced bool) bool {
	w.mu.Lock()
	_, was := w.forced.set[pos]
	if was == forced {
		w.mu.Unlock()
		return false
	}
	if forced {
		w.forced.set[pos] = struct{}{}
	} else {
		delete(w.forced.set, pos)
	}
	w.forced.dirty = true
	w.mu.Unlock()
	if forced {
		for range w.AcquireMany(forcedFootprint(pos)) {
		}
	} else {
		for _, c := range forcedFootprint(pos) {
			w.Release(c)
		}
	}
	return true
}

// ForcedChunks lists the forced chunks in /forceload query's order: sorted
// packed longs, so by z, then x as unsigned 32-bit.
// vanilla: ForceLoadCommand.listForceLoad, ChunkPos.pack
func (w *World) ForcedChunks() []ChunkPos {
	w.mu.RLock()
	out := make([]ChunkPos, 0, len(w.forced.set))
	for pos := range w.forced.set {
		out = append(out, pos)
	}
	w.mu.RUnlock()
	slices.SortFunc(out, func(a, b ChunkPos) int {
		if c := cmp.Compare(a.Z, b.Z); c != 0 {
			return c
		}
		return cmp.Compare(uint32(a.X), uint32(b.X))
	})
	return out
}

// AnyChunkForced reports whether any chunk is forced.
func (w *World) AnyChunkForced() bool {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return len(w.forced.set) > 0
}

// ChunkForced reports whether the chunk at pos is forced.
func (w *World) ChunkForced(pos ChunkPos) bool {
	w.mu.RLock()
	defer w.mu.RUnlock()
	_, ok := w.forced.set[pos]
	return ok
}

// saveForcedChunks writes the ticket file when the forced set changed since
// the last write.
func (w *World) saveForcedChunks() {
	w.mu.Lock()
	if !w.forced.dirty {
		w.mu.Unlock()
		return
	}
	w.forced.dirty = false
	w.mu.Unlock()
	items := slices.Clone(w.forced.others)
	for _, pos := range w.ForcedChunks() {
		items = append(items, nbt.Compound{
			"chunk_pos": nbt.IntArray{pos.X, pos.Z},
			"type":      nbt.String(forcedTicketType),
			"level":     nbt.Int(forcedTicketLevel),
		})
	}
	data := nbt.Compound{"tickets": nbt.List{Elem: nbt.TypeCompound, Items: items}}
	if err := writeSavedData(w.storage, chunkTicketsFile, data); err != nil {
		slog.Error("could not save the chunk tickets", "dimension", w.dimName, "error", err)
		w.mu.Lock()
		w.forced.dirty = true
		w.mu.Unlock()
	}
}
