package terrain

// The overworld's terrain splines, built over the continents, erosion,
// weirdness and folded-ridges coordinates. Floats are worked at run time in
// float32: a constant expression Go folded exactly would differ from Java's
// rounding at every step.
// vanilla: TerrainProvider

// terrainCoords are the coordinates the splines read: continentalness,
// erosion, weirdness and peaks-and-valleys.
type terrainCoords struct {
	set                                    *splineSet
	continents, erosion, weirdness, ridges Sampler
}

func (c terrainCoords) b(coord Sampler, tf func(float32) float32) *splineBuilder {
	return c.set.builder(coord, tf)
}

// vanilla: TerrainProvider.AMPLIFIED_OFFSET, AMPLIFIED_FACTOR, AMPLIFIED_JAGGEDNESS
func amplifiedOffset(v float32) float32 {
	if v < 0 {
		return v
	}
	return v * 2.0
}

func amplifiedFactor(v float32) float32 { return 1.25 - 6.25/(v+5.0) }

func amplifiedJaggedness(v float32) float32 { return v * 2.0 }

// peaksAndValleys folds weirdness into ridges. vanilla: TerrainProvider.peaksAndValleys
func peaksAndValleys(weirdness float32) float32 {
	return -(absF(absF(weirdness)-0.6666667) - 0.33333334) * 3.0
}

// offsetSpline is vanilla: TerrainProvider.overworldOffset.
func (c terrainCoords) offsetSpline(amplified bool) *splineNode {
	tf := identity
	if amplified {
		tf = amplifiedOffset
	}
	beach := c.erosionOffsetSpline(-0.15, 0.0, 0.0, 0.1, 0.0, -0.03, false, false, tf)
	low := c.erosionOffsetSpline(-0.1, 0.03, 0.1, 0.1, 0.01, -0.03, false, false, tf)
	mid := c.erosionOffsetSpline(-0.1, 0.03, 0.1, 0.7, 0.01, -0.03, true, true, tf)
	high := c.erosionOffsetSpline(-0.05, 0.03, 0.1, 1.0, 0.01, 0.01, true, true, tf)
	return c.b(c.continents, tf).
		add(-1.1, 0.044).add(-1.02, -0.2222).add(-0.51, -0.2222).add(-0.44, -0.12).add(-0.18, -0.12).
		addSpline(-0.16, beach).addSpline(-0.15, beach).addSpline(-0.1, low).addSpline(0.25, mid).addSpline(1.0, high).
		build()
}

// factorSpline is vanilla: TerrainProvider.overworldFactor.
func (c terrainCoords) factorSpline(amplified bool) *splineNode {
	tf := identity
	if amplified {
		tf = amplifiedFactor
	}
	return c.b(c.continents, identity).
		add(-0.19, 3.95).
		addSpline(-0.15, c.erosionFactor(6.25, true, identity)).
		addSpline(-0.1, c.erosionFactor(5.47, true, tf)).
		addSpline(0.03, c.erosionFactor(5.08, true, tf)).
		addSpline(0.06, c.erosionFactor(4.69, false, tf)).
		build()
}

// jaggednessSpline is vanilla: TerrainProvider.overworldJaggedness.
func (c terrainCoords) jaggednessSpline(amplified bool) *splineNode {
	tf := identity
	if amplified {
		tf = amplifiedJaggedness
	}
	return c.b(c.continents, tf).
		add(-0.11, 0.0).
		addSpline(0.03, c.erosionJaggedness(1.0, 0.5, 0.0, 0.0, tf)).
		addSpline(0.65, c.erosionJaggedness(1.0, 1.0, 1.0, 0.0, tf)).
		build()
}

// erosionJaggedness is vanilla: TerrainProvider.buildErosionJaggednessSpline.
func (c terrainCoords) erosionJaggedness(peak0, peak1, high0, high1 float32, tf func(float32) float32) *splineNode {
	at0 := c.ridgeJaggedness(peak0, high0, tf)
	at1 := c.ridgeJaggedness(peak1, high1, tf)
	return c.b(c.erosion, tf).
		addSpline(-1.0, at0).addSpline(-0.78, at1).addSpline(-0.5775, at1).add(-0.375, 0.0).
		build()
}

// ridgeJaggedness is vanilla: TerrainProvider.buildRidgeJaggednessSpline.
func (c terrainCoords) ridgeJaggedness(atPeakRidge, atHighRidge float32, tf func(float32) float32) *splineNode {
	highSliceStart := peaksAndValleys(0.4)
	highSliceEnd := peaksAndValleys(0.56666666)
	highSliceMiddle := (highSliceStart + highSliceEnd) / 2.0
	b := c.b(c.ridges, tf)
	b.add(highSliceStart, 0.0)
	if atHighRidge > 0 {
		b.addSpline(highSliceMiddle, c.weirdnessJaggedness(atHighRidge, tf))
	} else {
		b.add(highSliceMiddle, 0.0)
	}
	if atPeakRidge > 0 {
		b.addSpline(1.0, c.weirdnessJaggedness(atPeakRidge, tf))
	} else {
		b.add(1.0, 0.0)
	}
	return b.build()
}

// weirdnessJaggedness is vanilla: TerrainProvider.buildWeirdnessJaggednessSpline.
func (c terrainCoords) weirdnessJaggedness(factor float32, tf func(float32) float32) *splineNode {
	atNegative := float32(0.63 * factor)
	atPositive := float32(0.3 * factor)
	return c.b(c.weirdness, tf).add(-0.01, atNegative).add(0.01, atPositive).build()
}

// erosionFactor is vanilla: TerrainProvider.getErosionFactor.
func (c terrainCoords) erosionFactor(base float32, shattered bool, tf func(float32) float32) *splineNode {
	baseSpline := c.b(c.weirdness, tf).add(-0.2, 6.3).add(0.2, base).build()
	b := c.b(c.erosion, tf).
		addSpline(-0.6, baseSpline).
		addSpline(-0.5, c.b(c.weirdness, tf).add(-0.05, 6.3).add(0.05, 2.67).build()).
		addSpline(-0.35, baseSpline).
		addSpline(-0.25, baseSpline).
		addSpline(-0.1, c.b(c.weirdness, tf).add(-0.05, 2.67).add(0.05, 6.3).build()).
		addSpline(0.03, baseSpline)
	if shattered {
		weirdnessShattered := c.b(c.weirdness, tf).add(0.0, base).add(0.1, 0.625).build()
		ridgesShattered := c.b(c.ridges, tf).add(-0.9, base).addSpline(-0.69, weirdnessShattered).build()
		b.add(0.35, base).addSpline(0.45, ridgesShattered).addSpline(0.55, ridgesShattered).add(0.62, base)
	} else {
		hills := c.b(c.ridges, tf).addSpline(-0.7, baseSpline).add(-0.15, 1.37).build()
		peaks := c.b(c.ridges, tf).addSpline(0.45, baseSpline).add(0.7, 1.56).build()
		b.addSpline(0.05, peaks).addSpline(0.4, peaks).addSpline(0.45, hills).addSpline(0.55, hills).add(0.58, base)
	}
	return b.build()
}

func slope(y1, y2, x1, x2 float32) float32 { return (y2 - y1) / (x2 - x1) }

// mountainRidgeSpline is vanilla: TerrainProvider.buildMountainRidgeSplineWithPoints.
func (c terrainCoords) mountainRidgeSpline(modulation float32, saddle bool, tf func(float32) float32) *splineNode {
	b := c.b(c.ridges, tf)
	allowRiversBelow := float32(-0.7)
	minPoint := float32(-1.0)
	maxPoint := float32(1.0)
	minContinentalness := mountainContinentalness(minPoint, modulation, allowRiversBelow)
	maxContinentalness := mountainContinentalness(maxPoint, modulation, allowRiversBelow)
	zeroPoint := mountainRidgeZero(modulation)
	afterRiver := float32(-0.65)
	if afterRiver < zeroPoint && zeroPoint < 1.0 {
		afterRiverContinentalness := mountainContinentalness(afterRiver, modulation, allowRiversBelow)
		beforeRiver := float32(-0.75)
		beforeRiverContinentalness := mountainContinentalness(beforeRiver, modulation, allowRiversBelow)
		minSlope := slope(minContinentalness, beforeRiverContinentalness, minPoint, beforeRiver)
		b.addSlope(minPoint, minContinentalness, minSlope)
		b.add(beforeRiver, beforeRiverContinentalness)
		b.add(afterRiver, afterRiverContinentalness)
		zeroContinentalness := mountainContinentalness(zeroPoint, modulation, allowRiversBelow)
		maxSlope := slope(zeroContinentalness, maxContinentalness, zeroPoint, 1.0)
		smallOffset := float32(0.01)
		b.add(zeroPoint-smallOffset, zeroContinentalness)
		b.addSlope(zeroPoint, zeroContinentalness, maxSlope)
		b.addSlope(1.0, maxContinentalness, maxSlope)
	} else {
		simpleSlope := slope(minContinentalness, maxContinentalness, minPoint, maxPoint)
		if saddle {
			b.add(-1.0, max(0.2, minContinentalness))
			b.addSlope(0.0, lerpF32(0.5, minContinentalness, maxContinentalness), simpleSlope)
		} else {
			b.addSlope(-1.0, minContinentalness, simpleSlope)
		}
		b.addSlope(1.0, maxContinentalness, simpleSlope)
	}
	return b.build()
}

// mountainContinentalness is vanilla: TerrainProvider.mountainContinentalness.
func mountainContinentalness(ridge, modulation, allowRiversBelow float32) float32 {
	ridgeOffset := float32(1.17)
	ridgeAmplitude := float32(0.46082947)
	ridgeSlope := 1.0 - float32(float32(1.0-modulation)*0.5)
	ridgeIntersect := float32(0.5 * float32(1.0-modulation))
	adjusted := float32(float32(ridge+ridgeOffset) * ridgeAmplitude)
	continentalness := float32(adjusted*ridgeSlope) - ridgeIntersect
	if ridge < allowRiversBelow {
		return max(continentalness, -0.2222)
	}
	return max(continentalness, 0.0)
}

// mountainRidgeZero is vanilla: TerrainProvider.calculateMountainRidgeZeroContinentalnessPoint.
func mountainRidgeZero(modulation float32) float32 {
	ridgeOffset := float32(1.17)
	ridgeAmplitude := float32(0.46082947)
	ridgeSlope := 1.0 - float32(float32(1.0-modulation)*0.5)
	ridgeIntersect := float32(0.5 * float32(1.0-modulation))
	return ridgeIntersect/float32(ridgeAmplitude*ridgeSlope) - ridgeOffset
}

// erosionOffsetSpline is vanilla: TerrainProvider.buildErosionOffsetSpline.
func (c terrainCoords) erosionOffsetSpline(lowValley, hill, tallHill, mountainFactor, plain, swamp float32, extremeHills, saddle bool, tf func(float32) float32) *splineNode {
	veryLowErosionMountains := c.mountainRidgeSpline(lerpF32(mountainFactor, 0.6, 1.5), saddle, tf)
	lowErosionMountains := c.mountainRidgeSpline(lerpF32(mountainFactor, 0.6, 1.0), saddle, tf)
	mountains := c.mountainRidgeSpline(mountainFactor, saddle, tf)
	half := float32(0.5)
	widePlateau := c.ridgeSpline(lowValley-0.15, float32(half*mountainFactor), float32(lerpF32(0.5, 0.5, 0.5)*mountainFactor), float32(half*mountainFactor), float32(0.6*mountainFactor), 0.5, tf)
	narrowPlateau := c.ridgeSpline(lowValley, float32(plain*mountainFactor), float32(hill*mountainFactor), float32(half*mountainFactor), float32(0.6*mountainFactor), 0.5, tf)
	plains := c.ridgeSpline(lowValley, plain, plain, hill, tallHill, 0.5, tf)
	plainsFarInland := c.ridgeSpline(lowValley, plain, plain, hill, tallHill, 0.5, tf)
	hills := c.b(c.ridges, tf).add(-1.0, lowValley).addSpline(-0.4, plains).add(0.0, tallHill+0.07).build()
	swamps := c.ridgeSpline(-0.02, swamp, swamp, hill, tallHill, 0.0, tf)
	b := c.b(c.erosion, tf).
		addSpline(-0.85, veryLowErosionMountains).addSpline(-0.7, lowErosionMountains).addSpline(-0.4, mountains).
		addSpline(-0.35, widePlateau).addSpline(-0.1, narrowPlateau).addSpline(0.2, plains)
	if extremeHills {
		b.addSpline(0.4, plainsFarInland).addSpline(0.45, hills).addSpline(0.55, hills).addSpline(0.58, plainsFarInland)
	}
	b.addSpline(0.7, swamps)
	return b.build()
}

// ridgeSpline is vanilla: TerrainProvider.ridgeSpline.
func (c terrainCoords) ridgeSpline(valley, low, mid, high, peaks, minValleySteepness float32, tf func(float32) float32) *splineNode {
	d1 := max(float32(0.5*float32(low-valley)), minValleySteepness)
	d2 := float32(5.0 * float32(mid-low))
	return c.b(c.ridges, tf).
		addSlope(-1.0, valley, d1).
		addSlope(-0.4, low, min(d1, d2)).
		addSlope(0.0, mid, d2).
		addSlope(0.4, high, float32(2.0*float32(high-mid))).
		addSlope(1.0, peaks, float32(0.7*float32(peaks-high))).
		build()
}
