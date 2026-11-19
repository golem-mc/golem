package world

import (
	"math/rand"
	"slices"
	"strconv"
	"sync"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/blocklogic"
	"github.com/golem-mc/golem/internal/server/world/levelgen"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
)

// Bone meal growth past the crops (vanilla: BoneMealItem.growCrop). setClient
// is vanilla's flag 2, set flag 3.

// bonemealHorizontal is Direction.Plane.HORIZONTAL's order: north, east, south,
// west.
var bonemealHorizontal = [4]int32{2, 5, 3, 4}

// BoneMeal is one bone meal use on the block at (x, y, z), clicked on face
// (vanilla: BoneMealItem.useOn). eventPos is where the particles go, drops what
// the growth gave up; swung means growCrop answered, whose SUCCESS_SERVER is
// the only answer that swings the user's arm server-side.
func (p *Physics) BoneMeal(x, y, z int32, face int32, actor uuid.UUID) (changes []BlockChange, eventPos [3]int32, drops []registry.Item, used, swung bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.boneMealActor = actor
	defer func() { p.boneMealActor = uuid.Nil }()
	return p.boneMealVisit(x, y, z, face)
}

// DispenseBoneMeal is a dispenser's bone meal: growCrop on the target cell and
// growWaterPlant with no face (vanilla: the item's dispense behavior).
func (p *Physics) DispenseBoneMeal(x, y, z int32) (changes []BlockChange, eventPos [3]int32, drops []registry.Item, used bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	changes, eventPos, drops, used, _ = p.boneMealVisit(x, y, z, -1)
	return changes, eventPos, drops, used
}

// boneMealVisit serves both callers; face < 0 is the dispenser's: no face
// sturdiness, the water cell is (x, y, z) itself.
func (p *Physics) boneMealVisit(x, y, z, face int32) (changes []BlockChange, eventPos [3]int32, drops []registry.Item, used, swung bool) {
	if !p.listens(HookFertilize) {
		return p.boneMealRun(x, y, z, face)
	}
	// The growth writes as it decides, so the listeners see what it did and
	// a veto undoes it; the clients have been told nothing yet.
	p.w.JournalStart()
	changes, eventPos, drops, used, swung = p.boneMealRun(x, y, z, face)
	cells, olds, news := p.w.JournalStop()
	if !used || len(cells) == 0 {
		return changes, eventPos, drops, used, swung
	}
	net := netCells(cells, olds, news)
	if len(net) == 0 {
		return changes, eventPos, drops, used, swung
	}
	d := HookDecision{Kind: HookFertilize, X: x, Y: y, Z: z, Old: p.w.BlockAt(x, y, z), Actor: p.boneMealActor}
	for _, c := range net {
		d.Cells = append(d.Cells, BlockChange{X: c.x, Y: c.y, Z: c.z, State: c.new})
	}
	if p.ask(&d) {
		for _, c := range slices.Backward(net) {
			p.revertCell(c)
		}
		return nil, eventPos, nil, true, swung
	}
	return p.applyEditedCells(net, d.Cells, changes), eventPos, drops, true, swung
}

// boneMealRun is the growth itself.
func (p *Physics) boneMealRun(x, y, z, face int32) (changes []BlockChange, eventPos [3]int32, drops []registry.Item, used, swung bool) {
	state := p.w.BlockAt(x, y, z)
	if int(state) >= registry.StateCount {
		return nil, eventPos, nil, false, false
	}
	if drops, used = p.growCrop(x, y, z, state, &changes); used {
		return changes, [3]int32{x, y, z}, drops, true, true
	}
	if face < 0 {
		if p.growWaterPlant(x, y, z, face, &changes) {
			return changes, [3]int32{x, y, z}, nil, true, false
		}
		return nil, eventPos, nil, false, false
	}
	if !registry.FaceSturdy(state, int(face), registry.SupportFull) {
		return nil, eventPos, nil, false, false
	}
	off := protocol.FaceOffset[face]
	wx, wy, wz := x+off[0], y+off[1], z+off[2]
	if p.growWaterPlant(wx, wy, wz, face, &changes) {
		return changes, [3]int32{wx, wy, wz}, nil, true, false
	}
	return nil, eventPos, nil, false, false
}

// growCrop is the vanilla BoneMealItem.growCrop, the clicked block's own rule.
func (p *Physics) growCrop(x, y, z int32, state uint16, changes *[]BlockChange) ([]registry.Item, bool) {
	block := registry.BlockOf(state)
	if maxAge, ok := grownCrops[block]; ok {
		return nil, p.boneMealCrop(x, y, z, state, maxAge, changes)
	}
	if singleAgeCrops[block] != 0 {
		maxAge := singleAgeCrops[block]
		age, ok := registry.StateInt(state, registry.PropAge)
		if !ok || age >= int(maxAge) {
			return nil, false
		}
		p.bonemealProp(x, y, z, state, registry.PropAge, strconv.Itoa(age+1), changes)
		return nil, true
	}
	switch block {
	case registry.BlockOakSapling, registry.BlockSpruceSapling, registry.BlockBirchSapling,
		registry.BlockJungleSapling, registry.BlockAcaciaSapling, registry.BlockDarkOakSapling,
		registry.BlockCherrySapling, registry.BlockPaleOakSapling, registry.BlockPoplarSapling:
		// vanilla: SaplingBlock — the 0.45 roll gates advanceTree (tree.go). A
		// sapling the grower cannot grow, or without the tree's minimum height
		// below the build limit, is no target.
		g := saplingGrowers[block]
		if !p.canGrow(x, y, z, g) || !contains(y+g.minHeight) {
			return nil, false
		}
		if p.rng.Float32() < 0.45 {
			p.advanceTree(x, y, z, state, changes)
		}
		return nil, true
	case registry.BlockAzalea, registry.BlockFloweringAzalea:
		// vanilla: AzaleaBlock
		if !contains(y+azaleaGrower.minHeight+2) || fluidHolding(p.w.BlockAt(x, y+1, z)).kind != fluidNone {
			return nil, false
		}
		if p.rng.Float32() < 0.45 {
			p.growSaplingTree(x, y, z, state, changes)
		}
		return nil, true
	case registry.BlockGrassBlock:
		// vanilla: GrassBlock
		if !p.airAt(x, y+1, z) || !contains(y+1) {
			return nil, false
		}
		p.growGrassSpread(x, y, z, changes)
		return nil, true
	case registry.BlockShortGrass, registry.BlockFern:
		// vanilla: TallGrassBlock
		grown := registry.BlockTallGrass
		if block == registry.BlockFern {
			grown = registry.BlockLargeFern
		}
		if !p.tallPlantFits(x, y, z) {
			return nil, false
		}
		p.placeDoublePlant(x, y, z, grown, changes)
		return nil, true
	case registry.BlockLilac, registry.BlockPeony, registry.BlockRoseBush, registry.BlockSunflower:
		// vanilla: TallFlowerBlock, the flower given up as an item.
		return blockDrop(block), true
	case registry.BlockCaveVines, registry.BlockCaveVinesPlant:
		// vanilla: CaveVinesBlock and CaveVinesPlantBlock
		if registry.StateBool(state, registry.PropBerries) {
			return nil, false
		}
		p.bonemealProp(x, y, z, state, registry.PropBerries, "true", changes)
		return nil, true
	case registry.BlockBush, registry.BlockFireflyBush, registry.BlockRedShrub:
		// vanilla: BushBlock (bush, red shrub) and FireflyBushBlock, the same spread.
		if _, ok := p.spreadableNeighbour(x, y, z, registry.TagIDSupportsVegetation, false); !ok {
			return nil, false
		}
		p.spreadVegetation(x, y, z, block, registry.TagIDSupportsVegetation, changes)
		return nil, true
	case registry.BlockShortDryGrass:
		// vanilla: ShortDryGrassBlock
		p.setShaped(x, y, z, registry.BlockTallDryGrass.DefaultState(), changes)
		return nil, true
	case registry.BlockTallDryGrass:
		// vanilla: TallDryGrassBlock
		if _, ok := p.spreadableNeighbour(x, y, z, registry.TagIDSupportsDryVegetation, false); !ok {
			return nil, false
		}
		p.spreadVegetation(x, y, z, registry.BlockShortDryGrass, registry.TagIDSupportsDryVegetation, changes)
		return nil, true
	case registry.BlockSeagrass:
		// vanilla: SeagrassBlock
		if registry.BlockOf(p.w.BlockAt(x, y+1, z)) != registry.BlockWater {
			return nil, false
		}
		p.placeDoublePlant(x, y, z, registry.BlockTallSeagrass, changes)
		return nil, true
	case registry.BlockSeaPickle:
		// vanilla: SeaPickleBlock
		if !registry.StateBool(state, registry.PropWaterlogged) {
			return nil, false
		}
		if !registry.BlockOf(p.w.BlockAt(x, y-1, z)).InTag(registry.TagIDCoralBlocks) {
			return nil, false
		}
		p.growSeaPickles(x, y, z, state, changes)
		return nil, true
	case registry.BlockKelp, registry.BlockWeepingVines, registry.BlockTwistingVines:
		// vanilla: GrowingPlantHeadBlock
		g := growingPlantOf(block)
		fx, fy, fz := x+g.dir[0], y+g.dir[1], z+g.dir[2]
		if !g.canGrowInto(p, fx, fy, fz) || !contains(fy) {
			return nil, false
		}
		p.growPlantHead(g, x, y, z, state, changes)
		return nil, true
	case registry.BlockKelpPlant, registry.BlockWeepingVinesPlant, registry.BlockTwistingVinesPlant:
		// vanilla: GrowingPlantBodyBlock, the visit walked to the head.
		headX, headY, headZ, ok := p.plantHeadOf(block, x, y, z)
		if !ok {
			return nil, false
		}
		g := growingPlantOf(block)
		fx, fy, fz := headX+g.dir[0], headY+g.dir[1], headZ+g.dir[2]
		if !g.canGrowInto(p, fx, fy, fz) || !contains(fy) {
			return nil, false
		}
		p.growPlantHead(g, headX, headY, headZ, p.w.BlockAt(headX, headY, headZ), changes)
		return nil, true
	case registry.BlockMelonStem, registry.BlockPumpkinStem:
		// vanilla: StemBlock; at age 7 the new state's randomTick runs.
		return nil, p.boneMealStem(x, y, z, state, changes)
	case registry.BlockTorchflowerCrop:
		return nil, p.boneMealTorchflower(x, y, z, state, changes)
	case registry.BlockPitcherCrop:
		// vanilla: PitcherCropBlock
		return nil, p.boneMealPitcherCrop(x, y, z, state, changes)
	case registry.BlockPinkPetals, registry.BlockWildflowers:
		// vanilla: FlowerBedBlock; four petals are given up as an item.
		amount, ok := registry.StateInt(state, registry.PropFlowerAmount)
		if !ok {
			return nil, false
		}
		if amount < 4 {
			p.bonemealProp(x, y, z, state, registry.PropFlowerAmount, strconv.Itoa(amount+1), changes)
			return nil, true
		}
		return blockDrop(block), true
	case registry.BlockSmallDripleaf:
		// vanilla: SmallDripleafBlock, the upper half handing the visit down.
		if registry.StateValue(state, registry.PropHalf) == "upper" {
			if lower := p.w.BlockAt(x, y-1, z); registry.BlockOf(lower) == block {
				p.boneMealSmallDripleaf(x, y-1, z, lower, changes)
			}
			return nil, true
		}
		p.boneMealSmallDripleaf(x, y, z, state, changes)
		return nil, true
	case registry.BlockBigDripleaf:
		// vanilla: BigDripleafBlock
		if !p.dripleafCanGrowInto(x, y+1, z) {
			return nil, false
		}
		p.boneMealBigDripleaf(x, y, z, state, changes)
		return nil, true
	case registry.BlockBigDripleafStem:
		// vanilla: BigDripleafStemBlock
		return nil, p.boneMealBigDripleafStem(x, y, z, state, changes)
	case registry.BlockRootedDirt:
		// vanilla: RootedDirtBlock
		if !p.airAt(x, y-1, z) || !contains(y-1) {
			return nil, false
		}
		p.setShaped(x, y-1, z, registry.BlockHangingRoots.DefaultState(), changes)
		return nil, true
	case registry.BlockCrimsonNylium, registry.BlockWarpedNylium:
		// vanilla: NyliumBlock
		if !p.airAt(x, y+1, z) || !contains(y+1) {
			return nil, false
		}
		p.placeGrownFeature("nylium_bonemeal", false, x, y+1, z, nil, changes)
		return nil, true
	case registry.BlockNetherrack:
		// vanilla: NetherrackBlock; zero dampening above stands in for the
		// sky-light read.
		if registry.LightDampening(p.w.BlockAt(x, y+1, z)) != 0 {
			return nil, false
		}
		foundRed, foundBlue := p.nyliumAround(x, y, z)
		if !foundRed && !foundBlue {
			return nil, false
		}
		become := registry.BlockCrimsonNylium
		if foundRed && foundBlue {
			if p.rng.Intn(2) == 0 {
				become = registry.BlockWarpedNylium
			}
		} else if foundBlue {
			become = registry.BlockWarpedNylium
		}
		p.setShaped(x, y, z, become.DefaultState(), changes)
		return nil, true
	case registry.BlockCrimsonFungus, registry.BlockWarpedFungus:
		// vanilla: NetherFungusBlock
		below := registry.BlockOf(p.w.BlockAt(x, y-1, z))
		required, key := registry.BlockCrimsonNylium, "crimson_fungus_planted"
		if block == registry.BlockWarpedFungus {
			required, key = registry.BlockWarpedNylium, "warped_fungus_planted"
		}
		if below != required || !contains(y+1) {
			return nil, false
		}
		if p.rng.Float32() < 0.4 { // vanilla: NetherFungusBlock.isBonemealSuccess
			p.placeGrownFeature(key, true, x, y, z, nil, changes)
		}
		return nil, true
	case registry.BlockBrownMushroom, registry.BlockRedMushroom:
		// vanilla: MushroomBlock.growMushroom; a failed feature leaves the
		// mushroom standing, as vanilla puts it back.
		if !contains(y + 1) {
			return nil, false
		}
		if p.rng.Float32() < 0.4 { // vanilla: MushroomBlock.isBonemealSuccess
			key := "huge_brown_mushroom"
			if block == registry.BlockRedMushroom {
				key = "huge_red_mushroom"
			}
			p.placeGrownFeature(key, true, x, y, z, []BlockChange{{X: x, Y: y, Z: z, State: airState}}, changes)
		}
		return nil, true
	case registry.BlockMossBlock, registry.BlockPaleMossBlock:
		// vanilla: BonemealableFeaturePlacerBlock
		if !p.airAt(x, y+1, z) {
			return nil, false
		}
		key := "moss_patch_bonemeal"
		if block == registry.BlockPaleMossBlock {
			key = "pale_moss_patch_bonemeal"
		}
		p.placeGrownFeature(key, false, x, y+1, z, nil, changes)
		return nil, true
	case registry.BlockMangroveLeaves:
		// vanilla: MangroveLeavesBlock
		if !p.airAt(x, y-1, z) {
			return nil, false
		}
		if next, ok := registry.WithBool(registry.BlockMangrovePropagule.DefaultState(), registry.PropHanging, true); ok {
			p.setClientShaped(x, y-1, z, next, changes)
		}
		return nil, true
	case registry.BlockMangrovePropagule:
		// vanilla: MangrovePropaguleBlock, hanging: one age at a time with no
		// roll, ripe at 4 and then no target; standing: the sapling's stage.
		if registry.StateBool(state, registry.PropHanging) {
			age, ok := registry.StateInt(state, registry.PropAge)
			if !ok || age >= 4 {
				return nil, false
			}
			p.bonemealProp(x, y, z, state, registry.PropAge, strconv.Itoa(age+1), changes)
			return nil, true
		}
		if p.rng.Float32() >= 0.45 {
			return nil, true
		}
		p.advanceTree(x, y, z, state, changes)
		return nil, true
	case registry.BlockBambooSapling:
		// vanilla: BambooSaplingBlock
		if !p.airAt(x, y+1, z) || !contains(y+1) {
			return nil, false
		}
		p.growBambooSapling(x, y, z, changes)
		return nil, true
	case registry.BlockBamboo:
		// vanilla: BambooStalkBlock
		return nil, p.boneMealBamboo(x, y, z, changes)
	case registry.BlockPaleMossCarpet:
		// vanilla: MossyCarpetBlock
		if !registry.StateBool(state, registry.PropBottom) {
			return nil, false
		}
		topper, ok := MossyCarpetTopper(p.w, x, y, z, func() bool { return true })
		if !ok {
			return nil, false
		}
		p.setShaped(x, y+1, z, topper, changes)
		return nil, true
	case registry.BlockPaleHangingMoss:
		// vanilla: HangingMossBlock
		return nil, p.boneMealHangingMoss(x, y, z, state, changes)
	case registry.BlockGlowLichen:
		// vanilla: GlowLichenBlock, spreading from a random carried face toward
		// a random direction.
		state := p.w.BlockAt(x, y, z)
		if !p.lichenCanSpreadSomewhere(x, y, z, state) {
			return nil, false
		}
		p.spreadLichen(x, y, z, state, changes)
		return nil, true
	}
	return nil, false
}

// boneMealCrop adds 2-5 ages, a third of that for beetroot (vanilla:
// CropBlock.getBonemealAgeIncrease).
func (p *Physics) boneMealCrop(x, y, z int32, state uint16, maxAge int, changes *[]BlockChange) bool {
	age, ok := registry.StateInt(state, registry.PropAge)
	if !ok || age >= maxAge {
		return false
	}
	increase := 2 + p.rng.Intn(4)
	if registry.BlockOf(state) == registry.BlockBeetroots {
		increase /= 3
	}
	newAge := min(age+increase, maxAge)
	if newAge != age {
		if next, found := registry.WithInt(state, registry.PropAge, newAge); found {
			p.setClientShaped(x, y, z, next, changes)
		}
	}
	return true
}

// boneMealStem sets age min(7, age+nextInt(2, 5)), then at 7 runs the new
// state's randomTick; age 7 is no target.
// vanilla: StemBlock
func (p *Physics) boneMealStem(x, y, z int32, state uint16, changes *[]BlockChange) bool {
	age, ok := registry.StateInt(state, registry.PropAge)
	if !ok || age >= 7 {
		return false
	}
	newAge := min(age+2+p.rng.Intn(4), 7)
	p.bonemealProp(x, y, z, state, registry.PropAge, strconv.Itoa(newAge), changes)
	if newAge == 7 {
		p.stemFruit(x, y, z, registry.BlockOf(state), changes)
	}
	return true
}

// The fruit tags the stems' fruit stands on.
var (
	tagSupportsMelonStemFruit   = registry.BlockTagOf("minecraft:supports_melon_stem_fruit")
	tagSupportsPumpkinStemFruit = registry.BlockTagOf("minecraft:supports_pumpkin_stem_fruit")
)

// stemFruit is StemBlock.randomTick's age-7 arm: light 9, the growth-speed
// roll, then the fruit beside the stem on its fruit tag and the stem attached
// facing it.
func (p *Physics) stemFruit(x, y, z int32, stem registry.Block, changes *[]BlockChange) {
	if rawLightAt(p.w, x, y, z) < 9 {
		return
	}
	speed := p.cropGrowthSpeed(x, y, z, stem)
	if p.rng.Intn(int(25.0/speed)+1) != 0 {
		return
	}
	i := p.rng.Intn(4)
	fx, fz := x+horizontal4[i][0], z+horizontal4[i][1]
	fruit, attached, fruitTag := registry.BlockPumpkin, registry.BlockAttachedPumpkinStem, tagSupportsPumpkinStemFruit
	if stem == registry.BlockMelonStem {
		fruit, attached, fruitTag = registry.BlockMelon, registry.BlockAttachedMelonStem, tagSupportsMelonStemFruit
	}
	if !p.airAt(fx, y, fz) {
		return
	}
	if below := p.w.BlockAt(fx, y-1, fz); !stateInTag(below, fruitTag) {
		return
	}
	p.setShaped(fx, y, fz, fruit.DefaultState(), changes)
	if next, ok := registry.WithProp(attached.DefaultState(), registry.PropFacing, faceProps[bonemealHorizontal[i]]); ok {
		p.setShaped(x, y, z, next, changes)
	}
}

// airAt is the vanilla isEmptyBlock: strictly one of the three air blocks.
func (p *Physics) airAt(x, y, z int32) bool {
	return IsKnownAirState(p.w.BlockAt(x, y, z))
}

// fullWaterAt reports a full water block, including a falling column.
func (p *Physics) fullWaterAt(x, y, z int32) bool {
	f := blocklogic.FluidOf(p.w.BlockAt(x, y, z))
	return f.Pure && f.IsFullWater()
}

// waterAt reports water of any level, what a sea pickle spreads over.
func (p *Physics) waterAt(x, y, z int32) bool {
	return registry.BlockOf(p.w.BlockAt(x, y, z)) == registry.BlockWater
}

// bonemealProp rewrites one property at the position with a flag-2 write, whose
// shape pass reaches an observer.
func (p *Physics) bonemealProp(x, y, z int32, state uint16, key registry.Prop, value string, changes *[]BlockChange) {
	next, found := registry.WithProp(state, key, value)
	if !found {
		return
	}
	p.setClientShaped(x, y, z, next, changes)
}

// tallPlantFits is the grown plant's canSurvive: supports_vegetation ground and
// air above within the build height (vanilla: TallGrassBlock; VegetationBlock's
// ground from the dump's tag).
func (p *Physics) tallPlantFits(x, y, z int32) bool {
	if !registry.BlockOf(p.w.BlockAt(x, y-1, z)).InTag(registry.TagIDSupportsVegetation) {
		return false
	}
	return p.airAt(x, y+1, z) && contains(y+1)
}

// placeDoublePlant writes both halves with flag 2.
// vanilla: DoublePlantBlock.placeAt
func (p *Physics) placeDoublePlant(x, y, z int32, grown registry.Block, changes *[]BlockChange) {
	lower, _ := registry.WithProp(grown.DefaultState(), registry.PropHalf, "lower")
	upper, _ := registry.WithProp(grown.DefaultState(), registry.PropHalf, "upper")
	p.setClientShaped(x, y, z, lower, changes)
	p.setClientShaped(x, y+1, z, upper, changes)
}

// growGrassSpread makes 128 walks over the grass, each air cell reached growing
// short grass or, one in eight, a flower from the biome's
// can_spawn_from_bone_meal features.
// vanilla: GrassBlock.performBonemeal
func (p *Physics) growGrassSpread(x, y, z int32, changes *[]BlockChange) {
	for j := range 128 {
		tx, ty, tz := x, y+1, z
		abandoned := false
		for i := 0; i < j/16; i++ {
			dx := int32(p.rng.Intn(3)) - 1
			dy := (int32(p.rng.Intn(3)) - 1) * int32(p.rng.Intn(3)) / 2
			dz := int32(p.rng.Intn(3)) - 1
			tx, ty, tz = tx+dx, ty+dy, tz+dz
			if registry.BlockOf(p.w.BlockAt(tx, ty-1, tz)) != registry.BlockGrassBlock ||
				collisionOf(p.w.BlockAt(tx, ty, tz)).fullCube {
				abandoned = true
				break
			}
		}
		if abandoned {
			continue
		}
		testState := p.w.BlockAt(tx, ty, tz)
		if registry.BlockOf(testState) == registry.BlockShortGrass && p.rng.Intn(10) == 0 && p.tallPlantFits(tx, ty, tz) {
			// vanilla: TallGrassBlock.performBonemeal; the try places nothing
			// else.
			p.placeDoublePlant(tx, ty, tz, registry.BlockTallGrass, changes)
		}
		if !IsKnownAirState(testState) || !contains(ty) {
			continue
		}
		if p.rng.Intn(8) == 0 {
			p.placeGrassFlower(tx, ty, tz, changes)
			continue
		}
		p.setPlacedShaped(tx, ty, tz, registry.BlockShortGrass.DefaultState(), changes)
	}
}

// placeGrassFlower plants one of the biome's bone meal features: a feature from
// the list, then one of its states (vanilla:
// BiomeGenerationSettings.getBoneMealFeatures, SimpleBlockFeature).
func (p *Physics) placeGrassFlower(x, y, z int32, changes *[]BlockChange) {
	var features []grassFlower
	if b := p.w.Biome(x, y, z); b >= 0 && b < registry.BiomeCount {
		features = boneMealFlowers[b]
	}
	if len(features) == 0 {
		return
	}
	state := features[p.rng.Intn(len(features))](p.rng, x, y, z)
	b := registry.BlockOf(state)
	if b.IsDoublePlant() {
		if !p.airAt(x, y+1, z) || !contains(y+1) {
			return
		}
		p.placeDoublePlant(x, y, z, b, changes)
		return
	}
	p.setPlacedShaped(x, y, z, state, changes)
}

// blockDrop is the block's own item as a drop.
func blockDrop(b registry.Block) []registry.Item {
	if item, ok := b.Item(); ok {
		return []registry.Item{item}
	}
	return nil
}

// grassFlower picks one state of a bone meal feature for the cell (x, y, z).
type grassFlower func(r *rand.Rand, x, y, z int32) uint16

// flowerOf picks a default state; a repeated block is the provider's weight.
func flowerOf(blocks ...registry.Block) grassFlower {
	states := make([]uint16, len(blocks))
	for i, b := range blocks {
		states[i] = b.DefaultState()
	}
	return func(r *rand.Rand, _, _, _ int32) uint16 {
		return states[r.Intn(len(states))]
	}
}

// flowerBed places pink_petals or wildflowers: 1-4 flowers, any horizontal
// facing, all sixteen states equally likely.
func flowerBed(b registry.Block) grassFlower {
	base := b.DefaultState()
	return func(r *rand.Rand, _, _, _ int32) uint16 {
		i := r.Intn(4 * 4)
		state, _ := registry.WithProp(base, registry.PropFacing, flowerBedFacings[i%4])
		state, _ = registry.WithInt(state, registry.PropFlowerAmount, i/4+1)
		return state
	}
}

// flowerBedFacings is the order the bed features list their facings.
var flowerBedFacings = [4]string{"north", "east", "south", "west"}

// The can_spawn_from_bone_meal features, each with the states its provider may
// place.
var (
	flowerDefault     = flowerOf(registry.BlockPoppy, registry.BlockPoppy, registry.BlockDandelion)
	flowerPlain       = configuredFlower("minecraft:flower_plain")
	flowerForest      = configuredFlower("minecraft:flower_flower_forest")
	flowerSwamp       = flowerOf(registry.BlockBlueOrchid)
	flowerMeadow      = configuredFlower("minecraft:flower_meadow")
	flowerCherry      = flowerBed(registry.BlockPinkPetals)
	flowerWildflowers = flowerBed(registry.BlockWildflowers)
	flowerPaleGarden  = flowerOf(registry.BlockClosedEyeblossom)
)

// configuredFlower picks the state of a simple_block configured feature's
// provider at the cell, with a random stream seeded from the attempt's.
// vanilla: SimpleBlockFeature.place
func configuredFlower(name string) grassFlower {
	provider := sync.OnceValue(func() stateProvider {
		src, ok := levelgen.OverworldTreeFeature(false, name)
		if !ok {
			modelFail("configured feature %s", name)
		}
		return compileState(parseJSON(src)["to_place"])
	})
	return func(r *rand.Rand, x, y, z int32) uint16 {
		region := &decorRegion{rng: levelgen.NewWorldgenRandom(r.Int63())}
		return provider()(region, [3]int32{x, y, z})
	}
}

// boneMealFlowers maps each biome to its can_spawn_from_bone_meal features in
// list order; a biome with none has no entry (vanilla:
// BiomeGenerationSettings.getBoneMealFeatures).
var boneMealFlowers = [registry.BiomeCount][]grassFlower{
	registry.BiomeBambooJungle:           {flowerDefault},
	registry.BiomeBeach:                  {flowerDefault},
	registry.BiomeBirchForest:            {flowerWildflowers, flowerDefault},
	registry.BiomeCherryGrove:            {flowerCherry},
	registry.BiomeColdOcean:              {flowerDefault},
	registry.BiomeDarkForest:             {flowerDefault},
	registry.BiomeDeepColdOcean:          {flowerDefault},
	registry.BiomeDeepDark:               {flowerPlain},
	registry.BiomeDeepFrozenOcean:        {flowerDefault},
	registry.BiomeDeepLukewarmOcean:      {flowerDefault},
	registry.BiomeDeepOcean:              {flowerDefault},
	registry.BiomeDesert:                 {flowerDefault},
	registry.BiomeDripstoneCaves:         {flowerPlain},
	registry.BiomeFlowerForest:           {flowerForest},
	registry.BiomeForest:                 {flowerDefault},
	registry.BiomeFrozenOcean:            {flowerDefault},
	registry.BiomeFrozenRiver:            {flowerDefault},
	registry.BiomeIceSpikes:              {flowerDefault},
	registry.BiomeJungle:                 {flowerDefault},
	registry.BiomeLukewarmOcean:          {flowerDefault},
	registry.BiomeMeadow:                 {flowerMeadow, flowerWildflowers},
	registry.BiomeOcean:                  {flowerDefault},
	registry.BiomeOldGrowthBirchForest:   {flowerWildflowers, flowerDefault},
	registry.BiomeOldGrowthPineTaiga:     {flowerDefault},
	registry.BiomeOldGrowthSpruceTaiga:   {flowerDefault},
	registry.BiomePaleGarden:             {flowerPaleGarden},
	registry.BiomePlains:                 {flowerPlain},
	registry.BiomeRiver:                  {flowerDefault},
	registry.BiomeSavanna:                {flowerDefault},
	registry.BiomeSavannaPlateau:         {flowerDefault},
	registry.BiomeSnowyBeach:             {flowerDefault},
	registry.BiomeSnowyPlains:            {flowerDefault},
	registry.BiomeSnowyTaiga:             {flowerDefault},
	registry.BiomeSparseJungle:           {flowerDefault},
	registry.BiomeStonyShore:             {flowerDefault},
	registry.BiomeSunflowerPlains:        {flowerPlain},
	registry.BiomeSwamp:                  {flowerSwamp},
	registry.BiomeTaiga:                  {flowerDefault},
	registry.BiomeWarmOcean:              {flowerDefault},
	registry.BiomeWindsweptForest:        {flowerDefault},
	registry.BiomeWindsweptGravellyHills: {flowerDefault},
	registry.BiomeWindsweptHills:         {flowerDefault},
	registry.BiomeWindsweptSavanna:       {flowerDefault},
}

// spreadableNeighbour is the first empty horizontal neighbour surviving on the
// tag's ground, in face order or shuffled.
// vanilla: BonemealableBlock.findSpreadableNeighbourPos
func (p *Physics) spreadableNeighbour(x, y, z int32, groundTag registry.BlockTagID, shuffle bool) ([3]int32, bool) {
	order := [4]int{0, 1, 2, 3}
	if shuffle {
		for i := 3; i > 0; i-- {
			j := p.rng.Intn(i + 1)
			order[i], order[j] = order[j], order[i]
		}
	}
	for _, i := range order {
		nx, nz := x+horizontal4[i][0], z+horizontal4[i][1]
		if !p.airAt(nx, y, nz) {
			continue
		}
		if !registry.BlockOf(p.w.BlockAt(nx, y-1, nz)).InTag(groundTag) {
			continue
		}
		return [3]int32{nx, y, nz}, true
	}
	return [3]int32{}, false
}

// spreadVegetation is the vanilla BushBlock.performBonemeal.
func (p *Physics) spreadVegetation(x, y, z int32, block registry.Block, groundTag registry.BlockTagID, changes *[]BlockChange) {
	at, ok := p.spreadableNeighbour(x, y, z, groundTag, true)
	if !ok {
		return
	}
	p.setShaped(at[0], at[1], at[2], block.DefaultState(), changes)
}

// growSeaPickles seeds the water-on-coral diamond around the pickle 1 in 6 and
// fills the pickle to 4.
// vanilla: SeaPickleBlock.performBonemeal
func (p *Physics) growSeaPickles(x, y, z int32, state uint16, changes *[]BlockChange) {
	zSpan, zOffset, count := 1, 0, 0
	for col := range 5 {
		for zAt := 0; zAt < zSpan; zAt++ {
			for row := y - 1; row < y+1; row++ {
				px, py, pz := x-2+int32(col), row, z-int32(zOffset)+int32(zAt)
				if px == x && py == y && pz == z || p.rng.Intn(6) != 0 || !p.waterAt(px, py, pz) {
					continue
				}
				if !registry.BlockOf(p.w.BlockAt(px, py-1, pz)).InTag(registry.TagIDCoralBlocks) {
					continue
				}
				if next, ok := registry.WithInt(registry.BlockSeaPickle.DefaultState(), registry.PropPickles, p.rng.Intn(4)+1); ok {
					p.setShaped(px, py, pz, next, changes)
				}
			}
		}
		if count < 2 {
			zSpan += 2
			zOffset++
		} else {
			zSpan -= 2
			zOffset--
		}
		count++
	}
	p.bonemealProp(x, y, z, state, registry.PropPickles, "4", changes)
}

// growingPlant is one of the vines or the kelp (vanilla: GrowingPlantHeadBlock).
type growingPlant struct {
	head, body   registry.Block
	dir          [3]int32
	intoWater    bool
	blocksToGrow func(*Physics) int
	// growChance is the head's growPerTickProbability for the random tick.
	growChance float64
}

func oneBlockToGrow(*Physics) int { return 1 }

// netherVineBlocksToGrow is at least 1, each further one 0.826 as likely.
// vanilla: NetherVines.getBlocksToGrowWhenBonemealed
func netherVineBlocksToGrow(p *Physics) int {
	chance := 1.0
	count := 0
	for p.rng.Float64() < chance {
		chance *= 0.826
		count++
	}
	return count
}

// growingPlantOf is the block's chain, the kelp's for a block in none.
func growingPlantOf(b registry.Block) growingPlant {
	switch b {
	case registry.BlockWeepingVines, registry.BlockWeepingVinesPlant:
		return growingPlant{
			head: registry.BlockWeepingVines, body: registry.BlockWeepingVinesPlant,
			dir: [3]int32{0, -1, 0}, blocksToGrow: netherVineBlocksToGrow, growChance: 0.1,
		}
	case registry.BlockTwistingVines, registry.BlockTwistingVinesPlant:
		return growingPlant{
			head: registry.BlockTwistingVines, body: registry.BlockTwistingVinesPlant,
			dir: [3]int32{0, 1, 0}, blocksToGrow: netherVineBlocksToGrow, growChance: 0.1,
		}
	case registry.BlockCaveVines, registry.BlockCaveVinesPlant:
		return growingPlant{
			head: registry.BlockCaveVines, body: registry.BlockCaveVinesPlant,
			dir: [3]int32{0, -1, 0}, blocksToGrow: oneBlockToGrow, growChance: 0.1,
		}
	}
	return growingPlant{
		head: registry.BlockKelp, body: registry.BlockKelpPlant,
		dir: [3]int32{0, 1, 0}, intoWater: true, blocksToGrow: oneBlockToGrow, growChance: 0.14,
	}
}

// canGrowInto is what the chain's far cell must be: water for kelp, air for
// vines (vanilla: KelpBlock.canGrowInto, NetherVines.isValidGrowthState).
func (g growingPlant) canGrowInto(p *Physics, x, y, z int32) bool {
	if g.intoWater {
		return p.blockIs(x, y, z, registry.BlockWater)
	}
	return p.airAt(x, y, z)
}

// growPlantHead writes the head on along the growth, age capped at 25, turning
// vacated cells into body (updateShape's conversion, here without a neighbour
// update).
// vanilla: GrowingPlantHeadBlock.performBonemeal
func (p *Physics) growPlantHead(g growingPlant, x, y, z int32, state uint16, changes *[]BlockChange) {
	age, ok := registry.StateInt(state, registry.PropAge)
	if !ok {
		return
	}
	nextAge := min(age+1, 25)
	fx, fy, fz := x+g.dir[0], y+g.dir[1], z+g.dir[2]
	for n := g.blocksToGrow(p); n > 0 && g.canGrowInto(p, fx, fy, fz) && contains(fy); n-- {
		if next, ok := registry.WithInt(g.head.DefaultState(), registry.PropAge, nextAge); ok {
			p.setShaped(fx, fy, fz, next, changes)
		}
		if p.blockIs(x, y, z, g.head) {
			p.setShaped(x, y, z, g.body.DefaultState(), changes)
		}
		x, y, z = fx, fy, fz
		fx, fy, fz = x+g.dir[0], y+g.dir[1], z+g.dir[2]
		nextAge = min(nextAge+1, 25)
	}
}

// plantHeadOf walks a body chain to its head (vanilla:
// GrowingPlantBodyBlock.getHeadPos).
func (p *Physics) plantHeadOf(body registry.Block, x, y, z int32) (int32, int32, int32, bool) {
	g := growingPlantOf(body)
	cx, cy, cz := x, y, z
	for {
		nx, ny, nz := cx+g.dir[0], cy+g.dir[1], cz+g.dir[2]
		s := p.w.BlockAt(nx, ny, nz)
		if int(s) >= registry.StateCount {
			return 0, 0, 0, false
		}
		switch registry.BlockOf(s) {
		case g.head:
			return nx, ny, nz, true
		case g.body:
			cx, cy, cz = nx, ny, nz
		default:
			return 0, 0, 0, false
		}
	}
}

// boneMealPitcherCrop grows the lower half one age at a time under light 8, the
// upper half once two tall.
// vanilla: PitcherCropBlock
func (p *Physics) boneMealPitcherCrop(x, y, z int32, state uint16, changes *[]BlockChange) bool {
	lx, ly, lz := x, y, z
	if registry.StateValue(state, registry.PropHalf) != "lower" {
		ly = y - 1
	}
	lowerState := p.w.BlockAt(lx, ly, lz)
	if registry.BlockOf(lowerState) != registry.BlockOf(state) || registry.StateValue(lowerState, registry.PropHalf) != "lower" {
		return false
	}
	return p.growPitcherCrop(lx, ly, lz, lowerState, changes)
}

// growPitcherCrop grows one age with the lower half at (x, y, z) under light 8,
// adding the upper half once two tall, and reports whether it grew.
// vanilla: PitcherCropBlock.grow
func (p *Physics) growPitcherCrop(lx, ly, lz int32, state uint16, changes *[]BlockChange) bool {
	age, ok := registry.StateInt(state, registry.PropAge)
	if !ok || age >= 4 {
		return false
	}
	newAge := age + 1
	upper := p.w.BlockAt(lx, ly+1, lz)
	if rawLightAt(p.w, lx, ly, lz) < 8 || !contains(ly+1) ||
		(newAge >= 3 && !IsKnownAirState(upper) && registry.BlockOf(upper) != registry.BlockOf(state)) {
		return false
	}
	next, found := registry.WithProps(registry.BlockPitcherCrop.DefaultState(), registry.PropValue{P: registry.PropHalf, V: "lower"}, registry.PropValue{P: registry.PropAge, V: strconv.Itoa(newAge)})
	if !found {
		return false
	}
	p.setClientShaped(lx, ly, lz, next, changes)
	if newAge >= 3 {
		if upper, found := registry.WithProp(next, registry.PropHalf, "upper"); found {
			p.setShaped(lx, ly+1, lz, upper, changes)
		}
	}
	return true
}

// boneMealSmallDripleaf gives the cell above back to its fluid and grows the
// big dripleaf from here.
// vanilla: SmallDripleafBlock.performBonemeal
func (p *Physics) boneMealSmallDripleaf(x, y, z int32, state uint16, changes *[]BlockChange) {
	aboveState := p.w.BlockAt(x, y+1, z)
	if fs := fluidHolding(aboveState); fs.kind != fluidNone {
		p.setClient(x, y+1, z, fluidStateAt(fs.kind, fs.legacyLevel()), changes)
	} else {
		p.setClient(x, y+1, z, registry.BlockAir.DefaultState(), changes)
	}
	p.placeBigDripleaf(x, y, z, registry.StateValue(state, registry.PropFacing), changes)
}

// dripleafCanReplace is the vanilla BigDripleafBlock.canReplace.
func dripleafCanReplace(state uint16) bool {
	b := registry.BlockOf(state)
	return IsKnownAirState(state) || b == registry.BlockWater || b == registry.BlockSmallDripleaf
}

// dripleafCanGrowInto is BigDripleafBlock.canGrowInto within the build height.
func (p *Physics) dripleafCanGrowInto(x, y, z int32) bool {
	if !contains(y) {
		return false
	}
	return dripleafCanReplace(p.w.BlockAt(x, y, z))
}

// setDripleafCell writes a stem or leaf cell, waterlogged on a full water
// source (vanilla: BigDripleafBlock.place, BigDripleafStemBlock.place).
func (p *Physics) setDripleafCell(x, y, z int32, block registry.Block, facing string, changes *[]BlockChange) {
	fs := fluidHolding(p.w.BlockAt(x, y, z))
	state, _ := registry.WithProp(block.DefaultState(), registry.PropFacing, facing)
	state, _ = registry.WithBool(state, registry.PropWaterlogged, fs.kind == fluidWater && fs.amount == 8)
	p.setShaped(x, y, z, state, changes)
}

// placeBigDripleaf is a 2-5 tall stem with the leaf on top.
// vanilla: BigDripleafBlock.placeWithRandomHeight
func (p *Physics) placeBigDripleaf(x, y, z int32, facing string, changes *[]BlockChange) {
	desired := 2 + p.rng.Intn(4)
	cy := y
	height := 0
	for ; height < desired && p.dripleafCanGrowInto(x, cy, z); height++ {
		cy++
	}
	leafY := y + int32(height) - 1
	for sy := y; sy < leafY; sy++ {
		p.setDripleafCell(x, sy, z, registry.BlockBigDripleafStem, facing, changes)
	}
	p.setDripleafCell(x, leafY, z, registry.BlockBigDripleaf, facing, changes)
}

// boneMealBigDripleaf grows the leaf one cell up.
// vanilla: BigDripleafBlock.performBonemeal
func (p *Physics) boneMealBigDripleaf(x, y, z int32, state uint16, changes *[]BlockChange) {
	if !p.dripleafCanGrowInto(x, y+1, z) {
		return
	}
	facing := registry.StateValue(state, registry.PropFacing)
	p.setDripleafCell(x, y, z, registry.BlockBigDripleafStem, facing, changes)
	p.setDripleafCell(x, y+1, z, registry.BlockBigDripleaf, facing, changes)
}

// boneMealBigDripleafStem grows a leaf above the stem's chain.
// vanilla: BigDripleafStemBlock
func (p *Physics) boneMealBigDripleafStem(x, y, z int32, state uint16, changes *[]BlockChange) bool {
	cx, cy, cz := x, y, z
	for {
		nx, ny, nz := cx, cy+1, cz
		block := registry.BlockOf(p.w.BlockAt(nx, ny, nz))
		if block == registry.BlockBigDripleaf {
			if !p.dripleafCanGrowInto(nx, ny+1, nz) {
				return false
			}
			facing := registry.StateValue(state, registry.PropFacing)
			p.setDripleafCell(nx, ny, nz, registry.BlockBigDripleafStem, facing, changes)
			p.setDripleafCell(nx, ny+1, nz, registry.BlockBigDripleaf, facing, changes)
			return true
		}
		if block != registry.BlockBigDripleafStem {
			return false
		}
		cx, cy, cz = nx, ny, nz
	}
}

// nyliumAround reports crimson or warped nylium in the surrounding 3x3x3
// (vanilla: NetherrackBlock).
func (p *Physics) nyliumAround(x, y, z int32) (red, blue bool) {
	for dx := int32(-1); dx <= 1 && (!red || !blue); dx++ {
		for dy := int32(-1); dy <= 1 && (!red || !blue); dy++ {
			for dz := int32(-1); dz <= 1 && (!red || !blue); dz++ {
				block := registry.BlockOf(p.w.BlockAt(x+dx, y+dy, z+dz))
				if block == registry.BlockCrimsonNylium {
					red = true
				}
				if block == registry.BlockWarpedNylium {
					blue = true
				}
			}
		}
	}
	return red, blue
}

// boneMealBamboo grows 1 or 2 cells on top while the 16 ceiling and stage
// allow.
// vanilla: BambooStalkBlock
func (p *Physics) boneMealBamboo(x, y, z int32, changes *[]BlockChange) bool {
	heightAbove := p.bambooHeight(1, x, y, z)
	heightBelow := p.bambooHeight(-1, x, y, z)
	topState := p.w.BlockAt(x, y+heightAbove, z)
	growthY := y + heightAbove + 1
	if registry.BlockOf(topState) != registry.BlockBamboo || registry.StateValue(topState, registry.PropStage) == "1" ||
		heightAbove+heightBelow+1 >= 16 || !contains(growthY) || !p.airAt(x, growthY, z) {
		return false
	}
	total := heightAbove + heightBelow + 1
	for n := 1 + p.rng.Intn(2); n > 0; n-- {
		topY := y + heightAbove
		top := p.w.BlockAt(x, topY, z)
		if !p.blockIs(x, topY, z, registry.BlockBamboo) {
			return true
		}
		if total >= 16 || registry.StateValue(top, registry.PropStage) == "1" || !p.airAt(x, topY+1, z) || !contains(topY+1) {
			return true
		}
		p.growBambooCell(x, topY, z, top, total, changes)
		heightAbove++
		total++
	}
	return true
}

// bambooHeight counts the stalk above or below, up to 16 (vanilla:
// BambooStalkBlock.getHeightAboveUpToMax).
func (p *Physics) bambooHeight(dir, x, y, z int32) int32 {
	count := int32(0)
	for cy := y + dir; count < 16 && p.blockIs(x, cy, z, registry.BlockBamboo); cy += dir {
		count++
	}
	return count
}

// growBambooCell adds a cell, the two below setting its leaves, stage 1 from
// the 12th cell up.
// vanilla: BambooStalkBlock.growBamboo
func (p *Physics) growBambooCell(x, topY, z int32, top uint16, height int32, changes *[]BlockChange) {
	below, two := p.w.BlockAt(x, topY-1, z), p.w.BlockAt(x, topY-2, z)
	belowBamboo := p.blockIs(x, topY-1, z, registry.BlockBamboo)
	twoBamboo := p.blockIs(x, topY-2, z, registry.BlockBamboo)
	leaves := "none"
	if height >= 1 {
		if !belowBamboo || registry.StateValue(below, registry.PropLeaves) == "none" {
			leaves = "small"
		} else {
			leaves = "large"
			if twoBamboo {
				if next, ok := registry.WithProp(below, registry.PropLeaves, "small"); ok {
					p.setShaped(x, topY-1, z, next, changes)
				}
				if next, ok := registry.WithProp(two, registry.PropLeaves, "none"); ok {
					p.setShaped(x, topY-2, z, next, changes)
				}
			}
		}
	}
	topAge, topAged := registry.StateInt(top, registry.PropAge)
	age := 0
	if topAge == 1 || twoBamboo {
		age = 1
	}
	stage := 0
	if (height >= 11 && p.rng.Float32() < 0.25) || height == 15 {
		stage = 1
	}
	// Past the build ceiling setBlock fails without a neighbour pass: the
	// leaves below keep their rewrite and the stage dice stay spent, but no
	// cell is added and the stalk keeps its age.
	if !contains(topY + 1) {
		return
	}
	next, _ := registry.WithInt(registry.BlockBamboo.DefaultState(), registry.PropAge, age)
	next, _ = registry.WithProp(next, registry.PropLeaves, leaves)
	next, _ = registry.WithInt(next, registry.PropStage, stage)
	p.setShaped(x, topY+1, z, next, changes)
	// The stalk grown over cycles its age when the new cell is older (vanilla:
	// BambooStalkBlock.updateShape).
	if topAged && age > topAge {
		if cycled, found := registry.WithInt(top, registry.PropAge, (topAge+1)%2); found {
			p.setShaped(x, topY, z, cycled, changes)
		}
	}
}

// growBambooSapling puts a small-leaved stalk over the sapling, which the shape
// pass turns into bamboo (vanilla: BambooSaplingBlock.updateShape).
// vanilla: BambooSaplingBlock.growBamboo
func (p *Physics) growBambooSapling(x, y, z int32, changes *[]BlockChange) {
	if next, ok := registry.WithProp(registry.BlockBamboo.DefaultState(), registry.PropLeaves, "small"); ok {
		p.setShaped(x, y+1, z, next, changes)
	}
	p.setShaped(x, y, z, registry.BlockBamboo.DefaultState(), changes)
}

// bambooSaplingTick grows the stalk on a one-in-three roll under raw light 9 or
// more above.
// vanilla: BambooSaplingBlock.randomTick
func (p *Physics) bambooSaplingTick(x, y, z int32, changes *[]BlockChange) {
	if p.rng.Intn(3) != 0 || !p.airAt(x, y+1, z) || !contains(y+1) {
		return
	}
	if rawLightAt(p.w, x, y+1, z) < 9 {
		return
	}
	p.growBambooSapling(x, y, z, changes)
}

// bambooStalkTick grows one cell on a one-in-three roll under raw light 9 or
// more above, below the 16 ceiling. Only stage-0 stalks tick.
// vanilla: BambooStalkBlock.randomTick
func (p *Physics) bambooStalkTick(x, y, z int32, state uint16, changes *[]BlockChange) {
	if stage, ok := registry.StateInt(state, registry.PropStage); !ok || stage != 0 {
		return
	}
	if p.rng.Intn(3) != 0 || !p.airAt(x, y+1, z) {
		return
	}
	if rawLightAt(p.w, x, y+1, z) < 9 {
		return
	}
	height := p.bambooHeight(-1, x, y, z) + 1
	if height >= 16 {
		return
	}
	p.growBambooCell(x, y, z, state, height, changes)
}

// mossCarpetHasFaces is MossyCarpetBlock.hasFaces.
func mossCarpetHasFaces(state uint16) bool {
	for _, face := range bonemealHorizontal {
		if registry.StateBool(state, registry.PropBottom) || registry.StateValue(state, facePropIDs[face]) != "none" {
			return true
		}
	}
	return false
}

// boneMealHangingMoss grows the chain one cell down, the new tip written with
// flag 3 and the old one untipped (updateShape's rule, without a neighbour
// update).
// vanilla: HangingMossBlock
func (p *Physics) boneMealHangingMoss(x, y, z int32, state uint16, changes *[]BlockChange) bool {
	tx, ty, tz := x, y, z
	for registry.BlockOf(p.w.BlockAt(tx, ty-1, tz)) == registry.BlockPaleHangingMoss {
		ty--
	}
	if !p.airAt(tx, ty-1, tz) || !contains(ty-1) {
		return false
	}
	if next, ok := registry.WithBool(state, registry.PropTip, true); ok {
		p.setShaped(tx, ty-1, tz, next, changes)
	}
	if next, ok := registry.WithBool(state, registry.PropTip, false); ok {
		p.setShaped(tx, ty, tz, next, changes)
	}
	return true
}

// lichenCanSpreadSomewhere is GlowLichenBlock.isValidBonemealTarget.
func (p *Physics) lichenCanSpreadSomewhere(x, y, z int32, state uint16) bool {
	sp := p.lichenSpreader()
	for from := range 6 {
		for d := range 6 {
			if _, _, ok := sp.target(state, [3]int32{x, y, z}, from, d); ok {
				return true
			}
		}
	}
	return false
}

// spreadLichen shuffles the carried faces, and the first that spreads takes a
// shuffled direction.
// vanilla: MultifaceSpreader.spreadFromRandomFaceTowardRandomDirection
func (p *Physics) spreadLichen(x, y, z int32, state uint16, changes *[]BlockChange) {
	sp := p.lichenSpreader()
	for _, from := range p.shuffledDirections() {
		if !registry.StateBool(state, facePropIDs[from]) {
			continue
		}
		for _, dir := range p.shuffledDirections() {
			if pos, ns, ok := sp.target(state, [3]int32{x, y, z}, int(from), int(dir)); ok {
				p.setClientShaped(pos[0], pos[1], pos[2], ns, changes)
				return
			}
		}
	}
}

// lichenSpreader reads the world through the physics.
func (p *Physics) lichenSpreader() multifaceSpreader {
	return multifaceSpreader{registry.BlockGlowLichen, p.w.BlockAt}
}

// shuffledDirections is the six directions in the vanilla allShuffled order.
func (p *Physics) shuffledDirections() [6]int32 {
	order := [6]int32{0, 1, 2, 3, 4, 5}
	for i := 5; i > 0; i-- {
		j := p.rng.Intn(i + 1)
		order[i], order[j] = order[j], order[i]
	}
	return order
}

// growWaterPlant makes 128 tries planting the water column: seagrass, unless
// the biome grows corals from bone meal, where face 0's wall coral leads. face
// < 0 is the dispenser's.
// vanilla: BoneMealItem.growWaterPlant
func (p *Physics) growWaterPlant(wx, wy, wz int32, face int32, changes *[]BlockChange) bool {
	if !p.fullWaterAt(wx, wy, wz) {
		return false
	}
	for j := range 128 {
		tx, ty, tz := wx, wy, wz
		abandoned := false
		for i := 0; i < j/16; i++ {
			dx := int32(p.rng.Intn(3)) - 1
			dy := (int32(p.rng.Intn(3)) - 1) * int32(p.rng.Intn(3)) / 2
			dz := int32(p.rng.Intn(3)) - 1
			tx, ty, tz = tx+dx, ty+dy, tz+dz
			if collisionOf(p.w.BlockAt(tx, ty, tz)).fullCube {
				abandoned = true
				break
			}
		}
		if abandoned {
			continue
		}
		growState := registry.BlockSeagrass.DefaultState()
		if p.w.Biome(tx, ty, tz).InTag(registry.BiomeTagIDProducesCoralsFromBonemeal) {
			if j == 0 && face >= 2 && face <= 5 {
				walls := registry.TagIDWallCorals.Blocks()
				block := walls[p.rng.Intn(len(walls))]
				growState = block.DefaultState()
				growState, _ = registry.WithProp(growState, registry.PropFacing, faceProps[face])
			} else if p.rng.Intn(4) == 0 {
				bonemeals := registry.TagIDUnderwaterBonemeals.Blocks()
				block := bonemeals[p.rng.Intn(len(bonemeals))]
				growState = block.DefaultState()
			}
		}
		if stateInTag(growState, registry.TagIDWallCorals) {
			for d := 0; d < 4 && !p.waterPlantSurvives(growState, tx, ty, tz); d++ {
				growState, _ = registry.WithProp(growState, registry.PropFacing, faceProps[bonemealHorizontal[p.rng.Intn(4)]])
			}
		}
		if !p.waterPlantSurvives(growState, tx, ty, tz) {
			continue
		}
		testBlock := registry.BlockOf(p.w.BlockAt(tx, ty, tz))
		if testBlock == registry.BlockWater && p.fullWaterAt(tx, ty, tz) {
			p.setShaped(tx, ty, tz, growState, changes)
			continue
		}
		if testBlock != registry.BlockSeagrass {
			continue
		}
		if registry.BlockOf(p.w.BlockAt(tx, ty+1, tz)) != registry.BlockWater {
			continue
		}
		if p.rng.Intn(10) != 0 {
			continue
		}
		// vanilla: SeagrassBlock.performBonemeal
		p.placeDoublePlant(tx, ty, tz, registry.BlockTallSeagrass, changes)
	}
	return true
}

// waterPlantSurvives is the spread's canSurvive: seagrass
// (vanilla: SeagrassBlock.mayPlaceOn), wall corals on the block behind
// (vanilla: BaseCoralWallFanBlock.canSurvive), the other corals on sturdy
// ground below (vanilla: BaseCoralPlantTypeBlock.canSurvive).
func (p *Physics) waterPlantSurvives(state uint16, x, y, z int32) bool {
	switch {
	case registry.BlockOf(state) == registry.BlockSeagrass:
		below := p.w.BlockAt(x, y-1, z)
		return registry.FaceSturdy(below, dirUp, registry.SupportFull) &&
			!stateInTag(below, registry.TagIDCannotSupportSeagrass)
	case stateInTag(state, registry.TagIDWallCorals):
		facing := registry.StateValue(state, registry.PropFacing)
		off := protocol.FaceOffset[dirIndex(facing)]
		behind := p.w.BlockAt(x-off[0], y-off[1], z-off[2])
		return int(behind) < registry.StateCount && registry.FaceSturdy(behind, dirIndex(facing), registry.SupportFull)
	case stateInTag(state, registry.TagIDUnderwaterBonemeals):
		below := p.w.BlockAt(x, y-1, z)
		return int(below) < registry.StateCount && registry.FaceSturdy(below, dirUp, registry.SupportFull)
	}
	return true
}

// vanilla: SweetBerryBushBlock, CocoaBlock, ShelfMushroomBlock advance exactly one age.
var singleAgeCrops = [registry.BlockCount]uint8{
	registry.BlockSweetBerryBush: 3,
	registry.BlockCocoa:          2,
	registry.BlockShelfMushroom:  1,
}

// vanilla: TorchflowerCropBlock replaces the final age with the flower.
func (p *Physics) boneMealTorchflower(x, y, z int32, state uint16, changes *[]BlockChange) bool {
	age, ok := registry.StateInt(state, registry.PropAge)
	if !ok {
		return false
	}
	if next := age + 1; next > 1 {
		p.setClientShaped(x, y, z, registry.BlockTorchflower.DefaultState(), changes)
	} else {
		p.bonemealProp(x, y, z, state, registry.PropAge, "1", changes)
	}
	return true
}

// journalCell is a cell a journal saw written: the state it had before the
// first write and the one it holds after the last.
type journalCell struct {
	x, y, z  int32
	old, new uint16
}

// netCells folds a journal into one entry per cell, in first-write order, and
// drops the cells that ended where they began.
func netCells(cells [][3]int32, olds, news []uint16) []journalCell {
	var out []journalCell
	index := make(map[[3]int32]int, len(cells))
	for i, c := range cells {
		if at, ok := index[c]; ok {
			out[at].new = news[i]
			continue
		}
		index[c] = len(out)
		out = append(out, journalCell{c[0], c[1], c[2], olds[i], news[i]})
	}
	return slices.DeleteFunc(out, func(c journalCell) bool { return c.old == c.new })
}

// revertCell puts a journalled cell back unless another writer has changed it.
func (p *Physics) revertCell(c journalCell) {
	if p.w.BlockAt(c.x, c.y, c.z) == c.new {
		p.w.SetBlock(c.x, c.y, c.z, c.old)
	}
}

// applyEditedCells applies the listeners' list over what the growth wrote: a
// cell with another state gets it, one the listeners removed is restored, and
// both are reflected in the changes the clients are sent.
func (p *Physics) applyEditedCells(net []journalCell, edited []BlockChange, changes []BlockChange) []BlockChange {
	for _, c := range net {
		at := slices.IndexFunc(edited, func(e BlockChange) bool { return e.X == c.x && e.Y == c.y && e.Z == c.z })
		switch {
		case at < 0:
			p.revertCell(c)
			changes = slices.DeleteFunc(changes, func(ch BlockChange) bool { return ch.X == c.x && ch.Y == c.y && ch.Z == c.z })
		case edited[at].State != c.new:
			p.w.SetBlock(c.x, c.y, c.z, edited[at].State)
			for i := range changes {
				if changes[i].X == c.x && changes[i].Y == c.y && changes[i].Z == c.z {
					changes[i].State = edited[at].State
				}
			}
		}
	}
	return changes
}
