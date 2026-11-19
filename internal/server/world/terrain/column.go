package terrain

// NoiseColumn holds a base terrain column, bottom to top, before material rules.
// vanilla: NoiseColumn
type NoiseColumn struct {
	MinY   int32
	Blocks []Material
}

// BaseColumn samples the noise range intersecting the supplied height accessor.
// A disjoint range returns nil. vanilla: NoiseBasedChunkGenerator.getBaseColumn
func (rs *RandomState) BaseColumn(x, z, minY, height int32) *NoiseColumn {
	bottom, top := max(rs.MinY, minY), min(rs.MinY+rs.Height, minY+height)
	if top <= bottom {
		return nil
	}
	col := &NoiseColumn{MinY: bottom, Blocks: make([]Material, top-bottom)}
	rs.iterateNoiseColumn(x, z, bottom, top-bottom, func(y int32, m Material) bool {
		col.Blocks[y-bottom] = m
		return false
	})
	return col
}

// BaseHeight is one above the highest base block accepted by opaque, or minY
// when none qualifies. vanilla: NoiseBasedChunkGenerator.getBaseHeight
func (rs *RandomState) BaseHeight(x, z, minY, height int32, opaque func(Material) bool) int32 {
	bottom, top := max(rs.MinY, minY), min(rs.MinY+rs.Height, minY+height)
	if top <= bottom {
		return minY
	}
	y, ok := rs.iterateNoiseColumn(x, z, bottom, top-bottom, func(_ int32, m Material) bool { return opaque(m) })
	if !ok {
		return minY
	}
	return y + 1
}

// iterateNoiseColumn uses sampleVolume, whose float interpolation can differ
// from sampling the same positions with sampleValue.
// vanilla: NoiseBasedChunkGenerator.iterateNoiseColumn
func (rs *RandomState) iterateNoiseColumn(x, z, minY, height int32, visit func(int32, Material) bool) (int32, bool) {
	n := NewNoiseChunk(rs, nil, NewVolume(1, height, 1, x, minY, z))
	defer n.Recycle()
	density := n.SampleDensity()
	defer n.ReleaseDensity(density)
	for i := height - 1; i >= 0; i-- {
		y := minY + i
		m, ok := substanceMaterial(n.aquifer.ComputeSubstance(x, y, z, float64(density[i])))
		if !ok {
			m = rs.Materials.defaultMat
		}
		if visit(y, m) {
			return y, true
		}
	}
	return 0, false
}

// TopDefaultY is the highest block of the column whose substance is the default
// block, or the world's bottom when none is. It matches what DoFill reports
// over a chunk, without sampling the chunk.
func (rs *RandomState) TopDefaultY(x, z int32) int32 {
	n := NewNoiseChunk(rs, nil, NewVolume(1, rs.Height, 1, x, rs.MinY, z))
	defer n.Recycle()
	density := n.SampleDensity()
	defer n.ReleaseDensity(density)
	for i := rs.Height - 1; i >= 0; i-- {
		y := rs.MinY + i
		if n.aquifer.ComputeSubstance(x, y, z, float64(density[i])) == SubstanceDefault {
			return y
		}
	}
	return rs.MinY
}
