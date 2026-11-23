package world

import (
	"maps"

	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// carriedCodecs is, per block entity type the server keeps as a compound, what
// its codecs do to a command's tag: namespace the ids, and write the fields
// its saveAdditional always writes. Each takes the entry and returns it.
// vanilla: BlockEntity.loadWithComponents, BlockEntity.saveWithFullMetadata
var carriedCodecs = map[string]func(nbt.Compound){
	"minecraft:banner": func(e nbt.Compound) {
		mapListEntries(e, "patterns", func(layer nbt.CompoundTag) {
			qualifyString(layer, "pattern")
		})
	},
	"minecraft:skull": func(e nbt.Compound) {
		// A name string is ResolvableProfile.CODEC's compact form.
		if name, ok := nbt.Get[nbt.String](e, "profile"); ok {
			if validPlayerName(string(name)) {
				e["profile"] = nbt.Compound{"name": name}
			} else {
				delete(e, "profile")
			}
		}
	},
	"minecraft:structure_block": func(e nbt.Compound) {
		maps.Copy(e, ParseStructureBlock(e).Compound())
	},
	"minecraft:test_block": func(e nbt.Compound) {
		setDefault(e, "mode", nbt.String("fail"))
		setDefault(e, "message", nbt.String(""))
		setDefault(e, "powered", nbt.Byte(0))
	},
	"minecraft:trial_spawner": func(e nbt.Compound) {
		for _, key := range []string{"normal_config", "ominous_config"} {
			if config, ok := nbt.Get[nbt.CompoundTag](e, key); ok {
				config = nbt.Copy(config).(nbt.CompoundTag)
				mapListEntries(config.Entries(), "spawn_potentials", func(entry nbt.CompoundTag) {
					if data, ok := nbt.Get[nbt.CompoundTag](entry.Entries(), "data"); ok {
						entry.Set("data", qualifiedSpawnData(data))
					}
				})
				qualifyString(config, "items_to_drop_when_ominous")
				e[key] = config
			}
		}
		if data, ok := nbt.Get[nbt.CompoundTag](e, "spawn_data"); ok {
			e["spawn_data"] = qualifiedSpawnData(data)
		}
		qualifyString(e, "ejecting_loot_table")
	},
	"minecraft:vault": func(e nbt.Compound) {
		if config, ok := nbt.Get[nbt.CompoundTag](e, "config"); ok {
			config = nbt.Copy(config).(nbt.CompoundTag)
			if key, ok := config.Entries()["key_item"]; ok {
				config.Set("key_item", registry.QualifiedStackTag(key))
			}
			qualifyString(config, "loot_table")
			qualifyString(config, "override_loot_table_to_display")
			e["config"] = config
		}
	},
	"minecraft:brushable_block": brushable,
}

// brushable is the brushable block entity's item and loot table.
// vanilla: BrushableBlockEntity.loadAdditional
func brushable(e nbt.Compound) {
	if item, ok := e["item"]; ok {
		e["item"] = registry.QualifiedStackTag(item)
	}
	qualifyString(e, "LootTable")
}

// canonicalCarried shapes a carried compound as vanilla saves the entity it
// loads it into: the type's own codecs, then components even when empty. It
// modifies entry and returns it.
func canonicalCarried(entry nbt.Compound) nbt.Compound {
	id, _ := nbt.Get[nbt.String](entry, "id")
	if canon, ok := carriedCodecs[string(id)]; ok {
		canon(entry)
	}
	setDefault(entry, "components", nbt.Compound{})
	return entry
}

func setDefault(e nbt.Compound, key string, v nbt.Tag) {
	if _, ok := e[key]; !ok {
		e[key] = v
	}
}

// qualifyString namespaces the string at key, leaving any other tag.
func qualifyString(c nbt.CompoundTag, key string) {
	if v, ok := nbt.Get[nbt.String](c.Entries(), key); ok {
		c.Set(key, nbt.String(registry.Qualify(string(v))))
	}
}

// mapListEntries replaces e[key], a list of compounds, with a copy whose
// compounds fn has edited.
func mapListEntries(e nbt.Compound, key string, fn func(nbt.CompoundTag)) {
	list, ok := nbt.Get[nbt.List](e, key)
	if !ok || list.Elem != nbt.TypeCompound {
		return
	}
	list = nbt.Copy(list).(nbt.List)
	for _, item := range list.Items {
		if entry, ok := item.(nbt.CompoundTag); ok {
			fn(entry)
		}
	}
	e[key] = list
}

// validPlayerName reports a name ResolvableProfile.CODEC takes: at most 16
// characters, all printable ASCII but the space.
// vanilla: StringUtil.isValidPlayerName
func validPlayerName(name string) bool {
	if len(name) > 16 {
		return false
	}
	for i := range len(name) {
		if name[i] <= ' ' || name[i] >= 127 {
			return false
		}
	}
	return true
}
