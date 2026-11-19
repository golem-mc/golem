package world

import (
	"slices"

	"github.com/golem-mc/golem/registry"
)

// maxLightCells caps the cells a chunk remembers between relights; past it the
// chunk relights whole columns.
const maxLightCells = 512

// emitFlag marks a queue entry that lights its cell to the entry's value
// instead of spreading the cell's own.
const emitFlag uint8 = 0x80

// lightCell is a block whose write the light engine reads differently.
type lightCell struct{ x, y, z int32 }

// markLightCell records a write at section-local x, z and y counted from the
// world's bottom.
func (c *Chunk) markLightCell(x, y, z int32) {
	if len(c.lightCells) >= maxLightCells {
		c.lightOverflow = true
		return
	}
	c.lightCells = append(c.lightCells, uint32(x)|uint32(z)<<4|uint32(y)<<8)
}

// dropLightCells forgets the cells in the local column box, whose marks were
// just cleared.
func (c *Chunk) dropLightCells(lx0, lx1, lz0, lz1 int32) {
	c.lightCells = slices.DeleteFunc(c.lightCells, func(p uint32) bool {
		lx, lz := int32(p&15), int32(p>>4&15)
		return lx >= lx0 && lx <= lx1 && lz >= lz0 && lz <= lz1
	})
	if c.lightDirty == ([sectionSize]uint16{}) {
		c.lightCells, c.lightOverflow = nil, false
	}
}

// dirtyCells returns the cells written in the box since the last relight
// covering them, false when a mark has no cell behind it and the box must
// relight whole columns. The caller holds w.mu.
func (w *World) dirtyCells(minX, minZ, maxX, maxZ int32) ([]lightCell, bool) {
	var cells []lightCell
	for cx := floorDiv(minX, sectionSize); cx <= floorDiv(maxX, sectionSize); cx++ {
		for cz := floorDiv(minZ, sectionSize); cz <= floorDiv(maxZ, sectionSize); cz++ {
			loaded, ok := w.chunks[ChunkPos{cx, cz}]
			if !ok {
				continue
			}
			c := loaded.chunk
			lx0, lx1 := max(minX-cx*16, 0), min(maxX-cx*16, 15)
			lz0, lz1 := max(minZ-cz*16, 0), min(maxZ-cz*16, 15)
			mask := uint16(0xFFFF<<lz0) & uint16(0xFFFF>>(15-lz1))
			var covered [sectionSize]uint16
			for _, p := range c.lightCells {
				lx, lz := int32(p&15), int32(p>>4&15)
				if lx < lx0 || lx > lx1 || lz < lz0 || lz > lz1 {
					continue
				}
				covered[lx] |= 1 << lz
				cells = append(cells, lightCell{cx*16 + lx, int32(p>>8) + worldMinY, cz*16 + lz})
			}
			for lx := lx0; lx <= lx1; lx++ {
				marked := c.lightDirty[lx] & mask
				if marked&^covered[lx] != 0 || (marked != 0 && c.lightOverflow) {
					return nil, false
				}
			}
		}
	}
	return cells, true
}

// cellsBox is the smallest column box holding every cell.
func cellsBox(cells []lightCell) (minX, minZ, maxX, maxZ int32) {
	minX, minZ, maxX, maxZ = cells[0].x, cells[0].z, cells[0].x, cells[0].z
	for _, c := range cells[1:] {
		minX, maxX = min(minX, c.x), max(maxX, c.x)
		minZ, maxZ = min(minZ, c.z), max(maxZ, c.z)
	}
	return
}

// allDirs is every direction bit of a queue entry's layer byte: bit dir+1
// says the entry propagates along dir.
// vanilla: LightEngine.QueueEntry
const allDirs = 0x7E

func dirBit(dir int32) int { return 2 << dir }

var lightNeighbors = [6]struct{ dx, dy, dz, dir int32 }{
	{0, -1, 0, dirDown},
	{0, 1, 0, dirUp},
	{0, 0, -1, dirNorth},
	{0, 0, 1, dirSouth},
	{-1, 0, 0, dirWest},
	{1, 0, 0, dirEast},
}

// relightCells restores the fixpoint after the cells changed, touching only
// light that depended on them: the old blocks' light is removed, then spread
// back from whatever still holds it. The caller has exclusive use of the world's light.
// vanilla: LightEngine.runLightUpdates, BlockLightEngine.checkNode, SkyLightEngine.checkNode
func (w *World) relightCells(cells []lightCell) {
	var touched []*Chunk
	ctx := &lightCtx{w: w, touched: &touched}
	dq := lightQueues.Get().(*lightQueue)
	defer dq.release()
	iq := lightQueues.Get().(*lightQueue)
	defer iq.release()

	tops := make(map[[2]int32]int32, len(cells))
	for _, cl := range cells {
		col := [2]int32{cl.x, cl.z}
		if y, ok := tops[col]; !ok || cl.y > y {
			tops[col] = cl.y
		}
	}
	for _, cl := range cells {
		col := [2]int32{cl.x, cl.z}
		if y, ok := tops[col]; ok {
			delete(tops, col)
			ctx.updateSkySources(cl.x, y, cl.z, dq, iq)
		}
	}
	for _, cl := range cells {
		ctx.checkCell(cl, dq, iq)
	}
	ctx.runDecreases(dq, iq)
	ctx.runIncreases(iq)

	// touched holds the chunks a cell was written in; the cells' own chunks
	// follow, whose light the run may have left unchanged.
	written := len(touched)
	for _, cl := range cells {
		if c, ok := ctx.resolve(cl.x, cl.z); ok && !slices.Contains(touched, c) {
			touched = append(touched, c)
		}
	}
	for i, c := range touched {
		c.lightCorrect = true
		c.lightGen++
		if i >= written && c.hasLightForms() {
			continue
		}
		c.lightFormsStale = true
	}
}

// putLight sets the cell to v, lower or higher than it held.
func (ctx *lightCtx) putLight(layer int, x, y, z int32, v uint8) {
	c, ok := ctx.resolve(x, z)
	if !ok {
		return
	}
	si := lightSectionOf(y)
	if si < 0 || si >= lightSections {
		return
	}
	i, high := lightNibbleIndex(x&15, lightLocalY(y), z&15)
	var old uint8
	if s := ctx.layer(c, layer)[si]; s != nil {
		old = s[i] & 0x0F
		if high {
			old = s[i] >> 4
		}
	}
	if old == v {
		return
	}
	s := ctx.writable(c, layer, si)
	if high {
		s[i] = s[i]&0x0F | v<<4
	} else {
		s[i] = s[i]&0xF0 | v&0x0F
	}
	ctx.touch(c)
}

// updateSkySources moves the column's sky sources to where its blocks put them:
// lost sources are cleared and queued for decrease, new ones lit and queued
// where they can light a neighbour. top is the highest changed y of the column.
// vanilla: SkyLightEngine.updateSourcesInColumn
func (ctx *lightCtx) updateSkySources(x, top, z int32, dq, iq *lightQueue) {
	c, ok := ctx.resolve(x, z)
	if !ok || !contains(top) {
		return
	}
	lx, lz := x&15, z&15
	sky := ctx.layer(c, skyLayer)
	var topState uint16 = airState
	if contains(top + 1) {
		// No source above the changed cells: none below them before or after.
		if c.lightGet(sky, lx, top+1, lz) != 15 {
			return
		}
		topState = c.sharedBlockAt(lx, top+1, lz)
	}
	newFloor := top + 1
	for y := top; y >= worldMinY; y-- {
		state := c.sharedBlockAt(lx, y, lz)
		if (state != airState || topState != airState) &&
			(registry.LightDampening(state) != 0 || registry.LightFacesSeal(topState, dirDown, state)) {
			break
		}
		newFloor = y
		topState = state
	}
	oldFloor := top + 1
	for oldFloor > worldMinY && c.lightGet(sky, lx, oldFloor-1, lz) == 15 {
		oldFloor--
	}
	for y := oldFloor; y < newFloor; y++ {
		ctx.putLight(skyLayer, x, y, z, 0)
		mask := allDirs &^ dirBit(dirUp)
		if y == newFloor-1 {
			mask = allDirs
		}
		dq.push(x, y, z, 15, skyLayer|mask)
	}
	for y := newFloor; y < oldFloor; y++ {
		ctx.putLight(skyLayer, x, y, z, 15)
		if y == newFloor || ctx.sideDimmer(x, y, z, 14) {
			iq.push(x, y, z, 15, skyLayer|allDirs&^dirBit(dirUp))
		}
	}
}

// sideDimmer reports a loaded side neighbour holding sky light below v.
func (ctx *lightCtx) sideDimmer(x, y, z int32, v uint8) bool {
	for _, n := range lightNeighbors[2:] {
		nx, nz := x+n.dx, z+n.dz
		if _, ok := ctx.resolve(nx, nz); ok && ctx.getLight(skyLayer, nx, y, nz) < v {
			return true
		}
	}
	return false
}

// checkCell queues the light work for a cell whose block changed.
func (ctx *lightCtx) checkCell(cl lightCell, dq, iq *lightQueue) {
	state, ok := ctx.blockAt(cl.x, cl.y, cl.z)
	if !ok || !contains(cl.y) {
		return
	}
	emission := uint8(registry.LightEmission(state))
	old := ctx.getLight(blockLayer, cl.x, cl.y, cl.z)
	if emission < old {
		ctx.putLight(blockLayer, cl.x, cl.y, cl.z, 0)
		dq.push(cl.x, cl.y, cl.z, old, blockLayer|allDirs)
	} else {
		ctx.pullIn(cl, blockLayer, iq)
	}
	if emission > 0 {
		iq.push(cl.x, cl.y, cl.z, emission, blockLayer|allDirs|int(emitFlag))
	}

	old = ctx.getLight(skyLayer, cl.x, cl.y, cl.z)
	switch {
	case old == 15: // a sky source, which the column pass settled
		dq.push(cl.x, cl.y, cl.z, 15, skyLayer|allDirs&^dirBit(dirUp))
		iq.push(cl.x, cl.y, cl.z, 15, skyLayer|allDirs&^dirBit(dirUp))
	case old > 0:
		ctx.putLight(skyLayer, cl.x, cl.y, cl.z, 0)
		dq.push(cl.x, cl.y, cl.z, old, skyLayer|allDirs)
	default:
		ctx.pullIn(cl, skyLayer, iq)
	}
}

// pullIn queues each lit neighbour of the cell to spread into it.
func (ctx *lightCtx) pullIn(cl lightCell, layer int, iq *lightQueue) {
	for _, n := range lightNeighbors {
		nx, ny, nz := cl.x+n.dx, cl.y+n.dy, cl.z+n.dz
		if !contains(ny) {
			continue
		}
		if v := ctx.getLight(layer, nx, ny, nz); v > 0 {
			iq.push(nx, ny, nz, v, layer|dirBit(n.dir^1))
		}
	}
}

// runDecreases clears the light from dq's cells (each entry holds the level
// cleared): a neighbour holding less is cleared in turn, one holding as much is
// queued in iq to spread back.
// vanilla: BlockLightEngine.propagateDecrease
func (ctx *lightCtx) runDecreases(dq, iq *lightQueue) {
	for {
		e, ok := dq.pop()
		if !ok {
			return
		}
		layer := int(e.layer & 1)
		for _, n := range lightNeighbors {
			nx, ny, nz := e.x+n.dx, e.y+n.dy, e.z+n.dz
			if !contains(ny) || int(e.layer)&dirBit(n.dir) == 0 {
				continue
			}
			to := ctx.getLight(layer, nx, ny, nz)
			if to == 0 {
				continue
			}
			if to >= e.v {
				iq.push(nx, ny, nz, to, layer|dirBit(n.dir^1))
				continue
			}
			ctx.putLight(layer, nx, ny, nz, 0)
			if layer == skyLayer {
				dq.push(nx, ny, nz, to, layer|allDirs&^dirBit(n.dir^1))
				continue
			}
			state, _ := ctx.blockAt(nx, ny, nz)
			emission := uint8(registry.LightEmission(state))
			if emission < to {
				dq.push(nx, ny, nz, to, layer|allDirs&^dirBit(n.dir^1))
			}
			if emission > 0 {
				iq.push(nx, ny, nz, emission, layer|allDirs|int(emitFlag))
			}
		}
	}
}

// runIncreases spreads iq's light, skipping entries whose cell no longer holds
// the queued level.
// vanilla: LightEngine.propagateIncreases
func (ctx *lightCtx) runIncreases(iq *lightQueue) {
	for {
		e, ok := iq.pop()
		if !ok {
			return
		}
		layer := int(e.layer & 1)
		held := ctx.getLight(layer, e.x, e.y, e.z)
		if e.layer&emitFlag != 0 && held < e.v {
			ctx.putLight(layer, e.x, e.y, e.z, e.v)
			held = e.v
		}
		if held != e.v {
			continue
		}
		fromState, _ := ctx.blockAt(e.x, e.y, e.z)
		for _, n := range lightNeighbors {
			nx, ny, nz := e.x+n.dx, e.y+n.dy, e.z+n.dz
			if !contains(ny) || int(e.layer)&dirBit(n.dir) == 0 {
				continue
			}
			nbState, loaded := ctx.blockAt(nx, ny, nz)
			if !loaded {
				continue
			}
			cand := spread(e.v, int(n.dir), fromState, nbState)
			if cand == 0 {
				continue
			}
			if ctx.setLight(layer, nx, ny, nz, cand) {
				iq.push(nx, ny, nz, cand, layer|allDirs&^dirBit(n.dir^1))
			}
		}
	}
}
