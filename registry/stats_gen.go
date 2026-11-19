// Code generated from the statistic registries of a vanilla 26.3 server. DO NOT EDIT.

package registry

// StatTypes lists every statistic type, in the order that gives each its
// numeric ID in the minecraft:stat_type registry, the category ID the Award
// Statistics packet carries. The registry is built into the client and never
// sent, so these IDs are only valid for the exact version above.
var StatTypes = []string{
	"minecraft:mined",
	"minecraft:crafted",
	"minecraft:used",
	"minecraft:broken",
	"minecraft:picked_up",
	"minecraft:dropped",
	"minecraft:killed",
	"minecraft:killed_by",
	"minecraft:custom",
}

// StatType is a statistic type's ID in minecraft:stat_type.
type StatType int32

const (
	StatTypeMined    StatType = 0
	StatTypeCrafted  StatType = 1
	StatTypeUsed     StatType = 2
	StatTypeBroken   StatType = 3
	StatTypePickedUp StatType = 4
	StatTypeDropped  StatType = 5
	StatTypeKilled   StatType = 6
	StatTypeKilledBy StatType = 7
	StatTypeCustom   StatType = 8
)

// CustomStats lists every entry of the minecraft:custom_stat registry, the
// keys of the minecraft:custom statistic type, in ID order.
var CustomStats = []string{
	"minecraft:leave_game",
	"minecraft:play_time",
	"minecraft:total_world_time",
	"minecraft:time_since_death",
	"minecraft:time_since_rest",
	"minecraft:sneak_time",
	"minecraft:walk_one_cm",
	"minecraft:crouch_one_cm",
	"minecraft:sprint_one_cm",
	"minecraft:walk_on_water_one_cm",
	"minecraft:fall_one_cm",
	"minecraft:climb_one_cm",
	"minecraft:fly_one_cm",
	"minecraft:walk_under_water_one_cm",
	"minecraft:minecart_one_cm",
	"minecraft:boat_one_cm",
	"minecraft:pig_one_cm",
	"minecraft:happy_ghast_one_cm",
	"minecraft:horse_one_cm",
	"minecraft:aviate_one_cm",
	"minecraft:swim_one_cm",
	"minecraft:strider_one_cm",
	"minecraft:nautilus_one_cm",
	"minecraft:jump",
	"minecraft:drop",
	"minecraft:damage_dealt",
	"minecraft:damage_dealt_absorbed",
	"minecraft:damage_dealt_resisted",
	"minecraft:damage_taken",
	"minecraft:damage_blocked_by_shield",
	"minecraft:damage_absorbed",
	"minecraft:damage_resisted",
	"minecraft:deaths",
	"minecraft:mob_kills",
	"minecraft:animals_bred",
	"minecraft:player_kills",
	"minecraft:fish_caught",
	"minecraft:talked_to_villager",
	"minecraft:traded_with_villager",
	"minecraft:eat_cake_slice",
	"minecraft:fill_cauldron",
	"minecraft:use_cauldron",
	"minecraft:clean_armor",
	"minecraft:clean_banner",
	"minecraft:clean_shulker_box",
	"minecraft:interact_with_brewingstand",
	"minecraft:interact_with_beacon",
	"minecraft:inspect_dropper",
	"minecraft:inspect_hopper",
	"minecraft:inspect_dispenser",
	"minecraft:play_noteblock",
	"minecraft:tune_noteblock",
	"minecraft:pot_flower",
	"minecraft:trigger_trapped_chest",
	"minecraft:open_enderchest",
	"minecraft:enchant_item",
	"minecraft:play_record",
	"minecraft:interact_with_furnace",
	"minecraft:interact_with_crafting_table",
	"minecraft:open_chest",
	"minecraft:sleep_in_bed",
	"minecraft:sleep_in_straw_bed",
	"minecraft:open_shulker_box",
	"minecraft:open_barrel",
	"minecraft:interact_with_blast_furnace",
	"minecraft:interact_with_smoker",
	"minecraft:interact_with_lectern",
	"minecraft:interact_with_campfire",
	"minecraft:interact_with_cartography_table",
	"minecraft:interact_with_loom",
	"minecraft:interact_with_stonecutter",
	"minecraft:bell_ring",
	"minecraft:raid_trigger",
	"minecraft:raid_win",
	"minecraft:interact_with_anvil",
	"minecraft:interact_with_grindstone",
	"minecraft:target_hit",
	"minecraft:interact_with_smithing_table",
}

// CustomStat is a minecraft:custom statistic's key, its ID in
// minecraft:custom_stat.
type CustomStat int32

const (
	CustomStatLeaveGame                    CustomStat = 0
	CustomStatPlayTime                     CustomStat = 1
	CustomStatTotalWorldTime               CustomStat = 2
	CustomStatTimeSinceDeath               CustomStat = 3
	CustomStatTimeSinceRest                CustomStat = 4
	CustomStatSneakTime                    CustomStat = 5
	CustomStatWalkOneCm                    CustomStat = 6
	CustomStatCrouchOneCm                  CustomStat = 7
	CustomStatSprintOneCm                  CustomStat = 8
	CustomStatWalkOnWaterOneCm             CustomStat = 9
	CustomStatFallOneCm                    CustomStat = 10
	CustomStatClimbOneCm                   CustomStat = 11
	CustomStatFlyOneCm                     CustomStat = 12
	CustomStatWalkUnderWaterOneCm          CustomStat = 13
	CustomStatMinecartOneCm                CustomStat = 14
	CustomStatBoatOneCm                    CustomStat = 15
	CustomStatPigOneCm                     CustomStat = 16
	CustomStatHappyGhastOneCm              CustomStat = 17
	CustomStatHorseOneCm                   CustomStat = 18
	CustomStatAviateOneCm                  CustomStat = 19
	CustomStatSwimOneCm                    CustomStat = 20
	CustomStatStriderOneCm                 CustomStat = 21
	CustomStatNautilusOneCm                CustomStat = 22
	CustomStatJump                         CustomStat = 23
	CustomStatDrop                         CustomStat = 24
	CustomStatDamageDealt                  CustomStat = 25
	CustomStatDamageDealtAbsorbed          CustomStat = 26
	CustomStatDamageDealtResisted          CustomStat = 27
	CustomStatDamageTaken                  CustomStat = 28
	CustomStatDamageBlockedByShield        CustomStat = 29
	CustomStatDamageAbsorbed               CustomStat = 30
	CustomStatDamageResisted               CustomStat = 31
	CustomStatDeaths                       CustomStat = 32
	CustomStatMobKills                     CustomStat = 33
	CustomStatAnimalsBred                  CustomStat = 34
	CustomStatPlayerKills                  CustomStat = 35
	CustomStatFishCaught                   CustomStat = 36
	CustomStatTalkedToVillager             CustomStat = 37
	CustomStatTradedWithVillager           CustomStat = 38
	CustomStatEatCakeSlice                 CustomStat = 39
	CustomStatFillCauldron                 CustomStat = 40
	CustomStatUseCauldron                  CustomStat = 41
	CustomStatCleanArmor                   CustomStat = 42
	CustomStatCleanBanner                  CustomStat = 43
	CustomStatCleanShulkerBox              CustomStat = 44
	CustomStatInteractWithBrewingstand     CustomStat = 45
	CustomStatInteractWithBeacon           CustomStat = 46
	CustomStatInspectDropper               CustomStat = 47
	CustomStatInspectHopper                CustomStat = 48
	CustomStatInspectDispenser             CustomStat = 49
	CustomStatPlayNoteblock                CustomStat = 50
	CustomStatTuneNoteblock                CustomStat = 51
	CustomStatPotFlower                    CustomStat = 52
	CustomStatTriggerTrappedChest          CustomStat = 53
	CustomStatOpenEnderchest               CustomStat = 54
	CustomStatEnchantItem                  CustomStat = 55
	CustomStatPlayRecord                   CustomStat = 56
	CustomStatInteractWithFurnace          CustomStat = 57
	CustomStatInteractWithCraftingTable    CustomStat = 58
	CustomStatOpenChest                    CustomStat = 59
	CustomStatSleepInBed                   CustomStat = 60
	CustomStatSleepInStrawBed              CustomStat = 61
	CustomStatOpenShulkerBox               CustomStat = 62
	CustomStatOpenBarrel                   CustomStat = 63
	CustomStatInteractWithBlastFurnace     CustomStat = 64
	CustomStatInteractWithSmoker           CustomStat = 65
	CustomStatInteractWithLectern          CustomStat = 66
	CustomStatInteractWithCampfire         CustomStat = 67
	CustomStatInteractWithCartographyTable CustomStat = 68
	CustomStatInteractWithLoom             CustomStat = 69
	CustomStatInteractWithStonecutter      CustomStat = 70
	CustomStatBellRing                     CustomStat = 71
	CustomStatRaidTrigger                  CustomStat = 72
	CustomStatRaidWin                      CustomStat = 73
	CustomStatInteractWithAnvil            CustomStat = 74
	CustomStatInteractWithGrindstone       CustomStat = 75
	CustomStatTargetHit                    CustomStat = 76
	CustomStatInteractWithSmithingTable    CustomStat = 77
)
