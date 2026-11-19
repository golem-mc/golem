package entity

import (
	"math"
	"testing"
)

func TestVillagerLookChoosesVisibleNonPassengerTargetsByKind(t *testing.T) {
	w := flatWorld(t)
	m := NewMob(TypeVillager, 1, 0.5, 64, 0.5, 1)
	g := &villagerLookGoal{m: m}
	for _, tc := range []struct {
		k   int
		typ MobType
	}{
		{0, TypeCat}, {1, TypeVillager}, {3, TypeCow}, {4, TypeSquid},
		{6, TypeGlowSquid}, {7, TypeCod}, {8, TypeZombie},
	} {
		far := NewMob(tc.typ, 2, 7.5, 64, 0.5, 2)
		near := NewMob(tc.typ, 3, 3.5, 64, 0.5, 3)
		passenger := NewMob(tc.typ, 4, 0.5, 64, 0.5, 4)
		m.Passengers = []*Mob{passenger}
		tick := &tickView{w: w, others: []*Mob{m, far, near, passenger}}
		if got := g.choose(tick, tc.k).mob; got != near {
			t.Fatalf("choice %d: got %v, want nearest", tc.k, got)
		}
		near.Health = 0
		if got := g.choose(tick, tc.k).mob; got != far {
			t.Fatalf("choice %d picked a dead target", tc.k)
		}
		far.X = 8.51
		if got := g.choose(tick, tc.k).mob; got != nil {
			t.Fatalf("choice %d picked target outside eight blocks", tc.k)
		}
		far.X = 8.5
		if got := g.choose(tick, tc.k).mob; got != far {
			t.Fatalf("choice %d excluded eight-block boundary", tc.k)
		}
		far.X = 3.5
		for y := int32(64); y < 67; y++ {
			w.set(1, y, 0, defaultState(t, "minecraft:stone"))
		}
		if got := g.choose(tick, tc.k).mob; got != nil {
			t.Fatalf("choice %d saw through wall", tc.k)
		}
		for y := int32(64); y < 67; y++ {
			w.set(1, y, 0, 0)
		}
		far.gone = true
		if got := g.choose(tick, tc.k).mob; got != nil {
			t.Fatalf("choice %d picked removed target", tc.k)
		}
	}
	tick := &tickView{w: w, players: []Player{{EntityID: 10, X: 3.5, Y: 64, Z: 0.5}, {EntityID: 11, X: 2.5, Y: 64, Z: 0.5, Spectator: true}}}
	if got := g.choose(tick, 2).player; got != 10 {
		t.Fatalf("player %d", got)
	}
	tick.players[0].Dead = true
	if got := g.choose(tick, 2).player; got != 0 {
		t.Fatal("looked at dead player")
	}
}

func TestVillagerFullAndMinimalLookPackagesAndDurations(t *testing.T) {
	w := flatWorld(t)
	cat := NewMob(TypeCat, 2, 2.5, 64, 0.5, 2)
	tick := &tickView{w: w, others: []*Mob{cat}}
	for _, act := range []villagerActivity{actIdle, actPlay, actMeet, actWork, actRest, actPanic} {
		m := NewMob(TypeVillager, 1, 0.5, 64, 0.5, 1)
		m.vb.activity = act
		g := &villagerLookGoal{m: m}
		looks, pauses := 0, 0
		for range 200 {
			if !g.CanStart(tick) {
				t.Fatal("no pause fallback")
			}
			g.Start(tick)
			if g.waiting {
				pauses++
				if g.left < 30 || g.left > 60 || m.vb.look.mob != nil {
					t.Fatal("invalid pause")
				}
			} else {
				looks++
				if g.left < 45 || g.left > 90 || m.vb.look.mob != cat {
					t.Fatal("invalid gaze")
				}
				for range 3 {
					g.Tick(tick)
					(&villagerLookSink{m: m}).Tick(tick)
				}
				if math.Abs(float64(m.HeadYaw)+90) > 0.01 {
					t.Fatalf("head yaw %v", m.HeadYaw)
				}
			}
			if !g.ShouldContinue(tick) {
				t.Fatal("stopped immediately")
			}
			m.vb.activity = (act + 1) % 6
			if g.ShouldContinue(tick) {
				t.Fatal("look survived activity change")
			}
			m.vb.activity = act
			g.left = 0
			if g.ShouldContinue(tick) {
				t.Fatal("look survived timeout")
			}
			g.Stop(tick)
			if m.vb.look != (villagerLookTarget{}) {
				t.Fatal("stop retained target")
			}
		}
		full := act == actIdle || act == actPlay || act == actMeet
		if full && looks == 0 || !full && looks != 0 || pauses == 0 {
			t.Fatalf("activity %d: %d looks, %d pauses", act, looks, pauses)
		}
		m.Sleeping = true
		if g.CanStart(tick) {
			t.Fatal("sleeping villager looked around")
		}
		m.Sleeping, m.Trading = false, true
		if g.CanStart(tick) {
			t.Fatal("trading villager looked away")
		}
	}
}

func TestWeightedChoicesFavorHigherWeightsAndTryEachOnce(t *testing.T) {
	m := NewMob(TypeVillager, 1, 0, 64, 0, 1)
	counts := [3]int{}
	for range 10000 {
		choices := weightedChoices(m, 8, 2, 2)
		seen := [3]bool{}
		for _, c := range choices {
			if seen[c.kind] {
				t.Fatal("duplicate choice")
			}
			seen[c.kind] = true
		}
		counts[choices[0].kind]++
	}
	if counts[0] < 6400 || counts[0] > 7000 || counts[1] < 1400 || counts[2] < 1400 {
		t.Fatalf("weights 8:2:2 yielded %v", counts)
	}
}

func TestVillagerLookTracksPlayersAndStopsWhenTheyDisappear(t *testing.T) {
	w := flatWorld(t)
	m := NewMob(TypeVillager, 1, 0.5, 64, 0.5, 1)
	tick := &tickView{w: w, players: []Player{{EntityID: 10, X: 5.5, Y: 64, Z: 0.5}}}
	m.vb.look = villagerLookTarget{player: 10}
	g := &villagerLookGoal{m: m}
	g.Start(tick)
	if !g.ShouldContinue(tick) {
		t.Fatal("player not visible")
	}
	for range 3 {
		g.Tick(tick)
		(&villagerLookSink{m: m}).Tick(tick)
	}
	if m.HeadYaw != -90 {
		t.Fatalf("yaw %v", m.HeadYaw)
	}
	tick.players[0].X, tick.players[0].Z = 0.5, 5.5
	for range 3 {
		g.Tick(tick)
		(&villagerLookSink{m: m}).Tick(tick)
	}
	if m.HeadYaw != 0 {
		t.Fatalf("did not follow moving player: %v", m.HeadYaw)
	}
	tick.players = nil
	if g.ShouldContinue(tick) {
		t.Fatal("kept unloaded player")
	}
}

func TestVillagerLookChoosesPlayersUsingFollowRangeAndRawReach(t *testing.T) {
	w := flatWorld(t)
	m := NewMob(TypeVillager, 1, 0.5, 64, 0.5, 1)
	g := &villagerLookGoal{m: m}
	for _, tc := range []struct {
		name      string
		distance  float64
		sneaking  bool
		invisible bool
		want      int32
	}{
		{"standing", 7, false, false, 10},
		{"sneaking", 7, true, false, 10},
		{"sneaking boundary", 8, true, false, 10},
		{"outside raw reach", 8.01, false, false, 0},
		{"sneaking outside raw reach", 8.01, true, false, 0},
		{"invisible minimum reach", 2, false, true, 10},
		{"invisible outside visibility", 2.01, false, true, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tick := &tickView{w: w, players: []Player{{EntityID: 10, X: m.X + tc.distance, Y: 64, Z: 0.5, Sneaking: tc.sneaking, Invisible: tc.invisible}}}
			if got := g.choose(tick, 2).player; got != tc.want {
				t.Fatalf("player %d, want %d", got, tc.want)
			}
		})
	}
}

func TestVillagerHeldLookChecksVisibilityOnlyOnSelectionTicks(t *testing.T) {
	w := &countingWorld{testWorld: flatWorld(t)}
	m := NewMob(TypeVillager, 1, 0.5, 64, 0.5, 1)
	cat := NewMob(TypeCat, 2, 4.5, 64, 0.5, 2)
	tick := &tickView{w: w, others: []*Mob{cat}}
	m.vb.look = villagerLookTarget{mob: cat}
	g, sink := &villagerLookGoal{m: m, left: 50}, &villagerLookSink{m: m}
	sel := goalSelector{goals: []Goal{g, sink}, running: []Goal{g, sink}}
	if !m.vb.look.visible(m, tick) || w.reads == 0 {
		t.Fatal("target not visible")
	}
	rayReads := w.reads
	w.reads = 0
	sel.tick(tick, 0)
	if w.reads != rayReads {
		t.Fatalf("selection tick: %d block reads, want one ray's %d", w.reads, rayReads)
	}
	w.reads = 0
	sel.tickRunningGoals(tick, 0)
	if w.reads != 0 {
		t.Fatalf("intermediate tick raycast: %d block reads", w.reads)
	}
	cat.Health = 0
	sel.tick(tick, 0)
	if m.vb.look != (villagerLookTarget{}) || sel.isRunning(sink) {
		t.Fatal("sink retained a dead target after selection")
	}
}

func TestIdleVillagerWalksToItsLookTargetAndReplans(t *testing.T) {
	w := flatWorld(t)
	m := NewMob(TypeVillager, 1, 0.5, 64, 0.5, 1)
	tick := &tickView{w: w, players: []Player{{EntityID: 10, X: 5.5, Y: 64, Z: 0.5}}}
	m.vb.look = villagerLookTarget{player: 10}
	g := &lookWalkGoal{m: m}
	started := false
	for range 100 {
		if g.CanStart(tick) {
			started = true
			break
		}
	}
	if !started {
		t.Fatal("never walked to look target")
	}
	g.Start(tick)
	if !g.ShouldContinue(tick) || g.destination.X != 5 {
		t.Fatal("no walk toward player")
	}
	// A copied tracker survives expiration of LOOK_TARGET.
	m.vb.look = villagerLookTarget{}
	tick.players[0].X = 8.5
	g.Tick(tick)
	if g.destination.X != 8 {
		t.Fatal("did not replan moving player")
	}
	m.vb.activity = actWork
	if g.ShouldContinue(tick) {
		t.Fatal("idle walk continued at work")
	}
	m.vb.activity = actIdle
	tick.players = nil
	g.Tick(tick)
	if g.left != 0 || g.ShouldContinue(tick) {
		t.Fatal("walk retained missing target")
	}
	g.Stop(tick)
	if len(m.navOf().nav.path) != 0 {
		t.Fatal("stop retained navigation")
	}
}

func TestSolitaryChildCanWalkTowardPlayerLookTarget(t *testing.T) {
	w := flatWorld(t)
	tick := &tickView{w: w, players: []Player{{EntityID: 10, X: 5.5, Y: 64, Z: 0.5}}}
	for id := int32(1); id < 200; id++ {
		m := playingChild(id, 0.5)
		m.vb.look = villagerLookTarget{player: 10}
		g := &kidPlayGoal{m: m}
		if !g.CanStart(tick) || g.look.player == 0 {
			continue
		}
		g.Start(tick)
		if !g.ShouldContinue(tick) || g.destination.X != 5 || m.vb.kids.interaction != nil {
			t.Fatal("player look walk created wrong target")
		}
		tick.players[0].X = 8.5
		g.Tick(tick)
		if g.destination.X != 8 {
			t.Fatal("child did not replan player walk")
		}
		tick.players = nil
		g.Tick(tick)
		if g.left != 0 || g.ShouldContinue(tick) {
			t.Fatal("child retained missing player")
		}
		return
	}
	t.Fatal("child never selected player look walk")
}

func TestSolitaryChildDoesNotInventALookTarget(t *testing.T) {
	w := flatWorld(t)
	cow := NewMob(TypeCow, 50, 4.5, 64, 0.5, 50)
	tick := &tickView{w: w, others: []*Mob{cow}}
	for id := int32(1); id <= 100; id++ {
		m := playingChild(id, 0.5)
		g := &kidPlayGoal{m: m}
		if g.CanStart(tick) && (g.target == cow || g.look != (villagerLookTarget{})) {
			t.Fatal("look walk invented a target without LOOK_TARGET")
		}
	}
}

func TestChildLookSelectionRunsAlongsidePlayMovement(t *testing.T) {
	w := flatWorld(t)
	m := playingChild(1, 0.5)
	cow := NewMob(TypeCow, 50, 4.5, 64, 0.5, 50)
	tick := &tickView{w: w, others: []*Mob{cow}}
	seen := false
	for range 100 {
		m.ai.tick(tick, 0)
		for _, g := range m.ai.running {
			if _, ok := g.(*kidPlayGoal); ok && m.vb.look.mob == cow {
				seen = true
			}
		}
	}
	if !seen {
		t.Fatal("play movement starved weighted look selection")
	}
}

func TestVillagerLookTrackerFloorsNegativeBlockCoordinates(t *testing.T) {
	tick := &tickView{players: []Player{{EntityID: 10, X: -0.2, Y: 64.9, Z: -5.2}}}
	if p, ok := (villagerLookTarget{player: 10}).cell(tick); !ok || p != (Waypoint{-1, 64, -6}) {
		t.Fatalf("block position %v, present %v", p, ok)
	}
}
