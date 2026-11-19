package registry

// poiTypeOfBlock is shared by the world section index and the village rules.
// vanilla: PoiTypes.bootstrap
var poiTypeOfBlock = func() [BlockCount]string {
	var types [BlockCount]string
	for block, typ := range map[Block]string{
		BlockBlastFurnace: "armorer", BlockSmoker: "butcher", BlockCartographyTable: "cartographer",
		BlockBrewingStand: "cleric", BlockComposter: "farmer", BlockBarrel: "fisherman",
		BlockFletchingTable: "fletcher", BlockCauldron: "leatherworker", BlockLavaCauldron: "leatherworker",
		BlockWaterCauldron: "leatherworker", BlockPowderSnowCauldron: "leatherworker",
		BlockLectern: "librarian", BlockStonecutter: "mason", BlockLoom: "shepherd",
		BlockSmithingTable: "toolsmith", BlockGrindstone: "weaponsmith", BlockBell: "meeting",
		BlockBeehive: "beehive", BlockBeeNest: "bee_nest", BlockNetherPortal: "nether_portal",
		BlockLodestone: "lodestone", BlockTestInstanceBlock: "test_instance",
	} {
		types[block] = "minecraft:" + typ
	}
	for b := range Block(BlockCount) {
		// Blocks.BED excludes the straw bed.
		if b.InGroup(GroupBed) && b != BlockStrawBed {
			types[b] = "minecraft:home"
		}
		if b >= BlockLightningRod && b <= BlockWaxedOxidizedLightningRod {
			types[b] = "minecraft:lightning_rod"
		}
	}
	return types
}()

var poiStates = func() [StateCount]bool {
	var states [StateCount]bool
	for state := range uint16(StateCount) {
		typ := poiTypeOfBlock[BlockOf(state)]
		// StateValue's tables are populated later, in init functions.
		states[state] = typ != "" && (typ != "minecraft:home" || StateProps(state)["part"] == "head")
	}
	return states
}()

// IsPOI reports whether the block state is a point of interest.
// vanilla: PoiTypes.hasPoi
func IsPOI(state uint16) bool { return int(state) < len(poiStates) && poiStates[state] }

// POITypeOf is the registry id of a block state's point-of-interest type, ""
// for none.
// vanilla: PoiTypes.forState
func POITypeOf(state uint16) string {
	if !IsPOI(state) {
		return ""
	}
	return poiTypeOfBlock[BlockOf(state)]
}
