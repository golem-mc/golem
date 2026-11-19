package levelgen

import (
	"math"
	"sort"
)

// climateDimensions is Climate.PARAMETER_COUNT: temperature, humidity,
// continentalness, erosion, depth, weirdness and the offset.
const climateDimensions = 7

// climateParam is a quantized parameter range, both ends inclusive.
// vanilla: Climate.Parameter
type climateParam struct{ min, max int64 }

// distance is how far a target lies outside the range, 0 inside it.
func (p climateParam) distance(target int64) int64 {
	above := target - p.max
	if above > 0 {
		return above
	}
	return max(p.min-target, 0)
}

// span is the smallest range holding both.
func (p climateParam) span(o climateParam) climateParam {
	return climateParam{min(p.min, o.min), max(p.max, o.max)}
}

// climatePoint is a point of the parameter list: the six ranges and the
// offset, as the R-tree's seven dimensions.
// vanilla: Climate.ParameterPoint.parameterSpace
type climatePoint [climateDimensions]climateParam

// climateTarget is a sampled climate, quantized: temperature, humidity,
// continentalness, erosion, depth, weirdness and a zero offset.
// vanilla: Climate.TargetPoint.toParameterArray
type climateTarget [climateDimensions]int64

// rtreeNode is a leaf holding a biome, or a sub-tree over bounded children
// whose space spans theirs.
// vanilla: Climate.RTree.Node
type rtreeNode struct {
	space    climatePoint
	children []*rtreeNode
	biome    uint8
}

// rtreeChildren is Climate.RTree.CHILDREN_PER_NODE.
const rtreeChildren = 6

// distance is the squared distance from the target to the node's box.
// vanilla: Climate.RTree.Node.distance
func (n *rtreeNode) distance(target *climateTarget) int64 {
	var d int64
	for i := range climateDimensions {
		v := n.space[i].distance(target[i])
		d += v * v
	}
	return d
}

// distanceBelow returns the exact distance only while it can improve limit.
func (n *rtreeNode) distanceBelow(target *climateTarget, limit int64) int64 {
	var d int64
	for i := range climateDimensions {
		v := n.space[i].distance(target[i])
		d += v * v
		// Squared terms cannot decrease the sum; ties keep the candidate.
		if d >= limit {
			return limit
		}
	}
	return d
}

// search returns the leaf nearest the target, a later leaf replacing an
// earlier one only when strictly nearer, so the first of equals in tree order
// wins. vanilla: Climate.RTree.SubTree.search
func (n *rtreeNode) search(target *climateTarget, candidate *rtreeNode) *rtreeNode {
	if n.children == nil {
		return n
	}
	minDistance := int64(math.MaxInt64)
	if candidate != nil {
		minDistance = candidate.distance(target)
	}
	closest := candidate
	for _, child := range n.children {
		childDistance := child.distanceBelow(target, minDistance)
		if minDistance <= childDistance {
			continue
		}
		leaf := child.search(target, closest)
		leafDistance := childDistance
		if child != leaf {
			leafDistance = leaf.distance(target)
		}
		if minDistance <= leafDistance {
			continue
		}
		minDistance, closest = leafDistance, leaf
	}
	return closest
}

// newSubTree spans its children's boxes.
// vanilla: Climate.RTree.buildParameterSpace
func newSubTree(children []*rtreeNode) *rtreeNode {
	n := &rtreeNode{children: children}
	n.space = children[0].space
	for _, c := range children[1:] {
		for d := range climateDimensions {
			n.space[d] = c.space[d].span(n.space[d])
		}
	}
	return n
}

// centre is the integer-truncated middle of a range.
func (p climateParam) centre() int64 { return (p.min + p.max) / 2 }

func absInt64(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}

// sortNodes sorts by dimension's centre, then by the following dimensions'
// (wrapping), stably, so equal nodes keep the order of the previous sort.
// vanilla: Climate.RTree.sort
func sortNodes(nodes []*rtreeNode, dimension int, absolute bool) {
	sort.SliceStable(nodes, func(i, j int) bool {
		for d := range climateDimensions {
			k := (dimension + d) % climateDimensions
			a, b := nodes[i].space[k].centre(), nodes[j].space[k].centre()
			if absolute {
				a, b = absInt64(a), absInt64(b)
			}
			if a != b {
				return a < b
			}
		}
		return false
	})
}

// bucketize cuts the sorted nodes into runs of fanout^floor(log_fanout(n)) children.
// vanilla: Climate.RTree.bucketize
func bucketize(nodes []*rtreeNode, fanout int) []*rtreeNode {
	f := float64(fanout)
	expected := int(math.Pow(f, math.Floor(math.Log(float64(len(nodes))-0.01)/math.Log(f))))
	var buckets []*rtreeNode
	var run []*rtreeNode
	for _, n := range nodes {
		run = append(run, n)
		if len(run) < expected {
			continue
		}
		buckets = append(buckets, newSubTree(run))
		run = nil
	}
	if len(run) > 0 {
		buckets = append(buckets, newSubTree(run))
	}
	return buckets
}

// cost is the summed extent of a box.
// vanilla: Climate.RTree.cost
func (n *rtreeNode) cost() int64 {
	var c int64
	for _, p := range n.space {
		c += absInt64(p.max - p.min)
	}
	return c
}

// buildRTree is vanilla: Climate.RTree.build.
func buildRTree(children []*rtreeNode, fanout int) *rtreeNode {
	if len(children) == 1 {
		return children[0]
	}
	if len(children) <= fanout {
		sort.SliceStable(children, func(i, j int) bool {
			var a, b int64
			for d := range climateDimensions {
				a += absInt64(children[i].space[d].centre())
				b += absInt64(children[j].space[d].centre())
			}
			return a < b
		})
		return newSubTree(children)
	}
	minCost := int64(math.MaxInt64)
	minDimension := -1
	var minBuckets []*rtreeNode
	for d := range climateDimensions {
		sortNodes(children, d, false)
		buckets := bucketize(children, fanout)
		var total int64
		for _, b := range buckets {
			total += b.cost()
		}
		if minCost <= total {
			continue
		}
		minCost, minDimension, minBuckets = total, d, buckets
	}
	sortNodes(minBuckets, minDimension, true)
	built := make([]*rtreeNode, len(minBuckets))
	for i, b := range minBuckets {
		built[i] = buildRTree(b.children, fanout)
	}
	return newSubTree(built)
}
