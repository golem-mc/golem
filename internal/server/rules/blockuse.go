package rules

import (
	"math/rand"
	"strconv"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/blocklogic"
	"github.com/golem-mc/golem/registry"
)

// The decisions of blocks that answer an item, or a bare hand, on their own:
// cakes and candles, flower pots, composters and cauldrons. The server spends
// the items and plays the sounds.

// LevelEventComposterFill is played when an item goes into a composter, with
// data 1 when the level rose. vanilla: LevelEvent.COMPOSTER_FILL
const LevelEventComposterFill = 1500

// CakeSlice is what a bite of cake feeds: FoodData.eat(2, 0.1), whose
// saturation is nutrition × modifier × 2. vanilla: CakeBlock.eat
var CakeSlice = registry.Food{Nutrition: 2, Saturation: 0.4}

// CakeBite is a cake after one bite: one more bite taken, or air once the
// seventh is gone. A candle cake is eaten as a fresh cake. vanilla:
// CakeBlock.eat, CandleCakeBlock.useWithoutItem
func CakeBite(state uint16) (uint16, bool) {
	bites := "0"
	switch {
	case registry.BlockOf(state) == registry.BlockCake:
		bites = registry.StateValue(state, registry.PropBites)
	case registry.BlockOf(state).InTag(registry.TagIDCandleCakes):
	default:
		return 0, false
	}
	if bites == "6" {
		return airState, true
	}
	n, err := strconv.Atoi(bites)
	if err != nil {
		return 0, false
	}
	return registry.WithInt(registry.BlockCake.DefaultState(), registry.PropBites, n+1)
}

// CandleOnCake is the candle cake an untouched cake becomes when a candle
// is put on it. vanilla: CakeBlock.useItemOn
func CandleOnCake(state uint16, item registry.Item) (uint16, bool) {
	if state != registry.BlockCake.DefaultState() {
		return 0, false
	}
	block, ok := item.Block()
	if !ok || candleCake[block] == 0 {
		return 0, false
	}
	return (candleCake[block] - 1).DefaultState(), true
}

// candleCake maps each candle block to its candle cake block plus one, so
// zero means none.
var candleCake = func() *[registry.BlockCount]registry.Block {
	t := new([registry.BlockCount]registry.Block)
	for b := range registry.Block(registry.BlockCount) {
		if !b.InTag(registry.TagIDCandles) {
			continue
		}
		if cake, ok := registry.BlockByName(b.Name() + "_cake"); ok {
			t[b] = cake + 1
		}
	}
	return t
}()

// CandleOut is a lit candle, or a lit candle cake clicked on its upper half,
// put out by an empty hand. vanilla: CandleBlock.useItemOn,
// CandleCakeBlock.useItemOn
func CandleOut(state uint16, cursorY float32) (uint16, bool) {
	block := registry.BlockOf(state)
	if !registry.StateBool(state, registry.PropLit) ||
		(!block.InTag(registry.TagIDCandles) && !(block.InTag(registry.TagIDCandleCakes) && cursorY > 0.5)) {
		return 0, false
	}
	return registry.WithBool(state, registry.PropLit, false)
}

// The tables store the target plus one so zero means no entry.
var (
	pottedBy   = new([registry.ItemCount]registry.Block)
	potContent = new([registry.BlockCount]registry.Item)
)

func init() {
	for item, block := range registry.FlowerPotted {
		i, ok1 := registry.ItemByName(item)
		b, ok2 := registry.BlockByName(block)
		if ok1 && ok2 {
			pottedBy[i] = b + 1
		}
	}
	for block, item := range registry.FlowerPotContent {
		b, ok1 := registry.BlockByName(block)
		i, ok2 := registry.ItemByName(item)
		if ok1 && ok2 {
			potContent[b] = i + 1
		}
	}
}

// FlowerPotUse pots or unpots a plant; ItemAir means empty hand or no hand-back.
// vanilla: FlowerPotBlock.useItemOn, useWithoutItem
func FlowerPotUse(state uint16, item registry.Item, mainHand bool) (after uint16, taken bool, handBack registry.Item, used bool) {
	block := registry.BlockOf(state)
	content := potContent[block]
	if block != registry.BlockFlowerPot && content == 0 {
		return 0, false, registry.ItemAir, false
	}
	if item >= 0 && item < registry.ItemCount && pottedBy[item] != 0 {
		if content != 0 {
			return state, false, registry.ItemAir, true
		}
		return (pottedBy[item] - 1).DefaultState(), true, registry.ItemAir, true
	}
	if !mainHand {
		return 0, false, registry.ItemAir, false
	}
	if content == 0 {
		return state, false, registry.ItemAir, true
	}
	return registry.BlockFlowerPot.DefaultState(), false, content - 1, true
}

// ComposterInsert consumes a compostable below level 7 and evaluates its
// layers in the block interaction context. Level 7 spends only the click.
// vanilla: ComposterBlock.useItemOn, addLayer
func ComposterInsert(state uint16, stack protocol.ItemStack, ctx *LootContext, rng *rand.Rand) (after uint16, inserted, used bool) {
	layers, compostable := registry.StackCompostable(stack)
	if registry.BlockOf(state) != registry.BlockComposter || !compostable {
		return 0, false, false
	}
	level, _ := registry.StateInt(state, registry.PropLevel)
	switch level {
	case 8:
		return 0, false, false
	case 7:
		return state, false, true
	}
	// Each layer reads the current state, including within a villager's batch.
	context := *ctx
	context.BlockState, context.HasBlock = state, true
	roller := lootRoller{ctx: &context, rng: rng}
	if n := roller.numInt(layers); n > 0 {
		// Java adds as int before clamping, including overflow.
		next := max(int32(0), min(int32(7), int32(level)+n))
		after, _ = registry.WithInt(state, registry.PropLevel, int(next))
		return after, true, true
	}
	return state, true, true
}

// ComposterReady reports whether a composter holds bone meal to take out.
func ComposterReady(state uint16) bool {
	return registry.BlockOf(state) == registry.BlockComposter && registry.StateValue(state, registry.PropLevel) == "8"
}

// CauldronBucket is the result of pouring or filling a bucket.
// vanilla: CauldronInteractions
type CauldronBucket struct {
	State  uint16        // the cauldron's new state
	Bucket registry.Item // the item the bucket becomes
	Sound  registry.Sound
}

// CauldronBucketUse answers a bucket; refused spends the click, ok false passes it.
func CauldronBucketUse(state uint16, item registry.Item, waterAbove bool) (use CauldronBucket, refused, ok bool) {
	block := registry.BlockOf(state)
	if !block.InGroup(registry.GroupCauldron) {
		return CauldronBucket{}, false, false
	}
	level, _ := registry.StateInt(state, registry.PropLevel)
	full := func(b registry.Block) uint16 {
		state, _ := registry.WithInt(b.DefaultState(), registry.PropLevel, 3)
		return state
	}
	switch item {
	case registry.ItemWaterBucket:
		return CauldronBucket{full(registry.BlockWaterCauldron), registry.ItemBucket, registry.SoundItemBucketEmpty}, false, true
	case registry.ItemLavaBucket, registry.ItemPowderSnowBucket:
		if waterAbove {
			return CauldronBucket{}, true, true
		}
		if item == registry.ItemLavaBucket {
			return CauldronBucket{registry.BlockLavaCauldron.DefaultState(), registry.ItemBucket, registry.SoundItemBucketEmptyLava}, false, true
		}
		return CauldronBucket{full(registry.BlockPowderSnowCauldron), registry.ItemBucket, registry.SoundItemBucketEmptyPowderSnow}, false, true
	case registry.ItemBucket:
		empty := registry.BlockCauldron.DefaultState()
		switch {
		case block == registry.BlockWaterCauldron && level == 3:
			return CauldronBucket{empty, registry.ItemWaterBucket, registry.SoundItemBucketFill}, false, true
		case block == registry.BlockLavaCauldron:
			return CauldronBucket{empty, registry.ItemLavaBucket, registry.SoundItemBucketFillLava}, false, true
		case block == registry.BlockPowderSnowCauldron && level == 3:
			return CauldronBucket{empty, registry.ItemPowderSnowBucket, registry.SoundItemBucketFillPowderSnow}, false, true
		}
	}
	return CauldronBucket{}, false, false
}

// BellHit reports whether a click rings the bell: only a side face its
// attachment exposes (the skirt's two faces on a floor bell, the wall's two
// ends on a wall bell, any side on a ceiling bell), below the skirt's mouth.
// face is the packet's face number, also the ring event's direction value.
// vanilla: BellBlock.isProperHit
func BellHit(state uint16, face int32, clickY float32) bool {
	if registry.BlockOf(state) != registry.BlockBell || face < protocol.FaceBottom || face > protocol.FaceEast {
		return false
	}
	if face == protocol.FaceBottom || face == protocol.FaceTop || clickY > 0.8124 {
		// 0.8124 is vanilla's own constant, the skirt top just under 13/16.
		return false
	}
	zFace := face == protocol.FaceNorth || face == protocol.FaceSouth
	facing := registry.StateValue(state, registry.PropFacing)
	zFacing := facing == "north" || facing == "south"
	switch registry.StateValue(state, registry.PropAttachment) {
	case "floor":
		return zFacing == zFace
	case "single_wall", "double_wall":
		return zFacing != zFace
	case "ceiling":
		return true
	}
	return false
}

// ContainerHitSlot is the front cell a click on a shelf hit, numbered across
// then down from 0; false for any other face. facing is the shelf's; vanilla
// flips the hit axis for north and east faces.
// vanilla: SelectableSlotContainer.getHitSlot, ChiseledBookShelfBlock.getHitSlot
func ContainerHitSlot(facing string, rows, columns int, ctx PlacementContext) (int, bool) {
	if blocklogic.FaceName(int(ctx.Face)) != facing {
		return 0, false
	}
	// The default is the south case: the columns run west to east.
	// vanilla: SelectableSlotContainer.getRelativeHitCoordinatesForBlockFace
	across := ctx.CursorX
	switch facing {
	case "north":
		across = 1 - ctx.CursorX
	case "west":
		across = ctx.CursorZ
	case "east":
		across = 1 - ctx.CursorZ
	}
	// The rows count from the top; the columns count along the axis the
	// front lies across.
	return hitSection(across, columns) + hitSection(1-ctx.CursorY, rows)*columns, true
}

// hitSection is vanilla's getSection: which of maxSections even slices
// along an axis a coordinate falls into, clamped rather than read past the
// edge for a cursor outside the block.
func hitSection(coordinate float32, maxSections int) int {
	pixel := coordinate * 16
	section := int(pixel / (16 / float32(maxSections)))
	return min(max(section, 0), maxSections-1)
}

// CauldronItem is a bottle, a dyed item, a banner or a shulker box used on
// a cauldron.
type CauldronItem struct {
	State  uint16             // the cauldron's new state
	Result protocol.ItemStack // what the hand gets in place of one of the item
	// InPlace makes Result the whole held stack, the dye washed out of it,
	// rather than one item's replacement.
	InPlace bool
	// LimitCreative is createFilledResult's flag: a creative player keeps
	// the hand, and is given the result only when no slot already holds it.
	LimitCreative bool
	Sound         registry.Sound      // empty for the interactions that play none
	Stat          registry.CustomStat // the custom statistic the use counts
}

// lowerCauldron is LayeredCauldronBlock.lowerFillLevel on a water cauldron.
func lowerCauldron(level int) uint16 {
	if level <= 1 {
		return registry.BlockCauldron.DefaultState()
	}
	state, _ := registry.WithInt(registry.BlockWaterCauldron.DefaultState(), registry.PropLevel, level-1)
	return state
}

// CauldronItemUse answers a non-bucket stack on a cauldron; ok false passes the click.
// vanilla: CauldronInteractions.bootStrap
func CauldronItemUse(state uint16, held protocol.ItemStack) (use CauldronItem, ok bool) {
	item := registry.Item(held.ItemID)
	contents, hasContents := held.PotionContentsOf()
	// PotionContents.is(WATER) excludes custom effects; splash potions do not fill.
	waterBottle := item == registry.ItemPotion && hasContents && PotionIsWater(contents)
	glass := protocol.ItemStack{ItemID: int32(registry.ItemGlassBottle), Count: 1}
	switch registry.BlockOf(state) {
	case registry.BlockCauldron:
		if !waterBottle {
			return CauldronItem{}, false
		}
		return CauldronItem{
			State: registry.BlockWaterCauldron.DefaultState(), Result: glass,
			LimitCreative: true, Sound: registry.SoundItemBottleEmpty, Stat: registry.CustomStatUseCauldron,
		}, true
	case registry.BlockWaterCauldron:
	default:
		return CauldronItem{}, false
	}
	level, _ := registry.StateInt(state, registry.PropLevel)
	switch {
	case item == registry.ItemGlassBottle:
		water := protocol.ItemStack{ItemID: int32(registry.ItemPotion), Count: 1}
		water.SetPotion(waterPotion)
		return CauldronItem{State: lowerCauldron(level), Result: water, LimitCreative: true, Sound: registry.SoundItemBottleFill, Stat: registry.CustomStatUseCauldron}, true
	case waterBottle:
		if level >= 3 {
			return CauldronItem{}, false
		}
		after, _ := registry.WithInt(state, registry.PropLevel, level+1)
		return CauldronItem{
			State: after, Result: glass,
			LimitCreative: true, Sound: registry.SoundItemBottleEmpty, Stat: registry.CustomStatUseCauldron,
		}, true
	case registry.Item(held.ItemID).InTag(registry.ItemTagIDCauldronCanRemoveDye):
		if !held.HasDyedColor() {
			return CauldronItem{}, false
		}
		washed := held
		washed.RemoveDyedColor()
		return CauldronItem{State: lowerCauldron(level), Result: washed, InPlace: true, Stat: registry.CustomStatCleanArmor}, true
	case isBannerItem(item):
		cleaned := held
		cleaned.Count = 1
		if !cleaned.RemoveLastBannerPattern() {
			return CauldronItem{}, false
		}
		return CauldronItem{State: lowerCauldron(level), Result: cleaned, Stat: registry.CustomStatCleanBanner}, true
	case item.IsShulkerBox() && item != registry.ItemShulkerBox:
		plain := held
		plain.Count = 1
		plain.ItemID = int32(registry.ItemShulkerBox)
		return CauldronItem{State: lowerCauldron(level), Result: plain, Stat: registry.CustomStatCleanShulkerBox}, true
	}
	return CauldronItem{}, false
}

func isBannerItem(item registry.Item) bool {
	switch item {
	case registry.ItemWhiteBanner, registry.ItemOrangeBanner, registry.ItemMagentaBanner, registry.ItemLightBlueBanner,
		registry.ItemYellowBanner, registry.ItemLimeBanner, registry.ItemPinkBanner, registry.ItemGrayBanner,
		registry.ItemLightGrayBanner, registry.ItemCyanBanner, registry.ItemPurpleBanner, registry.ItemBlueBanner,
		registry.ItemBrownBanner, registry.ItemGreenBanner, registry.ItemRedBanner, registry.ItemBlackBanner:
		return true
	}
	return false
}
