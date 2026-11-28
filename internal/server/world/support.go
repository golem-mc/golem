package world

import (
	"github.com/golem-mc/golem/internal/server/blocklogic"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// canSurvive for every block that hangs on something, and the updateShape and
// neighborChanged checks that pop it when the support goes, precomputed per
// state so a block without a rule costs one table read.

// survivalKind names one canSurvive.
type survivalKind uint8

const (
	surviveAlways survivalKind = iota
	// surviveSturdy: the neighbour toward dir isFaceSturdy (FULL).
	surviveSturdy
	// surviveCenter: Block.canSupportCenter, which refuses an
	// unstable_bottom_center block above a hanging one.
	surviveCenter
	// surviveCenterFace: isFaceSturdy CENTER (CeilingHangingSignBlock).
	surviveCenterFace
	// surviveRigid: canSupportRigidBlock below, and beside an ascending
	// rail's raised end (BaseRailBlock.shouldBeRemoved; arg is that
	// direction plus one).
	surviveRigid
	// survivePlate: BasePressurePlateBlock, RIGID or canSupportCenter.
	survivePlate
	// surviveWire: RedStoneWireBlock.canSurviveOn, a sturdy top or a hopper.
	surviveWire
	// surviveSolid: the neighbour isSolid (signs, banners).
	surviveSolid
	// surviveNotAir: CarpetBlock, anything but air below.
	surviveNotAir
	surviveMossyCarpet
	surviveSnow
	// surviveCocoa: the block the pod faces is #supports_cocoa.
	surviveCocoa
	// surviveSoil: VegetationBlock.mayPlaceOn, the block below carrying
	// the neighbour bit arg names.
	surviveSoil
	surviveMushroom
	surviveCactusFlower
	surviveCrop
	// surviveVine and surviveMultiface: arg is the state's face mask.
	surviveVine
	surviveMultiface
	// surviveSpeleothem: dir points behind the tip.
	surviveSpeleothem
	// surviveGrowingPlant: dir points at what the plant grows from; arg
	// names its head and body pair.
	surviveGrowingPlant
	surviveBigDripleaf
	surviveBigDripleafStem
	surviveHangingMoss
	surviveSporeBlossom
	// surviveChorusFlower and surviveChorusPlant are the end growth's own
	// support rules, which the generic direction-walk cannot spell.
	surviveChorusFlower
	surviveChorusPlant
	// surviveOnWater: a water source below with no fluid over it, or a
	// block carrying the neighbour bit arg names (LilyPadBlock.mayPlaceOn,
	// FrogspawnBlock.mayPlaceOn).
	surviveOnWater
	// surviveSturdyUnless: a sturdy face toward dir on a block not carrying
	// the neighbour bit arg names (SeagrassBlock.mayPlaceOn).
	surviveSturdyUnless
	// surviveCollisionTop: the block below has a collision face or a sturdy
	// one toward the top (SeaPickleBlock.mayPlaceOn).
	surviveCollisionTop
)

// survival is one state's rule. dir points from the block toward what holds it.
// watch masks the directions whose change rechecks it (the updateShape or
// neighborChanged gate); zero means canSurvive only gates placement (a candle
// stays when its table goes). tick is the game-tick delay of the removal tick a
// block's updateShape schedules instead of answering air, zero to go at once.
type survival struct {
	kind  survivalKind
	dir   uint8
	arg   uint8
	watch uint8
	tick  uint8
}

const watchAll = 1<<dirCount - 1

// The neighbour bits the rules test: tags, and a few block identities.
const (
	tagVegetation = iota
	tagDryVegetation
	tagAzalea
	tagBamboo
	tagNetherSprouts
	tagCrimsonFungus
	tagWarpedFungus
	tagCrimsonRoots
	tagWarpedRoots
	tagWitherRose
	tagNetherWart
	tagBigDripleaf
	tagCocoa
	tagNoSnow
	tagSnowOverride
	tagMushroomLight
	tagCactusFlowerOverride
	tagUnstableBottomCenter
	tagSupportsMelonStem
	tagSupportsPumpkinStem
	tagSupportsLilyPad
	tagSupportsFrogspawn
	tagSupportsMangrovePropagule
	tagSupportsHangingMangrovePropagule
	tagCannotSupportKelp
	tagCannotSupportSeagrass
	isAirBit
	isHopperBit
	isFullSnowBit
	isMossyBaseBit
)

var neighbourTags = [...]struct {
	bit int
	tag string
}{
	{tagVegetation, "minecraft:supports_vegetation"},
	{tagDryVegetation, "minecraft:supports_dry_vegetation"},
	{tagAzalea, "minecraft:supports_azalea"},
	{tagBamboo, "minecraft:supports_bamboo"},
	{tagNetherSprouts, "minecraft:supports_nether_sprouts"},
	{tagCrimsonFungus, "minecraft:supports_crimson_fungus"},
	{tagWarpedFungus, "minecraft:supports_warped_fungus"},
	{tagCrimsonRoots, "minecraft:supports_crimson_roots"},
	{tagWarpedRoots, "minecraft:supports_warped_roots"},
	{tagWitherRose, "minecraft:supports_wither_rose"},
	{tagNetherWart, "minecraft:supports_nether_wart"},
	{tagBigDripleaf, "minecraft:supports_big_dripleaf"},
	{tagCocoa, "minecraft:supports_cocoa"},
	{tagNoSnow, "minecraft:cannot_support_snow_layer"},
	{tagSnowOverride, "minecraft:support_override_snow_layer"},
	{tagMushroomLight, "minecraft:overrides_mushroom_light_requirement"},
	{tagCactusFlowerOverride, "minecraft:support_override_cactus_flower"},
	{tagUnstableBottomCenter, "minecraft:unstable_bottom_center"},
	{tagSupportsMelonStem, "minecraft:supports_melon_stem"},
	{tagSupportsPumpkinStem, "minecraft:supports_pumpkin_stem"},
	{tagSupportsLilyPad, "minecraft:supports_lily_pad"},
	{tagSupportsFrogspawn, "minecraft:supports_frogspawn"},
	{tagSupportsMangrovePropagule, "minecraft:supports_mangrove_propagule"},
	{tagSupportsHangingMangrovePropagule, "minecraft:supports_hanging_mangrove_propagule"},
	{tagCannotSupportKelp, "minecraft:cannot_support_kelp"},
	{tagCannotSupportSeagrass, "minecraft:cannot_support_seagrass"},
}

// soilTags maps the vegetation types to the tag their mayPlaceOn reads.
var soilTags = map[registry.BlockType]int{
	registry.BlockTypeFlower:         tagVegetation,
	registry.BlockTypeTallFlower:     tagVegetation,
	registry.BlockTypeDoublePlant:    tagVegetation,
	registry.BlockTypeTallGrass:      tagVegetation,
	registry.BlockTypeFlowerBed:      tagVegetation,
	registry.BlockTypeSapling:        tagVegetation,
	registry.BlockTypeBush:           tagVegetation,
	registry.BlockTypeFireflyBush:    tagVegetation,
	registry.BlockTypeEyeblossom:     tagVegetation,
	registry.BlockTypeSweetBerryBush: tagVegetation,
	registry.BlockTypeAzalea:         tagAzalea,
	registry.BlockTypeDryVegetation:  tagDryVegetation,
	registry.BlockTypeShortDryGrass:  tagDryVegetation,
	registry.BlockTypeTallDryGrass:   tagDryVegetation,
	registry.BlockTypeNetherSprouts:  tagNetherSprouts,
	registry.BlockTypeWitherRose:     tagWitherRose,
	registry.BlockTypeNetherWart:     tagNetherWart,
	registry.BlockTypeBambooStalk:    tagBamboo,
	registry.BlockTypeBambooSapling:  tagBamboo,
}

// soilTagsByBlock covers blocks whose support tag is per block, not per type
// (NetherFungusBlock and NetherRootsBlock take it as an argument; attached
// stems have their own supports_*_stem tag).
var soilTagsByBlock = map[registry.Block]int{
	registry.BlockCrimsonFungus:       tagCrimsonFungus,
	registry.BlockWarpedFungus:        tagWarpedFungus,
	registry.BlockCrimsonRoots:        tagCrimsonRoots,
	registry.BlockWarpedRoots:         tagWarpedRoots,
	registry.BlockAttachedMelonStem:   tagSupportsMelonStem,
	registry.BlockAttachedPumpkinStem: tagSupportsPumpkinStem,
}

// growingPlants are the GrowingPlantBlock head and body pairs.
var growingPlants = map[registry.Block]struct {
	pair   uint8
	growth int
}{
	registry.BlockCaveVines:          {1, dirDown},
	registry.BlockCaveVinesPlant:     {1, dirDown},
	registry.BlockWeepingVines:       {2, dirDown},
	registry.BlockWeepingVinesPlant:  {2, dirDown},
	registry.BlockTwistingVines:      {3, dirUp},
	registry.BlockTwistingVinesPlant: {3, dirUp},
	registry.BlockKelp:               {kelpPair, dirUp},
	registry.BlockKelpPlant:          {kelpPair, dirUp},
}

// kelpPair is the growing-plant pair whose support also refuses
// cannot_support_kelp (KelpBlock.canAttachTo).
const kelpPair = 4

var (
	survivalOf [65536]survival
	// stateBits holds the neighbour bits from isAirBit up, which no tag names;
	// hasBit reads tag bits live.
	stateBits [65536]uint32
	// blockOf numbers the blocks, for the same-block tests.
	blockOf [65536]uint16
	// plantState marks the head and body states of the growing plants.
	plantState [65536]bool
)

func init() {
	blocks := map[registry.Block]uint16{}
	for id := range survivalOf {
		if _, _, ok := registry.BlockStateName(uint16(id)); !ok {
			continue
		}
		block := registry.BlockOf(uint16(id))
		b, seen := blocks[block]
		if !seen {
			b = uint16(len(blocks) + 1)
			blocks[block] = b
		}
		blockOf[id] = b
		_, plantState[id] = growingPlants[block]
		props := registry.StateProps(uint16(id))
		var bits uint32
		if IsAirState(uint16(id)) {
			bits |= 1 << isAirBit
		}
		if block == registry.BlockHopper {
			bits |= 1 << isHopperBit
		}
		switch {
		case block == registry.BlockSnow && props["layers"] == "8":
			bits |= 1 << isFullSnowBit
		case block == registry.BlockPaleMossCarpet && props["bottom"] == "true":
			bits |= 1 << isMossyBaseBit
		}
		stateBits[id] = bits
		survivalOf[id] = survivalFor(block, props)
	}
}

var cropBlocks = registry.BlockTagOf("minecraft:crops")

// survivalFor reads the rule of one state off its block's definition type.
func survivalFor(block registry.Block, props map[string]string) survival {
	typ := block.Type()
	facing := dirIndex(props["facing"])
	on := func(kind survivalKind, d int) survival {
		return survival{kind: kind, dir: uint8(d), watch: 1 << d}
	}
	anyChange := func(kind survivalKind, d int) survival {
		return survival{kind: kind, dir: uint8(d), watch: watchAll}
	}
	if isDoublePlantState(block, atoiOr(props["age"], 0)) {
		// Both halves are doublePlantUpdate's, which breaks the column.
		return survival{}
	}
	tag, soil := soilTags[typ]
	if t, ok := soilTagsByBlock[block]; ok {
		tag, soil = t, true
	}
	if soil {
		s := anyChange(surviveSoil, dirDown)
		s.arg = uint8(tag)
		if typ == registry.BlockTypeBambooStalk {
			s.tick = 1 // vanilla: BambooStalkBlock.updateShape
		}
		return s
	}
	if block.InTag(cropBlocks) {
		return anyChange(surviveCrop, dirDown)
	}
	if p, ok := growingPlants[block]; ok {
		s := on(surviveGrowingPlant, blocklogic.Opposite(p.growth))
		s.arg, s.tick = p.pair, 1
		return s
	}
	switch block.Type() {
	case registry.BlockTypeTorch, registry.BlockTypeRedstoneTorch:
		return on(surviveCenter, dirDown)
	case registry.BlockTypeWallTorch, registry.BlockTypeRedstoneWallTorch, registry.BlockTypeLadder,
		registry.BlockTypeTripWireHook, registry.BlockTypeAmethystCluster, registry.BlockTypeShelfMushroom:
		return on(surviveSturdy, blocklogic.Opposite(facing))
	case registry.BlockTypeLantern, registry.BlockTypeWeatheringLantern:
		if props["hanging"] == "true" {
			return on(surviveCenter, dirUp)
		}
		return on(surviveCenter, dirDown)
	case registry.BlockTypeButton, registry.BlockTypeLever:
		switch props["face"] {
		case "floor":
			return on(surviveSturdy, dirDown)
		case "ceiling":
			return on(surviveSturdy, dirUp)
		}
		return on(surviveSturdy, blocklogic.Opposite(facing))
	case registry.BlockTypeBell:
		// BellBlock.canSurvive: floor on a sturdy block below, ceiling on a
		// centre support above, walls on the face they point at. updateShape
		// pops it when the connected side stops holding, except a double wall,
		// which turns single (bellShapeUpdate).
		switch props["attachment"] {
		case "ceiling":
			return on(surviveCenter, dirUp)
		case "single_wall":
			return on(surviveSturdy, facing)
		case "double_wall":
			return survival{kind: surviveSturdy, dir: uint8(facing)}
		default:
			return on(surviveSturdy, dirDown)
		}
	case registry.BlockTypeStandingSign, registry.BlockTypeBanner:
		return on(surviveSolid, dirDown)
	case registry.BlockTypeWallSign, registry.BlockTypeWallBanner:
		return on(surviveSolid, blocklogic.Opposite(facing))
	case registry.BlockTypeCeilingHangingSign:
		return on(surviveCenterFace, dirUp)
	case registry.BlockTypeRail, registry.BlockTypePoweredRail, registry.BlockTypeDetectorRail:
		// BaseRailBlock.neighborChanged runs shouldBeRemoved on any change.
		s := anyChange(surviveRigid, dirDown)
		if d, ok := ascendingTo[props["shape"]]; ok {
			s.arg = uint8(d + 1)
		}
		return s
	case registry.BlockTypeRedstoneWire:
		return anyChange(surviveWire, dirDown)
	case registry.BlockTypeRepeater, registry.BlockTypeComparator:
		return anyChange(surviveRigid, dirDown)
	case registry.BlockTypePressurePlate, registry.BlockTypeWeightedPressurePlate:
		return on(survivePlate, dirDown)
	case registry.BlockTypeCandle:
		return survival{kind: surviveCenter, dir: dirDown}
	case registry.BlockTypeSnowLayer:
		return anyChange(surviveSnow, dirDown)
	case registry.BlockTypeWoolCarpet, registry.BlockTypeCarpet:
		return anyChange(surviveNotAir, dirDown)
	case registry.BlockTypeMossyCarpet:
		return anyChange(surviveMossyCarpet, dirDown)
	case registry.BlockTypeCocoa:
		// The pod's facing points at the log.
		return on(surviveCocoa, facing)
	case registry.BlockTypeLeafLitter:
		return anyChange(surviveSturdy, dirDown)
	case registry.BlockTypeMushroom:
		return anyChange(surviveMushroom, dirDown)
	case registry.BlockTypeCactusFlower:
		return anyChange(surviveCactusFlower, dirDown)
	case registry.BlockTypeHangingRoots:
		return on(surviveSturdy, dirUp)
	case registry.BlockTypeSporeBlossom:
		return on(surviveSporeBlossom, dirUp)
	case registry.BlockTypeHangingMoss:
		// vanilla: HangingMossBlock.updateShape, tick
		s := anyChange(surviveHangingMoss, dirUp)
		s.tick = 1
		return s
	case registry.BlockTypeBigDripleaf:
		return on(surviveBigDripleaf, dirDown)
	case registry.BlockTypeBigDripleafStem:
		return survival{kind: surviveBigDripleafStem, dir: dirDown, watch: 1<<dirDown | 1<<dirUp, tick: 1}
	case registry.BlockTypeVine:
		// VineBlock.updateShape ignores a change below.
		return survival{kind: surviveVine, arg: faceMask(props), watch: watchAll &^ (1 << dirDown)}
	case registry.BlockTypeGlowLichen, registry.BlockTypeSculkVein, registry.BlockTypeMultiface:
		return survival{kind: surviveMultiface, arg: faceMask(props), watch: faceMask(props)}
	case registry.BlockTypePointedDripstone, registry.BlockTypeSulfurSpike:
		// vanilla: SpeleothemBlock.updateShape, two ticks for a stalactite
		if props["vertical_direction"] == "down" {
			s := on(surviveSpeleothem, dirUp)
			s.tick = 2
			return s
		}
		s := on(surviveSpeleothem, dirDown)
		s.tick = 1
		return s
	case registry.BlockTypeDoor, registry.BlockTypeWeatheringCopperDoor:
		// vanilla: DoorBlock.canSurvive, a sturdy top under the lower half. The
		// pair's updates are doorLikeShapeUpdate's, which runs this check for
		// its half and holds the upper half to its partner, so nothing watches.
		if props["half"] != "upper" {
			return survival{kind: surviveSturdy, dir: dirDown}
		}
		return survival{}
	case registry.BlockTypeChorusFlower:
		// vanilla: ChorusFlowerBlock.updateShape, which ignores a change
		// from above — the flower's own growth direction.
		return survival{kind: surviveChorusFlower, dir: dirDown, watch: watchAll &^ (1 << dirUp), tick: 1}
	case registry.BlockTypeChorusPlant:
		s := anyChange(surviveChorusPlant, dirDown)
		s.tick = 1
		return s
	case registry.BlockTypeCake, registry.BlockTypeCandleCake:
		// vanilla: CakeBlock.updateShape, CandleCakeBlock.updateShape, only a change below
		return on(surviveSolid, dirDown)
	case registry.BlockTypeSeaPickle:
		return anyChange(surviveCollisionTop, dirDown)
	case registry.BlockTypeSeagrass:
		s := anyChange(surviveSturdyUnless, dirDown)
		s.arg = tagCannotSupportSeagrass
		return s
	case registry.BlockTypeCoralPlant, registry.BlockTypeBaseCoralPlant, registry.BlockTypeCoralFan,
		registry.BlockTypeBaseCoralFan:
		// vanilla: BaseCoralPlantTypeBlock.updateShape, only a change below
		return on(surviveSturdy, dirDown)
	case registry.BlockTypeCoralWallFan, registry.BlockTypeBaseCoralWallFan:
		return on(surviveSturdy, blocklogic.Opposite(facing))
	case registry.BlockTypeMangrovePropagule:
		// vanilla: MangrovePropaguleBlock.canSurvive; VegetationBlock.updateShape
		// rechecks on any change.
		if props["hanging"] == "true" {
			s := anyChange(surviveSoil, dirUp)
			s.arg = tagSupportsHangingMangrovePropagule
			return s
		}
		s := anyChange(surviveSoil, dirDown)
		s.arg = tagSupportsMangrovePropagule
		return s
	case registry.BlockTypeLilyPad:
		s := anyChange(surviveOnWater, dirDown)
		s.arg = tagSupportsLilyPad
		return s
	case registry.BlockTypeFrogspawn:
		s := anyChange(surviveOnWater, dirDown)
		s.arg = tagSupportsFrogspawn
		return s
	}
	return survival{}
}

// ascendingTo is the direction of each ascending rail shape's raised end.
var ascendingTo = map[string]int{
	"ascending_north": dirNorth, "ascending_south": dirSouth,
	"ascending_west": dirWest, "ascending_east": dirEast,
}

var faceProps = [dirCount]string{"down", "up", "north", "south", "west", "east"}

// facePropIDs is faceProps as registry properties.
var facePropIDs = [dirCount]registry.Prop{
	registry.PropDown, registry.PropUp, registry.PropNorth, registry.PropSouth, registry.PropWest, registry.PropEast,
}

// faceMask packs a multiface block's face flags by direction.
func faceMask(props map[string]string) uint8 {
	var m uint8
	for d, p := range faceProps {
		if props[p] == "true" {
			m |= 1 << d
		}
	}
	return m
}

func dirIndex(face string) int {
	for d, p := range faceProps {
		if p == face {
			return d
		}
	}
	return dirNorth
}

// neighbourTagIDs is neighbourTags by bit. Tag bits are read through
// Block.InTag at each test, not stored in stateBits, so tags a data pack swaps
// in at startup or /reload are the ones a rule sees.
var neighbourTagIDs = func() (ids [isAirBit]registry.BlockTagID) {
	for _, t := range neighbourTags {
		ids[t.bit] = registry.BlockTagOf(t.tag)
	}
	return ids
}()

func hasBit(state uint16, bit int) bool {
	if bit < len(neighbourTagIDs) {
		return stateInTag(state, neighbourTagIDs[bit])
	}
	return stateBits[state]>>bit&1 != 0
}

// canSupportCenter is Block.canSupportCenter for the state's face toward d.
func canSupportCenter(state uint16, d int) bool {
	if d == dirDown && hasBit(state, tagUnstableBottomCenter) {
		return false
	}
	return registry.FaceSturdy(state, d, registry.SupportCenter)
}

// supportingState is the state whose shape a neighbour offers for support: a
// retracting piston's base keeps its extended shape while it rides home; any
// other moving piston stays as it is, which supports nothing. A moved block's
// own shape is left out, as it only reaches a face when the ride is at rest.
// vanilla: PistonMovingBlockEntity.getCollisionShape
func supportingState(w BlockReader, x, y, z int32, state uint16) uint16 {
	if registry.BlockOf(state) != registry.BlockMovingPiston {
		return state
	}
	rides, ok := w.(interface {
		pistonRideAt(x, y, z int32) (pistonRide, bool)
	})
	if !ok {
		return state
	}
	r, ok := rides.pistonRideAt(x, y, z)
	if !ok || r.extending || !r.isSource || !pistonBlocks[registry.BlockOf(r.movedState)] {
		return state
	}
	base, _ := registry.WithBool(r.movedState, registry.PropExtended, true)
	return base
}

// canSurvive is the vanilla canSurvive of state at (x, y, z). A reader that is
// not a World has no light, so the light rules read 0.
func canSurvive(w BlockReader, x, y, z int32, state uint16) bool {
	s := survivalOf[state]
	if s.kind == surviveAlways {
		return true
	}
	d := int(s.dir)
	nx, ny, nz := x+dirDelta[d][0], y+dirDelta[d][1], z+dirDelta[d][2]
	n := supportingState(w, nx, ny, nz, w.BlockAt(nx, ny, nz))
	face := blocklogic.Opposite(d)
	switch s.kind {
	case surviveSturdy:
		return registry.FaceSturdy(n, face, registry.SupportFull)
	case surviveCenter:
		return canSupportCenter(n, face)
	case surviveCenterFace:
		return registry.FaceSturdy(n, face, registry.SupportCenter)
	case surviveRigid:
		if !registry.FaceSturdy(n, dirUp, registry.SupportRigid) {
			return false
		}
		if s.arg == 0 {
			return true
		}
		a := int(s.arg - 1)
		return registry.FaceSturdy(w.BlockAt(x+dirDelta[a][0], y, z+dirDelta[a][2]), dirUp, registry.SupportRigid)
	case survivePlate:
		return registry.FaceSturdy(n, dirUp, registry.SupportRigid) || canSupportCenter(n, dirUp)
	case surviveWire:
		return registry.FaceSturdy(n, dirUp, registry.SupportFull) || hasBit(n, isHopperBit)
	case surviveSolid:
		return registry.IsSolid(n)
	case surviveNotAir:
		return !hasBit(n, isAirBit)
	case surviveMossyCarpet:
		if hasBit(state, isMossyBaseBit) {
			return !hasBit(n, isAirBit)
		}
		return hasBit(n, isMossyBaseBit)
	case surviveSnow:
		switch {
		case hasBit(n, tagNoSnow):
			return false
		case hasBit(n, tagSnowOverride):
			return true
		}
		return registry.CollisionTopFull(n) || hasBit(n, isFullSnowBit)
	case surviveCocoa:
		return hasBit(n, tagCocoa)
	case surviveSoil:
		return hasBit(n, int(s.arg))
	case surviveMushroom:
		if hasBit(n, tagMushroomLight) {
			return true
		}
		return rawLightAt(w, x, y, z) < 13 && registry.IsSolidRender(n)
	case surviveCactusFlower:
		return hasBit(n, tagCactusFlowerOverride) || registry.FaceSturdy(n, dirUp, registry.SupportCenter)
	case surviveCrop:
		// vanilla: PitcherCropBlock.canSurvive hands an upper half below age 3
		// to DoublePlantBlock.canSurvive, which skips the light check.
		if registry.BlockOf(state) == registry.BlockPitcherCrop &&
			registry.StateValue(state, registry.PropHalf) == "upper" {
			return IsLowerHalfBelow(w, registry.BlockPitcherCrop, x, y, z)
		}
		if b := registry.BlockOf(state); b == registry.BlockMelonStem || b == registry.BlockPumpkinStem {
			// vanilla: StemBlock overrides only mayPlaceOn; the light rule is
			// CropBlock's.
			return farmlandBelow(w, x, y, z)
		}
		return cropCanSurvive(w, x, y, z)
	case surviveVine:
		return vineFaces(w, x, y, z, s.arg) != 0
	case surviveMultiface:
		return s.arg != 0 && multifaceFaces(w, x, y, z, s.arg) == s.arg
	case surviveSpeleothem:
		// SpeleothemBlock.isValidSpeleothemPlacement: a sturdy face behind
		// the tip, or the same block tipped the same way.
		return registry.FaceSturdy(n, face, registry.SupportFull) ||
			(blockOf[n] == blockOf[state] && survivalOf[n].dir == s.dir)
	case surviveGrowingPlant:
		if s.arg == kelpPair && hasBit(n, tagCannotSupportKelp) {
			return false
		}
		ns := survivalOf[n]
		return (ns.kind == surviveGrowingPlant && ns.arg == s.arg) ||
			registry.FaceSturdy(n, face, registry.SupportFull)
	case surviveBigDripleaf:
		k := survivalOf[n].kind
		return k == surviveBigDripleaf || k == surviveBigDripleafStem || hasBit(n, tagBigDripleaf)
	case surviveBigDripleafStem:
		above := survivalOf[w.BlockAt(x, y+1, z)].kind
		return (survivalOf[n].kind == surviveBigDripleafStem || hasBit(n, tagBigDripleaf)) &&
			(above == surviveBigDripleafStem || above == surviveBigDripleaf)
	case surviveHangingMoss:
		return registry.FaceAttachable(n, face) || blockOf[n] == blockOf[state]
	case surviveSporeBlossom:
		return canSupportCenter(n, face) && fluidHolding(w.BlockAt(x, y, z)).kind != fluidWater
	case surviveChorusFlower:
		return chorusFlowerCanSurvive(w, x, y, z)
	case surviveChorusPlant:
		return chorusPlantCanSurvive(w, x, y, z)
	case surviveSturdyUnless:
		return registry.FaceSturdy(n, face, registry.SupportFull) && !hasBit(n, int(s.arg))
	case surviveCollisionTop:
		return registry.CollisionReachesTop(n) || registry.FaceSturdy(n, face, registry.SupportFull)
	case surviveOnWater:
		return onWaterSource(w, x, y-1, z, int(s.arg))
	}
	return true
}

// onWaterSource reports whether the block above (x, y, z) can stand on it: a
// water source, or a block with neighbour bit tagBit, with no fluid above. Both
// fluid tags hold only the water source.
// vanilla: LilyPadBlock.mayPlaceOn, FrogspawnBlock.mayPlaceOn
func onWaterSource(w BlockReader, x, y, z int32, tagBit int) bool {
	below := w.BlockAt(x, y, z)
	fs := fluidHolding(below)
	supported := (fs.kind == fluidWater && fs.isSource()) || hasBit(below, tagBit)
	return supported && fluidHolding(w.BlockAt(x, y+1, z)).kind == fluidNone
}

// chorusFlowerCanSurvive: on a chorus plant or end-stone-like block, or hanging
// off exactly one horizontal chorus plant with air on its other sides.
// vanilla: ChorusFlowerBlock.canSurvive
func chorusFlowerCanSurvive(w BlockReader, x, y, z int32) bool {
	belowState := w.BlockAt(x, y-1, z)
	below := registry.BlockOf(belowState)
	if below == registry.BlockChorusPlant || below.InTag(registry.TagIDSupportsChorusFlower) {
		return true
	}
	if !IsKnownAirState(belowState) {
		return false
	}
	oneNeighbour := false
	for _, d := range horizontal4 {
		state := w.BlockAt(x+d[0], y, z+d[1])
		if registry.BlockOf(state) == registry.BlockChorusPlant {
			if oneNeighbour {
				return false
			}
			oneNeighbour = true
			continue
		}
		if !IsKnownAirState(state) {
			return false
		}
	}
	return oneNeighbour
}

// chorusPlantCanSurvive: held by a plant or support below, or a horizontal
// plant with one under it, but not sideways while something stands both above
// and below.
// vanilla: ChorusPlantBlock.canSurvive
func chorusPlantCanSurvive(w BlockReader, x, y, z int32) bool {
	belowState := w.BlockAt(x, y-1, z)
	below := registry.BlockOf(belowState)
	aboveAndBelow := !IsKnownAirState(w.BlockAt(x, y+1, z)) && !IsKnownAirState(belowState)
	for _, d := range horizontal4 {
		nx, nz := x+d[0], z+d[1]
		if registry.BlockOf(w.BlockAt(nx, y, nz)) != registry.BlockChorusPlant {
			continue
		}
		if aboveAndBelow {
			return false
		}
		neighbourBelow := registry.BlockOf(w.BlockAt(nx, y-1, nz))
		if neighbourBelow == registry.BlockChorusPlant || neighbourBelow.InTag(registry.TagIDSupportsChorusPlant) {
			return true
		}
	}
	return below == registry.BlockChorusPlant || below.InTag(registry.TagIDSupportsChorusPlant)
}

// vineFaces is the face mask VineBlock.getUpdatedState keeps of mask.
func vineFaces(w BlockReader, x, y, z int32, mask uint8) uint8 {
	above := w.BlockAt(x, y+1, z)
	var kept, aboveMask uint8
	if survivalOf[above].kind == surviveVine {
		aboveMask = survivalOf[above].arg
	}
	// vanilla: the up face asks isAcceptableNeighbour(above, DOWN), which tests
	// the top face of the block above.
	if mask&(1<<dirUp) != 0 && registry.FaceAttachable(above, dirUp) {
		kept |= 1 << dirUp
	}
	for d := dirNorth; d < dirCount; d++ {
		if mask&(1<<d) == 0 {
			continue
		}
		n := w.BlockAt(x+dirDelta[d][0], y, z+dirDelta[d][2])
		if registry.FaceAttachable(n, blocklogic.Opposite(d)) || aboveMask&(1<<d) != 0 {
			kept |= 1 << d
		}
	}
	return kept
}

// multifaceFaces is the part of mask MultifaceBlock.canAttachTo still holds.
func multifaceFaces(w BlockReader, x, y, z int32, mask uint8) uint8 {
	var kept uint8
	for d := range dirCount {
		if mask&(1<<d) == 0 {
			continue
		}
		n := w.BlockAt(x+dirDelta[d][0], y+dirDelta[d][1], z+dirDelta[d][2])
		if registry.FaceAttachable(n, blocklogic.Opposite(d)) {
			kept |= 1 << d
		}
	}
	return kept
}

// Survives is canSurvive for state at (x, y, z), true for a block without a
// rule.
func (w *World) Survives(x, y, z int32, state uint16) bool {
	return canSurvive(w, x, y, z, state)
}

// anyDirection is a change heard from every side, the piston landing's
// updateFromNeighbourShapes.
const anyDirection = -1

// supportCheck answers a change toward a direction, reporting whether the block
// went; a block with a survival tick is scheduled instead and stays for now.
func (p *Physics) supportCheck(x, y, z int32, state uint16, toward int, changes *[]BlockChange, fromShape ...bool) bool {
	s := survivalOf[state]
	if toward != anyDirection && s.watch&(1<<toward) == 0 {
		return false
	}
	if s.kind == surviveVine || s.kind == surviveMultiface {
		return p.trimFaces(x, y, z, state, toward, changes, len(fromShape) > 0 && fromShape[0])
	}
	if canSurvive(p.w, x, y, z, state) {
		return false
	}
	if s.tick != 0 {
		p.scheduleSurvival(x, y, z, p.now+int64(s.tick))
		if s.kind == surviveSpeleothem && s.dir == dirUp {
			p.fallBooked[blockKey{x, y, z}] = true
		}
		return false
	}
	p.popSurvivalLoss(x, y, z, state, changes)
	return true
}

// survivalTickEvent tags a survival tick's queue entry so a waterlogged block's
// fluid visit is not mistaken for it. Event bits: piston events the low five,
// survival five, leaf six, dried ghast seven.
const survivalTickEvent = 1 << 5

// scheduleSurvival books the block's survival tick at due: one per position,
// the first kept, apart from the fluid visit.
// vanilla: LevelChunkTicks.schedule, one queue for block ticks and another
// for fluid ticks
func (p *Physics) scheduleSurvival(x, y, z int32, due int64) {
	p.scheduleBooked(p.survivalBooked, x, y, z, due, survivalTickEvent)
}

// scheduleBooked queues an entry tagged event, booked for the cell's block,
// unless booked already holds the position.
// vanilla: ScheduledTick.type
func (p *Physics) scheduleBooked(booked map[blockKey]bool, x, y, z int32, due int64, event uint8) {
	p.scheduleBookedFor(booked, x, y, z, due, tickPriorityNormal, event, registry.BlockAir)
}

// scheduleBookedFor is scheduleBooked with the tick's priority and the block it
// is booked for (BlockAir for the cell's block, read once the tick is known to
// be new).
func (p *Physics) scheduleBookedFor(booked map[blockKey]bool, x, y, z int32, due int64, priority int, event uint8, block registry.Block) {
	_, unloaded := p.w.prepareTick(x, z, false, false)
	p.discardUnloadedTicks(unloaded)
	key := blockKey{x, y, z}
	if booked[key] {
		return
	}
	booked[key] = true
	if block == registry.BlockAir {
		block = registry.BlockOf(p.w.BlockAt(x, y, z))
	}
	p.seq++
	p.pushTick(scheduledTick{
		pos: key, due: due, priority: priority, seq: p.seq, event: event, block: block,
	})
	p.w.touchContainerChunk(ChunkPos{X: x >> 4, Z: z >> 4})
}

// survivalTick runs supportCheck's booked tick, reporting whether the block
// went. A stalactite that lost its hold falls block by block instead of
// popping, but only if its support check booked the fall: a waterlogged
// stalactite's fluid visit also reaches here with the flag clear and must not
// drop it.
// vanilla: BambooStalkBlock.tick, GrowingPlantBlock.tick,
// BigDripleafStemBlock.tick, SpeleothemBlock.tick
func (p *Physics) survivalTick(x, y, z int32, state uint16, fallBooked bool, changes *[]BlockChange, falls *[]FallingSpawn) bool {
	s := survivalOf[state]
	if s.tick == 0 {
		return false
	}
	// vanilla: SpeleothemBlock.tick drops a hanging stalactite without
	// retesting its hold, so a support put back during the delay does not stop
	// the fall.
	if s.kind == surviveSpeleothem && s.dir == dirUp {
		if !fallBooked {
			return false
		}
		p.fallStalactite(x, y, z, state, changes, falls)
		return true
	}
	if canSurvive(p.w, x, y, z, state) {
		return false
	}
	p.popBlock(x, y, z, state, changes)
	return true
}

// fallStalactite turns the column, from the broken block down to the tip, into
// one falling body per block, each cell left as the water it held. The tip
// hurts what it lands on: 6 for a one-block column, one more per block, capped
// at 40.
// vanilla: SpeleothemBlock.spawnFallingStalactite
func (p *Physics) fallStalactite(x, y, z int32, state uint16, changes *[]BlockChange, falls *[]FallingSpawn) {
	top := y
	for {
		if !isSpeleothemStalactite(state) {
			return
		}
		// vanilla: FallingBlockEntity.fall
		carried, _ := registry.WithBool(state, registry.PropWaterlogged, false)
		p.setShaped(x, y, z, RemovalLeaves(state), changes)
		spawn := FallingSpawn{X: x, Y: y, Z: z, State: carried}
		if isSpeleothemTip(state) {
			amount := max(1+top-y, 6)
			spawn.HurtEntities = true
			spawn.FallHurtAmount = float64(amount)
			spawn.FallHurtMax = 40
			*falls = append(*falls, spawn)
			return
		}
		*falls = append(*falls, spawn)
		y--
		state = p.w.BlockAt(x, y, z)
	}
}

// isSpeleothemTip reports the bottom of a hanging speleothem, plain or merged
// with the stalagmite below (SpeleothemBlock.isTip(state, true)).
func isSpeleothemTip(state uint16) bool {
	t := registry.StateValue(state, registry.PropThickness)
	return t == "tip" || t == "tip_merge"
}

// isSpeleothemStalactite reports a downward speleothem, what
// spawnFallingStalactite walks.
func isSpeleothemStalactite(state uint16) bool {
	return isSpeleothem[registry.BlockOf(state)] &&
		registry.StateValue(state, registry.PropVerticalDirection) == "down"
}

// SpeleothemThickness is the thickness the speleothem block, tipped toward tip,
// takes at (x, y, z), from the blocks in front of and behind the tip.
// mergeOpposingTips is vanilla's tryMerge: a placement merges with an opposing
// tip, an updateShape only where already TIP_MERGE. The cell need not hold the
// block.
// vanilla: SpeleothemBlock.calculateSpeleothemThickness
func SpeleothemThickness(w BlockReader, block registry.Block, tip int, mergeOpposingTips bool, x, y, z int32) string {
	base := blocklogic.Opposite(tip)
	fs := w.BlockAt(x+dirDelta[tip][0], y+dirDelta[tip][1], z+dirDelta[tip][2])
	front := registry.BlockOf(fs)
	frontSpeleothem := front.InTag(registry.TagIDSpeleothems)
	fv := registry.StateValue(fs, registry.PropVerticalDirection)
	if frontSpeleothem && front == block && fv == blocklogic.FaceName(base) {
		if mergeOpposingTips || registry.StateValue(fs, registry.PropThickness) == "tip_merge" {
			return "tip_merge"
		}
		return "tip"
	}
	if !frontSpeleothem || fv != blocklogic.FaceName(tip) {
		return "tip"
	}
	if isSpeleothemTip(fs) {
		return "frustum"
	}
	bs := w.BlockAt(x+dirDelta[base][0], y+dirDelta[base][1], z+dirDelta[base][2])
	if !registry.BlockOf(bs).InTag(registry.TagIDSpeleothems) || registry.StateValue(bs, registry.PropVerticalDirection) != blocklogic.FaceName(tip) {
		return "base"
	}
	return "middle"
}

// trimFaces is the vine's and multiface blocks' updateShape: drop faces that
// lost their hold, and go when none remain.
func (p *Physics) trimFaces(x, y, z int32, state uint16, toward int, changes *[]BlockChange, clientOnly bool) bool {
	s := survivalOf[state]
	var kept uint8
	switch {
	case s.kind == surviveVine:
		kept = vineFaces(p.w, x, y, z, s.arg)
	case toward == anyDirection:
		kept = multifaceFaces(p.w, x, y, z, s.arg)
	default:
		kept = s.arg &^ (1 << toward)
		kept |= multifaceFaces(p.w, x, y, z, s.arg&(1<<toward))
	}
	if kept == 0 {
		p.popBlock(x, y, z, state, changes)
		return true
	}
	if kept != s.arg {
		next := state
		for d, prop := range facePropIDs {
			next, _ = registry.WithBool(next, prop, kept&(1<<d) != 0)
		}
		if clientOnly {
			// The shape pass's replacement carries the shape flags: no neighbour wakes.
			// vanilla: Block.updateOrDestroy
			p.setClientShaped(x, y, z, next, changes)
		} else {
			p.setShaped(x, y, z, next, changes)
		}
	}
	return false
}

// popBlock is the destroyBlock an updateShape's air answer runs: the block
// drops, leaving its water, and the removal wakes as NotifyChanged's would.
func (p *Physics) popBlock(x, y, z int32, state uint16, changes *[]BlockChange) {
	p.removeWithDrop(x, y, z, state, changes, true)
}

// popSurvivalLoss removes a block whose support went; rails and diodes leave
// through neighborChanged, so they drop without the break event.
func (p *Physics) popSurvivalLoss(x, y, z int32, state uint16, changes *[]BlockChange) {
	if b := registry.BlockOf(state); railBlocks[b] || diodeBlocks[b] {
		p.dropBlock(x, y, z, state, changes)
		return
	}
	p.popBlock(x, y, z, state, changes)
}

// dropBlock is popBlock without the break event, for blocks that leave through
// neighborChanged (rails, diodes, the wire fallback) rather than updateShape.
// vanilla: BaseRailBlock.neighborChanged, DiodeBlock.neighborChanged
func (p *Physics) dropBlock(x, y, z int32, state uint16, changes *[]BlockChange) {
	p.removeWithDrop(x, y, z, state, changes, false)
}

func (p *Physics) removeWithDrop(x, y, z int32, state uint16, changes *[]BlockChange, event bool) {
	if p.skipWireShape || p.silentShapes {
		p.removeWithDropResetFlags(x, y, z, state, changes, event)
		return
	}
	leave := RemovalLeaves(state)
	if !p.w.SetBlock(x, y, z, leave) {
		return
	}
	// The block entity rides the drop: the server reads a skull's profile and
	// name from this compound after the entity has left the world.
	tag, _ := p.w.BlockEntityTag(x, y, z)
	// The block entity goes with the block, so a sign's text is not inherited
	// by the cell's next block.
	p.w.RemoveBlockEntity(x, y, z)
	*changes = append(*changes, physicsChange(x, y, z, state, leave))
	p.redstoneAfterRemoval(x, y, z, state, false, false, changes)
	p.neighborUpdate(x, y, z, state, leave, changes)
	// vanilla: Level.setBlock — the old state's updateIndirectNeighbourShapes
	// runs before the neighbour shape pass; only redstone wire overrides it,
	// for the wires a step above or below its connected sides.
	p.wireSlopeChanges(x, y, z, state, changes)
	p.shapeUpdateNeighbors(x, y, z, changes)
	e := destroyBlockEffect(state, tag, x, y, z)
	if !event {
		e.LevelEvent, e.LevelEventData, e.GameEvent = 0, 0, registry.GameEventNone
	}
	p.effects = append(p.effects, e)
}

// removeWithDropResetFlags keeps the flag restore's defer off ordinary destruction.
func (p *Physics) removeWithDropResetFlags(x, y, z int32, state uint16, changes *[]BlockChange, event bool) {
	// destroyBlock starts a flag-3 write, independent of its caller's flags.
	skip, silent := p.skipWireShape, p.silentShapes
	p.skipWireShape, p.silentShapes = false, false
	defer func() { p.skipWireShape, p.silentShapes = skip, silent }()
	p.removeWithDrop(x, y, z, state, changes, event)
}

// destroyBlockEffect is destroyBlock(pos, true)'s break sound and particles
// (level event 2001, or 2014 for a straw bed; the old state as data) and loot.
// vanilla: Level.destroyBlock
func destroyBlockEffect(state uint16, tag nbt.Compound, x, y, z int32) Effect {
	event := 2001
	if registry.BlockOf(state) == registry.BlockStrawBed {
		// vanilla: StrawBedBlock.spawnDestroyParticles — particles only.
		event = 2014
	}
	// The destroyed state still controls whether the vibration is dampened.
	return Effect{LevelEvent: event, LevelEventData: int32(state), BreakDrop: state, BreakTag: tag,
		GameEvent: gameEventBlockDestroy, GameEventAffected: state, X: x, Y: y, Z: z}
}

// bellShapeUpdate is BellBlock.updateShape's attachment half: a double wall
// losing a side turns single facing away from it, and a single wall whose free
// side gains a sturdy face turns double. Popping is supportCheck's.
// vanilla: BellBlock.updateShape
func (p *Physics) bellShapeUpdate(x, y, z int32, d [3]int32, state uint16, changes *[]BlockChange) {
	// vanilla: directionToNeighbour points from the bell toward the changed
	// block; the walk delta points the other way.
	toward := blocklogic.Opposite(directionOf(d))
	facing := dirIndex(registry.StateValue(state, registry.PropFacing))
	if toward != facing && toward != blocklogic.Opposite(facing) {
		return
	}
	neighbour := p.w.BlockAt(x+dirDelta[toward][0], y+dirDelta[toward][1], z+dirDelta[toward][2])
	var next uint16
	switch registry.StateValue(state, registry.PropAttachment) {
	case "double_wall":
		// Vanilla tests the neighbour's far face, toward the direction it lies
		// in, not the face toward the bell.
		if registry.FaceSturdy(neighbour, toward, registry.SupportFull) {
			return
		}
		next, _ = registry.WithProps(state,
			registry.PropValue{P: registry.PropAttachment, V: "single_wall"},
			registry.PropValue{P: registry.PropFacing, V: blocklogic.FaceName(blocklogic.Opposite(toward))})
	case "single_wall":
		if toward != blocklogic.Opposite(facing) || !registry.FaceSturdy(neighbour, facing, registry.SupportFull) {
			return
		}
		next, _ = registry.WithProp(state, registry.PropAttachment, "double_wall")
	default:
		return
	}
	p.setClientShaped(x, y, z, next, changes)
}
