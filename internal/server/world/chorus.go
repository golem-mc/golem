package world

import (
	"github.com/golem-mc/golem/internal/server/blocklogic"
	"github.com/golem-mc/golem/registry"
)

// The chorus flower's random tick: a young flower grows a stem up or branches
// sideways, and dies at age 5. The plant's six connection properties are built
// here; the shape pass keeps a written plant's connections current.

// chorusAgeMax is ChorusFlowerBlock.DEAD_AGE; a flower this old no longer
// random-ticks.
const chorusAgeMax = 5

// chorusHorizontalDirs is Direction.Plane.HORIZONTAL's order, which the branch
// roll picks from (as physics' horizontal4).
var chorusHorizontalDirs = [4]int{dirNorth, dirEast, dirSouth, dirWest}

// chorusFlowerTick grows a young flower up or sideways; one that can do neither
// dies.
// vanilla: ChorusFlowerBlock.randomTick
func (p *Physics) chorusFlowerTick(x, y, z int32, state uint16, changes *[]BlockChange) {
	above := y + 1
	if above > p.w.MinY()+p.w.Height()-1 || !p.airAt(x, above, z) {
		return
	}
	age, ok := registry.StateInt(state, registry.PropAge)
	if !ok || age >= chorusAgeMax {
		return
	}

	growUpwards := false
	pillarOnSupportBlock := false
	below := p.w.BlockAt(x, y-1, z)
	switch {
	case stateInTag(below, registry.TagIDSupportsChorusFlower):
		growUpwards = true
	case registry.BlockOf(below) == registry.BlockChorusPlant:
		height := int32(1)
		for range 4 {
			test := p.w.BlockAt(x, y-(height+1), z)
			if registry.BlockOf(test) != registry.BlockChorusPlant {
				pillarOnSupportBlock = stateInTag(test, registry.TagIDSupportsChorusFlower)
				break
			}
			height++
		}
		// The roll's bound is 5 when the pillar stands on a support block, else
		// 4.
		bound := 4
		if pillarOnSupportBlock {
			bound = 5
		}
		if height < 2 || int(height) <= p.rng.Intn(bound) {
			growUpwards = true
		}
	case IsAirState(below):
		growUpwards = true
	}

	if growUpwards && chorusAllNeighborsEmpty(p.w, x, above, z, -1) && p.airAt(x, y+2, z) {
		if plant, ok := p.chorusPlantState(x, y, z); ok {
			p.chorusWrite(x, y, z, plant, changes)
		}
		p.placeGrownFlower(x, above, z, age, changes)
		return
	}

	if age < 4 {
		branches := p.rng.Intn(4)
		if pillarOnSupportBlock {
			branches++
		}
		createdBranch := false
		for range branches {
			dir := chorusHorizontalDirs[p.rng.Intn(len(chorusHorizontalDirs))]
			d := dirDelta[dir]
			tx, tz := x+d[0], z+d[2]
			if p.airAt(tx, y, tz) && p.airAt(tx, y-1, tz) &&
				chorusAllNeighborsEmpty(p.w, tx, y, tz, blocklogic.Opposite(dir)) {
				p.placeGrownFlower(tx, y, tz, age+1, changes)
				createdBranch = true
			}
		}
		if createdBranch {
			if plant, ok := p.chorusPlantState(x, y, z); ok {
				p.chorusWrite(x, y, z, plant, changes)
			}
			return
		}
	}
	p.placeDeadFlower(x, y, z, changes)
}

// chorusAllNeighborsEmpty reports the four horizontal neighbours are air,
// except ignore (a direction index, or -1).
// vanilla: ChorusFlowerBlock.allNeighborsEmpty
func chorusAllNeighborsEmpty(w *World, x, y, z int32, ignore int) bool {
	for _, dir := range chorusHorizontalDirs {
		if dir == ignore {
			continue
		}
		d := dirDelta[dir]
		if s := w.BlockAt(x+d[0], y, z+d[2]); !IsKnownAirState(s) {
			return false
		}
	}
	return true
}

// placeGrownFlower writes a flower of age with its growth event; a write that
// did not take (an unloaded target reads as air) leaves no event.
// vanilla: ChorusFlowerBlock.placeGrownFlower
func (p *Physics) placeGrownFlower(x, y, z int32, age int, changes *[]BlockChange) {
	state, ok := registry.WithInt(registry.BlockChorusFlower.DefaultState(), registry.PropAge, age)
	if !ok {
		return
	}
	if !p.chorusWrite(x, y, z, state, changes) {
		return
	}
	p.randomEffects = append(p.randomEffects, Effect{LevelEvent: 1033, X: x, Y: y, Z: z})
}

// placeDeadFlower writes an age-5 flower with its death event.
// vanilla: ChorusFlowerBlock.placeDeadFlower
func (p *Physics) placeDeadFlower(x, y, z int32, changes *[]BlockChange) {
	state, ok := registry.WithInt(registry.BlockChorusFlower.DefaultState(), registry.PropAge, chorusAgeMax)
	if !ok {
		return
	}
	if !p.chorusWrite(x, y, z, state, changes) {
		return
	}
	p.randomEffects = append(p.randomEffects, Effect{LevelEvent: 1034, X: x, Y: y, Z: z})
}

// chorusWrite is a flag-2 setBlock: the six neighbours take the shape update
// (Level.setBlock's updateNeighbourShapes) without a neighbour-changed wake. It
// reports whether the write took.
func (p *Physics) chorusWrite(x, y, z int32, state uint16, changes *[]BlockChange) bool {
	old, ok := p.w.SwapBlock(x, y, z, state)
	if !ok {
		return false
	}
	*changes = append(*changes, physicsChange(x, y, z, old, state))
	p.shapeUpdateNeighbors(x, y, z, changes)
	return true
}

// ChorusPlantConnections is a chorus plant at (x, y, z) with each side joined
// to a plant or flower there, and the bottom also to a supports_chorus_plant
// block.
// vanilla: ChorusPlantBlock.getStateWithConnections
func ChorusPlantConnections(blockAt func(x, y, z int32) uint16, x, y, z int32) (uint16, bool) {
	state := registry.BlockChorusPlant.DefaultState()
	for d := range dirCount {
		n := blockAt(x+dirDelta[d][0], y+dirDelta[d][1], z+dirDelta[d][2])
		state, _ = registry.WithBool(state, facePropIDs[d], chorusJoins(n, d == dirDown))
	}
	return state, true
}

// chorusJoins reports a plant or flower, or below a supports_chorus_plant
// block.
func chorusJoins(n uint16, below bool) bool {
	if int(n) >= registry.StateCount {
		return false
	}
	b := registry.BlockOf(n)
	return b == registry.BlockChorusPlant || b == registry.BlockChorusFlower ||
		below && b.InTag(registry.TagIDSupportsChorusPlant)
}

func (p *Physics) chorusPlantState(x, y, z int32) (uint16, bool) {
	return ChorusPlantConnections(p.w.BlockAt, x, y, z)
}

// chorusPlantShapeUpdate books a one-tick break for a plant that no longer
// survives, otherwise sets the changed side's connection (per chorusJoins). d
// is the delta from the changed neighbour to the plant.
// vanilla: ChorusPlantBlock.updateShape
func (p *Physics) chorusPlantShapeUpdate(x, y, z int32, d [3]int32, changes *[]BlockChange) {
	state := p.w.BlockAt(x, y, z)
	if registry.BlockOf(state) != registry.BlockChorusPlant {
		return
	}
	if !chorusPlantCanSurvive(p.w, x, y, z) {
		p.schedule(x, y, z, p.now+int64(survivalOf[state].tick))
		return
	}
	// Vanilla's direction points toward the changed neighbour; the delta points
	// the other way.
	dir := blocklogic.Opposite(directionOf(d))
	if next := chorusPlantShapeState(state, dir, p.w.BlockAt(x-d[0], y-d[1], z-d[2])); next != state {
		p.setClientShaped(x, y, z, next, changes)
	}
}

// vanilla: ChorusPlantBlock.updateShape
func chorusPlantShapeState(state uint16, toward int, neighbor uint16) uint16 {
	next, _ := registry.WithBool(state, facePropIDs[toward], chorusJoins(neighbor, toward == dirDown))
	return next
}
