package entity

import (
	"math"
	"math/rand"
	"testing"

	"github.com/golem-mc/golem/internal/server/world"
)

// clipMoveEveryCell is clipMove with every cell of the padded sweep offered
// to the stop test, the reference the core-range skip must match bit for bit.
func clipMoveEveryCell(w World, c collideCtx, b AABB, axis int, d float64) (AABB, float64, bool) {
	if d == 0 {
		return b, math.Inf(-1), false
	}
	lo := [3]float64{b.minX, b.minY, b.minZ}
	hi := [3]float64{b.maxX, b.maxY, b.maxZ}
	sweptLo, sweptHi := lo, hi
	if d > 0 {
		sweptHi[axis] += d
	} else {
		sweptLo[axis] += d
	}
	// A shape must cross the box on both other axes to be in the way.
	p, q := (axis+1)%3, (axis+2)%3
	allowed := d
	obstacleTop := math.Inf(-1)
	// stop clamps the leading face to an obstacle crossing both other axes;
	// the tightest clamp's top is the step-up obstacle.
	stop := func(oLo, oHi [3]float64) {
		if hi[p] <= oLo[p] || lo[p] >= oHi[p] || hi[q] <= oLo[q] || lo[q] >= oHi[q] {
			return
		}
		if d > 0 && hi[axis] <= oLo[axis] {
			gap := oLo[axis] - hi[axis]
			if gap < allowed {
				allowed = gap
			}
			if gap <= allowed+physicsEpsilon {
				obstacleTop = math.Max(obstacleTop, oHi[1])
			}
		} else if d < 0 && lo[axis] >= oHi[axis] {
			gap := oHi[axis] - lo[axis]
			if gap > allowed {
				allowed = gap
			}
			if gap >= allowed-physicsEpsilon {
				obstacleTop = math.Max(obstacleTop, oHi[1])
			}
		}
	}
	// One cell past the swept span on every axis, since a shape can reach
	// out of its cell (a fence's 24/16).
	x0, x1 := int32(math.Floor(sweptLo[0]))-1, int32(math.Floor(sweptHi[0]))+1
	y0, y1 := int32(math.Floor(sweptLo[1]))-1, int32(math.Floor(sweptHi[1]))+1
	z0, z1 := int32(math.Floor(sweptLo[2]))-1, int32(math.Floor(sweptHi[2]))+1
	scratch := cellScratch.Get().(*[]uint16)
	defer cellScratch.Put(scratch)
	cells := w.BlocksIn(x0, y0, z0, x1, y1, z1, *scratch)
	*scratch = cells
	i := 0
	for x := x0; x <= x1; x++ {
		for y := y0; y <= y1; y++ {
			for z := z0; z <= z1; z, i = z+1, i+1 {
				state := cells[i]
				boxes := cellBoxes(w, c, state, x, y, z, b.minY)
				if len(boxes) == 0 {
					continue
				}
				ox, oz := world.CollisionOffset(state, x, z)
				for _, s := range boxes {
					oLo := [3]float64{float64(x) + ox + s.MinX, float64(y) + s.MinY, float64(z) + oz + s.MinZ}
					oHi := [3]float64{float64(x) + ox + s.MaxX, float64(y) + s.MaxY, float64(z) + oz + s.MaxZ}
					stop(oLo, oHi)
				}
			}
		}
	}
	if bodies, ok := w.(BodyWorld); ok {
		swept := AABB{sweptLo[0], sweptLo[1], sweptLo[2], sweptHi[0], sweptHi[1], sweptHi[2]}
		for _, hull := range bodies.BodyBoxes(swept) {
			stop([3]float64{hull.minX, hull.minY, hull.minZ}, [3]float64{hull.maxX, hull.maxY, hull.maxZ})
		}
	}
	lo[axis] += allowed
	hi[axis] += allowed
	return AABB{lo[0], lo[1], lo[2], hi[0], hi[1], hi[2]}, obstacleTop, allowed != d
}

// TestClipMoveCoreSkipMatchesEveryCell sweeps random boxes through random
// blocks, the shapes that reach out of their cell among them, and holds
// clipMove to the answer of the walk over every cell. Coordinates sit on
// cell edges and within the margin of them, where a skip could go wrong.
func TestClipMoveCoreSkipMatchesEveryCell(t *testing.T) {
	names := []string{
		"minecraft:stone", "minecraft:oak_fence", "minecraft:cobblestone_wall", "minecraft:oak_slab",
		"minecraft:bamboo", "minecraft:pointed_dripstone", "minecraft:scaffolding", "minecraft:powder_snow",
		"minecraft:oak_stairs", "minecraft:white_carpet", "minecraft:cactus", "minecraft:cauldron",
		"minecraft:oak_fence_gate", "minecraft:lava", "minecraft:moving_piston", "minecraft:soul_sand",
		"minecraft:oak_trapdoor", "minecraft:end_rod", "minecraft:oak_pressure_plate",
	}
	states := make([]uint16, len(names))
	for i, n := range names {
		states[i] = defaultState(t, n)
	}
	rng := rand.New(rand.NewSource(2759))
	// Values that sit on or a hair off a cell edge, and in between.
	coord := func(base float64) float64 {
		switch rng.Intn(4) {
		case 0:
			return base + float64(rng.Intn(9))/8
		case 1:
			return base + float64(rng.Intn(9))/8 + (rng.Float64()-0.5)*4e-6
		case 2:
			return base + float64(rng.Intn(5)) + 1e-7*float64(rng.Intn(3)-1)
		}
		return base + rng.Float64()*4
	}
	step := func() float64 {
		switch rng.Intn(5) {
		case 0:
			return 0
		case 1:
			return float64(rng.Intn(5)-2) * 0.5
		case 2:
			return (rng.Float64() - 0.5) * 2e-6
		}
		return (rng.Float64() - 0.5) * 3
	}
	for trial := range 4000 {
		w := newTestWorld()
		for x := int32(-1); x < 8; x++ {
			for y := int32(-1); y < 8; y++ {
				for z := int32(-1); z < 8; z++ {
					if rng.Intn(10) < 3 {
						w.set(x, y, z, states[rng.Intn(len(states))])
					}
				}
			}
		}
		lo := [3]float64{coord(0), coord(0), coord(0)}
		var size [3]float64
		for k := range size {
			size[k] = []float64{0, 0.25, 0.6, 1, 1.8}[rng.Intn(5)]
		}
		b := AABB{lo[0], lo[1], lo[2], lo[0] + size[0], lo[1] + size[1], lo[2] + size[2]}
		ctx := collideCtx{
			fallDistance: float64(rng.Intn(2)) * 3, walker: rng.Intn(2) == 0,
			lavaWalker: rng.Intn(3) == 0, fallingBlock: rng.Intn(4) == 0,
		}
		axis, d := rng.Intn(3), step()
		// A wider window makes every axis clip use subrows with nontrivial strides.
		movement := [3]float64{step(), step(), step()}
		movement[axis] = d
		cells := gatherMoveCells(w, b, movement[0], movement[1], movement[2], 0.6)
		ctx.moveCells = &cells
		gotBox, gotTop, gotBlocked := clipMove(w, ctx, b, axis, d)
		wantBox, wantTop, wantBlocked := clipMoveEveryCell(w, ctx, b, axis, d)
		cells.release()
		if gotBox != wantBox || gotTop != wantTop && !(math.IsInf(gotTop, -1) && math.IsInf(wantTop, -1)) || gotBlocked != wantBlocked {
			t.Fatalf("trial %d: box %+v axis %d d %v ctx %+v: clipMove = %+v, %v, %v; every cell = %+v, %v, %v",
				trial, b, axis, d, ctx, gotBox, gotTop, gotBlocked, wantBox, wantTop, wantBlocked)
		}
	}
}
