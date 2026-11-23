package world

import (
	"maps"
	"slices"

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
	"minecraft:banner": {fields: []string{"patterns", "CustomName"}, read: func(e nbt.Compound) {
		if layers, ok := bannerPatternsFromTag(e); ok {
			e["patterns"] = registry.BannerPatternsTag(layers)
		}
	}},
	"minecraft:skull": {fields: []string{"profile", "note_block_sound", "custom_name"}, read: func(e nbt.Compound) {
		// A name string is ResolvableProfile.CODEC's compact form.
		if name, ok := nbt.Get[nbt.String](e, "profile"); ok {
			if registry.ProfileComponentMalformed(name) == "" {
				e["profile"] = nbt.Compound{"name": name}
			} else {
				delete(e, "profile")
			}
		}
	}},
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
	"minecraft:trial_spawner": {fields: []string{
		"registered_players", "current_mobs", "cooldown_ends_at", "next_mob_spawns_at", "total_mobs_spawned",
		"spawn_data", "ejecting_loot_table", "normal_config", "ominous_config", "target_cooldown_length",
		"required_player_range",
	}, records: map[string][]string{"normal_config": trialConfigFields, "ominous_config": trialConfigFields}, read: func(e nbt.Compound) {
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
	}},
	"minecraft:test_instance_block": {fields: []string{"data", "errors"}, readInPlace: readTestInstanceBlock},
	"minecraft:end_gateway":         {fields: []string{"Age", "exit_portal", "ExactTeleport"}},
	"minecraft:beehive":             {fields: []string{"bees", "flower_pos"}},
}

// testBlockModes are TestBlockMode's names.
var testBlockModes = []string{"start", "log", "fail", "accept"}

// readCarried normalises a command's tag in place as the type's codecs read it.
func readCarried(chunk *Chunk, entry nbt.Compound) {
	id, _ := nbt.Get[nbt.String](entry, "id")
	kind := carriedKinds[string(id)]
	if kind.read != nil {
		kind.read(entry)
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
