package world

import (
	"cmp"
	"slices"

	"github.com/golem-mc/golem/nbt"
)

// pistonRide is a moving piston's transient block entity state, stored on
// Chunk separately from Chunk.entities so piston moves do not allocate a
// full BlockEntity or churn World.blockEntityOrder.
// vanilla: PistonMovingBlockEntity
type pistonRide struct {
	x, y, z      int32
	movedState   uint16
	dir          int8
	extending    bool
	isSource     bool
	createdTick  int64
	rideProgress float64
}

// codec walks a moving piston's ride in vanilla's shape: the block state
// as its codec writes it, the facing as the legacy direction byte. The
// progress is written finished and never read: the engine holds a move for
// two whole ticks without animating, so a reloaded move finishes where it
// was. The same-tick retraction's age check reads a creation tick no file
// carries, so a reloaded move is old.
// vanilla: PistonMovingBlockEntity.loadAdditional, saveAdditional
func (r *pistonRide) codec(c *beCodec) {
	field(c, BlockStateKeyPiston, &r.movedState, beMovedState, 0)
	field(c, "facing", &r.dir, beLegacyDirection, 0)
	if c.load {
		r.createdTick = -1
	} else {
		c.out.Set("progress", nbt.Float(1))
	}
	field(c, "extending", &r.extending, beFlag, false)
	field(c, "source", &r.isSource, beFlag, false)
}

func (r *pistonRide) loadFrom(entry nbt.Compound) {
	r.movedState, r.dir, r.extending, r.isSource = 0, 0, false, false
	r.createdTick, r.rideProgress = 0, 0
	r.codec(&beCodec{load: true, in: entry})
}

// saveTag builds the moving piston's saved compound in vanilla's shape.
// vanilla: BlockEntity.saveWithFullMetadata
func (r *pistonRide) saveTag() *nbt.OrderedCompound {
	tag := nbt.NewOrderedCompound(8)
	r.codec(&beCodec{out: tag})
	tag.Set("components", nbt.Compound{})
	tag.Set("id", nbt.String(KindPiston.Name()))
	tag.Set("x", nbt.Int(r.x))
	tag.Set("y", nbt.Int(r.y))
	tag.Set("z", nbt.Int(r.z))
	return tag
}

// updateTag builds the moving piston's chunk-packet update tag.
// vanilla: PistonMovingBlockEntity.getUpdateTag
func (r *pistonRide) updateTag() nbt.Compound {
	c := nbt.Compound{}
	r.codec(&beCodec{out: c, updateTag: true})
	return c
}

// decodePistonRide reads one "block_entities" entry if it is a moving piston.
func decodePistonRide(entry nbt.Compound) (pistonRide, bool) {
	id, ok := nbt.Get[nbt.String](entry, "id")
	if !ok || string(id) != KindPiston.Name() {
		return pistonRide{}, false
	}
	x, y, z := carriedPos(entry)
	r := pistonRide{x: x, y: y, z: z}
	r.loadFrom(entry)
	return r, true
}

func (c *Chunk) setPistonRide(r pistonRide) {
	if c.pistonRides == nil {
		c.pistonRides = make(map[int64]pistonRide)
	}
	c.pistonRides[entityKey(r.x, r.y, r.z)] = r
}

func (c *Chunk) pistonRide(x, y, z int32) (pistonRide, bool) {
	r, ok := c.pistonRides[entityKey(x, y, z)]
	return r, ok
}

func (c *Chunk) removePistonRide(x, y, z int32) bool {
	key := entityKey(x, y, z)
	if _, ok := c.pistonRides[key]; !ok {
		return false
	}
	delete(c.pistonRides, key)
	return true
}

// sortedPistonRides returns the chunk's active rides in deterministic order
// (nil when empty) for chunk saves and chunk packets.
func (c *Chunk) sortedPistonRides() []pistonRide {
	if len(c.pistonRides) == 0 {
		return nil
	}
	out := make([]pistonRide, 0, len(c.pistonRides))
	for _, r := range c.pistonRides {
		out = append(out, r)
	}
	slices.SortFunc(out, func(a, b pistonRide) int {
		if d := cmp.Compare(a.y, b.y); d != 0 {
			return d
		}
		if d := cmp.Compare(a.z, b.z); d != 0 {
			return d
		}
		return cmp.Compare(a.x, b.x)
	})
	return out
}

func (c *Chunk) encodePistonRides() []nbt.Tag {
	rides := c.sortedPistonRides()
	if len(rides) == 0 {
		return nil
	}
	out := make([]nbt.Tag, len(rides))
	for i := range rides {
		out[i] = rides[i].saveTag()
	}
	return out
}

// setPistonRide stores a moving-piston ride in a loaded chunk, replacing any
// existing block entity at its position.
func (w *World) setPistonRide(r pistonRide) bool {
	pos := ChunkPos{X: r.x >> 4, Z: r.z >> 4}
	w.mu.Lock()
	defer w.mu.Unlock()
	loaded, ok := w.chunks[pos]
	if !ok {
		return false
	}
	if old := loaded.chunk.removeBlockEntity(r.x, r.y, r.z); old != nil {
		w.removeBlockEntityOrder(old)
		old.removed.Store(true)
		loaded.containerGen.Add(1)
	}
	loaded.chunk.setPistonRide(r)
	loaded.markDirty()
	return true
}

// pistonRideAt returns the moving-piston ride at (x, y, z), false if none or unloaded.
func (w *World) pistonRideAt(x, y, z int32) (pistonRide, bool) {
	pos := ChunkPos{X: x >> 4, Z: z >> 4}
	w.mu.RLock()
	defer w.mu.RUnlock()
	loaded, ok := w.loadedAt(pos)
	if !ok {
		return pistonRide{}, false
	}
	return loaded.chunk.pistonRide(x, y, z)
}

// stepPistonRide updates a moving-piston ride's progress for tick now and
// returns the updated ride and its age in ticks, false if none or unloaded.
func (w *World) stepPistonRide(x, y, z int32, now int64) (pistonRide, int64, bool) {
	pos := ChunkPos{X: x >> 4, Z: z >> 4}
	w.mu.Lock()
	defer w.mu.Unlock()
	loaded, ok := w.loadedAt(pos)
	if !ok {
		return pistonRide{}, 0, false
	}
	r, ok := loaded.chunk.pistonRide(x, y, z)
	if !ok {
		return pistonRide{}, 0, false
	}
	age := now - r.createdTick
	r.rideProgress = min(max(float64(age)*pistonStep, 0), 1)
	loaded.chunk.setPistonRide(r)
	return r, age, true
}

// setPistonRideProgress sets a moving-piston ride's progress directly (for tests).
func (w *World) setPistonRideProgress(x, y, z int32, progress float64) bool {
	pos := ChunkPos{X: x >> 4, Z: z >> 4}
	w.mu.Lock()
	defer w.mu.Unlock()
	loaded, ok := w.loadedAt(pos)
	if !ok {
		return false
	}
	r, ok := loaded.chunk.pistonRide(x, y, z)
	if !ok {
		return false
	}
	r.rideProgress = progress
	loaded.chunk.setPistonRide(r)
	return true
}
