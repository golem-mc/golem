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

func TestRandomPositionMalusUsesMobPrices(t *testing.T) {
	for _, tc := range []struct {
		name     string
		kind     MobType
		block    registry.Block
		rejected bool
	}{
		{"cow water", TypeCow, registry.BlockWater, true},
		{"turtle water", TypeTurtle, registry.BlockWater, false},
		{"drowned water", TypeDrowned, registry.BlockWater, false},
		{"strider lava", TypeStrider, registry.BlockLava, false},
		{"strider fire", TypeStrider, registry.BlockFire, false},
		{"fox berries", TypeFox, registry.BlockSweetBerryBush, false},
		{"turtle berries", TypeTurtle, registry.BlockSweetBerryBush, true},
		{"turtle powder snow", TypeTurtle, registry.BlockPowderSnow, true},
		{"turtle honey", TypeTurtle, registry.BlockHoneyBlock, true},
		{"turtle closed door", TypeTurtle, registry.BlockOakDoor, true},
		{"cow rail", TypeCow, registry.BlockRail, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := newTestWorld()
			w.set(0, 63, 0, registry.BlockSand.DefaultState())
			w.set(0, 64, 0, tc.block.DefaultState())
			m := NewMob(tc.kind, 1, 0.5, 64, 0.5, 1)
			if got := hasMalus(w, m, Waypoint{0, 64, 0}); got != tc.rejected {
				t.Errorf("hasMalus = %v, want %v", got, tc.rejected)
			}
		})
	}
}

func TestRandomPositionMalusReadsFloorHazards(t *testing.T) {
	for _, block := range []registry.Block{registry.BlockHoneyBlock, registry.BlockMagmaBlock, registry.BlockSweetBerryBush} {
		t.Run(block.Name(), func(t *testing.T) {
			w := newTestWorld()
			w.set(0, 63, 0, block.DefaultState())
			m := NewMob(TypeTurtle, 1, 0.5, 64, 0.5, 1)
			if !hasMalus(w, m, Waypoint{0, 64, 0}) {
				t.Error("accepted the cell over a harmful floor")
			}
		})
	}
	w := newTestWorld()
	w.set(1, 64, 0, registry.BlockWater.DefaultState())
	m := NewMob(TypeCow, 1, 0.5, 64, 0.5, 1)
	if hasMalus(w, m, Waypoint{0, 64, 0}) {
		t.Error("an unsupported open cell acquired a neighbour's malus")
	}
}
