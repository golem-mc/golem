package registry

// FireIgniteOdds returns the spread odds, zero for waterlogged or nonflammable states.
// vanilla: FireBlock.getIgniteOdds
func FireIgniteOdds(state uint16) int {
	if int(state) >= StateCount || StateBool(state, PropWaterlogged) {
		return 0
	}
	return fireIgniteOdds[BlockOf(state)]
}

// FireBurnOdds returns the burn-out odds, with the same waterlogged rule as FireIgniteOdds.
// vanilla: FireBlock.getBurnOdds
func FireBurnOdds(state uint16) int {
	if int(state) >= StateCount || StateBool(state, PropWaterlogged) {
		return 0
	}
	return fireBurnOdds[BlockOf(state)]
}

// IgnitedByLava is the ignitedByLava block property for a block state, the
// flag the lava's fire-lighting reads; it is set per block, so unlike the
// fire odds above there is no waterlogged rule to apply.
// vanilla: LavaFluid.randomTick
func IgnitedByLava(state uint16) bool {
	return int(state) < StateCount && ignitedByLavaBlock[blockOfState[state]]
}

// ignitedByLavaBlock is ignitedByLava by block.
var ignitedByLavaBlock = BlocksWhere(func(name string) bool { return ignitedByLava[name] })
