package world

import (
	"maps"
	"slices"
	"strings"

	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// Data versions of the fixes a 1.18 to 1.20.2 chunk gets.
const (
	structuresConfiguredVersion = 2970 // vanilla: DataFixers.addFixers (StructuresBecomeConfiguredFix)
	filteredSignsVersion        = 3097 // vanilla: DataFixers.addFixers (FilteredSignsFix)
	blendingDataVersion         = 3108 // vanilla: DataFixers.addFixers (BlendingDataRemoveFromNetherEndFix)
	chunkStatusVersion          = 3450 // vanilla: DataFixers.addFixers (RemapChunkStatusFix)
	mobEffectIDVersion          = 3568 // vanilla: DataFixers.addFixers (MobEffectIdFix)
	scuteRenameVersion          = 3800 // vanilla: DataFixers.addFixers (Rename scute item to turtle_scute)
	signDoubleSidedVersion      = 3439 // vanilla: DataFixers.addFixers (BlockEntitySignDoubleSidedEditableTextFix)
	signFilteredVersion         = 3564 // vanilla: DataFixers.addFixers (DropInvalidSignDataFix)
	brushableBlockVersion       = 3438 // vanilla: DataFixers.addFixers (Rename Suspicious Sand to Brushable Block)
	potSherdsVersion            = 3448 // vanilla: DataFixers.addFixers (DecoratedPotFieldRenameFix)
	shardRenameVersion          = 3447 // vanilla: DataFixers.addFixers (Pottery shard item renaming)
	grassRenameVersion          = 3692 // vanilla: DataFixers.addFixers (Rename grass block to short_grass)
	clickEventVersion           = 4292 // vanilla: DataFixers.addFixers (TextComponentHoverAndClickEventFix)
	signsOpFlagVersion          = 5002 // vanilla: DataFixers.addFixers (SignsOpFlagFix)
)

// oldChunkFixedVersion is the data version a chunk from before it claims once
// fixOldChunkRoot, fixLegacyPalette and fixOldBlockEntity have run over it:
// every chunk and block entity fix up to it is applied, so the claim keeps the
// game, and this server, from running a fix such as the sign one over its own
// output, or over a block entity created after the upgrade, again. Plain item
// stacks are already in storedItemVersion's shape, which the game's
// componentization leaves alone.
const oldChunkFixedVersion = signsOpFlagVersion

// storedItemVersion is the data version from which stored item stacks are
// componentized.
// vanilla: DataFixers.addFixers (ItemStackComponentizationFix)
const storedItemVersion = 3818

// isLegacyStack reports whether a compound is an item stack in the pre-1.20.5
// shape (id and Count).
func isLegacyStack(entries nbt.Compound) bool {
	if _, ok := entries["id"].(nbt.String); !ok {
		return false
	}
	_, legacy := entries["Count"]
	return legacy
}

// upgradeLegacyStacks rewrites every plain pre-1.20.5 stack under tag to the
// current shape: Count becomes an int count, a tag becomes components and item
// names renamed since version follow. It returns the tag to store in place of
// the one given: a stack that is an ordered compound is replaced by a plain
// one, as the fixes add and remove keys, which an ordered compound's key list
// would not follow.
// vanilla: ItemStackComponentizationFix, ItemRenameFix
func upgradeLegacyStacks(tag nbt.Tag, version int32) nbt.Tag {
	switch v := tag.(type) {
	case nbt.List:
		for i, item := range v.Items {
			v.Items[i] = upgradeLegacyStacks(item, version)
		}
	case nbt.CompoundTag:
		entries := v.Entries()
		if isLegacyStack(entries) {
			if _, ordered := v.(*nbt.OrderedCompound); ordered {
				entries = maps.Clone(entries)
				v = entries
			}
			count, _ := nbt.IntValue(entries["Count"])
			delete(entries, "Count")
			entries["count"] = nbt.Int(count)
			name, _ := nbt.Get[nbt.String](entries, "id")
			if tag, ok := nbt.CompoundView(entries["tag"]); ok {
				fixStackUUIDsAndNames(tag, string(name), version)
				fixStackEffects(tag, string(name), version)
			}
			if name == "minecraft:pig_spawn_egg" && version < spawnEggVersion {
				fixSpawnEgg(entries)
				name, _ = nbt.Get[nbt.String](entries, "id")
			}
			name = nbt.String(renameOldItem(string(name), version))
			entries["id"] = name
			componentizeTag(entries, string(name))
		}
		for key, child := range entries {
			entries[key] = upgradeLegacyStacks(child, version)
		}
		return v
	}
	return tag
}

// renameOldItem applies the item renames between version and
// storedItemVersion.
// vanilla: DataFixers.addFixers (ItemRenameFix: v3438, v3447, v3692, v3800)
func renameOldItem(name string, version int32) string {
	if version < brushableBlockVersion {
		if to, ok := potShardRenames[name]; ok {
			name = to
		}
	}
	if version < shardRenameVersion && strings.HasSuffix(name, "_pottery_shard") {
		name = strings.TrimSuffix(name, "_pottery_shard") + "_pottery_sherd"
	}
	return RenameOldItem(name, version)
}

// potShardRenames are the pottery shard item names of before 3447 and their
// current names.
// vanilla: DataFixers.addFixers (Pottery shard renaming)
var potShardRenames = map[string]string{
	"minecraft:pottery_shard_archer":  "minecraft:archer_pottery_shard",
	"minecraft:pottery_shard_prize":   "minecraft:prize_pottery_shard",
	"minecraft:pottery_shard_arms_up": "minecraft:arms_up_pottery_shard",
	"minecraft:pottery_shard_skull":   "minecraft:skull_pottery_shard",
}

// fixOldBlockEntity applies the block entity fixes between a chunk's data
// version and the one decodeChunkNBT reads.
//
// It reports false for a block entity the current version no longer has.
func fixOldBlockEntity(entry nbt.CompoundTag, version int32) bool {
	if version == 0 {
		return true
	}
	tag := entry.Entries()
	id, _ := nbt.Get[nbt.String](tag, "id")
	kind, _ := BlockEntityKindByName(string(id))
	if version < bedBlockEntityVersion && string(id) == "minecraft:bed" {
		// vanilla: RemoveBlockEntityTagFix
		return false
	}
	if version < signsOpFlagVersion && IsSignKind(kind) {
		tag["allow_op_features"] = nbt.Byte(1)
	}
	if version >= tooltipDisplayVersion {
		if version < dataVersion {
			fixStoredComponents(entry, version)
			if kind == KindBeehive {
				upgradeBeeEntities(tag, version)
			}
			if kind == KindDecoratedPot {
				unflattenPotSherds(tag, version)
			}
		}
		return true
	}
	if version < storedItemVersion {
		upgradeLegacyStacks(entry, version)
	}
	switch string(id) {
	case "minecraft:furnace", "minecraft:blast_furnace", "minecraft:smoker":
		if version < furnaceRecipesVersion {
			fixFurnaceRecipes(tag)
		}
	case "minecraft:mob_spawner":
		if version < spawnerDataVersion {
			fixSpawnerData(tag, version)
		}
	case "minecraft:conduit", "minecraft:skull":
		if version < blockEntityUUIDFixed {
			fixBlockEntityUUIDs(string(id), tag)
		}
	}
	fixStoredComponents(entry, version)
	fixBlockEntityFields(string(id), kind, tag, version)
	switch {
	case string(id) == suspiciousSandID && version < brushableBlockVersion:
		// vanilla: BlockEntityRenameFix, EntityBrushableBlockFieldsRenameFix
		tag["id"] = nbt.String(KindBrushable.Name())
		renameField(tag, "loot_table", "LootTable")
		renameField(tag, "loot_table_seed", "LootTableSeed")
	case IsSignKind(kind):
		if version < filteredSignsVersion {
			for _, key := range signTextFields[4:8] {
				delete(tag, key)
			}
		}
		fixSignText(tag, version)
	case kind == KindDecoratedPot:
		fixPotSherds(tag, version)
		unflattenPotSherds(tag, version)
	case kind == KindBeacon && version < mobEffectIDVersion:
		fixBeaconEffect(tag, "Primary", "primary_effect")
		fixBeaconEffect(tag, "Secondary", "secondary_effect")
	}
	if version < textComponentVersion {
		unflattenBlockEntityText(tag)
	}
	if version < clickEventVersion && IsSignKind(kind) {
		fixSignClickEvents(tag)
	}
	return true
}

// fixBlockEntityFields applies the block entity renames and reshapes of data
// versions 3813 to 4068, in vanilla's order.
func fixBlockEntityFields(id string, kind BlockEntityKind, tag nbt.Compound, version int32) {
	if version < blockPosFormatVersion {
		if kind == KindBeehive {
			fixBlockPosField(tag, "FlowerPos", "flower_pos")
		} else if id == "minecraft:end_gateway" {
			fixBlockPosField(tag, "ExitPortal", "exit_portal")
		}
	}
	switch kind {
	case KindBeehive:
		if version < beehiveRenameVersion {
			fixBeehive(tag)
		}
		upgradeBeeEntities(tag, version)
	case KindBanner:
		if version < bannerPatternVersion {
			fixBannerPatterns(tag)
		}
	case KindSkull:
		if version < playerHeadProfileVersion {
			fixSkullProfile(tag)
		}
	case KindJukebox:
		if version < jukeboxSongTicksVersion {
			fixJukeboxTicks(tag)
		}
	}
	if version < lockPredicateVersion {
		fixContainerLock(tag)
	}
}

func renameField(tag nbt.Compound, from, to string) {
	if value, ok := tag[from]; ok {
		delete(tag, from)
		tag[to] = value
	}
}

// fixBeaconEffect replaces a beacon's numeric effect under from with the
// effect's name under to; an ID with no name drops the field.
// vanilla: MobEffectIdFix.updateMobEffectIdField
func fixBeaconEffect(tag nbt.Compound, from, to string) {
	value, ok := tag[from]
	if !ok {
		return
	}
	delete(tag, from)
	if id, ok := nbt.IntValue(value); ok && id >= 1 && int(id) <= len(oldMobEffectNames) {
		tag[to] = nbt.String(oldMobEffectNames[id-1])
	}
}

// oldMobEffectNames are the effects by their numeric IDs before
// mobEffectIDVersion, from 1.
// vanilla: MobEffectIdFix.ID_MAP
var oldMobEffectNames = [...]string{
	"minecraft:speed", "minecraft:slowness", "minecraft:haste", "minecraft:mining_fatigue",
	"minecraft:strength", "minecraft:instant_health", "minecraft:instant_damage", "minecraft:jump_boost",
	"minecraft:nausea", "minecraft:regeneration", "minecraft:resistance", "minecraft:fire_resistance",
	"minecraft:water_breathing", "minecraft:invisibility", "minecraft:blindness", "minecraft:night_vision",
	"minecraft:hunger", "minecraft:weakness", "minecraft:poison", "minecraft:wither",
	"minecraft:health_boost", "minecraft:absorption", "minecraft:saturation", "minecraft:glowing",
	"minecraft:levitation", "minecraft:luck", "minecraft:unluck", "minecraft:slow_falling",
	"minecraft:conduit_power", "minecraft:dolphins_grace", "minecraft:bad_omen", "minecraft:hero_of_the_village",
	"minecraft:darkness",
}

// suspiciousSandID is the block entity id KindBrushable had before
// brushableBlockVersion.
const suspiciousSandID = "minecraft:suspicious_sand"

// signTextFields are the pre-3439 fields the front and back text replace.
var signTextFields = []string{"Text1", "Text2", "Text3", "Text4", "FilteredText1", "FilteredText2", "FilteredText3", "FilteredText4", "Color", "GlowingText"}

// emptyComponent is the empty text component as 1.20.2 stores it.
// vanilla: LegacyComponentDataFixUtils.createEmptyComponent
const emptyComponent = `""`

func signLines(tag nbt.Compound, prefix string) (lines []nbt.Tag, found bool) {
	for i := 1; i <= 4; i++ {
		line, ok := tag[prefix+string(rune('0'+i))]
		lines = append(lines, line)
		found = found || ok
	}
	return lines, found
}

func messageList(items []nbt.Tag) nbt.List { return nbt.List{Elem: nbt.TypeString, Items: items} }

// fixSignText moves a sign's Text1..4 into front_text and back_text, and drops
// filtered lines that merely repeat the unfiltered ones.
// vanilla: BlockEntitySignDoubleSidedEditableTextFix, DropInvalidSignDataFix
func fixSignText(tag nbt.Compound, version int32) {
	if version < signDoubleSidedVersion {
		lines, _ := signLines(tag, "Text")
		for i, line := range lines {
			if line == nil {
				lines[i] = nbt.String(emptyComponent)
			}
		}
		color, ok := tag["Color"]
		if !ok {
			color = nbt.String("black")
		}
		glowing, ok := tag["GlowingText"]
		if !ok {
			glowing = nbt.Byte(0)
		}
		front := nbt.Compound{"messages": messageList(lines), "color": color, "has_glowing_text": glowing}
		if filtered, found := signLines(tag, "FilteredText"); found {
			for i, line := range filtered {
				if line == nil {
					filtered[i] = lines[i]
				}
			}
			front["filtered_messages"] = messageList(filtered)
		}
		empty := []nbt.Tag{nbt.String(emptyComponent), nbt.String(emptyComponent), nbt.String(emptyComponent), nbt.String(emptyComponent)}
		tag["front_text"] = front
		tag["back_text"] = nbt.Compound{"messages": messageList(empty), "color": nbt.String("black"), "has_glowing_text": nbt.Byte(0)}
		tag["is_waxed"] = nbt.Byte(0)
		tag["_filtered_correct"] = nbt.Byte(1)
	}
	if version < signFilteredVersion {
		if correct, _ := nbt.Get[nbt.Byte](tag, "_filtered_correct"); correct != 0 {
			delete(tag, "_filtered_correct")
		} else {
			for _, side := range []string{"front_text", "back_text"} {
				if text, ok := nbt.Get[nbt.Compound](tag, side); ok {
					dropRepeatedFilteredLines(text)
				}
			}
		}
	}
	for _, key := range signTextFields {
		delete(tag, key)
	}
}

// dropRepeatedFilteredLines fills a filtered line that is empty with its
// unfiltered one and drops the list when it then equals the messages.
// vanilla: DropInvalidSignDataFix.fixText
func dropRepeatedFilteredLines(text nbt.Compound) {
	filtered, ok := nbt.Get[nbt.List](text, "filtered_messages")
	if !ok {
		return
	}
	messages, _ := nbt.Get[nbt.List](text, "messages")
	same := len(filtered.Items) == len(messages.Items)
	items := make([]nbt.Tag, len(filtered.Items))
	for i, line := range filtered.Items {
		var fallback nbt.Tag = nbt.String(emptyComponent)
		if i < len(messages.Items) {
			fallback = messages.Items[i]
		}
		if s, ok := line.(nbt.String); ok && string(s) == emptyComponent {
			line = fallback
		}
		items[i] = line
		same = same && i < len(messages.Items) && nbt.Equal(line, messages.Items[i])
	}
	if same {
		delete(text, "filtered_messages")
		return
	}
	filtered.Items = items
	text["filtered_messages"] = filtered
}

// unflattenPotSherds turns a decorated pot's sherd list into per-side stacks.
// vanilla: PotDecorationsBlockEntityUnflatteningFix
func unflattenPotSherds(tag nbt.Compound, version int32) {
	if version >= potDecorationsVersion {
		return
	}
	v, ok := tag["sherds"]
	if !ok {
		v = nbt.List{Elem: nbt.TypeString}
	}
	tag["sherds"] = templateUnpackPot(v)
}

// fixPotSherds renames a decorated pot's shards to sherds along with the item
// names in them.
// vanilla: DecoratedPotFieldRenameFix, ItemRenameFix (v3438, v3447)
func fixPotSherds(tag nbt.Compound, version int32) {
	if version >= potSherdsVersion {
		return
	}
	shards, ok := nbt.Get[nbt.List](tag, "shards")
	if !ok {
		return
	}
	delete(tag, "shards")
	for i, item := range shards.Items {
		name, ok := item.(nbt.String)
		if !ok {
			continue
		}
		shards.Items[i] = nbt.String(renameOldItem(string(name), version))
	}
	tag["sherds"] = shards
}

// dropOldBlendingData removes the blending data of a nether or End chunk from
// before blendingDataVersion; only the overworld blends with old terrain.
// vanilla: BlendingDataRemoveFromNetherEndFix
func dropOldBlendingData(root nbt.Compound, dimension string) {
	if version, _ := nbt.Get[nbt.Int](root, "DataVersion"); version != 0 && version < blendingDataVersion && dimension != DimensionName {
		delete(root, "blending_data")
	}
}

// fixOldChunkRoot applies the root-level fixes of a chunk older than
// chunkStatusVersion: the structure renames and the namespaced status.
func fixOldChunkRoot(root nbt.Compound) {
	version, _ := nbt.Get[nbt.Int](root, "DataVersion")
	if version == 0 || version >= chunkStatusVersion {
		return
	}
	if version < structuresConfiguredVersion {
		if structures, ok := nbt.Get[nbt.Compound](root, "structures"); ok {
			fixStructureNames(structures, root)
		}
	}
	fixChunkStatus(root, "Status")
	if retrogen, ok := nbt.Get[nbt.Compound](root, "below_zero_retrogen"); ok {
		fixChunkStatus(retrogen, "target_status")
	}
}

// fixChunkStatus namespaces the status under key and renames the two removed
// ones.
// vanilla: RemapChunkStatusFix
func fixChunkStatus(c nbt.Compound, key string) {
	status, ok := nbt.Get[nbt.String](c, key)
	if !ok {
		return
	}
	name := registry.Qualify(string(status))
	switch name {
	case "minecraft:liquid_carvers":
		name = "minecraft:carvers"
	case "minecraft:heightmaps":
		name = "minecraft:spawn"
	}
	c[key] = nbt.String(name)
}

// structureConversion is an unconfigured structure's configured name: the
// first of biomes' names when the chunk's biome palettes name more of its
// biomes than of any other's, else fallback.
type structureConversion struct {
	fallback string
	biomes   []structureBiomeName
}

type structureBiomeName struct {
	name   string
	biomes []string
}

var (
	badlandsBiomes = []string{"minecraft:badlands", "minecraft:eroded_badlands", "minecraft:wooded_badlands"}
	// oceanBiomes are the ocean biomes in vanilla's list order.
	oceanBiomes = []string{"minecraft:deep_frozen_ocean", "minecraft:deep_cold_ocean", "minecraft:deep_ocean", "minecraft:deep_lukewarm_ocean", "minecraft:frozen_ocean", "minecraft:ocean", "minecraft:cold_ocean", "minecraft:lukewarm_ocean", "minecraft:warm_ocean"}
)

// structureConversions are the configured names of the structure keys a chunk
// before structuresConfiguredVersion uses.
// vanilla: StructuresBecomeConfiguredFix.CONVERSION_MAP
var structureConversions = map[string]structureConversion{
	"mineshaft": {"minecraft:mineshaft", []structureBiomeName{{"minecraft:mineshaft_mesa", badlandsBiomes}}},
	"shipwreck": {"minecraft:shipwreck", []structureBiomeName{{"minecraft:shipwreck_beached", []string{"minecraft:beach", "minecraft:snowy_beach"}}}},
	"ocean_ruin": {"minecraft:ocean_ruin_cold", []structureBiomeName{
		{"minecraft:ocean_ruin_warm", []string{"minecraft:warm_ocean", "minecraft:lukewarm_ocean", "minecraft:deep_lukewarm_ocean"}},
	}},
	"village": {"minecraft:village_plains", []structureBiomeName{
		{"minecraft:village_desert", []string{"minecraft:desert"}},
		{"minecraft:village_savanna", []string{"minecraft:savanna"}},
		{"minecraft:village_snowy", []string{"minecraft:snowy_plains"}},
		{"minecraft:village_taiga", []string{"minecraft:taiga"}},
	}},
	"ruined_portal": {"minecraft:ruined_portal", []structureBiomeName{
		{"minecraft:ruined_portal_desert", []string{"minecraft:desert"}},
		{"minecraft:ruined_portal_mountain", append(slices.Clip(badlandsBiomes), "minecraft:windswept_hills", "minecraft:windswept_forest", "minecraft:windswept_gravelly_hills", "minecraft:savanna_plateau", "minecraft:windswept_savanna", "minecraft:stony_shore", "minecraft:meadow", "minecraft:frozen_peaks", "minecraft:jagged_peaks", "minecraft:stony_peaks", "minecraft:snowy_slopes")},
		{"minecraft:ruined_portal_jungle", []string{"minecraft:bamboo_jungle", "minecraft:jungle", "minecraft:sparse_jungle"}},
		{"minecraft:ruined_portal_ocean", oceanBiomes},
	}},
	"pillager_outpost": {fallback: "minecraft:pillager_outpost"},
	"mansion":          {fallback: "minecraft:mansion"},
	"jungle_pyramid":   {fallback: "minecraft:jungle_pyramid"},
	"desert_pyramid":   {fallback: "minecraft:desert_pyramid"},
	"igloo":            {fallback: "minecraft:igloo"},
	"swamp_hut":        {fallback: "minecraft:swamp_hut"},
	"stronghold":       {fallback: "minecraft:stronghold"},
	"monument":         {fallback: "minecraft:monument"},
	"fortress":         {fallback: "minecraft:fortress"},
	"endcity":          {fallback: "minecraft:end_city"},
	"buried_treasure":  {fallback: "minecraft:buried_treasure"},
	"nether_fossil":    {fallback: "minecraft:nether_fossil"},
	"bastion_remnant":  {fallback: "minecraft:bastion_remnant"},
}

// configuredStructure is the configured name of an old structure key, false
// for one vanilla drops.
// vanilla: StructuresBecomeConfiguredFix.findUpdatedStructureType, guessConfiguration
func configuredStructure(key string, root nbt.Compound) (string, bool) {
	conversion, ok := structureConversions[strings.ToLower(key)]
	if !ok {
		return "", false
	}
	if len(conversion.biomes) == 0 {
		return conversion.fallback, true
	}
	// Counts palette entries, not cells; a tie goes to the name counted first.
	var order []string
	counts := map[string]int{}
	sections, _ := nbt.Get[nbt.List](root, "sections")
	for _, item := range sections.Items {
		sec, _ := nbt.CompoundView(item)
		biomes, _ := nbt.Get[nbt.Compound](sec, "biomes")
		palette, _ := nbt.Get[nbt.List](biomes, "palette")
		for _, entry := range palette.Items {
			biome, _ := entry.(nbt.String)
			for _, mapping := range conversion.biomes {
				if !slices.Contains(mapping.biomes, string(biome)) {
					continue
				}
				if counts[mapping.name] == 0 {
					order = append(order, mapping.name)
				}
				counts[mapping.name]++
				break
			}
		}
	}
	best := conversion.fallback
	for i, name := range order {
		if i == 0 || counts[name] > counts[best] {
			best = name
		}
	}
	return best, true
}

// fixStructureNames renames a chunk's structure starts and references to their
// configured structures, merging references that now share a name and dropping
// unknown, invalid and empty entries.
// vanilla: StructuresBecomeConfiguredFix.updateStarts, updateReferences
func fixStructureNames(structures, root nbt.Compound) {
	if starts, ok := nbt.Get[nbt.Compound](structures, "starts"); ok {
		fixed := nbt.Compound{}
		for key, value := range starts {
			start, _ := nbt.CompoundView(value)
			if id, ok := nbt.Get[nbt.String](start, "id"); !ok || id == "INVALID" {
				continue
			}
			name, ok := configuredStructure(key, root)
			if _, taken := fixed[name]; !ok || taken {
				continue
			}
			start["id"] = nbt.String(name)
			fixed[name] = start
		}
		structures["starts"] = fixed
	}
	if refs, ok := nbt.Get[nbt.Compound](structures, "References"); ok {
		fixed := nbt.Compound{}
		for key, value := range refs {
			list, _ := value.(nbt.LongArray)
			name, ok := configuredStructure(key, root)
			if len(list) == 0 || !ok {
				continue
			}
			prev, _ := fixed[name].(nbt.LongArray)
			fixed[name] = append(slices.Clip(prev), list...)
		}
		structures["References"] = fixed
	}
}
