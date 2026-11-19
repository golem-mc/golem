package world

import (
	"github.com/golem-mc/golem/internal/server/blocklogic"
	"github.com/golem-mc/golem/registry"
)

// VineSpreadRule answers the spread_vines game rule from the overworld's rules
// table.
type VineSpreadRule func() bool

// SetVineSpreadRule sets the spread_vines source; without one vines spread, the
// default.
func (p *Physics) SetVineSpreadRule(r VineSpreadRule) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.vineRule = r
}

// vineSpreadRadius and vineSpreadMax: a vine spreads only while fewer than five
// vines (itself included) stand within four blocks sideways and one up or down.
// vanilla: VineBlock.canSpread
const (
	vineSpreadRadius = 4
	vineSpreadMax    = 5
)

var vineHorizontals = [4]int{dirNorth, dirEast, dirSouth, dirWest}

// vineFaceDirs are the directions a vine has a face property for; it has none
// downward.
var vineFaceDirs = [5]int{dirUp, dirNorth, dirEast, dirSouth, dirWest}

// vineFaceSet holds a vine's faces by direction; dirDown stays false.
type vineFaceSet [dirCount]bool

// vineAcceptable reports whether the block offers its face opposite d to a vine
// reaching toward it along d.
// vanilla: VineBlock.isAcceptableNeighbour
func (p *Physics) vineAcceptable(x, y, z int32, d int) bool {
	return registry.FaceAttachable(p.w.BlockAt(x, y, z), blocklogic.Opposite(d))
}

func (p *Physics) vineCanSpread(x, y, z int32) bool {
	left := vineSpreadMax
	for dx := -int32(vineSpreadRadius); dx <= vineSpreadRadius; dx++ {
		for dy := int32(-1); dy <= 1; dy++ {
			for dz := -int32(vineSpreadRadius); dz <= vineSpreadRadius; dz++ {
				if !p.blockIs(x+dx, y+dy, z+dz, registry.BlockVine) {
					continue
				}
				if left--; left <= 0 {
					return false
				}
			}
		}
	}
	return true
}

// vineState builds a vine with the given faces.
func vineState(faces vineFaceSet) uint16 {
	state := registry.BlockVine.DefaultState()
	for _, d := range vineFaceDirs {
		state, _ = registry.WithBool(state, facePropIDs[d], faces[d])
	}
	return state
}

func vineHasSide(faces vineFaceSet) bool {
	return faces[dirNorth] || faces[dirEast] || faces[dirSouth] || faces[dirWest]
}

// vineWrite is a flag-2 setBlock: clients hear it and the neighbours' shape
// pass reaches observers.
// vanilla: Level.setBlock
func (p *Physics) vineWrite(x, y, z int32, faces vineFaceSet, changes *[]BlockChange) {
	state, ok := p.veto(HookSpread, x, y, z, vineState(faces), [3]int32{})
	if !ok {
		return
	}
	p.setClientShaped(x, y, z, state, changes)
}

// vineFacesOf reads the faces of a vine state.
func vineFacesOf(state uint16) vineFaceSet {
	var faces vineFaceSet
	for _, d := range vineFaceDirs {
		faces[d] = registry.StateBool(state, facePropIDs[d])
	}
	return faces
}

// vineFace is a vine with the one face d.
func vineFace(d int) vineFaceSet {
	var faces vineFaceSet
	faces[d] = true
	return faces
}

// vineRandomTick spreads a vine up, down and sideways.
// vanilla: VineBlock.randomTick
func (p *Physics) vineRandomTick(x, y, z int32, state uint16, changes *[]BlockChange) {
	if p.vineRule != nil && !p.vineRule() {
		return
	}
	if p.rng.Intn(4) != 0 {
		return
	}
	faces := vineFacesOf(state)
	dir := p.rng.Intn(dirCount)
	if dir != dirDown && dir != dirUp && !faces[dir] {
		p.vineSpreadSideways(x, y, z, dir, faces, changes)
		return
	}
	if dir == dirUp && y < p.w.MinY()+p.w.Height()-1 {
		if p.vineAcceptable(x, y+1, z, dirUp) {
			faces[dirUp] = true
			p.vineWrite(x, y, z, faces, changes)
			return
		}
		if p.airAt(x, y+1, z) {
			if !p.vineCanSpread(x, y, z) {
				return
			}
			above := faces
			for _, h := range vineHorizontals {
				if p.rng.Intn(2) == 0 && p.vineAcceptable(x+dirDelta[h][0], y+1, z+dirDelta[h][2], h) {
					continue
				}
				above[h] = false
			}
			if vineHasSide(above) {
				p.vineWrite(x, y+1, z, above, changes)
			}
			return
		}
	}
	if y <= p.w.MinY() {
		return
	}
	below := p.w.BlockAt(x, y-1, z)
	isAir := p.airAt(x, y-1, z)
	if !isAir && !p.blockIs(x, y-1, z, registry.BlockVine) {
		return
	}
	var before vineFaceSet
	if !isAir {
		before = vineFacesOf(below)
	}
	after := before
	for _, h := range vineHorizontals {
		if p.rng.Intn(2) == 1 && faces[h] {
			after[h] = true
		}
	}
	if before != after && vineHasSide(after) {
		p.vineWrite(x, y-1, z, after, changes)
	}
}

func (p *Physics) vineSpreadSideways(x, y, z int32, dir int, faces vineFaceSet, changes *[]BlockChange) {
	if !p.vineCanSpread(x, y, z) {
		return
	}
	tx, tz := x+dirDelta[dir][0], z+dirDelta[dir][2]
	if !p.airAt(tx, y, tz) {
		if p.vineAcceptable(tx, y, tz, dir) {
			faces[dir] = true
			p.vineWrite(x, y, z, faces, changes)
		}
		return
	}
	cw, ccw := blocklogic.Clockwise(dir), blocklogic.CounterClockwise(dir)
	cwX, cwZ := tx+dirDelta[cw][0], tz+dirDelta[cw][2]
	ccwX, ccwZ := tx+dirDelta[ccw][0], tz+dirDelta[ccw][2]
	opp := blocklogic.Opposite(dir)
	switch {
	case faces[cw] && p.vineAcceptable(cwX, y, cwZ, cw):
		p.vineWrite(tx, y, tz, vineFace(cw), changes)
	case faces[ccw] && p.vineAcceptable(ccwX, y, ccwZ, ccw):
		p.vineWrite(tx, y, tz, vineFace(ccw), changes)
	case faces[cw] && p.airAt(cwX, y, cwZ) && p.vineAcceptable(x+dirDelta[cw][0], y, z+dirDelta[cw][2], opp):
		p.vineWrite(cwX, y, cwZ, vineFace(opp), changes)
	case faces[ccw] && p.airAt(ccwX, y, ccwZ) && p.vineAcceptable(x+dirDelta[ccw][0], y, z+dirDelta[ccw][2], opp):
		p.vineWrite(ccwX, y, ccwZ, vineFace(opp), changes)
	case p.rng.Float32() < 0.05 && p.vineAcceptable(tx, y+1, tz, dirUp):
		p.vineWrite(tx, y, tz, vineFace(dirUp), changes)
	}
}
