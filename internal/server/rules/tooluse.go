package rules

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// Level events a tool's right-click plays. vanilla: LevelEvent
const (
	LevelEventExtinguishFire = 1009
	LevelEventWaxOn          = 3003
	LevelEventWaxOff         = 3004
	LevelEventScrape         = 3005
)

// SoundWaxOn plays beside the wax-on level event, which is particles only.
// vanilla: HoneycombItem.useOn
const SoundWaxOn = registry.SoundItemHoneycombWaxOn

// ToolUse is what an item's right-click does to a block: the axe's, the
// shovel's and the honeycomb's useOn. The tool takes one wear for it, or
// one of the item is spent when Consumes.
type ToolUse struct {
	State    uint16 // the block's new state
	Consumes bool
	Sound    registry.Sound // played to everyone but the user; empty for none
	// LevelEvent is played at the block (and at a double chest's other
	// half, when HasPartner), to everyone but the user; zero for none.
	LevelEvent int32
	// PartnerDX, PartnerDZ locate a double chest's other half.
	PartnerDX, PartnerDZ int32
	HasPartner           bool
}

// AxeConversion is what an axe turns a block state into, trying a strip, a
// scrape of one oxidation stage and a wax removal in that order. A strip
// keeps only the axis; the copper conversions keep every property.
// vanilla: AxeItem.evaluateNewBlockState
func AxeConversion(state uint16) (ToolUse, bool) {
	block := registry.BlockOf(state)
	var use ToolUse
	var ok bool
	if stripped, found := block.Stripped(); found {
		use.State, ok = registry.WithProps(stripped.DefaultState(), registry.PropValue{P: registry.PropAxis, V: registry.StateValue(state, registry.PropAxis)})
		use.Sound = registry.SoundItemAxeStrip
	} else if less, found := block.Scraped(); found {
		use.State, ok = registry.WithPropertiesOf(less, state)
		use.Sound, use.LevelEvent = registry.SoundItemAxeScrape, LevelEventScrape
	} else if bare, found := block.Unwaxed(); found {
		use.State, ok = registry.WithPropertiesOf(bare, state)
		use.Sound, use.LevelEvent = registry.SoundItemAxeWaxOff, LevelEventWaxOff
	}
	if !ok {
		return ToolUse{}, false
	}
	if use.LevelEvent != 0 && registry.BlockOf(state).InTag(registry.TagIDCopperChests) {
		use.PartnerDX, use.PartnerDZ, use.HasPartner = world.ChestConnectedOffset(state)
	}
	return use, true
}

// TransformHeldBackByShield reports whether a main-hand axe, hoe or shovel
// click is the player raising the off-hand shield instead of transforming
// the block, which a sneak overrides.
// vanilla: BlockTransformer.playerHasBlockingItemUseIntent
func TransformHeldBackByShield(hand int32, offHand protocol.ItemStack, sneaking bool) bool {
	if hand != protocol.HandMain || offHand.Empty() || sneaking {
		return false
	}
	_, blocks := registry.StackBlocksAttacks(offHand)
	return blocks
}

// ShovelConversion is what a shovel's click on a face of (x, y, z) does: a
// path flattened out of ground with air above it, unless heldBack (see
// TransformHeldBackByShield) or the face is the bottom. A lit campfire is put
// out by the campfire's own use instead. vanilla: ShovelItem.useOn
func ShovelConversion(w BlockReader, x, y, z, face int32, heldBack bool) (ToolUse, bool) {
	if face == protocol.FaceBottom {
		return ToolUse{}, false
	}
	cur := w.BlockAt(x, y, z)
	if flat, ok := registry.BlockOf(cur).Flattened(); ok && !heldBack {
		if above := w.BlockAt(x, y+1, z); world.IsKnownAirState(above) {
			return ToolUse{State: flat.DefaultState(), Sound: registry.SoundItemShovelFlatten}, true
		}
	}
	return ToolUse{}, false
}

// MudConversion is the mud a water bottle turns a convertible_to_mud block
// into; a click on the bottom face converts nothing. vanilla: PotionItem.useOn
func MudConversion(state uint16, face int32) (uint16, bool) {
	if face == protocol.FaceBottom {
		return 0, false
	}
	if !registry.BlockOf(state).InTag(registry.TagIDConvertibleToMud) {
		return 0, false
	}
	return registry.BlockMud.DefaultState(), true
}

// WaterDowseCells is the cells a water splash douses at the block its flight
// met: the block one step out along the face the potion broke against, the
// hit block itself, and that first cell's four horizontal neighbours.
// vanilla: AbstractThrownPotion.onHitBlock
func WaterDowseCells(x, y, z, face int32) [6][3]int32 {
	off := protocol.FaceOffset[face]
	ex, ey, ez := x+off[0], y+off[1], z+off[2]
	return [6][3]int32{
		{ex, ey, ez},
		{x, y, z},
		{ex + 1, ey, ez}, {ex - 1, ey, ez},
		{ex, ey, ez + 1}, {ex, ey, ez - 1},
	}
}

// DowseFire is what a water splash leaves of a block: fire becomes air
// (state 0); a lit candle or campfire is unlit (the campfire with level
// event 1009); candle reports a candle, whose extinguish sound the caller
// plays. false for blocks water ignores.
// vanilla: AbstractThrownPotion.dowseFire
func DowseFire(state uint16) (newState uint16, levelEvent int32, candle, ok bool) {
	if b := registry.BlockOf(state); b == registry.BlockFire || b == registry.BlockSoulFire {
		return 0, 0, false, true
	}
	if registry.BlockOf(state).InTag(registry.TagIDCampfires) {
		if !registry.StateLit(state) {
			return 0, 0, false, false
		}
		st, found := registry.WithBool(state, registry.PropLit, false)
		return st, LevelEventExtinguishFire, false, found
	}
	// A candle or a candle cake: CandleOut's unlit logic, the cursorY past
	// a half taking the cake branch water douses whether or not it is
	// clicked on its upper half.
	if st, out := CandleOut(state, 1); out {
		return st, 0, true, true
	}
	return 0, 0, false, false
}

// horizontalSteps is the x, z step along each horizontal facing: the side a
// carved pumpkin's seeds are thrown out of, the cell a bed's facing points at
// from its foot.
var horizontalSteps = map[string][2]int32{"north": {0, -1}, "south": {0, 1}, "west": {-1, 0}, "east": {1, 0}}

// PumpkinCarve is what shears make of a pumpkin clicked on face: a carved
// pumpkin facing the clicked side, or the player for a top or bottom click,
// and the horizontal step its seeds are thrown along. vanilla:
// PumpkinBlock.useItemOn
func PumpkinCarve(face int32, yaw float32) (state uint16, dx, dz int32) {
	facing := facingFromPlayer(yaw)
	if face != protocol.FaceTop && face != protocol.FaceBottom {
		off := protocol.FaceOffset[face]
		for name, s := range horizontalSteps {
			if s == [2]int32{off[0], off[2]} {
				facing = name
			}
		}
	}
	state, _ = registry.WithProp(registry.BlockCarvedPumpkin.DefaultState(), registry.PropFacing, facing)
	return state, horizontalSteps[facing][0], horizontalSteps[facing][1]
}

// HoneycombWax is what a honeycomb makes of a copper block: its waxed form
// with every property kept, the wax-on particles and sound for everyone but
// the user, and one honeycomb spent.
// vanilla: HoneycombItem.useOn
func HoneycombWax(state uint16) (ToolUse, bool) {
	waxed, ok := registry.BlockOf(state).Waxed()
	if !ok {
		return ToolUse{}, false
	}
	use := ToolUse{Sound: SoundWaxOn, LevelEvent: LevelEventWaxOn, Consumes: true}
	if use.State, ok = registry.WithPropertiesOf(waxed, state); !ok {
		return ToolUse{}, false
	}
	if registry.BlockOf(state).InTag(registry.TagIDCopperChests) {
		use.PartnerDX, use.PartnerDZ, use.HasPartner = world.ChestConnectedOffset(state)
	}
	return use, true
}

// FlintLight is the lit state of an unlit, dry campfire, candle or candle
// cake, which flint and steel lights instead of starting a fire. vanilla:
// FlintAndSteelItem.useOn, CampfireBlock.canLight, CandleBlock.canLight,
// CandleCakeBlock.canLight
func FlintLight(state uint16) (uint16, bool) {
	if registry.StateValue(state, registry.PropLit) != "false" {
		return 0, false
	}
	switch {
	case registry.BlockOf(state).InTag(registry.TagIDCampfires), registry.BlockOf(state).InTag(registry.TagIDCandles):
		if registry.StateValue(state, registry.PropWaterlogged) != "false" {
			return 0, false
		}
	case registry.BlockOf(state).InTag(registry.TagIDCandleCakes):
	default:
		return 0, false
	}
	return registry.WithBool(state, registry.PropLit, true)
}

// BerryPick is what picking a ripe berry block leaves: the loot table the
// berries roll, the block left behind, the pick sound, and the loot
// context the roll runs against.
type BerryPick struct {
	Table string
	After uint16
	Sound registry.Sound
	Loot  LootContext
}

// BerryHarvest is a ripe sweet berry bush or a glow-berried cave vine
// picked by hand; a bone meal in hand is left to grow a bush short of full
// instead. vanilla: SweetBerryBushBlock.useItemOn and useWithoutItem,
// CaveVines.use
func BerryHarvest(state uint16, boneMeal bool) (BerryPick, bool) {
	var pick BerryPick
	var ok bool
	// The roll reads the state as the pick found it — the bush's own
	// table pays one more berry for age 3.
	pick.Loot = LootContext{BlockState: state, HasBlock: true}
	age := registry.StateValue(state, registry.PropAge)
	switch {
	case registry.BlockOf(state) == registry.BlockSweetBerryBush && (age == "2" || age == "3"):
		if boneMeal && age != "3" {
			return BerryPick{}, false
		}
		pick.After, ok = registry.WithInt(state, registry.PropAge, 1)
		pick.Table, pick.Sound = "minecraft:harvest/sweet_berry_bush", registry.SoundBlockSweetBerryBushPickBerries
	case (registry.BlockOf(state) == registry.BlockCaveVines || registry.BlockOf(state) == registry.BlockCaveVinesPlant) && registry.StateBool(state, registry.PropBerries):
		pick.After, ok = registry.WithBool(state, registry.PropBerries, false)
		pick.Table, pick.Sound = "minecraft:harvest/cave_vine", registry.SoundBlockCaveVinesPickBerries
	default:
		return BerryPick{}, false
	}
	if !ok {
		return BerryPick{}, false
	}
	return pick, true
}

// ShearsTrim stops a growing plant's head (kelp, weeping and twisting
// vines, cave vines) by setting it to the last age. vanilla:
// ShearsItem.useOn
func ShearsTrim(state uint16) (ToolUse, bool) {
	switch registry.BlockOf(state) {
	case registry.BlockKelp, registry.BlockWeepingVines, registry.BlockTwistingVines, registry.BlockCaveVines:
	default:
		return ToolUse{}, false
	}
	if registry.StateValue(state, registry.PropAge) == "25" {
		return ToolUse{}, false
	}
	use := ToolUse{Sound: registry.SoundBlockGrowingPlantCrop}
	var ok bool
	use.State, ok = registry.WithInt(state, registry.PropAge, 25)
	return use, ok
}
