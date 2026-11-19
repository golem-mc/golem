package terrain

// The configured carvers, with the values of data/minecraft/worldgen/carver.

// overworldCave builds cave and cave_extra_underground, which differ in
// probability and the highest start y. vanilla: carver/cave.json, carver/cave_extra_underground.json
func overworldCave(probability float32, yMax int32) *caveCarver {
	return &caveCarver{
		probability:         probability,
		y:                   uniformHeight{anchor{anchorAboveBottom, 8}, anchor{anchorAbsolute, yMax}},
		count:               veryBiasedToBottomInt(0, 14),
		thickness:           trapezoidFloat(0, 3, 1),
		weirdThicknessBias:  true,
		roomVerticalRadius:  uniformFloat(0.1, 0.9),
		hRadius:             uniformFloat(0.7, 1.4),
		vRadius:             uniformFloat(0.8, 1.3),
		startVerticalRadius: constantFloat(1),
		floorLevel:          uniformFloat(-1, -0.4),
	}
}

// vanilla: carver/nether_cave.json
var netherCave = &caveCarver{
	probability:         0.2,
	y:                   uniformHeight{anchor{anchorAbsolute, 0}, anchor{anchorBelowTop, 1}},
	count:               veryBiasedToBottomInt(0, 9),
	thickness:           trapezoidFloat(0, 6, 2),
	roomVerticalRadius:  constantFloat(0.5),
	hRadius:             constantFloat(1),
	vRadius:             constantFloat(1),
	startVerticalRadius: constantFloat(5),
	floorLevel:          constantFloat(-0.7),
}

// vanilla: carver/canyon.json
var canyon = &canyonCarver{
	probability:                 0.01,
	y:                           uniformHeight{anchor{anchorAbsolute, 10}, anchor{anchorAbsolute, 67}},
	verticalRotation:            uniformFloat(-0.125, 0.125),
	distanceFactor:              uniformFloat(0.75, 1),
	thickness:                   trapezoidFloat(0, 6, 2),
	horizontalRadiusFactor:      uniformFloat(0.75, 1),
	yScale:                      constantFloat(3),
	widthSmoothness:             3,
	verticalRadiusDefaultFactor: 1,
	verticalRadiusCenterFactor:  0,
}

// OverworldCarvers is the carver list of every overworld biome that has one
// (all but the End's), in the order the seed index counts them.
// vanilla: BiomeDefaultFeatures.addDefaultCarversAndLakes
var OverworldCarvers = []Carver{overworldCave(0.15, 180), overworldCave(0.07, 47), canyon}

// NetherCarvers is the carver list of every Nether biome.
var NetherCarvers = []Carver{netherCave}
