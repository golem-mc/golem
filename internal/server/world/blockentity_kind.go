package world

// BlockEntityKind is a modelled block entity type; KindNone is the zero value
// and any unmodelled type (a carried entry) has no kind.
type BlockEntityKind uint8

// KindNone is no modelled kind.
const KindNone BlockEntityKind = 0

// The modelled block entity kinds, under their chunk-format ids. Any other
// block entity in a chunk (trial spawners, beehives) stays the compound it was
// read from, saved back untouched. The ender chest's contents are per player
// (the player file stores them), so its entity is only the lid counter and a
// home for the block's name.
const (
	KindBrushable BlockEntityKind = iota + 1
	KindChest
	KindTrappedChest
	KindFurnace
	KindBlastFurnace
	KindSmoker
	KindBrewingStand
	KindCrafter
	KindBarrel
	KindShulkerBox
	KindEnderChest
	KindDispenser
	KindDropper
	KindHopper
	KindComparator
	KindPiston
	KindSpawner
	// KindCommandBlock is all three command blocks' entity: the command, its
	// last success (which the comparator reads) and its run state.
	// vanilla: CommandBlockEntity, BaseCommandBlock
	KindCommandBlock
	KindBell
	KindJukebox
	// KindLectern keeps the book in slot 0 and its open page. It is no
	// Container in vanilla, so ContainerSize is zero.
	// vanilla: LecternBlockEntity
	KindLectern
	// The daylight detector's entity is empty; the block's 20-tick power reread
	// ticks in it.
	// vanilla: DaylightDetectorBlockEntity
	KindDaylightDetector
	// KindBeacon keeps the pyramid level and chosen effects; the payment stays
	// with the window, the menu's own container.
	KindBeacon
	// KindSign and KindHangingSign keep both sides' text and the wax, and hold
	// no items. One sign block each.
	// vanilla: sign, hanging_sign
	KindSign
	KindHangingSign
	// Containers whose items render but no window shows: the decorated pot's
	// one slot and the shelf's three. The pot also keeps plain-chest loot
	// tables.
	KindDecoratedPot
	KindShelf
	// Kinds of carried or implicit-component block entities.
	KindBanner
	KindSkull
	KindBeehive
	KindVault
	KindTrialSpawner
	// KindCreakingHeart keeps only the linked creaking; awake, dormant and
	// uprooted are block state.
	KindCreakingHeart
	// KindChiseledBookshelf keeps one book per cell and opens no window: the
	// six occupied bits are block state, so it renders without sending items,
	// and the last cell moved through is what a comparator reads.
	// vanilla: ChiseledBookShelfBlockEntity
	KindChiseledBookshelf
	// KindConduit keeps only its beam's target; active state and frame are
	// recomputed from the world each refresh.
	KindConduit
	// KindPotentSulfur keeps only the geyser countdown; the state is block state.
	// vanilla: PotentSulfurBlockEntity
	KindPotentSulfur
	// KindCampfire is both campfires: four cooking slots, each with its
	// progress and total.
	// vanilla: CampfireBlockEntity
	KindCampfire
	// KindSculkSensor keeps the last vibration's frequency, which its
	// comparator reads while active, and its vibration listener.
	// vanilla: SculkSensorBlockEntity
	KindSculkSensor
	// KindCalibratedSculkSensor keeps the same frequency and listener.
	// vanilla: CalibratedSculkSensorBlockEntity
	KindCalibratedSculkSensor
	// KindSculkShrieker keeps the warning level its last shriek carried and
	// its vibration listener.
	// vanilla: SculkShriekerBlockEntity
	KindSculkShrieker
	// KindSculkCatalyst keeps the charge cursors of the catalyst's spreader.
	// vanilla: SculkCatalystBlockEntity
	KindSculkCatalyst
	// KindCopperGolemStatue keeps only the golem's name, in the components
	// compound.
	// vanilla: CopperGolemStatueBlockEntity
	KindCopperGolemStatue
	// KindEnchantingTable keeps only the table's name and leftover components.
	// vanilla: EnchantingTableBlockEntity
	KindEnchantingTable
)

var kindNames = [...]string{
	KindBrushable:             "minecraft:brushable_block",
	KindChest:                 "minecraft:chest",
	KindTrappedChest:          "minecraft:trapped_chest",
	KindFurnace:               "minecraft:furnace",
	KindBlastFurnace:          "minecraft:blast_furnace",
	KindSmoker:                "minecraft:smoker",
	KindBrewingStand:          "minecraft:brewing_stand",
	KindCrafter:               "minecraft:crafter",
	KindBarrel:                "minecraft:barrel",
	KindShulkerBox:            "minecraft:shulker_box",
	KindEnderChest:            "minecraft:ender_chest",
	KindDispenser:             "minecraft:dispenser",
	KindDropper:               "minecraft:dropper",
	KindHopper:                "minecraft:hopper",
	KindComparator:            "minecraft:comparator",
	KindPiston:                "minecraft:piston",
	KindSpawner:               "minecraft:mob_spawner",
	KindCommandBlock:          "minecraft:command_block",
	KindBell:                  "minecraft:bell",
	KindJukebox:               "minecraft:jukebox",
	KindLectern:               "minecraft:lectern",
	KindDaylightDetector:      "minecraft:daylight_detector",
	KindBeacon:                "minecraft:beacon",
	KindSign:                  "minecraft:sign",
	KindHangingSign:           "minecraft:hanging_sign",
	KindDecoratedPot:          "minecraft:decorated_pot",
	KindShelf:                 "minecraft:shelf",
	KindBanner:                "minecraft:banner",
	KindSkull:                 "minecraft:skull",
	KindBeehive:               "minecraft:beehive",
	KindVault:                 "minecraft:vault",
	KindTrialSpawner:          "minecraft:trial_spawner",
	KindCreakingHeart:         "minecraft:creaking_heart",
	KindChiseledBookshelf:     "minecraft:chiseled_bookshelf",
	KindConduit:               "minecraft:conduit",
	KindCopperGolemStatue:     "minecraft:copper_golem_statue",
	KindPotentSulfur:          "minecraft:potent_sulfur",
	KindCampfire:              "minecraft:campfire",
	KindSculkSensor:           "minecraft:sculk_sensor",
	KindCalibratedSculkSensor: "minecraft:calibrated_sculk_sensor",
	KindSculkShrieker:         "minecraft:sculk_shrieker",
	KindSculkCatalyst:         "minecraft:sculk_catalyst",
	KindEnchantingTable:       "minecraft:enchanting_table",
}

// Name is the kind's chunk-format id.
func (k BlockEntityKind) Name() string {
	if int(k) < len(kindNames) {
		return kindNames[k]
	}
	return ""
}

// String returns Name.
func (k BlockEntityKind) String() string { return k.Name() }

var kindByName = func() map[string]BlockEntityKind {
	m := make(map[string]BlockEntityKind, len(kindNames))
	for k, name := range kindNames {
		if name != "" {
			m[name] = BlockEntityKind(k)
		}
	}
	return m
}()

// BlockEntityKindByName is the modelled kind with the id, false for any other.
func BlockEntityKindByName(name string) (BlockEntityKind, bool) {
	k, ok := kindByName[name]
	return k, ok
}
