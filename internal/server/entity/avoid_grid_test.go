package entity

import (
	"math/rand"
	"testing"
)

// TestAvoidThreatGridMatchesHerdScan checks that the grid query finds the
// threat the whole-herd scan does, over random herds that put threats on the
// edge of the reach.
func TestAvoidThreatGridMatchesHerdScan(t *testing.T) {
	w := patrolFloor(t)
	rng := rand.New(rand.NewSource(7))
	kinds := []MobType{TypeWolf, TypeZombie, TypeCow, TypeSkeleton, TypeCat, TypeCreaking}
	for round := range 30 {
		herd := make([]*Mob, 0, 120)
		rabbit := NewMob(TypeRabbit, 1, 0, 64, 0, 1)
		herd = append(herd, rabbit)
		for i := range 119 {
			x := float64(rng.Intn(41)-20) + rng.Float64()*float64(rng.Intn(2))
			z := float64(rng.Intn(41)-20) + rng.Float64()*float64(rng.Intn(2))
			y := 64 + float64(rng.Intn(9)-4)
			herd = append(herd, NewMob(kinds[rng.Intn(len(kinds))], int32(i+2), x, y, z, int64(i)))
		}
		grid := takePushGrid(len(herd), nil)
		grid.build(herd)
		for _, avoid := range []avoidKind{avoidWolves, avoidMonsters} {
			rg := &rabbitAvoidGoal{m: rabbit, avoid: avoid, maxDist: 10}
			if avoid == avoidMonsters {
				rg.maxDist = 4
			}
			tv := &tickView{w: w, others: herd}
			rabbit.push = nil
			x, y, z, ok := rg.nearest(tv)
			rabbit.push = grid
			gx, gy, gz, gok := rg.nearest(tv)
			if ok != gok || x != gx || y != gy || z != gz {
				t.Fatalf("round %d: scan (%v %v %v %v), grid (%v %v %v %v)", round, x, y, z, ok, gx, gy, gz, gok)
			}
		}
		for _, mobs := range [][]MobType{{TypeWolf}, {TypeCat, TypeCreaking}} {
			ag := &avoidGoal{m: rabbit, active: func() bool { return true }, maxDist: 8, mobs: mobs}
			tv := &tickView{w: w, others: herd}
			rabbit.push = nil
			x, y, z, _, mob, ok := ag.nearestThreat(tv)
			rabbit.push = grid
			gx, gy, gz, _, gmob, gok := ag.nearestThreat(tv)
			if ok != gok || x != gx || y != gy || z != gz || mob != gmob {
				t.Fatalf("round %d: scan (%v %v %v %v), grid (%v %v %v %v)", round, x, y, z, ok, gx, gy, gz, gok)
			}
		}
		rabbit.push = nil
		grid.release()
	}
}
