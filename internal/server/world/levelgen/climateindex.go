package levelgen

// ClimatePoint is a quantized parameter box and its biome, with temperature,
// humidity, continentalness, erosion, depth, weirdness and offset in that order.
// vanilla: Climate.ParameterPoint.parameterSpace
type ClimatePoint struct {
	Bounds [7][2]int64
	// Biome is an opaque id belonging to the caller.
	Biome uint8
}

// ClimateIndex is an immutable parameter tree shared by independent searches.
// vanilla: Climate.RTree
type ClimateIndex struct{ root *rtreeNode }

// NewClimateIndex builds a parameter tree with the configured children per node.
// Empty points, reversed ranges and a fanout below two are invalid.
// vanilla: Climate.RTree.create
func NewClimateIndex(points []ClimatePoint, fanout int) *ClimateIndex {
	if len(points) == 0 || fanout < 2 {
		panic("climate index needs points and at least two children per node")
	}
	leaves := make([]*rtreeNode, len(points))
	for i, p := range points {
		n := &rtreeNode{biome: p.Biome}
		for d, bounds := range p.Bounds {
			if bounds[0] > bounds[1] {
				panic("climate parameter has reversed bounds")
			}
			n.space[d] = climateParam{bounds[0], bounds[1]}
		}
		leaves[i] = n
	}
	return &ClimateIndex{buildRTree(leaves, fanout)}
}

// ClimateSearch keeps the last result, which wins equal fitness ties.
// A search belongs to one goroutine. vanilla: Climate.RTree.lastResult
type ClimateSearch struct {
	index *ClimateIndex
	last  *rtreeNode
}

// NewSearch starts a search with no previous result.
func (i *ClimateIndex) NewSearch() *ClimateSearch { return &ClimateSearch{index: i} }

// Find returns the nearest biome to the six quantized climate coordinates.
// vanilla: Climate.RTree.search, Climate.TargetPoint.toParameterArray
func (s *ClimateSearch) Find(target [6]int64) uint8 {
	t := climateTarget{target[0], target[1], target[2], target[3], target[4], target[5], 0}
	s.last = s.index.root.search(&t, s.last)
	return uint8(s.last.biome)
}
