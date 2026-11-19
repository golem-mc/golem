package terrain

// ProtoChunk is a chunk of materials under construction; Height follows Set
// like the WORLD_SURFACE_WG heightmap does. vanilla: ProtoChunk, Heightmap.update
type ProtoChunk struct {
	minY, height int32
	defaultMat   Material
	blocks       []Material
	tops         [256]int32
}

// NewProtoChunk returns an air chunk height blocks tall from minY.
func NewProtoChunk(minY, height int32, defaultMat Material) *ProtoChunk {
	c := &ProtoChunk{minY: minY, height: height, defaultMat: defaultMat, blocks: make([]Material, 256*height)}
	for i := range c.tops {
		c.tops[i] = minY
	}
	return c
}

// Reset empties the chunk for reuse, keeping its block buffer.
func (c *ProtoChunk) Reset() {
	clear(c.blocks)
	for i := range c.tops {
		c.tops[i] = c.minY
	}
}

func (c *ProtoChunk) index(x, y, z int32) int { return int(((z&15)*16+(x&15))*c.height + y - c.minY) }

func (c *ProtoChunk) MinY() int32 { return c.minY }

func (c *ProtoChunk) MaxY() int32 { return c.minY + c.height - 1 }

// Height is the y of the top block; an empty column reads minY - 1.
func (c *ProtoChunk) Height(lx, lz int32) int32 { return c.tops[lz*16+lx] - 1 }

func (c *ProtoChunk) TopFilledY() int32 {
	for s := c.height/16 - 1; s >= 0; s-- {
		for i := range 256 {
			for y := s * 16; y < s*16+16; y++ {
				if c.blocks[int32(i)*c.height+y] != Air {
					return c.minY + s*16 + 15
				}
			}
		}
	}
	return c.minY - 1
}

// Column is the column at local x, z, bottom first: index 0 is minY.
func (c *ProtoChunk) Column(x, z int32) []Material {
	return c.blocks[c.index(x, c.minY, z):][:c.height]
}

// Get is the material at x, y, z; x and z wrap into the chunk.
func (c *ProtoChunk) Get(x, y, z int32) Material { return c.blocks[c.index(x, y, z)] }

func (c *ProtoChunk) Block(x, y, z int32) ColumnBlock {
	switch m := c.Get(x, y, z); {
	case m == Air:
		return ColumnAir
	case m.IsFluid():
		return ColumnFluid
	case m == c.defaultMat:
		return ColumnDefault
	}
	return ColumnOther
}

func (c *ProtoChunk) Set(x, y, z int32, m Material) {
	c.blocks[c.index(x, y, z)] = m
	col := (z&15)*16 + (x & 15)
	first := c.tops[col]
	if y <= first-2 {
		return
	}
	if m != Air {
		if y >= first {
			c.tops[col] = y + 1
		}
	} else if first-1 == y {
		c.tops[col] = c.minY
		for i := y - 1; i >= c.minY; i-- {
			if c.Get(x, i, z) != Air {
				c.tops[col] = i + 1
				break
			}
		}
	}
}
