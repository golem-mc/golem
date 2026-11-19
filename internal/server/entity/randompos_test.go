package entity

import (
	"testing"

	"github.com/golem-mc/golem/registry"
)

// vanilla: GoalUtils.hasMalus, WalkNodeEvaluator.getPathTypeStatic
func TestRandomPositionPicksRejectNeighbourHazards(t *testing.T) {
	for _, hazard := range []registry.Block{registry.BlockWater, registry.BlockFire, registry.BlockLava, registry.BlockCactus, registry.BlockSweetBerryBush} {
		t.Run(hazard.Name(), func(t *testing.T) {
			w := newTestWorld()
			w.set(0, 63, 0, registry.BlockSand.DefaultState())
			w.set(1, 64, 0, hazard.DefaultState())
			cow := NewMob(TypeCow, 1, 0.5, 64, 0.5, 1)
			turtle := NewMob(TypeTurtle, 2, 0.5, 64, 0.5, 1)
			if _, ok := defaultRandomPos(w, cow, 0, 0); ok {
				t.Error("stroll accepted the only destination beside a hazard")
			}
			if _, ok := posTowards(w, turtle, 0, 0, [2]float64{10, 0}, turtleHomeAngle); ok {
				t.Error("turtle go-home accepted the only destination beside a hazard")
			}
			w.set(1, 64, 0, 0)
			if _, ok := defaultRandomPos(w, cow, 0, 0); !ok {
				t.Error("stroll rejected the destination after removing the hazard")
			}
			if _, ok := posTowards(w, turtle, 0, 0, [2]float64{10, 0}, turtleHomeAngle); !ok {
				t.Error("turtle go-home rejected the destination after removing the hazard")
			}
		})
	}
}
