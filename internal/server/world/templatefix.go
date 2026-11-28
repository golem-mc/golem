package world

import (
	"fmt"
	"strings"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

//go:generate go run ../../../tools/gen templatefix

// templateDataVersionFloor is the oldest structure DataVersion read: the oldest
// one the chunk upgrade's palette, block entity and entity fixes cover.
const templateDataVersionFloor = entityDataVersionFloor

// templateChainVersion is where the generated STRUCTURE chain's coverage starts;
// a file below it first takes the chunk upgrade's fixes.
const templateChainVersion = 4440

type templateFixEdge struct{ path, target string }

// upgradeTemplate runs the STRUCTURE projection of DataFixers in version order.
// The schema paths stop at remainder fields, including minecraft:custom_data.
// vanilla: TemplateSource.read, DataFixTypes.STRUCTURE
func upgradeTemplate(tag nbt.Compound) error {
	version, ok := nbt.IntField(tag, "DataVersion")
	if !ok {
		// vanilla: TemplateSource.read
		version = 500
	}
	if version < templateDataVersionFloor {
		return fmt.Errorf("structure: DataVersion %d is below supported minimum %d", version, templateDataVersionFloor)
	}
	if version < templateChainVersion {
		upgradeOldTemplate(tag, version)
	}
	for _, v := range templateFixStages {
		if int64(version)*10 < int64(v) {
			templateFixSchemas[v].value(tag, "structure", v)
		}
	}
	return nil
}

// upgradeOldTemplate applies the chunk upgrade's fixes to the palettes, block
// entities and entities of a structure written before templateChainVersion.
// vanilla: DataFixTypes.STRUCTURE (the BLOCK_STATE, BLOCK_ENTITY and ENTITY fixes)
func upgradeOldTemplate(tag nbt.Compound, version int32) {
	if palette, ok := nbt.Get[nbt.List](tag, "palette"); ok {
		fixLegacyPalette(palette, version)
	}
	if lists, ok := nbt.Get[nbt.List](tag, "palettes"); ok {
		for _, item := range lists.Items {
			if palette, ok := item.(nbt.List); ok {
				fixLegacyPalette(palette, version)
			}
		}
	}
	if blocks, ok := nbt.Get[nbt.List](tag, "blocks"); ok {
		for _, item := range blocks.Items {
			entry, ok := item.(nbt.Compound)
			if !ok {
				continue
			}
			be, ok := nbt.Get[nbt.Compound](entry, "nbt")
			if !ok {
				continue
			}
			if !fixOldBlockEntity(be, version) {
				delete(entry, "nbt")
				continue
			}
			if version < furnaceBurnTimeVersion {
				fixFurnaceBurnTime(be)
			}
		}
	}
	if list, ok := nbt.Get[nbt.List](tag, "entities"); ok {
		var nbts []nbt.Tag
		for _, item := range list.Items {
			if entry, ok := item.(nbt.Compound); ok {
				if e, ok := nbt.Get[nbt.Compound](entry, "nbt"); ok {
					nbts = append(nbts, e)
				}
			}
		}
		upgradeEntities(nbt.List{Elem: nbt.TypeCompound, Items: nbts}, version)
	}
}

// templateFixer holds one stage's input paths and supported pre-read hook.
type templateFixer struct {
	paths       map[string][]templateFixEdge
	unpackItems bool
}

func (schema templateFixer) value(value nbt.Tag, kind string, stage int32) nbt.Tag {
	if value == nil {
		return nil
	}
	// vanilla: V3818_5.UNPACK_PLAIN_ID
	if schema.unpackItems && kind == "item_stack" {
		if id, ok := value.(nbt.String); ok {
			value = nbt.Compound{"id": id, "count": nbt.Int(1)}
		}
	}
	// vanilla: NamespacedSchema.NAMESPACED_STRING_CODEC
	switch kind {
	case "block_name", "item_name", "recipe", "entity_name", "game_event_name":
		if s, ok := value.(nbt.String); ok && protocol.ValidIdentifier(string(s)) {
			value = nbt.String(registry.Qualify(string(s)))
		}
	case "entity", "block_entity":
		if c, ok := value.(nbt.Compound); ok {
			if id, ok := nbt.Get[nbt.String](c, "id"); ok && protocol.ValidIdentifier(string(id)) {
				c["id"] = nbt.String(registry.Qualify(string(id)))
			}
		}
	}
	for _, edge := range schema.paths[kind] {
		value = schema.path(value, edge.path, edge.target, stage)
	}
	c, _ := value.(nbt.Compound)
	id, _ := nbt.Get[nbt.String](c, "id")
	switch stage {
	case 45350: // vanilla: CopperGolemWeatherStateFix
		if kind == "entity" && registry.Qualify(string(id)) == "minecraft:copper_golem" {
			if _, ok := c["weather_state"]; ok {
				state, _ := nbt.IntField(c, "weather_state")
				name := "unaffected"
				if state >= 1 && state <= 3 {
					name = [...]string{"exposed", "weathered", "oxidized"}[state-1]
				}
				c["weather_state"] = nbt.String(name)
			}
		}
	case 45410: // vanilla: BlockRenameFix, ItemRenameFix
		switch kind {
		case "item_name", "block_name":
			if s, ok := value.(nbt.String); ok && registry.Qualify(string(s)) == "minecraft:chain" {
				return nbt.String("minecraft:iron_chain")
			}
		case "block_state":
			if name, ok := nbt.Get[nbt.String](c, "Name"); ok && registry.Qualify(string(name)) == "minecraft:chain" {
				c["Name"] = nbt.String("minecraft:iron_chain")
			}
		case "flat_block_state":
			if s, ok := value.(nbt.String); ok {
				name := string(s)
				end := strings.IndexAny(name, "[{")
				if end < 0 {
					end = len(name)
				}
				if registry.Qualify(name[:end]) == "minecraft:chain" {
					return nbt.String("minecraft:iron_chain" + name[end:])
				}
			}
		}
	case 46490: // vanilla: TridentAnimationFix
		if kind == "data_components" {
			consumable, _ := nbt.Get[nbt.Compound](c, "minecraft:consumable")
			templateRenameString(consumable, "animation", "spear", "trident")
		}
	case 47630: // vanilla: VillagerSetVillagerDataFinalized, ZombieVillagerSetVillagerDataFinalized
		if kind == "entity" && (registry.Qualify(string(id)) == "minecraft:villager" || registry.Qualify(string(id)) == "minecraft:zombie_villager") {
			c["VillagerDataFinalized"] = nbt.Byte(1)
		}
	case 47690: // vanilla: NamespacedTypeRenameFix (StonecutterRecipeRenameFix.RECIPE_RENAMES)
		if kind == "recipe" {
			if s, ok := value.(nbt.String); ok {
				if renamed, ok := templateRecipeRenames[registry.Qualify(string(s))]; ok {
					return nbt.String(renamed)
				}
			}
		}
	case 48850: // vanilla: RemoveBlockEntityTagFix
		if kind == "block_entity" && registry.Qualify(string(id)) == "minecraft:bed" {
			return nil
		}
	case 48880: // vanilla: RenameNameplateToNameTagFix, AttributesRenameFix
		if kind == "entity" {
			templateRenameAttributes(c["attributes"], "id")
		}
		if kind == "data_components" {
			templateRenameAttributes(c["minecraft:attribute_modifiers"], "type")
		}
	default:
		value = templateFix26_3(value, kind, stage)
	}
	return value
}

func templateRenameString(c nbt.Compound, key, from, to string) {
	if s, ok := nbt.Get[nbt.String](c, key); ok && string(s) == from {
		c[key] = nbt.String(to)
	}
}

func templateRenameAttributes(value nbt.Tag, key string) {
	list, _ := value.(nbt.List)
	for _, item := range list.Items {
		c, _ := item.(nbt.Compound)
		templateRenameString(c, key, "minecraft:nameplate_distance", "minecraft:name_tag_distance")
	}
}

// path interprets fields, lists (*), map keys (#), map values (@)
// and tagged choices (?field=value). Only a typed edge can enter a compound.
func (schema templateFixer) path(value nbt.Tag, path, target string, stage int32) nbt.Tag {
	if value == nil {
		return nil
	}
	if path == "" {
		return schema.value(value, target, stage)
	}
	step, rest, _ := strings.Cut(path, "/")
	c, _ := value.(nbt.Compound)
	switch step {
	case "*":
		if list, ok := value.(nbt.List); ok {
			for i, item := range list.Items {
				list.Items[i] = schema.path(item, rest, target, stage)
			}
			if len(list.Items) > 0 && list.Elem != list.Items[0].Type() {
				// A schema hook can turn a list of plain IDs into stacks.
				list.Elem = list.Items[0].Type()
				return list
			}
		}
	case "#":
		if c == nil {
			return value
		}
		renamed := nbt.Compound{}
		for key, item := range c {
			name := schema.path(nbt.String(key), rest, target, stage).(nbt.String)
			renamed[string(name)] = item
		}
		return renamed
	case "@":
		for key, item := range c {
			c[key] = schema.path(item, rest, target, stage)
		}
	default:
		if strings.HasPrefix(step, "?") {
			key, want, _ := strings.Cut(step[1:], "=")
			got, ok := nbt.Get[nbt.String](c, key)
			name := string(got)
			if key == "id" {
				name = registry.Qualify(name)
			}
			if ok && name == want {
				return schema.path(value, rest, target, stage)
			}
		} else if item, ok := c[step]; ok {
			fixed := schema.path(item, rest, target, stage)
			if fixed == nil {
				delete(c, step)
			} else {
				c[step] = fixed
			}
		}
	}
	return value
}
