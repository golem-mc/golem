package world

import (
	"slices"

	"github.com/golem-mc/golem/internal/server/blocklogic"
	"github.com/golem-mc/golem/registry"
)

const (
	// lightningRodTicks is how long a strike holds a rod powered.
	// vanilla: LightningRodBlock.ACTIVATION_TICKS
	lightningRodTicks = 8

	// LightningRodRange is the block radius a strike looks for a rod in.
	// vanilla: LightningRodBlock.RANGE
	LightningRodRange = 128

	// lightningRodTickEvent tags a struck rod's queue entry, so a waterlogged
	// rod's fluid visit neither stands for it nor blocks it.
	lightningRodTickEvent = 1<<5 | 1<<7

	// levelEventLightningRodSparks is the electric spark burst; the data is
	// the rod's axis, or -1 for sparks in every direction.
	// vanilla: LevelEvent.PARTICLES_ELECTRIC_SPARK
	levelEventLightningRodSparks = 3002
)

// isLightningRod reports a lightning rod of any stage, waxed or not; the eight
// blocks have consecutive IDs.
// vanilla: BlockTags.LIGHTNING_RODS
func isLightningRod(state uint16) bool {
	b := registry.BlockOf(state)
	return b >= registry.BlockLightningRod && b <= registry.BlockWaxedOxidizedLightningRod
}

// LightningRodAt reports a lightning rod at the cell.
func (w *World) LightningRodAt(x, y, z int32) bool { return isLightningRod(w.BlockAt(x, y, z)) }

// ClosestLightningRod finds the rod a strike over the column's centre goes to:
// the nearest within LightningRodRange that tops its column.
// vanilla: ServerLevel.findLightningRod
func (w *World) ClosestLightningRod(cx, cy, cz int32) (x, y, z int32, ok bool) {
	const r = LightningRodRange
	type cand struct {
		pos  [3]int32
		dist int64
	}
	var cands []cand
	w.mu.RLock()
	for chx := (cx - r) >> 4; chx <= (cx+r)>>4; chx++ {
		for chz := (cz - r) >> 4; chz <= (cz+r)>>4; chz++ {
			loaded, found := w.loadedAt(ChunkPos{X: chx, Z: chz})
			if !found {
				continue
			}
			for sec := range loaded.chunk.sections {
				s := &loaded.chunk.sections[sec]
				if s.rods == 0 {
					continue
				}
				baseY := int32(worldMinY + sec*sectionSize)
				for ly := range int32(sectionSize) {
					for lz := range int32(16) {
						for lx := range int32(16) {
							if !isLightningRod(s.at(blockIndex(lx, ly, lz))) {
								continue
							}
							bx, by, bz := chx<<4+lx, baseY+ly, chz<<4+lz
							dx, dy, dz := int64(bx-cx), int64(by-cy), int64(bz-cz)
							d := dx*dx + dy*dy + dz*dz
							if absI32(bx-cx) <= r && absI32(bz-cz) <= r && d <= r*r {
								cands = append(cands, cand{[3]int32{bx, by, bz}, d})
							}
						}
					}
				}
			}
		}
	}
	w.mu.RUnlock()
	slices.SortStableFunc(cands, func(a, b cand) int { return int(a.dist - b.dist) })
	for _, c := range cands {
		if w.SurfaceHeight(c.pos[0], c.pos[2]) == c.pos[1] {
			return c.pos[0], c.pos[1], c.pos[2], true
		}
	}
	return 0, 0, 0, false
}

// LightningStruck powers a rod at the cell and scrapes the weathering copper
// around it.
// vanilla: LightningBolt.tick
func (p *Physics) LightningStruck(x, y, z int32) ([]BlockChange, []Effect, []TNTPrime) {
	p.mu.Lock()
	defer p.mu.Unlock()
	var changes []BlockChange
	p.effects, p.wakePrimes = nil, nil
	if state := p.w.BlockAt(x, y, z); isLightningRod(state) {
		p.lightningRodPower(x, y, z, state, &changes)
	}
	p.clearCopperOnLightningStrike(x, y, z, &changes)
	effects, primes := p.takeWake()
	return changes, effects, primes
}

// lightningRodPower powers a struck rod and books the tick that releases it.
// vanilla: LightningRodBlock.onLightningStrike
func (p *Physics) lightningRodPower(x, y, z int32, state uint16, changes *[]BlockChange) {
	f := facingOfState(state)
	next, _ := registry.WithBool(state, registry.PropPowered, true)
	p.setClientShaped(x, y, z, next, changes)
	p.wireWakeNeighbors(x, y, z, changes)
	p.lightningRodUpdateNeighbours(x, y, z, f, changes)
	p.scheduleBooked(p.rodBooked, x, y, z, p.now+lightningRodTicks, lightningRodTickEvent)
	p.effects = append(p.effects, Effect{
		LevelEvent: levelEventLightningRodSparks, LevelEventData: lightningRodAxis(f), X: x, Y: y, Z: z,
	})
}

// lightningRodAxis is the ordinal of the rod's facing axis: x 0, y 1, z 2.
func lightningRodAxis(f int) int32 {
	switch f {
	case dirDown, dirUp:
		return 1
	case dirNorth, dirSouth:
		return 2
	}
	return 0
}

// lightningRodUpdateNeighbours wakes the neighbours of the block the rod
// strongly powers.
// vanilla: LightningRodBlock.updateNeighbours
func (p *Physics) lightningRodUpdateNeighbours(x, y, z int32, f int, changes *[]BlockChange) {
	front := blocklogic.Opposite(f)
	p.wireWakeNeighbors(x+dirDelta[front][0], y+dirDelta[front][1], z+dirDelta[front][2], changes)
}

// lightningRodTick releases a rod.
// vanilla: LightningRodBlock.tick
func (p *Physics) lightningRodTick(x, y, z int32, state uint16, changes *[]BlockChange) {
	next, _ := registry.WithBool(state, registry.PropPowered, false)
	p.setClientShaped(x, y, z, next, changes)
	p.wireWakeNeighbors(x, y, z, changes)
	p.lightningRodUpdateNeighbours(x, y, z, facingOfState(state), changes)
}

// lightningRodOnPlace books the release of a rod that arrived powered.
// vanilla: LightningRodBlock.onPlace
func (p *Physics) lightningRodOnPlace(x, y, z int32, state, oldState uint16) {
	if isLightningRod(oldState) || !registry.StatePowered(state) {
		return
	}
	p.scheduleBooked(p.rodBooked, x, y, z, p.now+lightningRodTicks, lightningRodTickEvent)
}

// copperPrevious is WeatheringCopper.PREVIOUS_BY_BLOCK; BlockAir marks no previous stage.
var copperPrevious = func() *[registry.BlockCount]registry.Block {
	previous := new([registry.BlockCount]registry.Block)
	for more := range registry.Block(registry.BlockCount) {
		if less, ok := more.Scraped(); ok {
			previous[more] = less
		}
	}
	return previous
}()

var waxedCopper = func() [registry.BlockCount]bool {
	var waxed [registry.BlockCount]bool
	for block := range registry.Block(registry.BlockCount) {
		_, waxed[block] = block.Unwaxed()
	}
	return waxed
}()

// clearCopperOnLightningStrike unoxidizes the struck copper and walks a few
// random paths over the copper around it, scraping a stage off each step; waxed
// copper starts walks but does not change.
// vanilla: LightningBolt.clearCopperOnLightningStrike
func (p *Physics) clearCopperOnLightningStrike(x, y, z int32, changes *[]BlockChange) {
	state := p.w.BlockAt(x, y, z)
	block := registry.BlockOf(state)
	if copperStages[block] == copperNoStage && !waxedCopper[block] {
		return
	}
	if copperStages[block] != copperNoStage {
		for copperPrevious[block] != registry.BlockAir {
			block = copperPrevious[block]
		}
		if id, ok := registry.WithPropertiesOf(block, state); ok {
			p.setShaped(x, y, z, id, changes)
		}
	}
	for range p.rng.Intn(3) + 3 {
		cx, cy, cz := x, y, z
		for range p.rng.Intn(8) + 1 {
			nx, ny, nz, ok := p.scrapeStepNear(cx, cy, cz, changes)
			if !ok {
				break
			}
			cx, cy, cz = nx, ny, nz
		}
	}
}

// scrapeStepNear tries ten random cells around a cell and scrapes the first
// weathering copper one stage.
// vanilla: LightningBolt.randomStepCleaningCopper
func (p *Physics) scrapeStepNear(x, y, z int32, changes *[]BlockChange) (int32, int32, int32, bool) {
	for range 10 {
		cx, cy, cz := x+int32(p.rng.Intn(3))-1, y+int32(p.rng.Intn(3))-1, z+int32(p.rng.Intn(3))-1
		state := p.w.BlockAt(cx, cy, cz)
		if copperStageOf(state) == copperNoStage {
			continue
		}
		if prev := copperPrevious[registry.BlockOf(state)]; prev != registry.BlockAir {
			if id, ok := registry.WithPropertiesOf(prev, state); ok {
				p.setShaped(cx, cy, cz, id, changes)
			}
		}
		p.effects = append(p.effects, Effect{LevelEvent: levelEventLightningRodSparks, LevelEventData: -1, X: cx, Y: cy, Z: cz})
		return cx, cy, cz, true
	}
	return 0, 0, 0, false
}
