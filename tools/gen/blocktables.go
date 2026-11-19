package main

// Block types whose class extends FallingBlock or schedules its own tick to hand
// the block to a FallingBlockEntity: the gravity blocks. Checked against the
// server jar: SandBlock, ColoredFallingBlock, ConcretePowderBlock, AnvilBlock and
// DragonEggBlock extend FallingBlock; BrushableBlock implements Fallable and falls
// on the same isFree test; ScaffoldingBlock falls a column's bottom once its
// distance runs out of support, the closest match for the engine's "nothing below
// holds it" trigger.
var fallingTypes = map[string]bool{
	"minecraft:sand":            true,
	"minecraft:colored_falling": true,
	"minecraft:concrete_powder": true,
	"minecraft:anvil":           true,
	"minecraft:dragon_egg":      true,
	"minecraft:brushable":       true,
	"minecraft:scaffolding":     true,
}

// Block types whose definition carries the block their kind turns into in
// water: the concrete powders, whose ConcretePowderBlock holds the concrete
// of the same colour the field names. The falling entity reads the pair off
// this table rather than off a name suffix, so a block whose powder and
// concrete spellings ever drift apart stays right.
var solidifyTypes = map[string]bool{
	"minecraft:concrete_powder": true,
}

// Block types whose class extends IronBarsBlock: iron bars, glass panes, stained
// glass panes and copper bars. They share the cross-collision properties and
// IronBarsBlock.attachsTo (connects to other IronBarsBlocks, walls and sturdy
// faces, not fences). Waxed copper bars use iron_bars, not weathering_copper_bar:
// waxing stops weathering, so the behaviour class swaps.
var barsTypes = map[string]bool{
	"minecraft:iron_bars":             true,
	"minecraft:stained_glass_pane":    true,
	"minecraft:weathering_copper_bar": true,
}

// The level events a falling block's landing plays, by definition type
// (AnvilBlock.onLand 1031, AnvilBlock.onBrokenAfterFall 1029,
// BrushableBlock.onBrokenAfterFall 2001, the speleothems' 1045 and 1052), read off
// the server jar. Every other gravity block lands silently on the server: the
// thud is the client's, on the block update.
var fallingLandEvents = map[string]int{
	"minecraft:anvil": 1031,
}

var fallingBrokenEvents = map[string]int{
	"minecraft:anvil":             1029,
	"minecraft:brushable":         2001,
	"minecraft:pointed_dripstone": 1045,
	"minecraft:sulfur_spike":      1052,
}

// blockGroup is one of the class families the block rules key on: the types and
// names it takes in, and the names it leaves out.
type blockGroupSpec struct {
	name              string
	types             []string
	members, excludes []string
}

// blockGroups lists the families in the order their flags are written.
var blockGroupSpecs = []blockGroupSpec{
	{"Door", []string{"minecraft:door", "minecraft:weathering_copper_door"}, []string{}, []string{}},
	{"Trapdoor", []string{"minecraft:trapdoor", "minecraft:weathering_copper_trapdoor"}, []string{}, []string{}},
	{"Bed", []string{"minecraft:bed"}, []string{}, []string{}},
	{"Slab", []string{"minecraft:slab", "minecraft:weathering_copper_slab"}, []string{}, []string{}},
	{"Stairs", []string{"minecraft:stair", "minecraft:weathering_copper_stair"}, []string{}, []string{}},
	{"Wall", []string{"minecraft:wall"}, []string{}, []string{}},
	{"Fence", []string{"minecraft:fence"}, []string{}, []string{}},
	{"FenceGate", []string{"minecraft:fence_gate"}, []string{}, []string{}},
	{"Leaves", []string{"minecraft:mangrove_leaves", "minecraft:tinted_particle_leaves", "minecraft:untinted_particle_leaves"}, []string{}, []string{}},
	{"WallTorch", []string{"minecraft:redstone_wall_torch", "minecraft:wall_torch"}, []string{}, []string{"minecraft:wall_torch"}},
	{"PressurePlate", []string{"minecraft:pressure_plate", "minecraft:weighted_pressure_plate"}, []string{}, []string{}},
	{"RailVariant", []string{"minecraft:detector_rail", "minecraft:powered_rail"}, []string{}, []string{}},
	{"Carpet", []string{"minecraft:carpet", "minecraft:mossy_carpet", "minecraft:wool_carpet"}, []string{}, []string{}},
	{"Button", []string{"minecraft:button"}, []string{}, []string{}},
	{"CoralFan", []string{"minecraft:base_coral_fan", "minecraft:coral_fan"}, []string{}, []string{}},
	{"CoralWallFan", []string{"minecraft:base_coral_wall_fan", "minecraft:coral_wall_fan"}, []string{}, []string{}},
	{"FlowerPot", []string{"minecraft:flower_pot"}, []string{}, []string{}},
	{"Sign", []string{"minecraft:ceiling_hanging_sign", "minecraft:standing_sign", "minecraft:wall_hanging_sign", "minecraft:wall_sign"}, []string{}, []string{}},
	{"WallSign", []string{"minecraft:wall_sign"}, []string{}, []string{}},
	{"HangingSign", []string{"minecraft:ceiling_hanging_sign", "minecraft:wall_hanging_sign"}, []string{}, []string{}},
	{"Banner", []string{"minecraft:banner", "minecraft:wall_banner"}, []string{}, []string{}},
	{"WallBanner", []string{"minecraft:wall_banner"}, []string{}, []string{}},
	{"Chest", []string{"minecraft:chest", "minecraft:copper_chest", "minecraft:ender_chest", "minecraft:trapped_chest", "minecraft:weathering_copper_chest"}, []string{}, []string{}},
	{"Shelf", []string{"minecraft:shelf"}, []string{}, []string{}},
	{"Head", []string{"minecraft:piglinwallskull", "minecraft:piston_head", "minecraft:player_head", "minecraft:player_wall_head", "minecraft:skull", "minecraft:wall_skull", "minecraft:wither_skull", "minecraft:wither_wall_skull"}, []string{}, []string{}},
	{"Lantern", []string{"minecraft:jack_o_lantern", "minecraft:lantern", "minecraft:weathering_lantern"}, []string{"minecraft:sea_lantern"}, []string{"minecraft:carved_pumpkin"}},
	{"Cauldron", []string{"minecraft:cauldron", "minecraft:lava_cauldron", "minecraft:layered_cauldron"}, []string{}, []string{}},
	{"Cake", []string{"minecraft:cake", "minecraft:candle_cake"}, []string{}, []string{}},
	{"Chain", []string{"minecraft:chain", "minecraft:weathering_copper_chain"}, []string{}, []string{}},
	{"CopperGolemStatue", []string{"minecraft:copper_golem_statue", "minecraft:weathering_copper_golem_statue"}, []string{}, []string{}},
	{"Bars", []string{"minecraft:iron_bars", "minecraft:stained_glass_pane", "minecraft:weathering_copper_bar"}, []string{}, []string{}},
	{"StainedGlass", []string{"minecraft:stained_glass"}, []string{}, []string{}},
	{"GrateVariant", []string{"minecraft:waterlogged_transparent", "minecraft:weathering_copper_grate"}, []string{}, []string{"minecraft:copper_grate"}},
	{"LightningRod", []string{"minecraft:lightning_rod", "minecraft:weathering_lightning_rod"}, []string{}, []string{}},
}
