package entity

import (
	"sync"

	"github.com/golem-mc/golem/registry"
)

// resolveKinds fills the registry-derived fields of every kinds entry. It
// runs from NewMob, after every init() has filled mobTracking.
var resolveKinds = sync.OnceFunc(func() {
	for t, k := range kinds {
		if k == nil {
			continue
		}
		k.typeID, k.typeIDOK = int32(t), true
		k.updateInterval = MobUpdateInterval(MobType(t))
	}
})

// inTag reports whether the body's type is in the tag; an unknown type is in
// none.
func (m *Mob) inTag(tag registry.EntityTypeTagID) bool {
	id, ok := m.typeIDOK()
	return ok && registry.EntityType(id).InTag(tag)
}

// typeIDOK is the wire entity type ID and whether the registry has the type;
// a kind NewMob has not resolved (a Mob built by hand) is its own Type.
func (m *Mob) typeIDOK() (int32, bool) {
	if k := m.kind(); k.typeIDOK {
		return k.typeID, true
	}
	return int32(m.Type), m.Type != NoMobType
}

// UpdateInterval is the tracker updateInterval of the body's type.
func (m *Mob) UpdateInterval() int {
	if k := m.kind(); k.updateInterval != 0 {
		return k.updateInterval
	}
	return MobUpdateInterval(m.Type)
}

// goneInPeaceful is GoneInPeaceful for the body's own kind.
func (m *Mob) goneInPeaceful() bool { return m.kind().goneInPeaceful() }

// monsterClass is MonsterClassKind for the body's own kind.
func (m *Mob) monsterClass() bool { return m.kind().monsterClass(m.Type) }
