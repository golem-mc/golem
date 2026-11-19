package registry

var explosionResistance = func() *[BlockCount]float64 {
	t := new([BlockCount]float64)
	for b := range Block(BlockCount) {
		t[b] = blockExplosionResistance[blockInfo[b].name]
	}
	return t
}()

// ExplosionResistance is the block's blast resistance, without any fluid
// held by its state; an unknown block resists nothing.
func (b Block) ExplosionResistance() float64 {
	if b >= BlockCount {
		return 0
	}
	return explosionResistance[b]
}
