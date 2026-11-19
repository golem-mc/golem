package registry

import "strings"

// spawnEggEntity maps a spawn egg item to its entity type, built once.
// Vanilla's Items.registerSpawnEgg registers every egg as
// <entity type>_spawn_egg, so the name is the mapping.
var spawnEggEntity = func() *[ItemCount]string {
	t := new([ItemCount]string)
	for i := range Item(ItemCount) {
		entity, ok := strings.CutSuffix(i.Name(), "_spawn_egg")
		if !ok {
			continue
		}
		if _, ok := EntityTypeID(entity); ok {
			t[i] = entity
		}
	}
	return t
}()

// SpawnEggEntity is the entity type a spawn egg's default entity_data
// component names.
func SpawnEggEntity(item Item) (string, bool) {
	if item < 0 || item >= ItemCount || spawnEggEntity[item] == "" {
		return "", false
	}
	return spawnEggEntity[item], true
}
