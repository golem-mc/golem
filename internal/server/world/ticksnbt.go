package world

import (
	"cmp"
	"slices"

	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// savedTick is a scheduled tick as a chunk file stores it: block or fluid,
// position, delay after the save, priority.
// vanilla: SavedTick (the i, x, y, z, t and p tags)
type savedTick struct {
	id       string
	x, y, z  int32
	delay    int32
	priority int32
	// seq is the booking order of a snapshot entry, for sorting; not saved.
	seq int64
}

// decodeSavedTicks reads block_ticks or fluid_ticks; an entry missing its id or
// position is dropped, and a negative delay is due at once.
// vanilla: SavedTick.codec
func decodeSavedTicks(root nbt.Compound, key string) []savedTick {
	list, ok := nbt.Get[nbt.List](root, key)
	if !ok {
		return nil
	}
	var out []savedTick
	for _, item := range list.Items {
		e, ok := nbt.CompoundView(item)
		if !ok {
			continue
		}
		id, okID := nbt.Get[nbt.String](e, "i")
		x, okX := nbt.Get[nbt.Int](e, "x")
		y, okY := nbt.Get[nbt.Int](e, "y")
		z, okZ := nbt.Get[nbt.Int](e, "z")
		if !okID || !okX || !okY || !okZ {
			continue
		}
		t, _ := nbt.Get[nbt.Int](e, "t")
		pr, _ := nbt.Get[nbt.Int](e, "p")
		out = append(out, savedTick{
			id: string(id), x: int32(x), y: int32(y), z: int32(z),
			delay: max(int32(t), 0), priority: int32(pr),
		})
	}
	return out
}

func encodeSavedTicks(ticks []savedTick) nbt.List {
	items := make([]nbt.Tag, len(ticks))
	for i, t := range ticks {
		items[i] = nbt.Compound{
			"i": nbt.String(t.id),
			"x": nbt.Int(t.x), "y": nbt.Int(t.y), "z": nbt.Int(t.z),
			"t": nbt.Int(t.delay), "p": nbt.Int(t.priority),
		}
	}
	return nbt.List{Elem: nbt.TypeCompound, Items: items}
}

// chunkTicks is a chunk's pending ticks in the two lists a file keeps.
type chunkTicks struct {
	block, fluid []savedTick
}

// loadedTick is a tick read from a chunk file, waiting for the physics engine
// to take it.
type loadedTick struct {
	savedTick
	fluid bool
}

// ticksOfChunk snapshots the chunk's saved ticks, using the source set by
// NewPhysics when present.
// Called with no world lock held: the engine's lock is taken before the
// world's.
func (w *World) ticksOfChunk(pos ChunkPos) chunkTicks {
	w.mu.RLock()
	if source := w.tickSource; source != nil {
		w.mu.RUnlock()
		return source(pos)
	}
	out := w.loadedTicksOfChunk(pos)
	w.mu.RUnlock()
	return out
}

// loadedTicksOfChunk copies the ticks still waiting for physics to adopt
// them. The caller holds w.mu.
func (w *World) loadedTicksOfChunk(pos ChunkPos) chunkTicks {
	var out chunkTicks
	for _, t := range w.pendingLoaded {
		if t.x>>4 != pos.X || t.z>>4 != pos.Z {
			continue
		}
		if t.fluid {
			out.fluid = append(out.fluid, t.savedTick)
		} else {
			out.block = append(out.block, t.savedTick)
		}
	}
	return out
}

// takeLoadedTicks returns and clears the ticks loaded from files and the chunks
// unloaded since the last call.
func (w *World) takeLoadedTicks() (loaded []loadedTick, unloaded []ChunkPos) {
	w.mu.Lock()
	defer w.mu.Unlock()
	loaded, unloaded = w.pendingLoaded, w.unloadedChunks
	w.pendingLoaded, w.unloadedChunks = nil, nil
	return loaded, unloaded
}

// queueLoadedTicks moves a chunk's loaded ticks to the world's hand-over list.
// The caller holds w.mu.
func (w *World) queueLoadedTicks(c *Chunk) {
	w.pendingLoaded = append(w.pendingLoaded, c.loadedTicks...)
	c.loadedTicks = nil
}

// dropChunkTicks notes the chunk left memory: its queued ticks go too, unsaved
// ones included. The caller holds w.mu.
func (w *World) dropChunkTicks(pos ChunkPos) {
	kept := w.pendingLoaded[:0]
	for _, t := range w.pendingLoaded {
		if t.x>>4 != pos.X || t.z>>4 != pos.Z {
			kept = append(kept, t)
		}
	}
	w.pendingLoaded = kept
	w.unloadedChunks = append(w.unloadedChunks, pos)
}

// prepareTick takes the unloads that must precede a new booking, with
// takeLoaded the loaded ticks too, and, for a scheduled tick, flags its chunk
// for a save. Vanilla does not mark a chunk unsaved for a new tick, losing
// ticks booked after the last save on shutdown.
func (w *World) prepareTick(x, z int32, dirty, takeLoaded bool) (loaded []loadedTick, unloaded []ChunkPos) {
	pos := ChunkPos{X: x >> 4, Z: z >> 4}
	w.mu.Lock()
	unloaded, w.unloadedChunks = w.unloadedChunks, nil
	if takeLoaded {
		loaded, w.pendingLoaded = w.pendingLoaded, nil
	}
	if c, ok := w.chunks[pos]; ok && dirty {
		c.markDirty()
	}
	w.mu.Unlock()
	return loaded, unloaded
}

// chunkTicks is physics' pending ticks inside the chunk, as the file lists
// them; piston-event entries are block events, not ticks, and are not saved.
func (p *Physics) chunkTicks(pos ChunkPos) chunkTicks {
	p.mu.Lock()
	// Snapshot both lists under p.mu so adoption cannot move an entry
	// between the two reads. The loaded entries retain their file order
	// and precede any new bookings.
	p.w.mu.RLock()
	out := p.w.loadedTicksOfChunk(pos)
	p.w.mu.RUnlock()
	pendingBlocks, pendingFluids := len(out.block), len(out.fluid)
	add := func(t scheduledTick) {
		if t.pos.x>>4 != pos.X || t.pos.z>>4 != pos.Z || p.tickCancelled(t) || !isTickEvent(t.event) {
			return
		}
		st := savedTick{
			x: t.pos.x, y: t.pos.y, z: t.pos.z,
			delay: int32(max(t.due-p.now, 0)), priority: int32(t.priority), seq: t.seq,
		}
		if t.fluid != 0 {
			st.id = t.fluid.name()
			out.fluid = append(out.fluid, st)
			return
		}
		st.id = bookedBlockName(t)
		out.block = append(out.block, st)
	}
	for _, t := range p.queue {
		add(t)
	}
	for _, t := range p.parked[pos] {
		add(t)
	}
	p.mu.Unlock()
	// The heap's backing slice is in heap order, not booking order; the
	// reload hands out sub-tick order from the saved list, and due and
	// priority are restored from each entry. Sorted after the unlock so
	// Physics.Tick is not held up.
	// vanilla: LevelChunkTicks.pack
	bySeq := func(a, b savedTick) int { return cmp.Compare(a.seq, b.seq) }
	slices.SortFunc(out.block[pendingBlocks:], bySeq)
	slices.SortFunc(out.fluid[pendingFluids:], bySeq)
	return out
}

// isTickEvent reports whether a queue entry tagged event is a scheduled tick
// (block or fluid); the rest are piston events, block events in vanilla.
func isTickEvent(event uint8) bool {
	switch event {
	case 0, leafTickEvent, coralTickEvent, redstoneTickEvent, sculkTickEvent, lightningRodTickEvent,
		driedGhastHydrationEvent, frogspawnHatchEvent, snifferEggHatchEvent, cauldronDripEvent, survivalTickEvent,
		scaffoldingTickEvent, dirtPathTickEvent, farmlandTickEvent:
		return true
	}
	return false
}

// fluidType is a fluid as vanilla's registry tells them apart: water and
// flowing water are two, as are lava and flowing lava. Zero is none.
type fluidType uint8

// fluidBook is a fluid visit's booking: one per position and type.
type fluidBook struct {
	pos blockKey
	typ fluidType
}

func fluidTypeOf(fs fluidState) fluidType {
	t := fluidType(fs.kind) << 1
	if fs.isSource() {
		t |= 1
	}
	return t
}

func (t fluidType) name() string {
	name := "minecraft:water"
	if fluidKind(t>>1) == fluidLava {
		name = "minecraft:lava"
	}
	if t&1 != 0 {
		return name
	}
	return "minecraft:flowing_" + name[len("minecraft:"):]
}

// fluidTypeNamed is the type a saved fluid tick names, zero for an unknown one.
func fluidTypeNamed(name string) fluidType {
	for _, kind := range []fluidKind{fluidWater, fluidLava} {
		for _, src := range []fluidType{0, 1} {
			if t := fluidType(kind)<<1 | src; t.name() == name {
				return t
			}
		}
	}
	return 0
}

// adoptLoadedTicks drops the ticks of unloaded chunks and queues loaded ones; a
// tick whose block changed since booking is dropped, as vanilla's tick would
// find another block. Runs under p.mu.
// vanilla: ServerLevel.tickBlock
func (p *Physics) adoptLoadedTicks() {
	p.adoptTicks(p.w.takeLoadedTicks())
}

// adoptTicks discards the unloaded chunks' ticks and then queues loaded.
// Runs under p.mu.
func (p *Physics) adoptTicks(loaded []loadedTick, unloaded []ChunkPos) {
	p.discardUnloadedTicks(unloaded)
	for _, t := range loaded {
		due := p.now + int64(t.delay)
		if t.fluid {
			// The booking keeps its saved type whatever the cell holds now;
			// dispatch checks the cell.
			// vanilla: LevelChunkTicks.<init>, ServerLevel.tickFluid
			if typ := fluidTypeNamed(t.id); typ != 0 {
				p.insertTick(blockKey{t.x, t.y, t.z}, typ, due, int(t.priority))
			}
			continue
		}
		p.adoptBlockTick(t.x, t.y, t.z, p.w.BlockAt(t.x, t.y, t.z), t.id, due, int(t.priority))
	}
}

// discardUnloadedTicks removes old bookings before any new booking can reuse
// their positions, including between game ticks while the world is frozen.
// Runs under p.mu.
// vanilla: LevelTicks.removeContainer
func (p *Physics) discardUnloadedTicks(unloaded []ChunkPos) {
	if len(unloaded) > 0 {
		gone := make(map[ChunkPos]bool, len(unloaded))
		for _, pos := range unloaded {
			gone[pos] = true
		}
		for _, pos := range unloaded {
			for _, t := range p.parked[pos] {
				if p.tickCancelled(t) {
					delete(p.cancelled, t.seq)
				} else {
					p.forgetBooking(t)
				}
			}
			delete(p.parked, pos)
		}
		kept := p.queue[:0]
		for _, t := range p.queue {
			if p.tickCancelled(t) {
				delete(p.cancelled, t.seq)
				continue
			}
			if gone[ChunkPos{X: t.pos.x >> 4, Z: t.pos.z >> 4}] {
				p.forgetBooking(t)
				continue
			}
			kept = append(kept, t)
		}
		p.queue = kept
		p.queue.init()
	}
}

// adoptBlockTick books a block tick for the block named id at a cell holding
// state, tagged and booked as that block's own ticks are; apart from a soil
// block's, which dispatch checks, it is dropped when the cell holds another
// block. Runs under p.mu.
func (p *Physics) adoptBlockTick(x, y, z int32, state uint16, id string, due int64, priority int) {
	// vanilla: TickPriority.byValue clamps an out-of-range saved priority.
	priority = max(tickPriorityExtremelyHigh, min(priority, 3))
	// vanilla: ServerLevel.tickBlock checks the cell's block against the booked type.
	switch id {
	case registry.BlockDirtPath.Name():
		p.scheduleSoil(registry.BlockDirtPath, x, y, z, due, priority)
		return
	case registry.BlockFarmland.Name():
		p.scheduleSoil(registry.BlockFarmland, x, y, z, due, priority)
		return
	}
	name, _, ok := registry.BlockStateName(state)
	if !ok || name != id {
		return
	}
	switch block := registry.BlockOf(state); {
	case block == registry.BlockMovingPiston:
		// A ride's landing whatever priority it was saved at.
		// vanilla: PistonMovingBlockEntity.tick
		p.schedulePistonFinalize(x, y, z, due)
	case block == registry.BlockFrogspawn:
		p.scheduleBooked(p.frogspawnBooked, x, y, z, due, frogspawnHatchEvent)
	case block == registry.BlockSnifferEgg:
		p.scheduleBooked(p.snifferEggBooked, x, y, z, due, snifferEggHatchEvent)
	case block == registry.BlockDriedGhast:
		p.scheduleDriedGhast(x, y, z, due)
	case block == registry.BlockCauldron, block == registry.BlockWaterCauldron:
		p.scheduleBooked(p.cauldronBooked, x, y, z, due, cauldronDripEvent)
	case block.IsLeaves():
		p.scheduleLeaf(x, y, z, due)
	case block == registry.BlockScaffolding:
		p.scheduleScaffolding(x, y, z, due)
	case isLivingCoral(block):
		p.scheduleBooked(p.coralBooked, x, y, z, due, coralTickEvent)
	case isSculkSensor(block), name == sculkShrieker:
		p.scheduleBooked(p.sculkBooked, x, y, z, due, sculkTickEvent)
	case block == registry.BlockBigDripleaf, block == registry.BlockDetectorRail:
		p.scheduleBooked(p.redstoneBooked, x, y, z, due, redstoneTickEvent)
	case isLightningRod(state):
		p.scheduleBooked(p.rodBooked, x, y, z, due, lightningRodTickEvent)
	default:
		if s := survivalOf[state]; s.tick != 0 {
			p.scheduleSurvival(x, y, z, due)
			if s.kind == surviveSpeleothem && s.dir == dirUp {
				// vanilla: SpeleothemBlock.tick drops a hanging stalactite on any block tick.
				p.fallBooked[blockKey{x, y, z}] = true
			}
			return
		}
		p.schedulePriority(x, y, z, due, priority)
	}
}

// forgetBooking clears the bookkeeping a dropped queue entry held.
func (p *Physics) forgetBooking(t scheduledTick) {
	switch t.event {
	case frogspawnHatchEvent:
		delete(p.frogspawnBooked, t.pos)
	case snifferEggHatchEvent:
		delete(p.snifferEggBooked, t.pos)
	case driedGhastHydrationEvent:
		delete(p.ghastBooked, t.pos)
	case cauldronDripEvent:
		delete(p.cauldronBooked, t.pos)
	case leafTickEvent:
		delete(p.leafBooked, t.pos)
	case scaffoldingTickEvent:
		delete(p.scaffoldingBooked, t.pos)
	case dirtPathTickEvent:
		delete(p.dirtPathBooked, t.pos)
	case farmlandTickEvent:
		delete(p.farmlandBooked, t.pos)
	case coralTickEvent:
		delete(p.coralBooked, t.pos)
	case redstoneTickEvent:
		delete(p.redstoneBooked, t.pos)
	case sculkTickEvent:
		delete(p.sculkBooked, t.pos)
	case lightningRodTickEvent:
		delete(p.rodBooked, t.pos)
	case survivalTickEvent:
		delete(p.survivalBooked, t.pos)
		delete(p.fallBooked, t.pos)
	case 0:
		if t.fluid != 0 {
			delete(p.fluidBooked, fluidBook{t.pos, t.fluid})
			return
		}
		delete(p.pending, t.pos)
		if t.priority == tickPriorityFinalize {
			delete(p.finalizeSeq, t.pos)
		}
	default:
		delete(p.pendingEvent, t.pos)
	}
}
