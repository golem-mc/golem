package terrain

// vanilla: NoiseRouterData
const (
	// yBelowBottom and yAboveTop bound the y function. vanilla: DimensionType.MIN_Y * 2, MAX_Y * 2
	yBelowBottom = -4064
	yAboveTop    = 4062
)

// overworldRouter is vanilla: NoiseRouterData.bootstrap and overworld, for the
// normal, large-biomes and amplified presets.
func (rs *RandomState) overworldRouter(dim Dimension) Router {
	g := rs.graph
	large := dim == OverworldLarge
	amplified := dim == OverworldAmplified
	noise := func(name string) *NoiseStack { return rs.Noise("minecraft:" + name) }
	suffix := ""
	if large {
		suffix = "_large"
	}

	// These graph inputs depend only on x and z; slice outside each cache
	// so its stored volume also has one y row.
	cache2D := func(s Sampler) Sampler { return Slice(AxisY, 0, g.Cache(s)) }
	shiftX := cache2D(ShiftA(noise("offset")))
	shiftZ := cache2D(ShiftB(noise("offset")))
	shifted := func(name string) Sampler { return Slice(AxisY, 0, ShiftedNoise2D(shiftX, shiftZ, 0.25, noise(name))) }

	temperature := shifted("temperature" + suffix)
	vegetation := shifted("vegetation" + suffix)
	continents := cache2D(shifted("continentalness" + suffix))
	erosion := cache2D(shifted("erosion" + suffix))
	ridge := cache2D(shifted("ridge"))
	// vanilla: NoiseRouterData.peaksAndValleys
	ridgesFolded := Mul(Add(Abs(Add(Abs(ridge), constant(-0.6666667))), constant(-0.33333334)), constant(-3.0))
	jaggedNoise := Slice(AxisY, 0, NoiseSampler(noise("jagged"), 1500.0, 0.0))
	base3d := NewBlendedNoise(rs.terrainRandom(), 0.25, 0.125, 80.0, 160.0, 8.0)

	spline := func(f func(c terrainCoords) *splineNode) Sampler {
		set := &splineSet{}
		c := terrainCoords{set, continents, erosion, ridge, ridgesFolded}
		root := f(c)
		return set.Spline(root)
	}
	// vanilla: NoiseRouterData.splineWithBlending
	withBlending := func(s, target Sampler) Sampler { return cache2D(Lerp(BlendAlpha(), target, s)) }

	offset := withBlending(Add(constant(-0.50375), spline(func(c terrainCoords) *splineNode { return c.offsetSpline(amplified) })), BlendOffset())
	factor := withBlending(spline(func(c terrainCoords) *splineNode { return c.factorSpline(amplified) }), constant(10.0))
	depth := offsetToDepth(offset)
	unscaledJaggedness := withBlending(spline(func(c terrainCoords) *splineNode { return c.jaggednessSpline(amplified) }), constant(0.0))
	jaggedness := cache2D(Mul(unscaledJaggedness, HalfNegative(jaggedNoise)))
	initialDensity := noiseGradientDensity(factor, Add(depth, jaggedness))
	slopedCheese := g.Cache(Add(initialDensity, base3d))
	prelim := preliminarySurfaceLevel(offset, factor, amplified)
	chunkSurface := Interpolated(prelim, 16, 1)

	y := YClampedGradient(yBelowBottom, yAboveTop, yBelowBottom, yAboveTop)
	spaghettiRoughness := g.Cache(Mul(
		MappedNoise(noise("spaghetti_roughness_modulator"), 1.0, 1.0, 0.0, -0.1),
		Add(Abs(NoiseSampler(noise("spaghetti_roughness"), 1.0, 1.0)), constant(-0.4))))
	entrances := rs.entrances(spaghettiRoughness)
	surfaceWithEntrances := Min(slopedCheese, Mul(entrances, constant(5.0)))
	underground := rs.underground(slopedCheese, entrances, spaghettiRoughness)
	caves := RangeChoice(slopedCheese, -1000000.0, 1.5625, surfaceWithEntrances, underground)
	final := Add(Min(postProcess(slideOverworld(amplified, caves), 4, 8), rs.noodle(y)), BeardifierSampler())

	exclusion := Min(Sub(constant(-0.225), erosion), Max(Sub(depth, constant(0.9)), constant(0.0)))
	n := func(name string, yScale float64) Sampler { return NoiseSampler(noise(name), 1.0, yScale) }
	return Router{
		Temperature: temperature, Vegetation: vegetation, Continents: continents, Erosion: erosion,
		Depth: depth, Ridges: ridge, ChunkSurfaceLevel: chunkSurface, FinalDensity: final,
		Veins: rs.oreVeins(y),
		Aquifer: &AquiferConfig{
			Barrier:               n("aquifer_barrier", 0.5),
			FluidLevelFloodedness: n("aquifer_fluid_level_floodedness", 0.67),
			FluidLevelSpread:      n("aquifer_fluid_level_spread", 0.7142857142857143),
			Lava:                  n("aquifer_lava", 1.0),
			Exclusion:             exclusion,
			SurfaceLevel:          prelim,
		},
	}
}

// offsetToDepth is vanilla: NoiseRouterData.offsetToDepth.
func offsetToDepth(offset Sampler) Sampler {
	return Add(YClampedGradient(-64, 320, 1.5, -1.5), offset)
}

// noiseGradientDensity is vanilla: NoiseRouterData.noiseGradientDensity.
func noiseGradientDensity(factor, depthWithJaggedness Sampler) Sampler {
	return Mul(QuarterNegative(Mul(depthWithJaggedness, factor)), constant(4.0))
}

// slideOverworld is vanilla: NoiseRouterData.slideOverworld.
func slideOverworld(amplified bool, caves Sampler) Sampler {
	topStart, topEnd, bottomTarget := int32(80), int32(64), float32(0.1171875)
	if amplified {
		topStart, topEnd, bottomTarget = 16, 0, 0.4
	}
	return slide(caves, -64, 384, topStart, topEnd, -0.078125, 0, 24, bottomTarget)
}

// preliminarySurfaceLevel is vanilla: NoiseRouterData.preliminarySurfaceLevel.
func preliminarySurfaceLevel(offset, factor Sampler, amplified bool) Sampler {
	upperBound := Remap(Sub(Div(constant(0.2734375), factor), offset), 1.5, -1.5, -64.0, 320.0)
	upperBound = Clamp(upperBound, -40.0, 320.0)
	density := Add(slideOverworld(amplified, Clamp(Add(noiseGradientDensity(factor, offsetToDepth(offset)), constant(-0.703125)), -64.0, 64.0)), constant(-0.390625))
	return FindTopSurface(density, upperBound, -64, 8)
}

// yLimitedInterpolatable is vanilla: NoiseRouterData.yLimitedInterpolatable.
func yLimitedInterpolatable(y, whenInRange Sampler, minY, maxY int32, whenOutOfRange float32, cellXZ, cellY int32) Sampler {
	return Interpolated(RangeChoice(y, float32(minY), float32(maxY+1), whenInRange, constant(whenOutOfRange)), cellXZ, cellY)
}

// rarityNoise is vanilla: QuantizedSpaghettiRarity.noiseFunctionForRarity.
func rarityNoise(n Noise, rarity float32) Sampler {
	return Mul(NoiseSampler(n, 1.0/float64(rarity), 1.0/float64(rarity)), constant(rarity))
}

// wrapRarity3D is vanilla: QuantizedSpaghettiRarity.wrapRarity3d.
func wrapRarity3D(input Sampler, n Noise) Sampler {
	return Abs(IntervalSelect(input, []float32{-0.5, 0.0, 0.5},
		[]Sampler{rarityNoise(n, 0.75), rarityNoise(n, 1.0), rarityNoise(n, 1.5), rarityNoise(n, 2.0)}))
}

// wrapRarity2D is vanilla: QuantizedSpaghettiRarity.wrapRarity2d.
func wrapRarity2D(input Sampler, n Noise) Sampler {
	return Abs(IntervalSelect(input, []float32{-0.75, -0.5, 0.5, 0.75},
		[]Sampler{rarityNoise(n, 0.5), rarityNoise(n, 0.75), rarityNoise(n, 1.0), rarityNoise(n, 2.0), rarityNoise(n, 3.0)}))
}

// entrances is vanilla: NoiseRouterData.entrances.
func (rs *RandomState) entrances(spaghettiRoughness Sampler) Sampler {
	noise := func(name string) *NoiseStack { return rs.Noise("minecraft:" + name) }
	rarityModulator := rs.graph.Cache(NoiseSampler(noise("spaghetti_3d_rarity"), 2.0, 1.0))
	thickness := MappedNoise(noise("spaghetti_3d_thickness"), 1.0, 1.0, -0.065, -0.088)
	cave1 := wrapRarity3D(rarityModulator, noise("spaghetti_3d_1"))
	cave2 := wrapRarity3D(rarityModulator, noise("spaghetti_3d_2"))
	spaghetti3D := Clamp(Add(Max(cave1, cave2), thickness), -1.0, 1.0)
	bigNoise := NoiseSampler(noise("cave_entrance"), 0.75, 0.5)
	big := Add(Add(bigNoise, constant(0.37)), YClampedGradient(-10, 30, 0.3, 0.0))
	return rs.graph.Cache(Min(big, Add(spaghettiRoughness, spaghetti3D)))
}

// noodle is vanilla: NoiseRouterData.noodle.
func (rs *RandomState) noodle(y Sampler) Sampler {
	noise := func(name string) *NoiseStack { return rs.Noise("minecraft:" + name) }
	const ridgeFrequency = 2.6666666666666665
	toggle := yLimitedInterpolatable(y, NoiseSampler(noise("noodle"), 1.0, 1.0), -60, 320, -1, 4, 8)
	thickness := yLimitedInterpolatable(y, MappedNoise(noise("noodle_thickness"), 1.0, 1.0, -0.05, -0.1), -60, 320, 0, 4, 8)
	ridgeA := yLimitedInterpolatable(y, NoiseSampler(noise("noodle_ridge_a"), ridgeFrequency, ridgeFrequency), -60, 320, 0, 4, 8)
	ridgeB := yLimitedInterpolatable(y, NoiseSampler(noise("noodle_ridge_b"), ridgeFrequency, ridgeFrequency), -60, 320, 0, 4, 8)
	ridged := Mul(Max(Abs(ridgeA), Abs(ridgeB)), constant(1.5))
	return RangeChoice(toggle, -1000000.0, 0.0, constant(64.0), Add(thickness, ridged))
}

// pillars is vanilla: NoiseRouterData.pillars.
func (rs *RandomState) pillars() Sampler {
	noise := func(name string) *NoiseStack { return rs.Noise("minecraft:" + name) }
	source := NoiseSampler(noise("pillar"), 25.0, 0.3)
	rareness := MappedNoise(noise("pillar_rareness"), 1.0, 1.0, 0.0, -2.0)
	thickness := MappedNoise(noise("pillar_thickness"), 1.0, 1.0, 0.0, 1.1)
	withRareness := Add(Mul(source, constant(2.0)), rareness)
	return rs.graph.Cache(Mul(withRareness, Cube(thickness)))
}

// spaghetti2D is vanilla: NoiseRouterData.spaghetti2D.
func (rs *RandomState) spaghetti2D() Sampler {
	noise := func(name string) *NoiseStack { return rs.Noise("minecraft:" + name) }
	modulator := NoiseSampler(noise("spaghetti_2d_modulator"), 2.0, 1.0)
	cave := wrapRarity2D(modulator, noise("spaghetti_2d"))
	elevation := MappedNoise(noise("spaghetti_2d_elevation"), 1.0, 0.0, -8.0, 8.0)
	thickness := rs.graph.Cache(MappedNoise(noise("spaghetti_2d_thickness"), 2.0, 1.0, -0.6, -1.3))
	sloped := Abs(Add(Slice(AxisY, 0, rs.graph.Cache(elevation)), YClampedGradient(-64, 320, 8.0, -40.0)))
	layerRidged := Cube(Add(sloped, thickness))
	caveNoise := Add(cave, Mul(thickness, constant(0.083)))
	return Clamp(Max(caveNoise, layerRidged), -1.0, 1.0)
}

// underground is vanilla: NoiseRouterData.underground.
func (rs *RandomState) underground(slopedCheese, entrances, spaghettiRoughness Sampler) Sampler {
	noise := func(name string) *NoiseStack { return rs.Noise("minecraft:" + name) }
	spaghetti2D := rs.spaghetti2D()
	layerNoise := NoiseSampler(noise("cave_layer"), 1.0, 8.0)
	layerizedCaverns := Mul(Square(layerNoise), constant(4.0))
	cheese := NoiseSampler(noise("cave_cheese"), 1.0, 0.6666666666666666)
	solidified := Add(Clamp(Add(cheese, constant(0.27)), -1.0, 1.0),
		Clamp(Add(Mul(slopedCheese, constant(-0.64)), constant(1.5)), 0.0, 0.5))
	baseCave := Add(layerizedCaverns, solidified)
	subtractions := Min(Min(baseCave, entrances), Add(spaghetti2D, spaghettiRoughness))
	pillarsRaw := rs.pillars()
	pillars := RangeChoice(pillarsRaw, -1000000.0, 0.03, constant(-1000000.0), pillarsRaw)
	return Max(subtractions, pillars)
}

// oreVeins is vanilla: NoiseRouterData.registerOreVeins.
func (rs *RandomState) oreVeins(y Sampler) *VeinConfig {
	noise := func(name string) *NoiseStack { return rs.Noise("minecraft:" + name) }
	// The interpolated range covers both veins' y, in whole cells of 8:
	// floorDiv(-60, 8) * 8 and (floorDiv(50, 8) + 1) * 8, inclusive.
	const minY, maxY = -64, 56
	toggle := rs.graph.Cache(yLimitedInterpolatable(y, NoiseSampler(noise("ore_veininess"), 1.5, 1.5), minY, maxY, 0, 4, 8))
	veinA := Abs(yLimitedInterpolatable(y, NoiseSampler(noise("ore_vein_a"), 4.0, 4.0), minY, maxY, 1, 4, 8))
	veinB := Abs(yLimitedInterpolatable(y, NoiseSampler(noise("ore_vein_b"), 4.0, 4.0), minY, maxY, 1, 4, 8))
	mask := rs.graph.Cache(RangeChoice(toggle, -0.4, 0.4, constant(-1.0), Sub(constant(0.08), Max(veinA, veinB))))
	noVein := constant(-1.0)
	// vanilla: NoiseRouterData.createOreVeinDensity
	density := func(veinMinY, veinMaxY int32, veininess Sampler) Vein {
		edge := Min(Sub(constant(float32(veinMaxY)), y), Sub(y, constant(float32(veinMinY))))
		roundoff := ClampedMap(edge, 0.0, 20.0, -0.2, 0.0)
		inner := RangeChoice(Add(Sub(veininess, constant(0.4)), roundoff), 0.0, 1000000.0, constant(0.7), noVein)
		return Vein{RangeChoice(y, float32(veinMinY), float32(veinMaxY), RangeChoice(mask, 0.0, 1000000.0, inner, noVein), noVein), veinMinY, veinMaxY - 1}
	}
	return &VeinConfig{
		Copper:   density(0, 50, toggle),
		Iron:     density(-60, -8, Negate(toggle)),
		Richness: ClampedMap(Abs(toggle), 0.4, 0.6, 0.1, 0.3),
		Gap:      Sub(constant(-0.3), NoiseSampler(noise("ore_gap"), 1.0, 1.0)),
	}
}
