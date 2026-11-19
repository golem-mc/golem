package levelgen

// NetherBiome is one of the Nether's five biomes.
type NetherBiome uint8

// The Nether's biomes, in the order the parameter list names them.
const (
	NetherWastes NetherBiome = iota
	SoulSandValley
	CrimsonForest
	WarpedForest
	BasaltDeltas
)

// quantizeCoord is vanilla: Climate.quantizeCoord, worked in float.
func quantizeCoord(v float32) int64 {
	return int64(float32(v * 10000))
}
