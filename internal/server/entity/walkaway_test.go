package entity

import (
	"testing"

	"github.com/golem-mc/golem/registry"
)

// vanilla: SetWalkTargetAwayFrom.create, LandRandomPos.movePosUpOutOfSolid
func TestWalkTargetAwayFromKeepsWalkEndsOutOfWater(t *testing.T) {
	w := newTestWorld()
	// Solid picks below the pond must climb to its water and be rejected,
	// rather than being resolved by pathfinding to a submerged destination.
	for y := int32(56); y <= 63; y++ {
		floor(w, -24, -24, 24, 24, y, registry.BlockStone.DefaultState())
	}
	floor(w, 3, -24, 24, 24, 64, registry.BlockWater.DefaultState())
	floor(w, -24, -24, 2, 24, 64, registry.BlockStone.DefaultState())
	for _, name := range []string{"hoglin retreat", "hoglin repellent", "piglin retreat", "piglin repellent"} {
		t.Run(name, func(t *testing.T) {
			walks := 0
			for seed := range int64(100) {
				typ := TypeHoglin
				if name == "piglin retreat" || name == "piglin repellent" {
					typ = TypePiglin
				}
				m := NewMob(typ, 1, 1.5, 65, 0.5, seed)
				m.OnGround = true
				threat := NewMob(TypePiglin, 2, -1.5, 65, 0.5, 1)
				tick := &tickView{w: w, others: []*Mob{m, threat}}
				switch name {
				case "hoglin retreat":
					m.hoglin.retreatFrom = threat.ID
					(&hoglinRetreatGoal{m: m}).flee(tick)
				case "piglin retreat":
					m.barter.from = threat.ID
					(&retreatGoal{m: m}).flee(tick)
				default:
					g := hoglinRepelGoal{m: m, pace: 1, source: func() (Waypoint, bool) {
						return Waypoint{-2, 65, 0}, true
					}}
					g.CanStart(tick)
				}
				nav := &m.navOf().nav
				if len(nav.path) == 0 {
					continue
				}
				walks++
				end := nav.path[len(nav.path)-1]
				if registry.BlockOf(w.BlockAt(end.X, end.Y, end.Z)) == registry.BlockWater {
					t.Fatalf("seed %d: walk ends in water at %v (pick %v)", seed, end, nav.end)
				}
				if registry.IsSolid(w.BlockAt(nav.end.X, nav.end.Y, nav.end.Z)) {
					t.Fatalf("seed %d: picked solid cell %v", seed, nav.end)
				}
			}
			if walks == 0 {
				t.Fatal("never chose a dry walk away along the bank")
			}
		})
	}
}
