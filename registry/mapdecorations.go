package registry

import "github.com/golem-mc/golem/internal/protocol"

func init() {
	protocol.ResolveMapDecorationType = func(name string) (string, bool) {
		name = Qualify(name)
		_, _, ok := MapDecorationType(name)
		return name, ok
	}
}

// MapDecorationIsExploration reports whether a marker type is an exploration
// map element: the ones with an item tint.
// vanilla: MapDecorationType.explorationMapElement
func MapDecorationIsExploration(id int32) bool {
	for _, entry := range mapDecorationTypes {
		if entry.id == id {
			return entry.color != -1
		}
	}
	return false
}

// MapDecorationType returns a marker's wire ID and optional item tint (-1 for none).
func MapDecorationType(name string) (id, color int32, ok bool) {
	entry, ok := mapDecorationTypes[name]
	if !ok {
		return 0, -1, false
	}
	return entry.id, entry.color, true
}
