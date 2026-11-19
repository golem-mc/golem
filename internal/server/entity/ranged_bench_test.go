package entity

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/registry"
)

// BenchmarkActiveRangedGoals measures one goal tick with a visible target.
// Positions stay fixed so every iteration strafes or backs off, rather than
// eventually losing the target or walking out of range.
func BenchmarkActiveRangedGoals(b *testing.B) {
	for _, kind := range []MobType{TypeSkeleton, TypePiglin} {
		b.Run(kind.Name(), func(b *testing.B) {
			w := newTestWorld()
			floor(w, -2, -2, 5, 2, 63, defaultState(b, "minecraft:stone"))
			m := NewMob(kind, 1, 0.5, 64, 0.5, 1)
			m.TargetID = 7
			view := &tickView{w: w, players: []Player{{EntityID: 7, X: 3.5, Y: 64, Z: 0.5, Huntable: true}}}
			var goal Goal
			if kind == TypeSkeleton {
				m.MainHand = protocol.ItemStack{ItemID: int32(registry.ItemBow), Count: 1}
				goal = &rangedAttackGoal{m: m, priority: priorityRanged, seeTime: rangedSeeTicks, strafingTime: 0}
			} else {
				m.MainHand = protocol.ItemStack{ItemID: int32(registry.ItemCrossbow), Count: 1}
				goal = newPiglinCrossbowGoal(m, priorityRanged)
			}
			b.ReportAllocs()
			for b.Loop() {
				view.events = view.events[:0]
				m.riderWalk.set = false
				goal.Tick(view)
			}
			if !m.riderWalk.set {
				b.Fatal("the goal did not request a strafe or back-off")
			}
		})
	}
}
