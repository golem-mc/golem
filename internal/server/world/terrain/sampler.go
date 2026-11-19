package terrain

import (
	"math"
	"sync"
)

// Sampler is vanilla: DensitySampler.
type Sampler interface {
	// Sample is vanilla: DensitySampler.sampleValue.
	Sample(c *Context, x, y, z int32) float32
	// SampleVolume is vanilla: DensitySampler.sampleVolume.
	SampleVolume(c *Context, out []float32, v Volume)
}

// sampleVolumeNaive samples every position of a volume one by one.
// vanilla: DensitySampler.sampleVolumeNaive
func sampleVolumeNaive(c *Context, out []float32, v Volume, s Sampler) {
	index := 0
	for z := range v.SizeZ {
		bz := v.BlockZ(z)
		for x := range v.SizeX {
			bx := v.BlockX(x)
			for y := range v.SizeY {
				out[index] = s.Sample(c, bx, v.BlockY(y), bz)
				index++
			}
		}
	}
}

// cacheCell holds what one cache node remembers. vanilla: SamplerContext.CacheCell
type cacheCell struct {
	vol    Volume
	buf    []float32
	hasBuf bool
	key    int64
	value  float32
}

// Context is the state a chunk's samplers share: the beardifier, the cache
// cells of cache nodes and scratch buffers. vanilla: SamplerContext
type Context struct {
	// Beardifier is the chunk's structure density; nil samples as 0.
	Beardifier *Beardifier
	// caches is nil when caching is off. vanilla: SamplerContext.EMPTY_UNCACHED
	caches  []cacheCell
	scratch [][]float32
	// pool is the scratchPools index this context borrows from and returns to.
	pool int
}

// NewContext returns a context with caching on. vanilla: SamplerContext.Builder.enableCaches
func NewContext(b *Beardifier) *Context { return newContext(b, 0) }

func newContext(b *Beardifier, pool int) *Context {
	c := &Context{Beardifier: b, caches: []cacheCell{}, pool: pool}
	if s, ok := scratchPools[pool].Get().(*[][]float32); ok {
		c.scratch = *s
	}
	return c
}

// scratchPools hold the free scratch buffers of finished contexts, so a chunk
// does not allocate its density volumes anew. Chunk-sized contexts have their
// own pool (index 1), so a column's small buffers do not stand in for a
// chunk's.
var scratchPools [2]sync.Pool

// recycle hands the free scratch buffers to the next context; the context must
// not be used afterwards.
func (c *Context) recycle() {
	for i := range c.caches {
		if b := c.caches[i].buf; b != nil {
			c.scratch = append(c.scratch, b[:cap(b)])
		}
	}
	c.caches = nil
	s := c.scratch
	c.scratch = nil
	scratchPools[c.pool].Put(&s)
}

// NewUncachedContext returns a context without caching. vanilla: SamplerContext.EMPTY_UNCACHED
func NewUncachedContext() *Context { return &Context{} }

// acquire returns a scratch buffer of n values, the smallest free one that
// fits; release hands it back. vanilla: SamplerContext.acquireBuffer
func (c *Context) acquire(n int) []float32 {
	best := -1
	for i, b := range c.scratch {
		if cap(b) >= n && (best < 0 || cap(b) < cap(c.scratch[best])) {
			best = i
		}
	}
	if best < 0 {
		return make([]float32, n)
	}
	b := c.scratch[best]
	last := len(c.scratch) - 1
	c.scratch[best] = c.scratch[last]
	c.scratch = c.scratch[:last]
	return b[:n]
}

func (c *Context) release(b []float32) { c.scratch = append(c.scratch, b) }

// cell returns the cache cell of a cache id, or nil when caching is off.
func (c *Context) cell(id int) *cacheCell {
	if c.caches == nil {
		return nil
	}
	for len(c.caches) <= id {
		c.caches = append(c.caches, cacheCell{value: float32(math.NaN())})
	}
	return &c.caches[id]
}

// blockPosKey is vanilla: BlockPos.asLong.
func blockPosKey(x, y, z int32) int64 {
	return int64(x&0x3FFFFFF)<<38 | int64(z&0x3FFFFFF)<<12 | int64(y&0xFFF)
}

// cacheSampler is a cache node: it remembers the volume sampled last and the
// value sampled last. vanilla: CachingDensitySampler
type cacheSampler struct {
	id    int
	input Sampler
}

// SampleVolume is vanilla: SamplerContext.sampleVolumeCached.
func (s *cacheSampler) SampleVolume(c *Context, out []float32, v Volume) {
	cell := c.cell(s.id)
	if cell == nil {
		s.input.SampleVolume(c, out, v)
		return
	}
	if !cell.hasBuf || cell.vol != v {
		cell.vol = v
		if cap(cell.buf) >= v.Size() {
			cell.buf = cell.buf[:v.Size()]
		} else {
			if cell.buf != nil {
				c.release(cell.buf)
			}
			cell.buf = c.acquire(v.Size())
		}
		cell.hasBuf = true
		s.input.SampleVolume(c, cell.buf, v)
	}
	copy(out, cell.buf)
}

// Sample is vanilla: SamplerContext.sampleValueCached.
func (s *cacheSampler) Sample(c *Context, x, y, z int32) float32 {
	cell := c.cell(s.id)
	if cell == nil {
		return s.input.Sample(c, x, y, z)
	}
	key := blockPosKey(x, y, z)
	if cell.key == key && !math.IsNaN(float64(cell.value)) {
		return cell.value
	}
	if cell.hasBuf {
		if i := cell.vol.IndexOfBlock(x, y, z); i != -1 {
			return cell.buf[i]
		}
	}
	value := s.input.Sample(c, x, y, z)
	cell.key = key
	cell.value = value
	return value
}

// graph numbers the cache nodes of one set of samplers; two caches of the
// same function share a node. vanilla: DensityFunctionCompiler.reuseOrPrepareCache
type graph struct {
	next  int
	cache map[Sampler]*cacheSampler
}

func newGraph() *graph { return &graph{cache: map[Sampler]*cacheSampler{}} }

// Cache wraps a sampler in a cache node. vanilla: DensityFunctions.cache
func (g *graph) Cache(in Sampler) Sampler {
	if s, ok := g.cache[in]; ok {
		return s
	}
	s := &cacheSampler{g.next, in}
	g.next++
	g.cache[in] = s
	return s
}
