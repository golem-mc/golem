package entity

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/world"
)

var turtleEggSearchFound bool

// BenchmarkZombieTurtleEggSearch is one zombie's turtle egg search over flat
// ground holding no egg, the case almost every world's zombies are in.
func BenchmarkZombieTurtleEggSearch(b *testing.B) {
	w := world.NewDimensionWorld(b.TempDir(), true, world.NewFlatGenerator())
	defer w.Close()
	// The search reaches 23 cells from the zombie's cell: chunks -2 to 1.
	for cx := int32(-2); cx <= 1; cx++ {
		for cz := int32(-2); cz <= 1; cz++ {
			pos := world.ChunkPos{X: cx, Z: cz}
			w.Acquire(pos)
			defer w.Release(pos)
		}
	}
	g := newZombieAttackTurtleEggGoal(NewMob(TypeZombie, 1, 0.5, world.GroundLevel, 0.5, 1))
	b.ReportAllocs()
	for b.Loop() {
		turtleEggSearchFound = g.find(w)
	}
	if turtleEggSearchFound {
		b.Fatal("the search found an egg on flat ground")
	}
}
