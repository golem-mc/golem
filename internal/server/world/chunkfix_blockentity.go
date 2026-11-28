package world

import (
	"strings"

	"github.com/golem-mc/golem/nbt"
)

// Data versions of the block entity fixes of 1.20.5 to 1.21.9.
const (
	blockPosFormatVersion    = 3813 // vanilla: DataFixers.addFixers (BlockPosFormatAndRenamesFix)
	beehiveRenameVersion     = 3818 // vanilla: DataFixers.addFixers (BeehiveFieldRenameFix)
	bannerPatternVersion     = 3818 // vanilla: DataFixers.addFixers (BannerPatternFormatFix, v3818_1)
	playerHeadProfileVersion = 3820 // vanilla: DataFixers.addFixers (PlayerHeadBlockProfileFix)
	jukeboxSongTicksVersion  = 3945 // vanilla: DataFixers.addFixers (JukeboxTicksSinceSongStartedFix)
	lockPredicateVersion     = 4068 // vanilla: DataFixers.addFixers (ContainerBlockEntityLockPredicateFix)
	bedBlockEntityVersion    = 4885 // vanilla: DataFixers.addFixers (RemoveBlockEntityTagFix)
)

// bannerPatternIDs are the short banner pattern codes and their ids.
// vanilla: BannerPatternFormatFix.PATTERN_ID_MAP
var bannerPatternIDs = map[string]string{
	"b": "minecraft:base", "bl": "minecraft:square_bottom_left", "br": "minecraft:square_bottom_right",
	"tl": "minecraft:square_top_left", "tr": "minecraft:square_top_right", "bs": "minecraft:stripe_bottom",
	"ts": "minecraft:stripe_top", "ls": "minecraft:stripe_left", "rs": "minecraft:stripe_right",
	"cs": "minecraft:stripe_center", "ms": "minecraft:stripe_middle", "drs": "minecraft:stripe_downright",
	"dls": "minecraft:stripe_downleft", "ss": "minecraft:small_stripes", "cr": "minecraft:cross",
	"sc": "minecraft:straight_cross", "bt": "minecraft:triangle_bottom", "tt": "minecraft:triangle_top",
	"bts": "minecraft:triangles_bottom", "tts": "minecraft:triangles_top", "ld": "minecraft:diagonal_left",
	"rd": "minecraft:diagonal_up_right", "lud": "minecraft:diagonal_up_left", "rud": "minecraft:diagonal_right",
	"mc": "minecraft:circle", "mr": "minecraft:rhombus", "vh": "minecraft:half_vertical",
	"hh": "minecraft:half_horizontal", "vhr": "minecraft:half_vertical_right", "hhb": "minecraft:half_horizontal_bottom",
	"bo": "minecraft:border", "cbo": "minecraft:curly_border", "gra": "minecraft:gradient",
	"gru": "minecraft:gradient_up", "bri": "minecraft:bricks", "glb": "minecraft:globe",
	"cre": "minecraft:creeper", "sku": "minecraft:skull", "flo": "minecraft:flower",
	"moj": "minecraft:mojang", "pig": "minecraft:piglin",
}

// fixBlockPosField turns a {X,Y,Z} compound under from into an int array under
// to; a value that is not such a compound moves as it is.
// vanilla: BlockPosFormatAndRenamesFix.fixFields, ExtraDataFixUtils.fixBlockPos
func fixBlockPosField(tag nbt.Compound, from, to string) {
	value, ok := takeKey(tag, from)
	if !ok {
		return
	}
	if pos, ok := nbt.CompoundView(value); ok {
		x, okX := nbt.IntValue(pos["X"])
		y, okY := nbt.IntValue(pos["Y"])
		z, okZ := nbt.IntValue(pos["Z"])
		if okX && okY && okZ {
			value = nbt.IntArray{x, y, z}
		}
	}
	tag[to] = value
}

// fixBeehive renames a beehive's bees and their fields.
// vanilla: BeehiveFieldRenameFix
func fixBeehive(tag nbt.Compound) {
	renameField(tag, "Bees", "bees")
	bees, _ := nbt.Get[nbt.List](tag, "bees")
	for _, item := range bees.Items {
		if bee, ok := nbt.CompoundView(item); ok {
			renameField(bee, "EntityData", "entity_data")
			renameField(bee, "TicksInHive", "ticks_in_hive")
			renameField(bee, "MinOccupationTicks", "min_ticks_in_hive")
		}
	}
}

// upgradeBeeEntities upgrades the entity each of a hive's bees holds.
// vanilla: V1460 and V3818 type bees[].entity_data as ENTITY
func upgradeBeeEntities(tag nbt.Compound, version int32) {
	bees, _ := nbt.Get[nbt.List](tag, "bees")
	for _, item := range bees.Items {
		if bee, ok := nbt.CompoundView(item); ok {
			upgradeEmbeddedEntity(bee["entity_data"], "", version)
		}
	}
}

// fixBannerPatterns renames a banner's layers to the id and colour name form.
// vanilla: BannerPatternFormatFix.fixTag
func fixBannerPatterns(tag nbt.Compound) {
	renameField(tag, "Patterns", "patterns")
	patterns, ok := nbt.Get[nbt.List](tag, "patterns")
	if !ok {
		return
	}
	for _, item := range patterns.Items {
		layer, ok := nbt.CompoundView(item)
		if !ok {
			continue
		}
		renameField(layer, "Pattern", "pattern")
		if code, ok := layer["pattern"].(nbt.String); ok {
			if id, ok := bannerPatternIDs[string(code)]; ok {
				layer["pattern"] = nbt.String(id)
			}
		}
		color, _ := nbt.IntValue(layer["Color"])
		delete(layer, "Color")
		layer["color"] = nbt.String(dyeColorName(color))
	}
}

// dyeColorName is the dye name of a colour number; a number out of range is
// white.
// vanilla: ExtraDataFixUtils.dyeColorIdToName
func dyeColorName(id int32) string {
	if id < 0 || int(id) >= len(dyeNames) {
		return dyeNames[0]
	}
	return dyeNames[id]
}

// fixSkullProfile turns a head's SkullOwner (or ExtraType) into a profile.
// vanilla: PlayerHeadBlockProfileFix
func fixSkullProfile(tag nbt.Compound) {
	owner, ok := tag["SkullOwner"]
	if !ok {
		owner, ok = tag["ExtraType"]
	}
	if !ok {
		return
	}
	delete(tag, "SkullOwner")
	delete(tag, "ExtraType")
	tag["profile"] = skullProfile(owner)
}

// fixJukeboxTicks replaces a jukebox's start tick bookkeeping with the ticks
// since its song started.
// vanilla: JukeboxTicksSinceSongStartedFix
func fixJukeboxTicks(tag nbt.Compound) {
	count, _ := nbt.LongValue(tag["TickCount"])
	start, _ := nbt.LongValue(tag["RecordStartTick"])
	for _, key := range []string{"IsPlaying", "TickCount", "RecordStartTick"} {
		delete(tag, key)
	}
	if count-start > 0 {
		tag["ticks_since_song_started"] = nbt.Long(count - start)
	}
}

// fixContainerLock turns a Lock string into an item predicate on the custom
// name; an empty or non-string lock is dropped.
// vanilla: ContainerBlockEntityLockPredicateFix, LockComponentPredicateFix.fixLock
func fixContainerLock(tag nbt.Compound) {
	value, ok := takeKey(tag, "Lock")
	if !ok {
		return
	}
	name, _ := value.(nbt.String)
	if name == "" {
		return
	}
	tag["lock"] = lockPredicate(string(name))
}

// lockPredicate is the item predicate matching a stack named name.
// vanilla: LockComponentPredicateFix.fixLock
func lockPredicate(name string) nbt.Compound {
	quoted := strings.NewReplacer(`"`, `\"`, `\`, `\\`).Replace(name)
	return nbt.Compound{"components": nbt.Compound{"minecraft:custom_name": nbt.String(`"` + quoted + `"`)}}
}

// fixBannerOverride moves an ominous banner's custom name into its item name
// component and gives it the uncommon rarity.
// vanilla: BannerEntityCustomNameToOverrideComponentFix, OminousBannerRarityFix
func fixBannerOverride(tag nbt.Compound, version int32) {
	if version < customNameOverrideVersion {
		if key, ok := jsonTranslationKey(tag["CustomName"]); ok && key == ominousBannerKey {
			tag["components"] = nbt.Compound{"minecraft:item_name": tag["CustomName"], "minecraft:hide_additional_tooltip": nbt.Compound{}}
			delete(tag, "CustomName")
		}
	}
	if version < ominousBannerRarityVersion {
		if c, ok := tag["components"].(nbt.CompoundTag); ok {
			fixOminousBannerRarity(c)
			if version < textComponentVersion {
				unflattenText(c.Entries(), "minecraft:item_name")
			}
		}
	}
}
