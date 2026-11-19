package world

import "github.com/golem-mc/golem/registry"

// markAbove queues up to two non-air blocks above a feature's write for FULL
// shape updates. Unsupported plants stay until then, so later features see them.
// vanilla: Feature.markAboveForPostProcessing, LevelChunk.postProcessGeneration
func (r *decorRegion) markAbove(x, y, z int32) {
	for i := int32(1); i <= 2; i++ {
		if r.src.isAir(r.get(x, y+i, z)) {
			return
		}
		ps := r.protoAt(x, z)
		ps.shapes = append(ps.shapes, posKey{x & 15, y + i - r.minY, z & 15})
	}
}

// dropUnsupported settles a marked plant after its support changed; cane and
// a dripleaf stem wait for their scheduled tick, while tall plants lose both
// halves in order.
// vanilla: Block.updateFromNeighbourShapes
func (r *decorRegion) dropUnsupported(x, y, z int32) {
	state := r.get(x, y, z)
	if pos := [3]int32{x, y, z}; !stateSurvives(r, state, pos) {
		// SugarCaneBlock.updateShape and BigDripleafStemBlock.updateShape
		// schedule the block's death without changing state.
		if b := registry.BlockOf(state); b == registry.BlockSugarCane || b == registry.BlockBigDripleafStem {
			r.scheduleTick(x, y, z)
			return
		}
		if registry.BlockOf(state).InTag(registry.TagIDRails) {
			return // vanilla: BaseRailBlock.updateShape
		}
		r.set(x, y, z, airState)
	}
}
