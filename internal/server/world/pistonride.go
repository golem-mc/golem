package world

import (
	"cmp"
	"maps"
	"slices"

	"github.com/golem-mc/golem/internal/protocol"
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
	rideProgress float64 // progress at the start of the current sweep
	customName   *protocol.Text
	pluginData   nbt.Compound
}

// codec reads and writes a moving piston's saved fields. Reloads skip the
// sweep and set createdTick to -1 to treat the ride as old for same-tick
// retraction. Saves and chunk update tags put the carried block at rest.
// vanilla: PistonMovingBlockEntity.loadAdditional, saveAdditional
func (r *pistonRide) codec(c *beCodec) {
	if !c.updateTag {
		field(c, "plugins", &r.pluginData, bePluginData, nil)
	}
	field(c, "CustomName", &r.customName, beText, nil)
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
	delete(c.pistonHandles, key)
	return true
}

// snapshotPistonRides copies the chunk's active rides into a fresh slice
// (nil when empty) so callers can sort and encode after releasing World.mu.
func (c *Chunk) snapshotPistonRides() []pistonRide {
	if len(c.pistonRides) == 0 {
		return nil
	}
	out := make([]pistonRide, 0, len(c.pistonRides))
	for _, r := range c.pistonRides {
		out = append(out, r)
	}
	return out
}

func sortPistonRides(rides []pistonRide) {
	if len(rides) <= 1 {
		return
	}
	slices.SortFunc(rides, func(a, b pistonRide) int {
		if d := cmp.Compare(a.y, b.y); d != 0 {
			return d
		}
		if d := cmp.Compare(a.z, b.z); d != 0 {
			return d
		}
		return cmp.Compare(a.x, b.x)
	})
}

// sortedPistonRides returns the chunk's active rides in deterministic order
// (nil when empty) for chunk saves and plugin enumeration.
func (c *Chunk) sortedPistonRides() []pistonRide {
	out := c.snapshotPistonRides()
	sortPistonRides(out)
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
		loaded.containerGen.Add(1)
	}
	delete(loaded.chunk.pistonHandles, entityKey(r.x, r.y, r.z))
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

// PistonBlockEntity identifies one loaded, active moving-piston ride.
type PistonBlockEntity struct {
	w     *World
	chunk *Chunk
	pos   [3]int32
}

// Pos returns the block position.
func (b *PistonBlockEntity) Pos() [3]int32 { return b.pos }

// PistonBlockEntity returns a live handle for an active moving-piston ride
// without loading a chunk.
func (w *World) PistonBlockEntity(x, y, z int32) *PistonBlockEntity {
	w.mu.Lock()
	defer w.mu.Unlock()
	l, ok := w.loadedAt(ChunkPos{X: x >> 4, Z: z >> 4})
	if !ok {
		return nil
	}
	if _, ok := l.chunk.pistonRide(x, y, z); !ok {
		return nil
	}
	return w.pistonHandle(l.chunk, x, y, z)
}

// PistonBlockEntities lists a live chunk's active moving-piston rides in
// deterministic order.
func (w *World) PistonBlockEntities(c *Chunk) []*PistonBlockEntity {
	w.mu.Lock()
	defer w.mu.Unlock()
	l, ok := w.loadedAt(ChunkPos{X: c.x, Z: c.z})
	if !ok || l.chunk != c || len(c.pistonRides) == 0 {
		return nil
	}
	rides := c.sortedPistonRides()
	out := make([]*PistonBlockEntity, 0, len(rides))
	for i := range rides {
		r := &rides[i]
		out = append(out, w.pistonHandle(c, r.x, r.y, r.z))
	}
	return out
}

func (w *World) pistonHandle(c *Chunk, x, y, z int32) *PistonBlockEntity {
	key := entityKey(x, y, z)
	if b := c.pistonHandles[key]; b != nil {
		return b
	}
	if c.pistonHandles == nil {
		c.pistonHandles = make(map[int64]*PistonBlockEntity)
	}
	b := &PistonBlockEntity{w: w, chunk: c, pos: [3]int32{x, y, z}}
	c.pistonHandles[key] = b
	return b
}

// loaded checks load and replacement identity with the world lock held.
func (b *PistonBlockEntity) loaded() (*loadedChunk, pistonRide, bool) {
	x, y, z := b.pos[0], b.pos[1], b.pos[2]
	l, ok := b.w.loadedAt(ChunkPos{X: x >> 4, Z: z >> 4})
	if !ok || l.chunk != b.chunk || l.chunk.pistonHandles[entityKey(x, y, z)] != b {
		return nil, pistonRide{}, false
	}
	r, ok := l.chunk.pistonRide(x, y, z)
	return l, r, ok
}

// IsValid reports whether the moving-piston ride is still live.
func (b *PistonBlockEntity) IsValid() bool {
	b.w.mu.RLock()
	defer b.w.mu.RUnlock()
	_, _, ok := b.loaded()
	return ok
}

// ReadPluginData visits one private value without copying unrelated values.
func (b *PistonBlockEntity) ReadPluginData(key string, read func(nbt.Tag)) {
	b.w.mu.RLock()
	defer b.w.mu.RUnlock()
	if _, r, ok := b.loaded(); ok {
		read(r.pluginData[key])
	}
}

// PluginDataKeys lists accepted private keys without a gameplay wake.
func (b *PistonBlockEntity) PluginDataKeys(keep func(nbt.Tag) bool) []string {
	b.w.mu.RLock()
	defer b.w.mu.RUnlock()
	_, r, ok := b.loaded()
	if !ok {
		return nil
	}
	return pluginDataKeys(r.pluginData, keep)
}

// SetPluginData changes one private value; nil deletes it and stale handles do nothing.
func (b *PistonBlockEntity) SetPluginData(key string, value nbt.Tag) {
	b.w.mu.Lock()
	defer b.w.mu.Unlock()
	l, r, ok := b.loaded()
	if !ok {
		return
	}
	data := maps.Clone(r.pluginData)
	setPluginData(&data, key, value)
	r.pluginData = data
	b.chunk.setPistonRide(r)
	l.markDirty()
}
