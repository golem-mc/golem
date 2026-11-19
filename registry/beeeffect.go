package registry

// BeeEffect is the effect a flower gives a bee that touches it or is fed it.
type BeeEffect struct {
	Effect   int32
	Duration int32 // ticks
}

var beeEffects = func() (t [BlockCount]*BeeEffect) {
	t[BlockWitherRose] = &BeeEffect{MustMobEffect("minecraft:wither"), 40}
	t[BlockOpenEyeblossom] = &BeeEffect{MustMobEffect("minecraft:poison"), 25}
	return t
}()

// BeeInteractionEffect returns the effect the block gives a bee, false for a
// block that gives none. vanilla: FlowerBlock.getBeeInteractionEffect
func (b Block) BeeInteractionEffect() (BeeEffect, bool) {
	if b >= BlockCount || beeEffects[b] == nil {
		return BeeEffect{}, false
	}
	return *beeEffects[b], true
}

// BeeInteractionEffect returns the effect a bee takes from feeding it the
// item, false for an item that places no such flower.
// vanilla: Bee.mobInteract
func (i Item) BeeInteractionEffect() (BeeEffect, bool) {
	b, ok := i.Block()
	if !ok {
		return BeeEffect{}, false
	}
	return b.BeeInteractionEffect()
}
