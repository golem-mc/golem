package registry

// EntityType is an entity type's protocol ID in the minecraft:entity_type
// registry.
type EntityType int32

// Name returns the type's registry name, or "" out of range.
func (t EntityType) Name() string {
	if t < 0 || int(t) >= len(EntityTypes) {
		return ""
	}
	return EntityTypes[t]
}

// String returns Name.
func (t EntityType) String() string { return t.Name() }

// EntityTypeByName returns the entity type of a registry name.
func EntityTypeByName(name string) (EntityType, bool) {
	id, ok := entityTypeIDs[name]
	return EntityType(id), ok
}
