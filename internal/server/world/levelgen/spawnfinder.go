package levelgen

import "math"

// FindSpawnPosition chooses a block position nearest the target climates.
// sample takes quart coordinates and returns the six quantized climate axes.
// vanilla: Climate.SpawnFinder
func FindSpawnPosition(sample func(qx, qz int32) [6]int64, targets []ClimatePoint) (x, z int32) {
	if len(targets) == 0 {
		return 0, 0
	}
	fitness := func(x, z int32) int64 {
		t := sample(x>>2, z>>2)
		t[4] = 0
		best := int64(math.MaxInt64)
		for _, point := range targets {
			var distance int64
			for axis, bounds := range point.Bounds {
				var value int64
				if axis < len(t) {
					value = t[axis]
				}
				d := (climateParam{bounds[0], bounds[1]}).distance(value)
				distance += d * d
			}
			best = min(best, distance)
		}
		return best*2048*2048 + int64(x)*int64(x) + int64(z)*int64(z)
	}
	best := fitness(0, 0)
	for _, search := range [][2]float32{{2048, 512}, {512, 32}} {
		originX, originZ := x, z
		angle, radius := float32(0), search[1]
		for radius <= search[0] {
			cx := originX + int32(math.Sin(float64(angle))*float64(radius))
			cz := originZ + int32(math.Cos(float64(angle))*float64(radius))
			if f := fitness(cx, cz); f < best {
				x, z, best = cx, cz, f
			}
			angle += search[1] / radius
			if float64(angle) > math.Pi*2 {
				angle = 0
				radius += search[1]
			}
		}
	}
	return x, z
}

// OverworldSpawnTargets is the overworld's surface climate away from rivers.
// vanilla: OverworldBiomeBuilder.spawnTarget
func OverworldSpawnTargets() []ClimatePoint {
	points := make([]ClimatePoint, 2)
	for i, weirdness := range [][2]float32{{-1, -0.16}, {0.16, 1}} {
		for axis, bounds := range [7][2]float32{{-1, 1}, {-1, 1}, {-0.11, 1}, {-1, 1}, {0, 0}, weirdness, {0, 0}} {
			points[i].Bounds[axis] = [2]int64{quantizeCoord(bounds[0]), quantizeCoord(bounds[1])}
		}
	}
	return points
}
