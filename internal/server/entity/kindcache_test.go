package entity

import (
	"testing"

	"github.com/golem-mc/golem/registry"
)

func TestResolvedKindsMatchRegistry(t *testing.T) {
	resolveKinds()
	for i, k := range kinds {
		if k == nil {
			continue
		}
		ty := MobType(i)
		id, ok := registry.EntityTypeID(ty.Name())
		if k.typeID != id || k.typeIDOK != ok {
			t.Errorf("%s: kind has %d,%v, registry %d,%v", ty, k.typeID, k.typeIDOK, id, ok)
		}
		if k.updateInterval != MobUpdateInterval(ty) {
			t.Errorf("%s: update interval %d, want %d", ty, k.updateInterval, MobUpdateInterval(ty))
		}
	}
}
