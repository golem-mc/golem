package registry

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/nbt"
)

// typedEntityDataOf reads an entity_data or block_entity_data value: a
// compound naming its type under "id", which lookup resolves, the rest the
// entity's tag.
// vanilla: TypedEntityData.codec
func typedEntityDataOf(value nbt.Tag, lookup func(string) (int32, bool)) (int32, nbt.CompoundTag, bool) {
	tag, ok := compoundTagOrSNBT(value)
	if !ok {
		return 0, nil, false
	}
	name, ok := tag.Entries()["id"].(nbt.String)
	if !ok || !protocol.ValidIdentifier(string(name)) {
		return 0, nil, false
	}
	id, ok := lookup(Qualify(string(name)))
	if !ok {
		return 0, nil, false
	}
	rest := nbt.ShallowCopyCompound(tag)
	rest.Delete("id")
	return id, rest, true
}

// typedEntityDataTag writes typed entity data back with its type under "id".
func typedEntityDataTag(name string, tag nbt.CompoundTag) nbt.CompoundTag {
	out := nbt.ShallowCopyCompound(tag)
	out.Set("id", nbt.String(name))
	return out
}

// compoundTagOrSNBT reads CustomData.COMPOUND_TAG_CODEC retaining history.
func compoundTagOrSNBT(value nbt.Tag) (nbt.CompoundTag, bool) {
	switch v := value.(type) {
	case nbt.CompoundTag:
		return v, true
	case nbt.String:
		tag, err := nbt.ParseOrderedSNBT(string(v))
		return tag, err == nil
	}
	return nil, false
}

// compoundOrSNBT reads a compound, or an SNBT string holding one.
// vanilla: CustomData.COMPOUND_TAG_CODEC
func compoundOrSNBT(value nbt.Tag) (nbt.Compound, bool) {
	tag, ok := compoundTagOrSNBT(value)
	if !ok {
		return nil, false
	}
	view := tag.Entries()
	// A present empty guard still requires a block entity.
	if view == nil {
		view = nbt.Compound{}
	}
	return view, true
}
