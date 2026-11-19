// Package registry holds the registries a server sends a client during
// configuration, and the game data generated alongside them.
//
// The client needs every built-in entry's name but not its data: sharing the
// built-in data pack, it fills the data in itself. So registries_gen.go is a
// list of names, extracted from the vanilla data pack of Version.
package registry

import (
	"strings"

	"github.com/golem-mc/golem/nbt"
)

// Registry is a set of named entries synchronized with the client. The position
// of an entry in Entries is the numeric ID other packets use to refer to it.
type Registry struct {
	Name    string
	Entries []string
	// Data holds the data packs' entries, sent with their contents since the
	// client's copy of the core pack lacks them or differs; nil for none.
	Data map[string]nbt.Tag
}

// ID returns the numeric ID of an entry, or -1 if the registry has no such
// entry.
func (r Registry) ID(entry string) int32 {
	for i, e := range r.Entries {
		if e == entry {
			return int32(i)
		}
	}
	return -1
}

// Tag names a group of registry entries.
type Tag struct {
	Name    string
	Entries []string
}

// TaggedRegistry holds the tags defined for one registry.
type TaggedRegistry struct {
	Registry string
	Tags     []Tag
}

// BuiltinTag groups entries of a registry compiled into the client, by the
// client's numeric IDs, since the server has no names for them.
type BuiltinTag struct {
	Name string
	IDs  []int32
}

// BuiltinTaggedRegistry holds the tags defined for one built-in registry.
type BuiltinTaggedRegistry struct {
	Registry string
	Tags     []BuiltinTag
}

// Get returns the registry with the given name.
func Get(name string) (Registry, bool) {
	for _, r := range Current() {
		if r.Name == name {
			return r, true
		}
	}
	return Registry{}, false
}

// itemIDs indexes Items by name once; a player file resolves a name per
// stack, out of some fifteen hundred.
var itemIDs = func() map[string]int32 {
	ids := make(map[string]int32, len(Items))
	for id, name := range Items {
		ids[name] = int32(id)
	}
	return ids
}()

// Qualify gives name the minecraft namespace when it carries none or an empty
// one, the default vanilla's ResourceLocation.parse takes.
func Qualify(name string) string {
	if i := strings.IndexByte(name, ':'); i >= 0 {
		if i == 0 {
			return "minecraft" + name
		}
		return name
	}
	return "minecraft:" + name
}

// ItemID returns an item's numeric ID. An item this version lacks (saved by
// a newer server, or removed) reads as absent rather than as another item.
func ItemID(name string) (int32, bool) {
	id, ok := itemIDs[name]
	return id, ok
}

// ItemName returns the name an item is stored under, given the numeric ID it
// travels as.
func ItemName(id int32) (string, bool) {
	if id < 0 || int(id) >= len(Items) {
		return "", false
	}
	return Items[id], true
}

// Items sharing a block's name but retaining the item description prefix.
// vanilla: Items.WHEAT, Items.createBlockItemWithCustomItemName
var itemDescriptionPrefix = [ItemCount]bool{
	ItemWheat:      true,
	ItemNetherWart: true,
	ItemResinClump: true,
}

// ItemTranslationKey returns the client's description key for an item.
// vanilla: Item.getDescriptionId, Items.registerBlock
func ItemTranslationKey(id int32) (string, bool) {
	name, ok := ItemName(id)
	if !ok {
		return "", false
	}
	kind := "item."
	if _, isBlock := FindState(name, nil); isBlock && !itemDescriptionPrefix[Item(id)] {
		kind = "block."
	}
	return kind + strings.Replace(name, ":", ".", 1), true
}

// entityTypeIDs indexes EntityTypes by name once.
var entityTypeIDs = func() map[string]int32 {
	ids := make(map[string]int32, len(EntityTypes))
	for id, name := range EntityTypes {
		ids[name] = int32(id)
	}
	return ids
}()

// EntityTypeID returns the numeric ID of an entity type, or false for a name
// the registry does not carry — a name the game has no entity behind.
func EntityTypeID(name string) (int32, bool) {
	id, ok := entityTypeIDs[Qualify(name)]
	return id, ok
}

// EntityTypeTagMembers returns the entity type names a built-in tag groups,
// or false for an unknown tag.
func EntityTypeTagMembers(tag string) ([]string, bool) {
	for _, r := range tagsNow.Load().builtin {
		if r.Registry != "minecraft:entity_type" {
			continue
		}
		for _, t := range r.Tags {
			if t.Name != tag {
				continue
			}
			names := make([]string, 0, len(t.IDs))
			for _, id := range t.IDs {
				if id >= 0 && int(id) < len(EntityTypes) {
					names = append(names, EntityTypes[id])
				}
			}
			return names, true
		}
	}
	return nil, false
}

// ItemHasTag reports whether an item ID is in a built-in item tag
// (ItemTags.BREWING_FUEL and the like).
func ItemHasTag(item int32, tag string) bool {
	return tagsNow.Load().item[tag][item]
}

// soundEventIDs indexes SoundEvents by name once.
var soundEventIDs = func() map[string]int32 {
	ids := make(map[string]int32, len(SoundEvents))
	for id, name := range SoundEvents {
		ids[name] = int32(id)
	}
	return ids
}()

// SoundEventID returns a sound event's numeric ID, which the sound packets
// carry since the registry is built into the client; false for an unknown
// name.
func SoundEventID(name string) (int32, bool) {
	id, ok := soundEventIDs[name]
	return id, ok
}

// MustID returns the numeric ID of an entry in a registry. It panics if either
// does not exist, so it is only for entries this package is generated with.
func MustID(registry, entry string) int32 {
	r, ok := Get(registry)
	if !ok {
		panic("unknown registry " + registry)
	}
	id := r.ID(entry)
	if id < 0 {
		panic("unknown entry " + entry + " in registry " + registry)
	}
	return id
}

// MustItem returns an item's numeric ID, panicking on an unknown name; for
// items this package is generated with only.
func MustItem(name string) int32 {
	id, ok := ItemID(name)
	if !ok {
		panic("unknown item " + name)
	}
	return id
}

// MustState returns the block state for a name and property overrides,
// panicking on an unknown combination; for generated blocks only.
func MustState(name string, overrides map[string]string) uint16 {
	state, ok := FindState(name, overrides)
	if !ok {
		panic("unknown block state " + name)
	}
	return state
}
