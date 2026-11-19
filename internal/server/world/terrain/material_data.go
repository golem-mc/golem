package terrain

// The material rules of each dimension, written out from the data pack's
// worldgen/material_rule and worldgen/material_condition entries.
// vanilla: data/minecraft/worldgen/material_rule

// The named conditions. vanilla: data/minecraft/worldgen/material_condition
var (
	onFloor            = stoneDepth(0, false, 0, false)
	onCeiling          = stoneDepth(0, false, 0, true)
	underFloor         = stoneDepth(0, true, 0, false)
	underCeiling       = stoneDepth(0, true, 0, true)
	deepUnderFloor     = stoneDepth(0, true, 6, false)
	veryDeepUnderFloor = stoneDepth(0, true, 30, false)
	notUnderwater      = water(-1, 0, false)
	notUnderDeepWater  = water(-6, -1, true)
)

var (
	bedrockFloor = ifTrue(verticalGradient("bedrock_floor", aboveBottom(0), aboveBottom(5)), block(Bedrock))
	bedrockRoof  = ifTrue(not(verticalGradient("bedrock_roof", belowTop(5), belowTop(0))), block(Bedrock))

	copperOreVein = oreVein(func(v *VeinConfig) Vein { return v.Copper }, CopperOre, RawCopperBlock, Granite)
	ironOreVein   = oreVein(func(v *VeinConfig) Vein { return v.Iron }, DeepslateIronOre, RawIronBlock, Tuff)

	sandOrSandstoneIfCeiling = sequence(ifTrue(onCeiling, block(Sandstone)), block(Sand))
	gravelOrStoneIfCeiling   = sequence(ifTrue(onCeiling, block(Stone)), block(Gravel))

	powderSnowSurface      = ifTrue(noiseThreshold("powder_snow", 0.35, 0.6, false), ifTrue(notUnderwater, block(PowderSnow)))
	powderSnowUnderSurface = ifTrue(noiseThreshold("powder_snow", 0.45, 0.58, false), ifTrue(notUnderwater, block(PowderSnow)))

	sulfurCaveBands = sequence(
		ifTrue(noiseThreshold("sulfur_cave_gradient", -0.4000000059604645, -0.10000000149011612, true), block(Cinnabar)),
		ifTrue(noiseThreshold("sulfur_cave_gradient", 0.0, 0.4000000059604645, true), block(Sulfur)),
		ifTrue(noiseThreshold("sulfur_cave_gradient", 0.4000000059604645, maxDouble, true), block(Cinnabar)))

	overworldUnderground = sequence(
		ifTrue(isBiome("sulfur_caves"), sulfurCaveBands),
		ifTrue(verticalGradient("deepslate", absolute(0), absolute(8)), block(Deepslate)))

	// overworldRule is vanilla: material_rule/overworld.
	overworldRule = sequence(bedrockFloor, copperOreVein, ironOreVein,
		ifTrue(abovePreliminarySurface, overworldSurface), overworldUnderground)

	// endRule is vanilla: material_rule/end.
	endRule = block(EndStone)
)

// surfaceNoiseBands is the three terracotta stripes the surface noise cuts
// into badlands: bands of -0.909 to -0.5454, -0.1818 to 0.1818 and 0.5454 to 0.909.
func surfaceNoiseBands(m Material, fallback materialRule) materialRule {
	return sequence(
		ifTrue(noiseThreshold("surface", -0.909, -0.5454, false), block(m)),
		ifTrue(noiseThreshold("surface", -0.1818, 0.1818, false), block(m)),
		ifTrue(noiseThreshold("surface", 0.5454, 0.909, false), block(m)),
		fallback)
}

func surfaceAbove(noise string, lo float64, m Material) materialRule {
	return ifTrue(noiseThreshold(noise, lo, maxDouble, false), block(m))
}

var (
	defaultBiomeSurface = sequence(ifTrue(notUnderwater, block(GrassBlock)), block(Dirt))

	// biomeSurface and underBiomeSurface are vanilla: material_rule/overworld/biome_surface and under_biome_surface.
	biomeSurface = sequence(
		ifTrue(isBiome("frozen_peaks"), sequence(
			ifTrue(steep, block(PackedIce)),
			ifTrue(noiseThreshold("packed_ice", 0.0, 0.2, false), block(PackedIce)),
			ifTrue(noiseThreshold("ice", 0.0, 0.025, false), block(Ice)),
			ifTrue(notUnderwater, block(SnowBlock)))),
		ifTrue(isBiome("snowy_slopes"), sequence(
			ifTrue(steep, block(Stone)), powderSnowSurface, ifTrue(notUnderwater, block(SnowBlock)))),
		ifTrue(isBiome("jagged_peaks"), sequence(ifTrue(steep, block(Stone)), ifTrue(notUnderwater, block(SnowBlock)))),
		ifTrue(isBiome("grove"), sequence(powderSnowSurface, ifTrue(notUnderwater, block(SnowBlock)))),
		sharedBiomeSurface,
		ifTrue(isBiome("windswept_savanna"), sequence(
			surfaceAbove("surface", 0.21212121212121213, Stone), surfaceAbove("surface", -0.06060606060606061, CoarseDirt))),
		ifTrue(isBiome("windswept_gravelly_hills"), sequence(
			ifTrue(noiseThreshold("surface", 0.24242424242424243, maxDouble, false), gravelOrStoneIfCeiling),
			surfaceAbove("surface", 0.12121212121212122, Stone),
			ifTrue(noiseThreshold("surface", -0.12121212121212122, maxDouble, false), defaultBiomeSurface),
			gravelOrStoneIfCeiling)),
		ifTrue(isBiome("old_growth_pine_taiga", "old_growth_spruce_taiga"), sequence(
			surfaceAbove("surface", 0.21212121212121213, CoarseDirt), surfaceAbove("surface", -0.11515151515151514, Podzol))),
		ifTrue(isBiome("ice_spikes"), ifTrue(notUnderwater, block(SnowBlock))),
		ifTrue(isBiome("mushroom_fields"), block(Mycelium)),
		ifTrue(isBiome("dappled_forest"), surfaceAbove("small_patch", 1.2000000476837158, CoarseDirt)),
		defaultBiomeSurface)

	underBiomeSurface = sequence(
		ifTrue(isBiome("frozen_peaks"), sequence(
			ifTrue(steep, block(PackedIce)),
			ifTrue(noiseThreshold("packed_ice", -0.5, 0.2, false), block(PackedIce)),
			ifTrue(noiseThreshold("ice", -0.0625, 0.025, false), block(Ice)),
			ifTrue(notUnderwater, block(SnowBlock)))),
		ifTrue(isBiome("snowy_slopes"), sequence(
			ifTrue(steep, block(Stone)), powderSnowUnderSurface, ifTrue(notUnderwater, block(SnowBlock)))),
		ifTrue(isBiome("jagged_peaks"), block(Stone)),
		ifTrue(isBiome("grove"), sequence(powderSnowUnderSurface, block(Dirt))),
		sharedBiomeSurface,
		ifTrue(isBiome("windswept_savanna"), surfaceAbove("surface", 0.21212121212121213, Stone)),
		ifTrue(isBiome("windswept_gravelly_hills"), sequence(
			ifTrue(noiseThreshold("surface", 0.24242424242424243, maxDouble, false), gravelOrStoneIfCeiling),
			surfaceAbove("surface", 0.12121212121212122, Stone),
			surfaceAbove("surface", -0.12121212121212122, Dirt),
			gravelOrStoneIfCeiling)),
		block(Dirt))

	// sharedBiomeSurface is the nested sequence both biome surfaces carry.
	sharedBiomeSurface = sequence(
		ifTrue(isBiome("stony_peaks"), sequence(ifTrue(noiseThreshold("calcite", -0.0125, 0.0125, false), block(Calcite)), block(Stone))),
		ifTrue(isBiome("stony_shore"), sequence(
			ifTrue(noiseThreshold("gravel", -0.05, 0.05, false), gravelOrStoneIfCeiling), block(Stone))),
		ifTrue(isBiome("windswept_hills"), surfaceAbove("surface", 0.12121212121212122, Stone)),
		ifTrue(isBiome("warm_ocean", "beach", "snowy_beach"), sandOrSandstoneIfCeiling),
		ifTrue(isBiome("desert"), sandOrSandstoneIfCeiling),
		ifTrue(isBiome("dripstone_caves"), block(Stone)),
		ifTrue(isBiome("sulfur_caves"), sequence(sulfurCaveBands, block(Stone))),
		ifTrue(isBiome("mangrove_swamp"), block(Mud)))
)

// overworldSurface is vanilla: material_rule/overworld/surface.
var overworldSurface = sequence(
	ifTrue(onFloor, sequence(
		ifTrue(isBiome("wooded_badlands"), ifTrue(yAbove(absolute(97), 2, false), surfaceNoiseBands(CoarseDirt, defaultBiomeSurface))),
		ifTrue(isBiome("swamp"), ifTrue(yAbove(absolute(62), 0, false),
			ifTrue(not(yAbove(absolute(63), 0, false)), ifTrue(noiseThreshold("surface_swamp", 0.0, maxDouble, false), block(Water))))),
		ifTrue(isBiome("mangrove_swamp"), ifTrue(yAbove(absolute(60), 0, false),
			ifTrue(not(yAbove(absolute(63), 0, false)), ifTrue(noiseThreshold("surface_swamp", 0.0, maxDouble, false), block(Water))))))),
	ifTrue(isBiome("badlands", "eroded_badlands", "wooded_badlands"), sequence(
		ifTrue(onFloor, sequence(
			ifTrue(yAbove(absolute(256), 0, false), block(OrangeTerracotta)),
			ifTrue(yAbove(absolute(74), 1, true), surfaceNoiseBands(Terracotta, bandlands)),
			ifTrue(notUnderwater, sequence(ifTrue(onCeiling, block(RedSandstone)), block(RedSand))),
			ifTrue(not(hole), block(OrangeTerracotta)),
			ifTrue(notUnderDeepWater, block(WhiteTerracotta)),
			gravelOrStoneIfCeiling)),
		ifTrue(yAbove(absolute(63), -1, true), sequence(
			ifTrue(yAbove(absolute(63), 0, false), ifTrue(not(yAbove(absolute(74), 1, true)), block(OrangeTerracotta))),
			bandlands)),
		ifTrue(underFloor, ifTrue(notUnderDeepWater, block(WhiteTerracotta))))),
	ifTrue(onFloor, ifTrue(notUnderwater, sequence(
		ifTrue(isBiome("frozen_ocean", "deep_frozen_ocean"), ifTrue(hole, block(Air))),
		biomeSurface))),
	ifTrue(notUnderDeepWater, sequence(
		ifTrue(onFloor, ifTrue(isBiome("frozen_ocean", "deep_frozen_ocean"), ifTrue(hole, block(Water)))),
		ifTrue(underFloor, underBiomeSurface),
		ifTrue(isBiome("warm_ocean", "beach", "snowy_beach"), ifTrue(deepUnderFloor, block(Sandstone))),
		ifTrue(isBiome("desert"), ifTrue(veryDeepUnderFloor, block(Sandstone))))),
	ifTrue(onFloor, sequence(
		ifTrue(isBiome("frozen_peaks", "jagged_peaks"), block(Stone)),
		ifTrue(isBiome("warm_ocean", "lukewarm_ocean", "deep_lukewarm_ocean"), sandOrSandstoneIfCeiling),
		gravelOrStoneIfCeiling)))

// netherRule is vanilla: material_rule/nether.
var netherRule = sequence(bedrockFloor, bedrockRoof,
	ifTrue(yAbove(belowTop(5), 0, false), block(Netherrack)),
	ifTrue(isBiome("basalt_deltas"), sequence(
		ifTrue(underCeiling, block(Basalt)),
		ifTrue(underFloor, sequence(netherGravelPatch, ifTrue(noiseThreshold("nether_state_selector", 0.0, maxDouble, false), block(Basalt)), block(Blackstone))))),
	ifTrue(isBiome("soul_sand_valley"), sequence(
		ifTrue(underCeiling, sequence(ifTrue(noiseThreshold("nether_state_selector", 0.0, maxDouble, false), block(SoulSand)), block(SoulSoil))),
		ifTrue(underFloor, sequence(netherGravelPatch, ifTrue(noiseThreshold("nether_state_selector", 0.0, maxDouble, false), block(SoulSand)), block(SoulSoil))))),
	ifTrue(onFloor, sequence(
		ifTrue(not(yAbove(absolute(32), 0, false)), ifTrue(hole, block(Lava))),
		ifTrue(isBiome("warped_forest"), netherForestFloor(WarpedWartBlock, WarpedNylium)),
		ifTrue(isBiome("crimson_forest"), netherForestFloor(NetherWartBlock, CrimsonNylium)))),
	ifTrue(isBiome("nether_wastes"), sequence(
		ifTrue(underFloor, ifTrue(noiseThreshold("soul_sand_layer", -0.012, maxDouble, false), sequence(
			ifTrue(not(hole), ifTrue(yAbove(absolute(30), 0, true), ifTrue(not(yAbove(absolute(35), 0, true)), block(SoulSand)))),
			block(Netherrack)))),
		ifTrue(onFloor, ifTrue(yAbove(absolute(31), 0, false), ifTrue(not(yAbove(absolute(35), 0, true)),
			ifTrue(noiseThreshold("gravel_layer", -0.012, maxDouble, false), sequence(
				ifTrue(yAbove(absolute(32), 0, false), block(Gravel)),
				ifTrue(not(hole), block(Gravel))))))))),
	block(Netherrack))

// netherGravelPatch is the gravel the patch noise lays between y 30 and 35.
var netherGravelPatch = ifTrue(noiseThreshold("patch", -0.012, maxDouble, false),
	ifTrue(yAbove(absolute(30), 0, true), ifTrue(not(yAbove(absolute(35), 0, true)), block(Gravel))))

// netherForestFloor is the nylium floor of the warped and crimson forests.
func netherForestFloor(wart, nylium Material) materialRule {
	return ifTrue(not(noiseThreshold("netherrack", 0.54, maxDouble, false)), ifTrue(yAbove(absolute(31), 0, false),
		sequence(ifTrue(noiseThreshold("nether_wart", 1.17, maxDouble, false), block(wart)), block(nylium))))
}
