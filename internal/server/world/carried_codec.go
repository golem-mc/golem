package world

import (
	"maps"
	"math"
	"slices"
	"strings"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// carriedKind is, for a block entity type the server keeps as a compound, what
// its codecs do to a command's tag: read normalises the values the way its
// loadAdditional reads them and writes the fields saveAdditional always writes;
// fields is the order saveAdditional writes them in, which a compound keeps for
// the ties of its display order.
// vanilla: BlockEntity.loadWithComponents, BlockEntity.saveWithFullMetadata
type carriedKind struct {
	fields      []string
	records     map[string][]string
	read        func(nbt.Compound)
	readInPlace func(entry, previous nbt.Compound)
	// readAt is read for a kind that also needs its block state.
	readAt func(e nbt.Compound, state uint16)
}

// vanilla: TrialSpawnerConfig.DIRECT_CODEC, VaultConfig.CODEC
var (
	trialConfigFields = []string{
		"spawn_range", "total_mobs", "simultaneous_mobs", "total_mobs_added_per_player",
		"simultaneous_mobs_added_per_player", "ticks_between_spawn", "spawn_potentials",
		"loot_tables_to_eject", "items_to_drop_when_ominous",
	}
	vaultConfigFields = []string{
		"loot_table", "activation_range", "deactivation_range", "key_item", "override_loot_table_to_display",
	}
)

var carriedKinds = map[string]carriedKind{
	"minecraft:banner": {fields: []string{"patterns", "CustomName"}, read: readBannerPatterns},
	"minecraft:skull":  {fields: []string{"profile", "note_block_sound", "custom_name"}, read: readSkull},
	"minecraft:structure_block": {fields: structureBlockFields(), read: func(e nbt.Compound) {
		ParseStructureBlock(e).save(func(k string, v nbt.Tag) { e[k] = v })
	}},
	"minecraft:test_block": {fields: []string{"mode", "message", "powered"}, read: func(e nbt.Compound) {
		// vanilla: TestBlockMode.CODEC, getStringOr, getBooleanOr
		mode, _ := nbt.Get[nbt.String](e, "mode")
		if !slices.Contains(testBlockModes, string(mode)) {
			mode = "fail"
		}
		message, _ := nbt.Get[nbt.String](e, "message")
		powered := nbt.Byte(0)
		if nbt.BoolField(e, "powered") {
			powered = 1
		}
		e["mode"], e["message"], e["powered"] = mode, message, powered
	}},
	"minecraft:jigsaw": {fields: []string{"name", "target", "pool", "final_state", "joint", "placement_priority", "selection_priority"}, readAt: readJigsaw},
	"minecraft:trial_spawner": {fields: []string{
		"registered_players", "current_mobs", "cooldown_ends_at", "next_mob_spawns_at", "total_mobs_spawned",
		"spawn_data", "ejecting_loot_table", "normal_config", "ominous_config", "target_cooldown_length",
		"required_player_range",
	}, records: map[string][]string{"normal_config": trialConfigFields, "ominous_config": trialConfigFields}, read: func(e nbt.Compound) {
		// FullConfig.MAP_CODEC holds both configs: one refused leaves the default pair.
		for _, key := range []string{"normal_config", "ominous_config"} {
			if config, ok := nbt.Get[nbt.CompoundTag](e, key); ok && !trialConfigInRange(config.Entries()) {
				delete(e, "normal_config")
				delete(e, "ominous_config")
				break
			}
		}
		for _, key := range []string{"normal_config", "ominous_config"} {
			if config, ok := nbt.Get[nbt.CompoundTag](e, key); ok {
				config = blockRecord(config, trialConfigFields...)
				if list, ok := beSpawnPotentials.read(config.Entries(), "spawn_potentials"); ok {
					config.Set("spawn_potentials", list)
				}
				if list, ok := nbt.Get[nbt.List](config.Entries(), "loot_tables_to_eject"); ok {
					list = nbt.Copy(list).(nbt.List)
					for _, tag := range list.Items {
						if weighted, ok := tag.(nbt.CompoundTag); ok {
							qualifyString(weighted, "data")
						}
					}
					config.Set("loot_tables_to_eject", list)
				}
				qualifyString(config, "items_to_drop_when_ominous")
				e[key] = config
			}
		}
		if data, ok := beSpawnData.read(e, "spawn_data"); ok {
			e["spawn_data"] = data
		}
		if s, ok := beLootTable.read(e, "ejecting_loot_table"); ok {
			e["ejecting_loot_table"] = nbt.String(s)
		}
	}},
	"minecraft:vault": {
		fields:  []string{"config", "shared_data", "server_data"},
		records: map[string][]string{"config": vaultConfigFields},
		read:    readVaultConfig,
	},
	"minecraft:brushable_block": {fields: []string{"LootTable", "LootTableSeed", "item"}, read: func(e nbt.Compound) {
		if item, ok := e["item"]; ok {
			e["item"] = registry.CodecStackTag(item)
		}
		qualifyString(e, "LootTable")
		// ItemStack.CODEC refuses an empty stack, which reads as no item.
		if stack, ok := nbt.Get[nbt.CompoundTag](e, "item"); ok {
			if item, ok := registry.StoredStack(stack); ok && item.Empty() {
				delete(e, "item")
			}
		}
	}},
	"minecraft:test_instance_block": {fields: []string{"data", "errors"}, readInPlace: readTestInstanceBlock},
	"minecraft:end_gateway": {fields: []string{"Age", "exit_portal", "ExactTeleport"}, read: func(e nbt.Compound) {
		readBlockPos(e, "exit_portal", true)
	}},
	"minecraft:beehive": {fields: []string{"bees", "flower_pos"}, read: func(e nbt.Compound) {
		readBees(e)
		readBlockPos(e, "flower_pos", false)
	}},
}

// trialConfigInRange reports whether the numeric fields of a trial spawner
// config are numbers within TrialSpawnerConfig.DIRECT_CODEC's ranges.
func trialConfigInRange(config nbt.Compound) bool {
	for _, f := range []struct {
		key      string
		min, max float64
	}{
		{"spawn_range", 1, 128},
		{"total_mobs", 0, math.MaxFloat32},
		{"simultaneous_mobs", 0, math.MaxFloat32},
		{"total_mobs_added_per_player", 0, math.MaxFloat32},
		{"simultaneous_mobs_added_per_player", 0, math.MaxFloat32},
		{"ticks_between_spawn", 0, math.MaxInt32},
	} {
		tag, ok := config[f.key]
		if !ok {
			continue
		}
		v, ok := nbt.NumberValue(tag)
		if !ok {
			return false
		}
		if f.key == "spawn_range" || f.key == "ticks_between_spawn" {
			v = math.Trunc(v)
		}
		if v < f.min || v > f.max {
			return false
		}
	}
	return true
}

// testBlockModes are TestBlockMode's names.
var testBlockModes = []string{"start", "log", "fail", "accept"}

// freshCarriedTag supplies constructor values that differ from loading empty NBT.
// vanilla: StructureBlockEntity.<init>, TestBlockEntity.<init>, JigsawBlockEntity.<init>
func freshCarriedTag(block registry.Block, state uint16) nbt.Compound {
	tag := nbt.Compound{"components": nbt.Compound{}}
	switch block {
	case registry.BlockStructureBlock:
		tag["mode"] = nbt.String(structureModeNames[structureModeOfState(state)])
	case registry.BlockTestBlock:
		tag["mode"] = nbt.String(registry.StateValue(state, registry.PropMode))
	case registry.BlockJigsaw:
		tag["name"] = nbt.String("minecraft:empty")
		tag["target"] = nbt.String("minecraft:empty")
		tag["pool"] = nbt.String("minecraft:empty")
		tag["final_state"] = nbt.String("minecraft:air")
		tag["joint"] = nbt.String("rollable") // The constructor uses this for every orientation.
		tag["placement_priority"] = nbt.Int(0)
		tag["selection_priority"] = nbt.Int(0)
	case registry.BlockEndGateway:
		tag["Age"] = nbt.Long(0)
	}
	return tag
}

// readBannerPatterns keeps the layers BannerPatternLayers.CODEC accepts: a
// list codec drops a refused element and keeps the rest, and no layer left
// saves no patterns.
// vanilla: BannerBlockEntity.loadAdditional, ValueInput.read (resultOrPartial)
func readBannerPatterns(e nbt.Compound) {
	list, ok := nbt.Get[nbt.List](e, "patterns")
	if !ok {
		delete(e, "patterns")
		return
	}
	var layers []protocol.BannerPatternLayer
	for _, item := range list.Items {
		if one, ok := registry.BannerPatternsOf(nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{item}}); ok {
			layers = append(layers, one...)
		}
	}
	if len(layers) == 0 {
		delete(e, "patterns")
		return
	}
	e["patterns"] = registry.BannerPatternsTag(layers)
}

// readSkull keeps a profile and note block sound their codecs accept; a name
// string is ResolvableProfile.CODEC's compact form.
// vanilla: SkullBlockEntity.loadAdditional, Identifier.CODEC
func readSkull(e nbt.Compound) {
	if profile, ok := e["profile"]; ok {
		switch {
		case registry.ProfileComponentMalformed(profile) != "":
			delete(e, "profile")
		default:
			if name, ok := profile.(nbt.String); ok {
				e["profile"] = nbt.Compound{"name": name}
			}
		}
	}
	if sound, ok := e["note_block_sound"]; ok {
		name, isString := sound.(nbt.String)
		if !isString || !protocol.ValidIdentifier(string(name)) {
			delete(e, "note_block_sound")
		} else {
			e["note_block_sound"] = nbt.String(registry.Qualify(string(name)))
		}
	}
}

// readBlockPos keeps a BlockPos.CODEC value, three ints as an int array, and
// drops anything else; spawnable limits the position to the world's bounds.
// vanilla: BlockPos.CODEC, Level.isInSpawnableBounds
func readBlockPos(e nbt.Compound, key string, spawnable bool) {
	tag, ok := e[key]
	if !ok {
		return
	}
	var pos nbt.IntArray
	switch v := tag.(type) {
	case nbt.IntArray:
		pos = v
	case nbt.List:
		for _, item := range v.Items {
			n, isInt := item.(nbt.Int)
			if !isInt {
				break
			}
			pos = append(pos, int32(n))
		}
		if len(pos) != len(v.Items) {
			pos = nil
		}
	}
	const horizontal, vertical = 30000000, 20000000
	if len(pos) != 3 || spawnable && (pos[0] < -horizontal || pos[0] > horizontal ||
		pos[2] < -horizontal || pos[2] > horizontal || pos[1] < -vertical || pos[1] > vertical) {
		delete(e, key)
		return
	}
	e[key] = pos
}

// readBees keeps the occupants Occupant.CODEC accepts and drops each refused
// one, so the list is always saved.
// vanilla: BeehiveBlockEntity.loadAdditional, Occupant.LIST_CODEC
func readBees(e nbt.Compound) {
	list, ok := nbt.Get[nbt.List](e, "bees")
	if !ok {
		return
	}
	kept := list
	kept.Items = nil
	for _, item := range list.Items {
		if _, ok := registry.BeesOf(nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{item}}); ok {
			kept.Items = append(kept.Items, item)
		}
	}
	e["bees"] = kept
}

// readJigsaw reads every field the way loadAdditional does, so each is saved:
// a refused id is minecraft:empty, a refused joint the one the orientation
// implies.
// vanilla: JigsawBlockEntity.loadAdditional, StructureTemplate.getDefaultJointType
func readJigsaw(e nbt.Compound, state uint16) {
	for _, key := range []string{"name", "target", "pool"} {
		id, _ := nbt.Get[nbt.String](e, key)
		if !protocol.ValidIdentifier(string(id)) {
			id = "minecraft:empty"
		}
		e[key] = nbt.String(registry.Qualify(string(id)))
	}
	if _, ok := nbt.Get[nbt.String](e, "final_state"); !ok {
		e["final_state"] = nbt.String("minecraft:air")
	}
	if joint, _ := nbt.Get[nbt.String](e, "joint"); joint != "rollable" && joint != "aligned" {
		front, _, _ := strings.Cut(registry.StateValue(state, registry.PropOrientation), "_")
		joint = "aligned"
		if front == "up" || front == "down" {
			joint = "rollable"
		}
		e["joint"] = joint
	}
	for _, key := range []string{"placement_priority", "selection_priority"} {
		v, _ := nbt.IntField(e, key)
		e[key] = nbt.Int(v)
	}
}

// readCarried normalises a command's tag in place as the type's codecs read it.
func readCarried(chunk *Chunk, entry nbt.Compound) {
	id, _ := nbt.Get[nbt.String](entry, "id")
	kind := carriedKinds[string(id)]
	if kind.read != nil {
		kind.read(entry)
	}
	if kind.readAt != nil {
		x, y, z := carriedPos(entry)
		kind.readAt(entry, chunk.BlockAt(x&15, y, z&15))
	}
	if kind.readInPlace != nil {
		x, y, z := carriedPos(entry)
		previous, _ := chunk.carriedBlockEntityAt(x, y, z)
		if previous["id"] != id {
			previous = nil
		}
		kind.readInPlace(entry, previous)
	}
}

// orderedCarried is a carried compound in the order saveWithFullMetadata
// writes it: the type's own fields, components, then id and position; other
// fields follow.
func orderedCarried(entry nbt.Compound) nbt.CompoundTag {
	id, _ := nbt.Get[nbt.String](entry, "id")
	kind := carriedKinds[string(id)]
	if kind.readInPlace != nil {
		// Old chunk files may still carry the original command input.
		entry = maps.Clone(entry)
		kind.readInPlace(entry, nil)
	}
	tag := blockRecord(entry, slices.Concat(kind.fields, []string{"components", "id", "x", "y", "z"})...)
	for key, fields := range kind.records {
		if record, ok := nbt.Get[nbt.CompoundTag](entry, key); ok {
			tag.Set(key, blockRecord(record, fields...))
		}
	}
	return tag
}

func readTestInstanceBlock(entry, previous nbt.Compound) {
	data, ok := ReadTestInstanceData(entry["data"])
	if !ok {
		data, _ = ReadTestInstanceData(previous["data"])
	}
	entry["data"] = data.Tag()
}

// vanilla: VaultConfig.CODEC, VaultConfig.validate
func readVaultConfig(entry nbt.Compound) {
	config, ok := nbt.Get[nbt.Compound](entry, "config")
	if !ok {
		return
	}
	out := nbt.NewOrderedCompound(len(vaultConfigFields))
	for _, key := range vaultConfigFields {
		switch key {
		case "activation_range", "deactivation_range":
			if value, ok := nbt.NumberValue(config[key]); ok {
				out.Set(key, nbt.Double(value))
			}
		case "key_item":
			if stack, ok := nbt.Get[nbt.CompoundTag](config, key); ok {
				if item, ok := registry.StoredStack(stack); ok && !item.Empty() {
					out.Set(key, registry.StackTag(item))
				}
			}
		default:
			if value, ok := nbt.Get[nbt.String](config, key); ok && protocol.ValidIdentifier(string(value)) {
				out.Set(key, nbt.String(registry.Qualify(string(value))))
			}
		}
	}
	activation, hasActivation := nbt.Get[nbt.Double](out.Entries(), "activation_range")
	if !hasActivation {
		activation = 4
	}
	deactivation, hasDeactivation := nbt.Get[nbt.Double](out.Entries(), "deactivation_range")
	if !hasDeactivation {
		deactivation = 4.5
	}
	if activation > deactivation {
		delete(entry, "config")
	} else {
		entry["config"] = out
	}
}

// structureBlockFields are the keys StructureBlock.save writes, in its order.
func structureBlockFields() []string {
	var keys []string
	NewStructureBlock(StructureData).save(func(k string, _ nbt.Tag) { keys = append(keys, k) })
	return keys
}

// qualifyString namespaces the string at key, leaving any other tag.
func qualifyString(c nbt.CompoundTag, key string) {
	if v, ok := nbt.Get[nbt.String](c.Entries(), key); ok {
		c.Set(key, nbt.String(registry.Qualify(string(v))))
	}
}
