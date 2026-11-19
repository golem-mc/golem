package rules

import "github.com/golem-mc/golem/internal/server/world"

type portalRectangle struct {
	min           world.SpawnPos
	width, height int32
	axis          [2]int32
}

// portalRectangleAt finds the largest same-state rectangle around a portal cell.
// vanilla: BlockUtil.getLargestRectangleAround
func portalRectangleAt(w BlockReader, at world.SpawnPos) portalRectangle {
	state := w.BlockAt(at.X, at.Y, at.Z)
	dx, dz, ok := portalAxisOf(state)
	if !ok {
		return portalRectangle{min: at, width: 1, height: 1, axis: [2]int32{1, 0}}
	}
	limit := func(x, y, z, sx, sy, sz, maxSteps int32) int32 {
		var n int32
		for n < maxSteps && w.BlockAt(x+sx*(n+1), y+sy*(n+1), z+sz*(n+1)) == state {
			n++
		}
		return n
	}
	left := limit(at.X, at.Y, at.Z, -dx, 0, -dz, 21)
	right := limit(at.X, at.Y, at.Z, dx, 0, dz, 21)
	var down, up [43]int32
	down[left] = limit(at.X, at.Y, at.Z, 0, -1, 0, 21)
	up[left] = limit(at.X, at.Y, at.Z, 0, 1, 0, 21)
	for _, step := range []int32{-1, 1} {
		end := right
		if step < 0 {
			end = left
		}
		for i := int32(1); i <= end; i++ {
			idx := left + step*i
			x, z := at.X+dx*step*i, at.Z+dz*step*i
			down[idx] = limit(x, at.Y, z, 0, -1, 0, down[idx-step])
			up[idx] = limit(x, at.Y, z, 0, 1, 0, up[idx-step])
		}
	}
	best := portalRectangle{min: at, axis: [2]int32{dx, dz}}
	// Each histogram starts no lower than the seed column's bottom. Bounds
	// narrow away from that column, as in BlockUtil's boundsByAxis1.
	for bottom := -down[left]; bottom <= 0; bottom++ {
		var heights [44]int32
		count := left + right + 1
		for i := range count {
			if bottom >= -down[i] {
				heights[i] = up[i] - bottom + 1
			}
		}
		var stack [44]int32
		n := 0
		for i := int32(0); i <= count; i++ {
			for n > 0 && heights[stack[n-1]] > heights[i] {
				h := heights[stack[n-1]]
				n--
				start := int32(0)
				if n > 0 {
					start = stack[n-1] + 1
				}
				if width := i - start; width*h > best.width*best.height {
					best.min = world.SpawnPos{X: at.X + dx*(start-left), Y: at.Y + bottom, Z: at.Z + dz*(start-left)}
					best.width, best.height = width, h
				}
			}
			stack[n] = i
			n++
		}
	}
	return best
}

// NetherPortalPosition maps the entry offset, centering living bodies and vehicles when centered is true.
// vanilla: PortalShape.getRelativePosition, NetherPortalBlock.createDimensionTransition
func NetherPortalPosition(from, to BlockReader, entry, exit world.SpawnPos, pos [3]float64, width, height float64, centered bool) [3]float64 {
	right, up, forward := 0.5, 0.0, 0.0
	if IsPortalState(from.BlockAt(entry.X, entry.Y, entry.Z)) {
		r := portalRectangleAt(from, entry)
		along, across := pos[0], pos[2]
		minAlong, minAcross := float64(r.min.X), float64(r.min.Z)
		if r.axis[1] != 0 {
			along, across = across, along
			minAlong, minAcross = minAcross, minAlong
		}
		if span := float64(r.width) - width; span > 0 {
			right = max(0, min(1, (along-minAlong-width/2)/span))
		}
		if span := float64(r.height) - height; span > 0 {
			up = max(0, min(1, (pos[1]-float64(r.min.Y))/span))
		}
		if !centered {
			forward = across - minAcross - 0.5
		}
	}
	r := portalRectangleAt(to, exit)
	along := width/2 + (float64(r.width)-width)*right
	across := 0.5 + forward
	if r.axis[1] != 0 {
		along, across = across, along
	}
	return [3]float64{float64(r.min.X) + along, float64(r.min.Y) + (float64(r.height)-height)*up, float64(r.min.Z) + across}
}
