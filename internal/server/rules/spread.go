package rules

import (
	"math"
	"math/rand"

	"github.com/golem-mc/golem/registry"
)

// spreadMaxIterations bounds the relaxation loop. vanilla: SpreadPlayersCommand.MAX_ITERATION_COUNT
const spreadMaxIterations = 10000

// SpreadPoint is a spot on the horizontal plane /spreadplayers is placing an
// entity at.
type SpreadPoint struct{ X, Z float64 }

// SpreadArea is the square a spread happens in and the floor it lands on.
// MaxHeight is the highest block an entity may stand under; MinY the lowest
// block of the dimension.
type SpreadArea struct {
	MinX, MinZ, MaxX, MaxZ float64
	MaxHeight, MinY        int32
}

func (p SpreadPoint) dist(o SpreadPoint) float64 {
	dx, dz := p.X-o.X, p.Z-o.Z
	return math.Sqrt(dx*dx + dz*dz)
}

func (p *SpreadPoint) randomize(r *rand.Rand, a SpreadArea) {
	p.X = spreadNextDouble(r, a.MinX, a.MaxX)
	p.Z = spreadNextDouble(r, a.MinZ, a.MaxZ)
}

// spreadNextDouble is Mth.nextDouble: min when the range is empty.
func spreadNextDouble(r *rand.Rand, lo, hi float64) float64 {
	if lo >= hi {
		return lo
	}
	return r.Float64()*(hi-lo) + lo
}

// clamp pulls the point into the area, reporting whether it moved.
// vanilla: SpreadPlayersCommand.Position.clamp
func (p *SpreadPoint) clamp(a SpreadArea) bool {
	changed := false
	if p.X < a.MinX {
		p.X, changed = a.MinX, true
	} else if p.X > a.MaxX {
		p.X, changed = a.MaxX, true
	}
	if p.Z < a.MinZ {
		p.Z, changed = a.MinZ, true
	} else if p.Z > a.MaxZ {
		p.Z, changed = a.MaxZ, true
	}
	return changed
}

// SpreadY is the y an entity at the point stands at: the first block, going
// down from above the ceiling, with two air blocks over it. The ceiling plus
// one when the column has no such floor.
// vanilla: SpreadPlayersCommand.Position.getSpawnY
func SpreadY(w BlockReader, p SpreadPoint, a SpreadArea) int32 {
	x, z := BlockOf(p.X), BlockOf(p.Z)
	y := a.MaxHeight + 1
	air2Above := isAirState(w.BlockAt(x, y, z))
	y--
	air1Above := isAirState(w.BlockAt(x, y, z))
	for y > a.MinY {
		y--
		currentIsAir := isAirState(w.BlockAt(x, y, z))
		if !currentIsAir && air1Above && air2Above {
			return y + 1
		}
		air2Above, air1Above = air1Above, currentIsAir
	}
	return a.MaxHeight + 1
}

// spreadSafe: the floor is below the ceiling, is not a liquid, and is in
// entities_can_teleport_to (so not short grass, a flower, a carpet or a rail).
// vanilla: SpreadPlayersCommand.Position.isSafe
func spreadSafe(w BlockReader, p SpreadPoint, a SpreadArea) bool {
	y := SpreadY(w, p, a) - 1
	state := w.BlockAt(BlockOf(p.X), y, BlockOf(p.Z))
	return y < a.MaxHeight && !spreadIsLiquid(state) && CanTeleportTo(state)
}

// spreadIsLiquid is BlockState.liquid(): the water, lava and bubble column
// blocks (a waterlogged block is not one).
func spreadIsLiquid(state uint16) bool {
	b := registry.BlockOf(state)
	return b == registry.BlockBubbleColumn || b.Type() == registry.BlockTypeLiquid
}

// SpreadPositions places count points inside the area, at least spread apart
// where the space allows, each over a safe floor. It returns the points and the
// smallest distance between two seen in the last pass; ok is false when the
// iteration limit ran out first, with the points as they stood.
// vanilla: SpreadPlayersCommand.createInitialPositions, spreadPositions
func SpreadPositions(r *rand.Rand, w BlockReader, count int, spread float64, a SpreadArea) (points []SpreadPoint, minDistance float64, ok bool) {
	points = make([]SpreadPoint, count)
	for i := range points {
		points[i].randomize(r, a)
	}

	hasCollisions := true
	minDistance = math.MaxFloat32
	iteration := 0
	for ; iteration < spreadMaxIterations && hasCollisions; iteration++ {
		hasCollisions = false
		minDistance = math.MaxFloat32
		for i := range points {
			p := &points[i]
			neighbours := 0
			var avg SpreadPoint
			for j := range points {
				if i == j {
					continue
				}
				n := points[j]
				d := p.dist(n)
				minDistance = math.Min(d, minDistance)
				if !(d < spread) {
					continue
				}
				neighbours++
				avg.X += n.X - p.X
				avg.Z += n.Z - p.Z
			}
			if neighbours > 0 {
				avg.X /= float64(neighbours)
				avg.Z /= float64(neighbours)
				length := math.Sqrt(avg.X*avg.X + avg.Z*avg.Z)
				if length > 0 {
					p.X -= avg.X / length
					p.Z -= avg.Z / length
				} else {
					p.randomize(r, a)
				}
				hasCollisions = true
			}
			if p.clamp(a) {
				hasCollisions = true
			}
		}
		if hasCollisions {
			continue
		}
		for i := range points {
			if spreadSafe(w, points[i], a) {
				continue
			}
			points[i].randomize(r, a)
			hasCollisions = true
		}
	}
	if minDistance == math.MaxFloat32 {
		minDistance = 0
	}
	return points, minDistance, iteration < spreadMaxIterations
}

// SpreadDistance is SpreadPoint.dist for the callers that average the
// closest neighbour of each point.
func SpreadDistance(a, b SpreadPoint) float64 { return a.dist(b) }
