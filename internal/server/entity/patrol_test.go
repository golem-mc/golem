package entity

import (
	"fmt"
	"math/rand"
	"testing"

	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

type patrolTestWorld struct {
	spawnTestWorld
	village   bool
	effective float32
}

func (w patrolTestWorld) CloseToVillage(x, y, z int32, sections int) bool { return w.village }
func (w patrolTestWorld) EffectiveDifficulty(x, z int32) float32          { return w.effective }
func (w patrolTestWorld) OverworldClockTicks() int64                      { return patrolEarlyGameTicks }

func patrolFloor(t *testing.T) patrolTestWorld {
	t.Helper()
	w := patrolTestWorld{spawnTestWorld: newSpawnTestWorld(), effective: 2.1}
	w.bright = true
	w.diff, w.diffUp = 2, true
	stone, _ := registry.FindState("minecraft:stone", nil)
	for x := int32(-60); x <= 60; x++ {
		for z := int32(-60); z <= 60; z++ {
			w.set(x, 63, z, stone)
		}
	}
	return w
}

func TestPatrolSpawnGatesAndTimer(t *testing.T) {
	w := patrolFloor(t)
	player := SpawnPlayer{X: 0, Y: 64, Z: 0}
	for _, test := range []struct {
		name   string
		change func(*patrolTestWorld, *SpawnPlayer)
		want   int
	}{
		{"day", func(*patrolTestWorld, *SpawnPlayer) {}, 4},
		{"night", func(w *patrolTestWorld, _ *SpawnPlayer) { w.bright = false }, 0},
		{"village", func(w *patrolTestWorld, _ *SpawnPlayer) { w.village = true }, 0},
		{"spectator", func(_ *patrolTestWorld, p *SpawnPlayer) { p.Spectator = true }, 0},
		{"unloaded", func(w *patrolTestWorld, _ *SpawnPlayer) { w.loaded = false }, 0},
		{"mushrooms", func(w *patrolTestWorld, _ *SpawnPlayer) { w.biomeName = "minecraft:mushroom_fields" }, 0},
		{"peaceful", func(w *patrolTestWorld, _ *SpawnPlayer) { w.diff, w.diffUp = 0, true }, 0},
		{"light", func(w *patrolTestWorld, _ *SpawnPlayer) {
			for x := int32(-60); x <= 60; x++ {
				for z := int32(-60); z <= 60; z++ {
					w.blockLights[[3]int32{x, 64, z}] = 9
				}
			}
		}, 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			world, p := w, player
			world.blockLights = make(map[[3]int32]uint8)
			test.change(&world, &p)
			found := 0
			for seed := int64(1); seed <= 100; seed++ {
				var s PatrolSpawner
				rng := rand.New(rand.NewSource(seed))
				if !s.Due(rng) || s.nextTick < 11999 || s.nextTick > 13198 {
					t.Fatalf("initial countdown %d", s.nextTick)
				}
				n := 0
				s.Check(world, rng, []SpawnPlayer{p}, func(r PatrolSpawn) {
					if r.Leader != (n == 0) {
						t.Fatal("leader order")
					}
					n++
				})
				if n > 0 {
					found = n
					break
				}
			}
			if found != test.want {
				t.Fatalf("group size %d, want %d", found, test.want)
			}
		})
	}
	s := PatrolSpawner{nextTick: 2}
	rng := rand.New(rand.NewSource(1))
	if s.Due(rng) || !s.Due(rng) {
		t.Fatal("timer did not count down")
	}
}

func TestPatrolSavedFieldsAndDespawn(t *testing.T) {
	w := newTestWorld()
	for _, kind := range []MobType{TypePillager, TypeVindicator, TypeEvoker, TypeWitch, TypeRavager} {
		m := NewMob(kind, 1, 0, 64, 0, 1)
		m.Patrolling, m.PatrolLeader = true, true
		m.PatrolTarget = &Waypoint{X: 100, Y: 64, Z: -200}
		tag := nbt.Compound{}
		m.SaveTo(tag, func() int64 { return 0 })
		back := NewMob(kind, 2, 0, 64, 0, 2)
		back.LoadFrom(tag, w, func() int64 { return 0 })
		if !back.Patrolling || !back.PatrolLeader || back.PatrolTarget == nil || *back.PatrolTarget != *m.PatrolTarget {
			t.Fatalf("%s lost patrol fields", kind)
		}
		m.NoActionTicks = 1000
		for range 10000 {
			if m.despawnStep(w, []Player{{X: 64, Y: 64}}) {
				t.Fatal("patrol randomly despawned inside 128 blocks")
			}
		}
		if !m.despawnStep(w, []Player{{X: 129, Y: 64}}) {
			t.Fatal("patrol survived beyond 128 blocks")
		}
		back.LoadFrom(nbt.Compound{}, w, func() int64 { return 0 })
		if back.Patrolling || back.PatrolLeader || back.PatrolTarget != nil {
			t.Fatal("missing fields did not reset patrol")
		}
	}
}

func TestPatrolGoalCompanionsAndFailure(t *testing.T) {
	for _, indexed := range []bool{false, true} {
		t.Run(fmt.Sprintf("grid=%t", indexed), func(t *testing.T) {
			w := patrolFloor(t)
			leader := NewMob(TypePillager, 1, 0, 64, 0, 1)
			follower := NewMob(TypePillager, 2, 2, 64, 0, 2)
			leader.Patrolling, leader.PatrolLeader = true, true
			leader.PatrolTarget = &Waypoint{X: 100, Y: 64, Z: 0}
			g := patrolGoal{m: leader}
			if indexed {
				grid := takePushGrid(2, nil)
				defer grid.release()
				grid.add(leader, 0)
				grid.add(follower, 1)
				leader.push = grid
			}
			tick := tickView{w: w, others: []*Mob{leader, follower}}
			if !g.CanStart(&tick) {
				t.Fatal("patrol cannot start")
			}
			g.Tick(&tick)
			if leader.navDone() || !follower.Patrolling || follower.PatrolTarget == nil {
				t.Fatal("leader did not navigate and recruit")
			}
			if want := (Waypoint{X: 9, Y: 64, Z: 2}); *follower.PatrolTarget != want {
				t.Fatalf("shared destination %v, want %v", *follower.PatrolTarget, want)
			}
			leader.stopSeeking()
			follower.raidWalk.view.Active = true
			g.Tick(&tick)
			if leader.Patrolling {
				t.Fatal("active raid companion kept patrol alive")
			}
			leader.Patrolling = true
			follower.raidWalk.view.Active = false
			// A wall-filled world cannot produce a path; retry only after 200 ticks.
			blocked := newSpawnTestWorld()
			blocked.allStone = true
			blocked.stoneState, _ = registry.FindState("minecraft:stone", nil)
			tick.w = blocked
			g.Tick(&tick)
			if g.cooldownUntil != leader.Age+200 || g.CanStart(&tick) {
				t.Fatal("failed path has no cooldown")
			}
			leader.Age += 200
			if !g.CanStart(&tick) {
				t.Fatal("patrol failed to resume")
			}
		})
	}
}

func TestPatrolCompanionGridBounds(t *testing.T) {
	w := patrolFloor(t)
	for _, tc := range []struct {
		name               string
		x, y, z            float64
		kind               MobType
		active, dead, want bool
	}{
		{"corner", 16.5, 64, -16.5, TypePillager, false, false, true},
		{"outside x", 16.7, 64, 0, TypePillager, false, false, false},
		{"outside z", 0, 64, -16.7, TypePillager, false, false, false},
		{"above", 0, 81, 0, TypePillager, false, false, true},
		{"outside above", 0, 83, 0, TypePillager, false, false, false},
		{"below", 0, 47, 0, TypePillager, false, false, true},
		{"outside below", 0, 46, 0, TypePillager, false, false, false},
		{"other raider", 2, 64, 0, TypeVindicator, false, false, true},
		{"non raider", 2, 64, 0, TypeCow, false, false, false},
		{"active raid", 2, 64, 0, TypePillager, true, false, false},
		{"dead", 2, 64, 0, TypePillager, false, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			leader := NewMob(TypePillager, 1, 0, 64, 0, 1)
			leader.Patrolling, leader.PatrolLeader = true, true
			leader.PatrolTarget = &Waypoint{X: 100, Y: 64}
			other := NewMob(tc.kind, 2, tc.x, tc.y, tc.z, 2)
			other.raidWalk.view.Active, other.Dead = tc.active, tc.dead
			seen := 0
			grid := takePushGrid(102, &seen)
			defer grid.release()
			grid.add(leader, 0)
			grid.add(other, 1)
			for i := range 100 {
				grid.add(NewMob(TypePillager, int32(i+3), float64(500+i), 64, 0, int64(i)), i+2)
			}
			leader.push = grid
			g := patrolGoal{m: leader}
			g.Tick(&tickView{w: w})
			if leader.Patrolling != tc.want || other.Patrolling != tc.want {
				t.Fatalf("leader patrolling %t, companion recruited %t, want %t", leader.Patrolling, other.Patrolling, tc.want)
			}
			if seen > 2 {
				t.Fatalf("query examined %d mobs, want at most the two nearby mobs", seen)
			}
		})
	}
}
