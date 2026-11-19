package server

import (
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
)

// adventureTarget defers entity serialization until a predicate needs NBT.
func adventureTarget(w *world.World, x, y, z int32, state uint16) rules.AdventureTarget {
	return rules.AdventureTarget{
		State: state,
		ReadNBT: func() nbt.Compound {
			tag, _ := w.BlockEntityTag(x, y, z)
			return tag
		},
	}
}
