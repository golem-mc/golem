package levelgen

import (
	"crypto/sha256"
	"encoding/binary"
	"math"
)

// BiomeZoomSeed is the world seed hashed for the biome zoom: the first eight
// bytes of the SHA-256 of its little-endian bytes, read little-endian.
// vanilla: BiomeManager.obfuscateSeed
func BiomeZoomSeed(seed int64) int64 {
	var in [8]byte
	binary.LittleEndian.PutUint64(in[:], uint64(seed))
	sum := sha256.Sum256(in[:])
	return int64(binary.LittleEndian.Uint64(sum[:8]))
}

// ZoomParent is the lowest of the eight quarts ZoomedQuart chooses among for a
// block; the others are it plus 0 or 1 on each axis.
func ZoomParent(x, y, z int32) (px, py, pz int32) {
	return (x - 2) >> 2, (y - 2) >> 2, (z - 2) >> 2
}

// zoomBounds is the minimum squared distance after any allowed axis jitter.
var zoomBounds = func() (bounds [4][2]float64) {
	for f := range bounds {
		for side := range bounds[f] {
			d := max(math.Abs(float64(f)/4-float64(side))-0.45, 0)
			bounds[f][side] = float64(d * d)
		}
	}
	return
}()

// ZoomedQuart returns the quart whose noise biome a block position reads: of
// the eight quart corners around the position shifted by -2, the one nearest
// once each corner is jittered by up to 0.45 from the zoom seed.
// vanilla: BiomeManager.getBiome
func ZoomedQuart(zoomSeed int64, x, y, z int32) (qx, qy, qz int32) {
	absX, absY, absZ := x-2, y-2, z-2
	parentX, parentY, parentZ := ZoomParent(x, y, z)
	fractX := float64(absX&3) / 4.0
	fractY := float64(absY&3) / 4.0
	fractZ := float64(absZ&3) / 4.0
	minI := 0
	minDistance := 0.0
	// Corner i is x<<2 | y<<1 | z, visited in order so ties keep the lower
	// index; the corners sharing x, or x and y, share the jitter's first steps.
	for xi := range int32(2) {
		cx := parentX + xi
		rx := lcgNext(zoomSeed, int64(cx))
		for yi := range int32(2) {
			cy := parentY + yi
			rxy := lcgNext(rx, int64(cy))
			for zi := range int32(2) {
				bound := float64(zoomBounds[absZ&3][zi]+zoomBounds[absY&3][yi]) + zoomBounds[absX&3][xi]
				if xi|yi|zi != 0 && bound >= minDistance {
					continue
				}
				fx, fy, fz := fiddleTail(zoomSeed, rxy, cx, cy, parentZ+zi)
				d := fiddledSum(fractX-float64(xi), fractY-float64(yi), fractZ-float64(zi), fx, fy, fz)
				if i := int(xi<<2 | yi<<1 | zi); i == 0 || minDistance > d {
					minI, minDistance = i, d
				}
			}
		}
	}
	return parentX + int32(minI>>2&1), parentY + int32(minI>>1&1), parentZ + int32(minI&1)
}

// lcgNext is vanilla: LinearCongruentialGenerator.next.
func lcgNext(rval, c int64) int64 {
	rval *= rval*6364136223846793005 + 1442695040888963407
	return rval + c
}

// fiddleOffsets is a corner's jitter on each axis.
// vanilla: BiomeManager.getFiddledDistance
func fiddleOffsets(seed int64, x, y, z int32) (fx, fy, fz float64) {
	return fiddleTail(seed, lcgNext(lcgNext(seed, int64(x)), int64(y)), x, y, z)
}

// fiddleTail is fiddleOffsets from rval, the seed already stepped by x and y.
func fiddleTail(seed, rval int64, x, y, z int32) (fx, fy, fz float64) {
	rval = lcgNext(rval, int64(z))
	rval = lcgNext(rval, int64(x))
	rval = lcgNext(rval, int64(y))
	rval = lcgNext(rval, int64(z))
	fx = fiddle(rval)
	rval = lcgNext(rval, seed)
	fy = fiddle(rval)
	rval = lcgNext(rval, seed)
	return fx, fy, fiddle(rval)
}

func fiddledSum(dx, dy, dz, fiddleX, fiddleY, fiddleZ float64) float64 {
	sz, sy, sx := dz+fiddleZ, dy+fiddleY, dx+fiddleX
	return float64(float64(sz*sz)+float64(sy*sy)) + float64(sx*sx)
}

// ZoomColumn is ZoomedQuart down one block column. The corner jitters of a
// quart layer serve the four y values inside it and the layer above, so
// walking a column computes each once.
type ZoomColumn struct {
	seed             int64
	parentX, parentZ int32
	fractX, fractZ   float64
	layers           [2]zoomLayer // by corner y & 1
}

// zoomLayer is the jitter of the four xz corners of one quart layer, [x][z].
type zoomLayer struct {
	cy  int32
	ok  bool
	off [2][2][3]float64
}

// NewZoomColumn starts a walk down the column of block (x, z).
func NewZoomColumn(zoomSeed int64, x, z int32) *ZoomColumn {
	absX, absZ := x-2, z-2
	return &ZoomColumn{
		seed: zoomSeed, parentX: absX >> 2, parentZ: absZ >> 2,
		fractX: float64(absX&3) / 4.0, fractZ: float64(absZ&3) / 4.0,
	}
}

func (c *ZoomColumn) layer(cy int32) *zoomLayer {
	l := &c.layers[cy&1]
	if l.ok && l.cy == cy {
		return l
	}
	l.cy, l.ok = cy, true
	for xi := range int32(2) {
		for zi := range int32(2) {
			fx, fy, fz := fiddleOffsets(c.seed, c.parentX+xi, cy, c.parentZ+zi)
			l.off[xi][zi] = [3]float64{fx, fy, fz}
		}
	}
	return l
}

// At is ZoomedQuart(zoomSeed, x, y, z) for the column's x and z.
func (c *ZoomColumn) At(y int32) (qx, qy, qz int32) {
	absY := y - 2
	parentY := absY >> 2
	fractY := float64(absY&3) / 4.0
	layers := [2]*zoomLayer{c.layer(parentY), c.layer(parentY + 1)}
	minI := 0
	minDistance := 0.0
	for i := range 8 {
		xi, yi, zi := (i>>2)&1, (i>>1)&1, i&1
		dx, dy, dz := c.fractX-float64(xi), fractY-float64(yi), c.fractZ-float64(zi)
		o := &layers[yi].off[xi][zi]
		d := fiddledSum(dx, dy, dz, o[0], o[1], o[2])
		if i == 0 || minDistance > d {
			minI, minDistance = i, d
		}
	}
	return c.parentX + int32(minI>>2&1), parentY + int32(minI>>1&1), c.parentZ + int32(minI&1)
}

// fiddle is a corner's jitter on one axis, in [-0.45, 0.45).
// vanilla: BiomeManager.getFiddle
func fiddle(rval int64) float64 {
	uniform := float64((uint64(rval)>>24)&1023) / 1024.0
	return (uniform - 0.5) * 0.9
}
