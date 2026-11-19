package world

import (
	"github.com/golem-mc/golem/internal/server/blocklogic"
	"github.com/golem-mc/golem/registry"
)

const (
	spongeRadius   = 6  // vanilla: SpongeBlock.MAX_DEPTH
	spongeMaxCount = 65 // vanilla: SpongeBlock.removeWaterBreadthFirstSearch, the breadthFirstTraversal limit (MAX_COUNT is 64)
)

// spongeChanged runs the sponge rule at the position: a sponge soaks up nearby
// water, a wet sponge placed where water evaporates dries. placed means the
// position itself was just written; old is what stood there.
// vanilla: SpongeBlock.onPlace, neighborChanged; WetSpongeBlock.onPlace
func (p *Physics) spongeChanged(x, y, z int32, block registry.Block, placed bool, old uint16, changes *[]BlockChange) bool {
	switch block {
	case registry.BlockSponge:
		if placed {
			if registry.BlockOf(old) == block {
				return true // onPlace skips a block that only changed state
			}
		}
		p.spongeAbsorb(x, y, z, !placed, changes)
		return true
	case registry.BlockWetSponge:
		if placed && p.w.WaterEvaporates() {
			p.setShaped(x, y, z, registry.BlockSponge.DefaultState(), changes)
			p.effects = append(p.effects,
				Effect{LevelEvent: 2009, X: x, Y: y, Z: z},
				Effect{
					SoundID: registry.SoundBlockWetSpongeDries,
					X:       x, Y: y, Z: z, Volume: 1,
					Pitch: (1 + p.rng.Float32()*0.2) * 0.7,
				})
		}
		return true
	}
	return false
}

// spongeAbsorb takes water breadth-first, up to spongeRadius away and
// spongeMaxCount cells, turning the sponge wet if it took any. held means it
// runs inside a neighbour update: the writes then wake neighbours only after
// the search and the wet-sponge write, whereas a placed sponge's writes wake at
// once, so the first runs a whole nested absorb before the outer search
// continues.
// vanilla: SpongeBlock.tryAbsorbWater, removeWaterBreadthFirstSearch;
// CollectingNeighborUpdater.addAndRun
func (p *Physics) spongeAbsorb(x, y, z int32, held bool, changes *[]BlockChange) {
	type write struct {
		x, y, z    int32
		old, state uint16
	}
	var woken []write
	setCell := func(x, y, z int32, state uint16) {
		if !held {
			p.setShaped(x, y, z, state, changes)
			return
		}
		old, ok := p.w.SwapBlock(x, y, z, state)
		if !ok {
			return
		}
		*changes = append(*changes, physicsChange(x, y, z, old, state))
		woken = append(woken, write{x, y, z, old, state})
	}
	var allow map[blockKey]bool
	if p.listens(HookSponge) {
		var vetoed bool
		if allow, vetoed = p.spongeAsk(x, y, z); vetoed {
			return
		}
	}
	count := p.spongeSearch(x, y, z, allow, setCell)
	wet := false
	if count > 1 {
		_, wet = p.setPlaced(x, y, z, registry.BlockWetSponge.DefaultState(), changes)
		if wet && !held {
			p.shapeUpdateNeighbors(x, y, z, changes)
		}
		p.effects = append(p.effects, Effect{
			SoundID: registry.SoundBlockSpongeAbsorb,
			X:       x, Y: y, Z: z, Volume: 1, Pitch: 1,
		})
	}
	// A held search's shape passes join the same queue: each cell's neighbour
	// update then its shape pass, the wet sponge's pass last.
	// vanilla: CollectingNeighborUpdater.shapeUpdate goes through addAndRun
	for _, w := range woken {
		p.neighborUpdate(w.x, w.y, w.z, w.old, w.state, changes)
		p.shapeUpdateNeighbors(w.x, w.y, w.z, changes)
	}
	if wet && held {
		p.shapeUpdateNeighbors(x, y, z, changes)
	}
}

// spongeTakeWater removes a cell's water and reports whether it had any: water
// goes, a waterlogged block dries, kelp and seagrass break with drops.
// vanilla: SpongeBlock.removeWaterBreadthFirstSearch, BucketPickup.pickupBlock
func (p *Physics) spongeTakeWater(x, y, z int32, set func(x, y, z int32, state uint16)) bool {
	state := p.w.BlockAt(x, y, z)
	if fluidHolding(state).kind != fluidWater {
		return false
	}
	if b := registry.BlockOf(state); blocklogic.IsWaterPlant(b) && b != registry.BlockBubbleColumn {
		tag, _ := p.w.BlockEntityTag(x, y, z)
		p.effects = append(p.effects, Effect{BreakDrop: state, BreakTag: tag, X: x, Y: y, Z: z})
		set(x, y, z, airState)
		return true
	}
	if blocklogic.FluidOf(state).Pure || registry.BlockOf(state) == registry.BlockBubbleColumn {
		set(x, y, z, airState)
		return true
	}
	next, ok := registry.WithBool(state, registry.PropWaterlogged, false)
	if !ok {
		return false
	}
	set(x, y, z, next)
	return true
}

type spongeNode struct {
	x, y, z int32
	depth   int
}

// spongeSearch is the breadth-first search itself: it hands every cell it takes
// water from to set and returns the count, the sponge's own cell included. A
// non-nil allow limits the taken cells to those it holds.
func (p *Physics) spongeSearch(x, y, z int32, allow map[blockKey]bool, set func(x, y, z int32, state uint16)) int {
	visited := map[blockKey]bool{}
	queue := []spongeNode{{x, y, z, 0}}
	count := 0
	for len(queue) > 0 {
		n := queue[0]
		queue = queue[1:]
		key := blockKey{n.x, n.y, n.z}
		if visited[key] {
			continue
		}
		visited[key] = true
		if n.x != x || n.y != y || n.z != z {
			if allow != nil && !allow[key] {
				continue
			}
			if !p.spongeTakeWater(n.x, n.y, n.z, set) {
				continue
			}
		}
		count++
		if count >= spongeMaxCount {
			break
		}
		if n.depth < spongeRadius {
			for _, d := range dirDelta {
				queue = append(queue, spongeNode{n.x + d[0], n.y + d[1], n.z + d[2], n.depth + 1})
			}
		}
	}
	return count
}

// spongeAsk runs a search that writes nothing and asks the listeners about the
// cells it would dry. It returns the cells they kept (nil when all) and whether
// they vetoed; a search that finds no water asks nothing. The search's drop
// effects are discarded, as the real one books them again.
func (p *Physics) spongeAsk(x, y, z int32) (allow map[blockKey]bool, vetoed bool) {
	booked := len(p.effects)
	var cells [][3]int32
	p.spongeSearch(x, y, z, nil, func(cx, cy, cz int32, _ uint16) { cells = append(cells, [3]int32{cx, cy, cz}) })
	p.effects = p.effects[:booked]
	if len(cells) == 0 {
		return nil, false
	}
	d := HookDecision{Kind: HookSponge, X: x, Y: y, Z: z, Old: p.w.BlockAt(x, y, z), Blocks: cells}
	if p.ask(&d) {
		return nil, true
	}
	if len(d.Blocks) == len(cells) {
		return nil, false
	}
	allow = make(map[blockKey]bool, len(d.Blocks))
	for _, c := range d.Blocks {
		allow[blockKey{c[0], c[1], c[2]}] = true
	}
	return allow, false
}
