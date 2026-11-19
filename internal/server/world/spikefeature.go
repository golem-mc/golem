package world

import "math"

// spikeFeature is SpikeFeature with its configured replacement predicates.
// vanilla: SpikeFeature.place
func spikeFeature(cfg jobj) featureFn {
	jonly(cfg, "state", "can_place_on", "can_replace")
	state := compileBlockState(cfg["state"])
	canPlaceOn, canReplace := compilePred(cfg["can_place_on"]), compilePred(cfg["can_replace"])
	return func(r *decorRegion, p [3]int32) bool {
		for r.isAir(p[0], p[1], p[2]) && p[1] > r.minY+2 {
			p[1]--
		}
		if !canPlaceOn(r, p) {
			return false
		}
		p[1] += r.rng.NextIntN(4)
		height := r.rng.NextIntN(4) + 7
		width := height/4 + r.rng.NextIntN(2)
		if width > 1 && r.rng.NextIntN(60) == 0 {
			p[1] += 10 + r.rng.NextIntN(30)
		}
		for y := range height {
			scale := (1 - float32(y)/float32(height)) * float32(width)
			rad := int32(math.Ceil(float64(scale)))
			for x := -rad; x <= rad; x++ {
				dx := float32(abs32(x)) - .25
				for z := -rad; z <= rad; z++ {
					dz := float32(abs32(z)) - .25
					if (x != 0 || z != 0) && dx*dx+dz*dz > scale*scale ||
						(x == -rad || x == rad || z == -rad || z == rad) && r.rng.NextFloat() > .75 {
						continue
					}
					at := [3]int32{p[0] + x, p[1] + y, p[2] + z}
					if r.isAir(at[0], at[1], at[2]) || canReplace(r, at) {
						r.set(at[0], at[1], at[2], state)
					}
					at[1] = p[1] - y
					if y != 0 && rad > 1 && (r.isAir(at[0], at[1], at[2]) || canReplace(r, at)) {
						r.set(at[0], at[1], at[2], state)
					}
				}
			}
		}
		pillarWidth := max(int32(0), min(int32(1), width-1))
		for x := -pillarWidth; x <= pillarWidth; x++ {
			for z := -pillarWidth; z <= pillarWidth; z++ {
				at := [3]int32{p[0] + x, p[1] - 1, p[2] + z}
				run := int32(50)
				if abs32(x) == 1 && abs32(z) == 1 {
					run = r.rng.NextIntN(5)
				}
				for at[1] > 50 {
					if !r.isAir(at[0], at[1], at[2]) && !canReplace(r, at) && r.get(at[0], at[1], at[2]) != state {
						break
					}
					r.set(at[0], at[1], at[2], state)
					at[1]--
					run--
					if run <= 0 {
						at[1] -= r.rng.NextIntN(5) + 1
						run = r.rng.NextIntN(5)
					}
				}
			}
		}
		return true
	}
}
