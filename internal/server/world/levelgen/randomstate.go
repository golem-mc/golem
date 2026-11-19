package levelgen

// NewXoroshiroNamedRandom is the positional factory RandomState hands out for
// a name when the dimension's noise settings use the Xoroshiro random source,
// as the overworld's do; name carries its namespace, "minecraft:bedrock_floor".
// vanilla: RandomState.getOrCreateRandomFactory
func NewXoroshiroNamedRandom(seed int64, name string) PositionalRandomFactory {
	return NewXoroshiroRandom(seed).ForkPositional().FromHashOf(name).ForkPositional()
}

// NewLegacyNamedRandom is NewXoroshiroNamedRandom for noise settings with
// legacy_random_source, as the nether's have.
// vanilla: RandomState.getOrCreateRandomFactory
func NewLegacyNamedRandom(seed int64, name string) PositionalRandomFactory {
	return NewLegacyRandom(seed).ForkPositional().FromHashOf(name).ForkPositional()
}
