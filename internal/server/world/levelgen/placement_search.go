package levelgen

import "math"

// NearestRandomSpread scans the grid cells in square rings of growing radius
// around a chunk and returns the placement chunk of the first cell, in scan
// order, whose structure starts there. The radius counts cells, not chunks.
// vanilla: ChunkGenerator.getNearestGeneratedStructure (random spread)
func NearestRandomSpread(seed int64, spacing, separation, salt, originChunkX, originChunkZ int32, maxRadius int, starts func(chunkX, chunkZ int32) bool) (int32, int32, bool) {
	for radius := int32(0); radius <= int32(maxRadius); radius++ {
		if cx, cz, ok := RandomSpreadRing(seed, spacing, separation, salt, originChunkX, originChunkZ, radius, starts); ok {
			return cx, cz, true
		}
	}
	return 0, 0, false
}

// RandomSpreadRing scans the edge cells of one ring and returns the placement
// chunk of the first cell, in scan order, whose structure starts there.
// vanilla: ChunkGenerator.getNearestGeneratedStructure (random spread)
func RandomSpreadRing(seed int64, spacing, separation, salt, originChunkX, originChunkZ, radius int32, starts func(chunkX, chunkZ int32) bool) (int32, int32, bool) {
	for x := -radius; x <= radius; x++ {
		xEdge := x == -radius || x == radius
		for z := -radius; z <= radius; z++ {
			if !xEdge && z != -radius && z != radius {
				continue
			}
			cx, cz := RandomSpreadChunk(seed, originChunkX+spacing*x, originChunkZ+spacing*z, spacing, separation, salt)
			if starts(cx, cz) {
				return cx, cz, true
			}
		}
	}
	return 0, 0, false
}

// RingStart is a ring chunk before its move to a preferred biome, with the
// random that picks the move. Each ring chunk has its own random, so one can be
// resolved without the others.
type RingStart struct {
	X, Z   int32
	search RandomSource
}

// Resolve moves the start to a quart of the preferred biome within 112 blocks,
// or leaves it where it is when there is none. It consumes the start's random,
// so it is called once.
func (s RingStart) Resolve(preferred func(qx, qz int32) bool) [2]int32 {
	if bx, bz, ok := findBiomeHorizontal(s.X*16+8, s.Z*16+8, 112, preferred, s.search); ok {
		return [2]int32{bx >> 4, bz >> 4}
	}
	return [2]int32{s.X, s.Z}
}

// ResolveFixed is Resolve over a source of one biome everywhere: when that
// biome is preferred, the start moves to a random block within 112 of its
// chunk's centre, or else stays. It consumes the start's random too.
// vanilla: FixedBiomeSource.findBiomeHorizontal
func (s RingStart) ResolveFixed(preferred bool) [2]int32 {
	if !preferred {
		return [2]int32{s.X, s.Z}
	}
	x := s.X*16 + 8 - 112 + s.search.NextIntN(225)
	z := s.Z*16 + 8 - 112 + s.search.NextIntN(225)
	return [2]int32{x >> 4, z >> 4}
}

// ConcentricRingStarts lays out the chunks of a concentric_rings placement
// before any moves to a preferred biome: the stronghold set puts them 32 apart,
// 3 in the first ring, 128 in all.
// vanilla: ChunkGeneratorStructureState.generateRingPositions
func ConcentricRingStarts(seed int64, distance, spread, count int) []RingStart {
	random := NewLegacyRandom(seed)
	angle := random.NextDouble() * math.Pi * 2
	positionInCircle, circle := 0, 0
	out := make([]RingStart, 0, count)
	for i := range count {
		dist := float64(4*distance+distance*circle*6) + (random.NextDouble()-0.5)*(float64(distance)*2.5)
		initialX := int32(javaRound(math.Cos(angle) * dist))
		initialZ := int32(javaRound(math.Sin(angle) * dist))
		out = append(out, RingStart{X: initialX, Z: initialZ, search: random.Fork()})
		angle += math.Pi * 2 / float64(spread)
		positionInCircle++
		if positionInCircle != spread {
			continue
		}
		positionInCircle = 0
		circle++
		spread += 2 * spread / (circle + 1)
		spread = min(spread, count-i)
		angle += random.NextDouble() * math.Pi * 2
	}
	return out
}

// ConcentricRingChunks resolves every start of ConcentricRingStarts.
// preferred says whether the biome at a quart position is one a start moves to.
func ConcentricRingChunks(seed int64, distance, spread, count int, preferred func(qx, qz int32) bool) [][2]int32 {
	starts := ConcentricRingStarts(seed, distance, spread, count)
	out := make([][2]int32, len(starts))
	for i, s := range starts {
		out[i] = s.Resolve(preferred)
	}
	return out
}

// javaRound is Math.round: the floor of x + 0.5.
func javaRound(x float64) int64 { return int64(math.Floor(x + 0.5)) }

// findBiomeHorizontal samples every quart of the square of the radius around
// the block and picks one allowed quart uniformly at random, returning its
// block corner.
// vanilla: BiomeSource.findBiomeHorizontal with findClosest false
func findBiomeHorizontal(originX, originZ, radius int32, allowed func(qx, qz int32) bool, random RandomSource) (int32, int32, bool) {
	centreX, centreZ := originX>>2, originZ>>2
	noiseRadius := radius >> 2
	var rx, rz int32
	found, have := int32(0), false
	for z := -noiseRadius; z <= noiseRadius; z++ {
		for x := -noiseRadius; x <= noiseRadius; x++ {
			qx, qz := centreX+x, centreZ+z
			if !allowed(qx, qz) {
				continue
			}
			if !have || random.NextIntN(found+1) == 0 {
				rx, rz, have = qx<<2, qz<<2, true
			}
			found++
		}
	}
	return rx, rz, have
}
