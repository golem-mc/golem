package levelgen

import (
	"math"
	"testing"
)

// TestZoomColumnMatchesZoomedQuart pins the cached walk to the per-block
// function over several columns and every y of the world.
func TestZoomColumnMatchesZoomedQuart(t *testing.T) {
	t.Parallel()
	seed := BiomeZoomSeed(12345)
	for _, xz := range [][2]int32{{0, 0}, {5, -7}, {-33, 100}, {1023, -1025}, {15, 15}} {
		col := NewZoomColumn(seed, xz[0], xz[1])
		for y := int32(330); y >= -70; y-- {
			wx, wy, wz := ZoomedQuart(seed, xz[0], y, xz[1])
			gx, gy, gz := col.At(y)
			if wx != gx || wy != gy || wz != gz {
				t.Fatalf("column %v y=%d: got %d,%d,%d, want %d,%d,%d", xz, y, gx, gy, gz, wx, wy, wz)
			}
		}
	}
}

// TestZoomedQuartMatchesVanillasLoop pins ZoomedQuart, which shares the
// jitter's first steps between corners, to vanilla's loop over the eight.
func TestZoomedQuartMatchesVanillasLoop(t *testing.T) {
	t.Parallel()
	for _, worldSeed := range []int64{0, 12345, -7, math.MinInt64, math.MaxInt64} {
		seed := BiomeZoomSeed(worldSeed)
		for _, base := range []int32{0, -40, math.MinInt32, math.MinInt32 + 4, math.MaxInt32 - 4, math.MaxInt32} {
			for fx := range int32(4) {
				for fy := range int32(4) {
					for fz := range int32(4) {
						x, y, z := base+fx, base+fy, base+fz
						want := zoomedQuartVanilla(seed, x, y, z)
						qx, qy, qz := ZoomedQuart(seed, x, y, z)
						if [3]int32{qx, qy, qz} != want {
							t.Fatalf("seed %d block %d,%d,%d: got %d,%d,%d want %v", worldSeed, x, y, z, qx, qy, qz, want)
						}
					}
				}
			}
		}
		for x := int32(-37); x < 37; x++ {
			for z := int32(-37); z < 37; z++ {
				for y := int32(-66); y < 70; y += 3 {
					want := zoomedQuartVanilla(seed, x, y, z)
					if qx, qy, qz := ZoomedQuart(seed, x, y, z); [3]int32{qx, qy, qz} != want {
						t.Fatalf("seed %d block %d,%d,%d: got %d,%d,%d, want %v", worldSeed, x, y, z, qx, qy, qz, want)
					}
				}
			}
		}
	}
}

// zoomedQuartVanilla is vanilla: BiomeManager.getBiome, line for line.
func zoomedQuartVanilla(seed int64, x, y, z int32) [3]int32 {
	absX, absY, absZ := x-2, y-2, z-2
	parentX, parentY, parentZ := absX>>2, absY>>2, absZ>>2
	fractX, fractY, fractZ := float64(absX&3)/4.0, float64(absY&3)/4.0, float64(absZ&3)/4.0
	minI, minDistance := 0, 0.0
	for i := range 8 {
		cx, cy, cz := parentX, parentY, parentZ
		dx, dy, dz := fractX, fractY, fractZ
		if i&4 != 0 {
			cx, dx = parentX+1, fractX-1.0
		}
		if i&2 != 0 {
			cy, dy = parentY+1, fractY-1.0
		}
		if i&1 != 0 {
			cz, dz = parentZ+1, fractZ-1.0
		}
		rval := seed
		for _, coord := range []int64{int64(cx), int64(cy), int64(cz), int64(cx), int64(cy), int64(cz)} {
			rval = vanillaZoomNext(rval, coord)
		}
		fx := vanillaZoomFiddle(rval)
		rval = vanillaZoomNext(rval, seed)
		fy := vanillaZoomFiddle(rval)
		rval = vanillaZoomNext(rval, seed)
		fz := vanillaZoomFiddle(rval)
		sz, sy, sx := dz+fz, dy+fy, dx+fx
		if d := float64(float64(sz*sz)+float64(sy*sy)) + float64(sx*sx); i == 0 || minDistance > d {
			minI, minDistance = i, d
		}
	}
	return [3]int32{parentX + int32(minI>>2&1), parentY + int32(minI>>1&1), parentZ + int32(minI&1)}
}

func vanillaZoomNext(rval, c int64) int64 {
	rval *= rval*6364136223846793005 + 1442695040888963407
	return rval + c
}
func vanillaZoomFiddle(rval int64) float64 {
	uniform := float64(((rval>>24)%1024+1024)%1024) / 1024.0
	return (uniform - 0.5) * 0.9
}
