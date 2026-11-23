package world

import (
	"slices"

	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// carriedKind is, for a block entity type the server keeps as a compound, what
// its codecs do to a command's tag: read normalises the values the way its
// loadAdditional reads them and writes the fields saveAdditional always writes;
// fields is the order saveAdditional writes them in, which a compound keeps for
// the ties of its display order; save, when set, writes them itself, in order.
// vanilla: BlockEntity.loadWithComponents, BlockEntity.saveWithFullMetadata
type carriedKind struct {
	fields []string
	read   func(nbt.Compound)
	save   func(e nbt.Compound, set func(string, nbt.Tag))
}

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
	"minecraft:structure_block": {save: func(e nbt.Compound, set func(string, nbt.Tag)) {
		ParseStructureBlock(e).save(set)
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
	}, read: func(e nbt.Compound) {
		for _, key := range []string{"normal_config", "ominous_config"} {
			if config, ok := nbt.Get[nbt.CompoundTag](e, key); ok {
				config = blockRecord(config, "spawn_range", "total_mobs", "simultaneous_mobs", "total_mobs_added_per_player",
					"simultaneous_mobs_added_per_player", "ticks_between_spawn", "spawn_potentials", "loot_tables_to_eject",
					"items_to_drop_when_ominous")
				if list, ok := beSpawnPotentials.read(config.Entries(), "spawn_potentials"); ok {
					config.Set("spawn_potentials", list)
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
	"minecraft:vault": {fields: []string{"config", "shared_data", "server_data"}, read: func(e nbt.Compound) {
		if config, ok := nbt.Get[nbt.CompoundTag](e, "config"); ok {
			config = nbt.ShallowCopyCompound(config)
			if key, ok := config.Entries()["key_item"]; ok {
				config.Set("key_item", registry.CodecStackTag(key))
			}
			qualifyString(config, "loot_table")
			qualifyString(config, "override_loot_table_to_display")
			e["config"] = config
		}
	}},
	"minecraft:brushable_block": {fields: []string{"LootTable", "LootTableSeed", "item"}, read: func(e nbt.Compound) {
		if item, ok := e["item"]; ok {
			e["item"] = registry.CodecStackTag(item)
		}
		qualifyString(e, "LootTable")
	}},
	"minecraft:test_instance_block": {fields: []string{"data", "errors"}},
	"minecraft:end_gateway":         {fields: []string{"Age", "exit_portal", "ExactTeleport"}},
	"minecraft:beehive":             {fields: []string{"bees", "flower_pos"}},
}

// testBlockModes are TestBlockMode's names.
var testBlockModes = []string{"start", "log", "fail", "accept"}

// readCarried normalises a command's tag in place as the type's codecs read it.
func readCarried(entry nbt.Compound) {
	id, _ := nbt.Get[nbt.String](entry, "id")
	if kind, ok := carriedKinds[string(id)]; ok && kind.read != nil {
		kind.read(entry)
	}
}

// orderedCarried writes a carried compound's fields once in the order
// saveWithFullMetadata does: the type's own, components, then id and position;
// other fields follow.
func orderedCarried(entry nbt.Compound) *nbt.OrderedCompound {
	id, _ := nbt.Get[nbt.String](entry, "id")
	kind := carriedKinds[string(id)]
	out := nbt.NewOrderedCompound(len(entry))
	if kind.save != nil {
		kind.save(entry, out.Set)
	}
	for _, key := range append(slices.Clone(kind.fields), "components", "id", "x", "y", "z") {
		if v, ok := entry[key]; ok {
			out.Set(key, v)
		}
	}
	for _, key := range nbt.HashOrderedKeys(entry) {
		if _, done := out.Entries()[key]; !done {
			out.Set(key, entry[key])
		}
	}
	return out
}

// keepCarriedOrder is entry in codec order when the compound it replaces was
// kept so; an edit rewrites the map, which would lose the order.
func keepCarriedOrder(old nbt.CompoundTag, entry nbt.Compound) nbt.CompoundTag {
	if isOrdered(old) {
		return orderedCarried(entry)
	}
	return entry
}

func isOrdered(tag nbt.CompoundTag) bool {
	_, ordered := tag.(*nbt.OrderedCompound)
	return ordered
}

// qualifyString namespaces the string at key, leaving any other tag.
func qualifyString(c nbt.CompoundTag, key string) {
	if v, ok := nbt.Get[nbt.String](c.Entries(), key); ok {
		c.Set(key, nbt.String(registry.Qualify(string(v))))
	}
}
