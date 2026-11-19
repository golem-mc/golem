package world

import (
	"sync"

	"github.com/golem-mc/golem/nbt"
)

// scatteredPiece fixes the shared piece's height on its first placement.
// vanilla: ScatteredFeaturePiece
type scatteredPiece struct {
	mu             sync.Mutex
	box            jbox
	dir            int
	heightPosition int32
}

func (p *scatteredPiece) bounds() jbox {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.box
}

// header requires the piece lock, as do the ground-alignment methods.
func (p *scatteredPiece) header(id string, w, h, d int32) nbt.Compound {
	return nbt.Compound{"id": nbt.String(id), "GD": nbt.Int(0), "O": nbt.Int((p.dir + 2) & 3),
		"BB":    nbt.IntArray{p.box.minX, p.box.minY, p.box.minZ, p.box.maxX, p.box.maxY, p.box.maxZ},
		"Width": nbt.Int(w), "Height": nbt.Int(h), "Depth": nbt.Int(d), "HPos": nbt.Int(p.heightPosition)}
}

// vanilla: ScatteredFeaturePiece.updateAverageGroundHeight
func (p *scatteredPiece) averageGround(r *decorRegion, chunk jbox) bool {
	if p.heightPosition >= 0 {
		return true
	}
	var total, count int32
	for z := p.box.minZ; z <= p.box.maxZ; z++ {
		for x := p.box.minX; x <= p.box.maxX; x++ {
			if chunk.inside(x, 64, z) {
				total += r.heightOf(hmMotionBlockingNoLeaves, x, z)
				count++
			}
		}
	}
	if count == 0 {
		return false
	}
	p.heightPosition = total / count
	dy := p.heightPosition - p.box.minY
	p.box.minY += dy
	p.box.maxY += dy
	return true
}

// vanilla: ScatteredFeaturePiece.updateHeightPositionToLowestGroundHeight
func (p *scatteredPiece) lowestGround(r *decorRegion, offset int32) bool {
	if p.heightPosition >= 0 {
		return true
	}
	low := r.minY + r.height
	for z := p.box.minZ; z <= p.box.maxZ; z++ {
		for x := p.box.minX; x <= p.box.maxX; x++ {
			low = min(low, r.heightOf(hmMotionBlockingNoLeaves, x, z))
		}
	}
	p.heightPosition = low
	dy := p.heightPosition - p.box.minY - offset
	p.box.minY += dy
	p.box.maxY += dy
	return true
}
