package registry

// Block group predicates: "is this block one of the X". Each group has one
// predicate here, so a caller need not know where a group comes from.
//
// Vanilla decides a group with a tag (captured by tools/gen registries)
// or an instanceof (stood in for by definition types in tools/gen blocks);
// which one is a fact about vanilla, settled once here. Where an instanceof
// lists the same blocks as a tag, prefer the tag: it follows a version bump
// on its own, while a hand-listed type set silently misses a new type.
// IsFenceGate and IsLeaves are that case; registry_test.go pins both.
//
// IsBars cannot be a tag: minecraft:bars holds only iron and copper bars
// (IronBarsBlock uses it for face culling), while attachsTo tests
// instanceof IronBarsBlock, which every glass pane satisfies, and the panes
// are in no tag.
//
// To add a group, add a tag constant and a predicate here. inBlockTag is
// unexported so tag names cannot spread to call sites again.
const (
	// TagFences is BlockTags.FENCES, tested by FenceBlock.isSameFence.
	TagFences = "minecraft:fences"
	// TagWoodenFences is BlockTags.WOODEN_FENCES, which isSameFence
	// requires two connected fences to agree on.
	TagWoodenFences = "minecraft:wooden_fences"
	// TagWalls is BlockTags.WALLS, tested by WallBlock.connectsTo and by
	// IronBarsBlock.attachsTo.
	TagWalls = "minecraft:walls"
	// TagWallPostOverride: blocks above a wall that raise its post even
	// where the wall runs straight. vanilla: WallBlock.shouldRaisePost
	TagWallPostOverride = "minecraft:wall_post_override"
	// TagShulkerBoxes is BlockTags.SHULKER_BOXES, the last term of
	// Block.isExceptionForConnection.
	TagShulkerBoxes = "minecraft:shulker_boxes"
	// TagFenceGates stands in for instanceof FenceGateBlock, listing the
	// same twelve blocks. vanilla: FenceBlock.connectsTo, WallBlock.connectsTo
	TagFenceGates = "minecraft:fence_gates"
	// TagLeaves stands in for "instanceof LeavesBlock", the first term of
	// Block.isExceptionForConnection; the tag lists the same eleven blocks.
	TagLeaves = "minecraft:leaves"
	// TagStairs stands in for instanceof StairBlock, listing the same
	// blocks. vanilla: StairBlock.isStairs
	TagStairs = "minecraft:stairs"
	// TagReplaceable is BlockTags.REPLACEABLE, the blocks a placement
	// overwrites instead of being rejected by.
	TagReplaceable = "minecraft:replaceable"
	// TagPreventMobSpawningInside: the rails, no mob spawns standing in
	// one. vanilla: NaturalSpawner.isValidEmptySpawnBlock
	TagPreventMobSpawningInside = "minecraft:prevent_mob_spawning_inside"
	// TagFire is BlockTags.FIRE, one third of NodeEvaluator.isBurningBlock,
	// the danger a mob may not be spawned into.
	TagFire = "minecraft:fire"
	// TagSoulFireBase is BlockTags.SOUL_FIRE_BASE_BLOCKS, the blocks a fire
	// lit or spreading above stands as the soul fire on.
	TagSoulFireBase = "minecraft:soul_fire_base_blocks"
	// TagInfiniburnOverworld is the infiniburn tag a dimension type names,
	// the nether's included: the blocks fire on never burns out on.
	TagInfiniburnOverworld = "minecraft:infiniburn_overworld"
	// TagInfiniburnNether is minecraft:infiniburn_nether, which holds the
	// overworld tag and nothing more.
	TagInfiniburnNether = "minecraft:infiniburn_nether"
	// TagInfiniburnEnd is minecraft:infiniburn_end, which adds the bedrock
	// to the overworld tag.
	TagInfiniburnEnd = "minecraft:infiniburn_end"
	// TagPreventsNearbyLeafDecay: the trunks and stems leaves measure
	// their distance from, as distance zero.
	TagPreventsNearbyLeafDecay = "minecraft:prevents_nearby_leaf_decay"
	// TagEnchantmentPowerProvider: the blocks an enchanting table draws
	// power from; only the bookshelf in this version.
	TagEnchantmentPowerProvider = "minecraft:enchantment_power_provider"
	// TagEnchantmentPowerTransmitter: what a table's power passes through
	// to a bookshelf (air and short plants).
	TagEnchantmentPowerTransmitter = "minecraft:enchantment_power_transmitter"
	// TagFallDamageResetting: the blocks that reset a fall (climbables, web,
	// sweet berry bush). vanilla: Entity.move
	TagFallDamageResetting = "minecraft:fall_damage_resetting"
)

// Resolved once so the predicates below index a bitset instead of hashing a tag name.
var (
	idTagEnchantmentPowerProvider    = BlockTagOf(TagEnchantmentPowerProvider)
	idTagEnchantmentPowerTransmitter = BlockTagOf(TagEnchantmentPowerTransmitter)
	idTagFallDamageResetting         = BlockTagOf(TagFallDamageResetting)
	idTagFenceGates                  = BlockTagOf(TagFenceGates)
	idTagFences                      = BlockTagOf(TagFences)
	idTagFire                        = BlockTagOf(TagFire)
	idTagInfiniburnEnd               = BlockTagOf(TagInfiniburnEnd)
	idTagInfiniburnNether            = BlockTagOf(TagInfiniburnNether)
	idTagInfiniburnOverworld         = BlockTagOf(TagInfiniburnOverworld)
	idTagLeaves                      = BlockTagOf(TagLeaves)
	idTagPreventMobSpawningInside    = BlockTagOf(TagPreventMobSpawningInside)
	idTagPreventsNearbyLeafDecay     = BlockTagOf(TagPreventsNearbyLeafDecay)
	idTagReplaceable                 = BlockTagOf(TagReplaceable)
	idTagShulkerBoxes                = BlockTagOf(TagShulkerBoxes)
	idTagSoulFireBase                = BlockTagOf(TagSoulFireBase)
	idTagStairs                      = BlockTagOf(TagStairs)
	idTagWallPostOverride            = BlockTagOf(TagWallPostOverride)
	idTagWalls                       = BlockTagOf(TagWalls)
	idTagWoodenFences                = BlockTagOf(TagWoodenFences)
)

// inBlockTag reports whether a block is in a vanilla block tag. An unknown
// tag has no members rather than panicking: the tags come from one version's
// wire dump. vanilla: BlockState.is(TagKey)
func inBlockTag(name, tag string) bool {
	return NameInTag(name, BlockTagOf(tag))
}

// IsFence reports whether a block is in BlockTags.FENCES, whose
// north/south/east/west properties follow the four horizontal neighbours.
// vanilla: FenceBlock.isSameFence, FenceBlock.updateShape
func (b Block) IsFence() bool {
	return b.InTag(idTagFences)
}

// IsWoodenFence reports whether a fence is in BlockTags.WOODEN_FENCES; two
// fences connect only when they agree on it, which keeps wooden fences off
// the nether brick fence. vanilla: FenceBlock.isSameFence
func (b Block) IsWoodenFence() bool {
	return b.InTag(idTagWoodenFences)
}

// IsFenceGate reports whether a block is a fence gate, whose facing decides
// that fences connect to its two perpendicular sides.
// vanilla: FenceGateBlock.connectsToDirection
func (b Block) IsFenceGate() bool {
	return b.InTag(idTagFenceGates)
}

// IsWall reports whether a block is in BlockTags.WALLS; walls and fences
// connect to each other (see ConnectsToFence).
// vanilla: WallBlock.connectsTo, IronBarsBlock.attachsTo
func (b Block) IsWall() bool {
	return b.InTag(idTagWalls)
}

// IsWallPostOverride reports whether a block above a wall forces its post
// up (torches, signs, banners, pressure plates, tripwire, cactus flower).
// vanilla: WallBlock.shouldRaisePost
func (b Block) IsWallPostOverride() bool {
	return b.InTag(idTagWallPostOverride)
}

// IsBars reports whether a block is an IronBarsBlock subclass: iron, copper
// bars and every glass pane. Bars connect to other bars, walls and sturdy
// faces, not fences or gates. Read from BarsBlocks, not a tag; see the top
// of this file. vanilla: IronBarsBlock.attachsTo
func (b Block) IsBars() bool {
	return b < BlockCount && barsBlocks[b]
}

// IsCrossCollision reports whether a block is a CrossCollisionBlock (fence
// or bars), with neighbour-driven north/south/east/west properties. Fences
// are read by class, not the fences tag, which a data pack may extend to
// blocks without those properties.
// vanilla: CrossCollisionBlock
func (b Block) IsCrossCollision() bool {
	return b.Type() == BlockTypeFence || b.IsBars()
}

// IsLeaves reports whether a block is a leaf block, which is excluded from
// fence, wall and bars connections despite its full cube.
// vanilla: Block.isExceptionForConnection
func (b Block) IsLeaves() bool {
	return b.InTag(idTagLeaves)
}

// IsStairs reports whether a block is a stair, which a neighbour stair's
// corner shape reads. vanilla: StairBlock.isStairs
func (b Block) IsStairs() bool {
	return b.InTag(idTagStairs)
}

// IsDoublePlant reports whether a block is a DoublePlantBlock (tall flowers
// and grass, large fern, pitcher, small dripleaf, tall seagrass). No tag
// or definition type collects them, but the lower/upper half property does:
// only doors share it, and only doors hinge.
func (b Block) IsDoublePlant() bool {
	return b < BlockCount && doublePlantBlock[b]
}

var doublePlantBlock = BlocksWhere(isDoublePlantName)

func isDoublePlantName(name string) bool {
	states, ok := AllStates[name]
	if !ok {
		return false
	}
	props := ParseStateProps(states[0].Props)
	half := props["half"]
	_, hinged := props["hinge"]
	return (half == "lower" || half == "upper") && !hinged
}

// IsShulkerBox reports whether a block is a shulker box, the last term of
// Block.isExceptionForConnection. A name suffix would miss the undyed
// minecraft:shulker_box, which the tag holds.
func (b Block) IsShulkerBox() bool {
	return b.InTag(idTagShulkerBoxes)
}

// IsReplaceable reports whether placing a block may overwrite this one, per
// minecraft:replaceable. Flowers, saplings and crops are not in it despite
// having no collision, so placing into one is rejected.
func (b Block) IsReplaceable() bool {
	return b.InTag(idTagReplaceable)
}

// IsReplaceable is Block.IsReplaceable for a caller that only holds a registry name.
//
// Deprecated: resolve the name once with BlockByName and use Block.IsReplaceable.
func IsReplaceable(name string) bool {
	return NameInTag(name, idTagReplaceable)
}

// IsPreventMobSpawningInside reports whether no mob may spawn standing in a
// block (the rails). vanilla: NaturalSpawner.isValidEmptySpawnBlock
func (b Block) IsPreventMobSpawningInside() bool {
	return b.InTag(idTagPreventMobSpawningInside)
}

// IsPreventMobSpawningInside is Block.IsPreventMobSpawningInside for a caller that only holds a registry name.
func IsPreventMobSpawningInside(name string) bool {
	return NameInTag(name, idTagPreventMobSpawningInside)
}

// CanFallThrough reports whether a falling block falls through this block:
// air, fire, water, lava or replaceable blocks. A waterlogged solid holds it.
// vanilla: FallingBlock.isFree
func (b Block) CanFallThrough() bool {
	return b == BlockAir || b == BlockWater || b == BlockLava || b.InTag(idTagFire) || b.IsReplaceable()
}

// CanFallThrough is Block.CanFallThrough for a state, refusing invalid IDs.
func CanFallThrough(state uint16) bool {
	return int(state) < StateCount && BlockOf(state).CanFallThrough()
}

// IsBurningBlock reports whether a mob burns in this state: fire, lava,
// magma, a lava cauldron or a lit campfire.
// vanilla: NodeEvaluator.isBurningBlock
func IsBurningBlock(state uint16) bool {
	b := BlockOf(state)
	return b.InTag(idTagFire) || b == BlockLava || b == BlockMagmaBlock || b == BlockLavaCauldron ||
		IsLitCampfire(state)
}

// IsLitCampfire reports whether the state is a lit campfire or soul campfire.
// vanilla: CampfireBlock.isLitCampfire
func IsLitCampfire(state uint16) bool {
	return BlockOf(state).InTag(TagIDCampfires) && StateBool(state, PropLit)
}

// IsSoulFireBase reports whether fire on a block is soul fire (soul sand and
// soil). vanilla: SoulFireBlock.canSurviveOnBlock
func (b Block) IsSoulFireBase() bool {
	return b.InTag(idTagSoulFireBase)
}

// IsInfiniburn reports whether fire on a block burns forever in the
// dimension: netherrack and magma everywhere, bedrock in the end too. An
// unknown dimension plays by the overworld's rules. vanilla: FireBlock.tick
func (b Block) IsInfiniburn(dimension string) bool {
	switch dimension {
	case "minecraft:the_nether":
		return b.InTag(idTagInfiniburnNether)
	case "minecraft:the_end":
		return b.InTag(idTagInfiniburnEnd)
	default:
		return b.InTag(idTagInfiniburnOverworld)
	}
}

// IsPreventsNearbyLeafDecay reports whether a block is distance zero to
// the leaves beside it (logs, wood, stems). vanilla: LeavesBlock.updateDistance
func (b Block) IsPreventsNearbyLeafDecay() bool {
	return b.InTag(idTagPreventsNearbyLeafDecay)
}

// IsEnchantmentPowerProvider reports whether a block powers an enchanting
// table (the bookshelf). vanilla: EnchantingTableBlock.isValidBookShelf
func (b Block) IsEnchantmentPowerProvider() bool {
	return b.InTag(idTagEnchantmentPowerProvider)
}

// IsEnchantmentPowerTransmitter reports whether a table's power passes
// through a block halfway to a bookshelf (air, short plants).
// vanilla: EnchantingTableBlock.isValidBookShelf
func (b Block) IsEnchantmentPowerTransmitter() bool {
	return b.InTag(idTagEnchantmentPowerTransmitter)
}

// IsFallDamageResetting reports whether a block name is in
// BlockTags.FALL_DAMAGE_RESETTING, the blocks a step's fall sweep stops on.
func (b Block) IsFallDamageResetting() bool {
	return b.InTag(idTagFallDamageResetting)
}

// BlockInTag reports whether a block name is in the block tag named, which
// is what a block predicate's tag form tests against. It stays for parsing
// command and datapack text, not for game logic.
func BlockInTag(name, tag string) bool {
	return inBlockTag(name, tag)
}

// BlockTagKnown reports whether the server holds and sends the named block
// tag; "logs" and "minecraft:logs" ask the same.
func BlockTagKnown(tag string) bool {
	return CurrentTagLookup().blockTagKnown(tag)
}

func (lookup *TagLookup) blockTagKnown(tag string) bool {
	_, ok := lookup.BlockTagMembers(Qualify(tag))
	return ok
}
