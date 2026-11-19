package entity

import "testing"

func frogScene(t *testing.T) (*Mob, *Mob, *tickView) {
	t.Helper()
	w := newTestWorld()
	floor(w, -12, -12, 12, 12, 63, defaultState(t, "minecraft:stone"))
	frog := NewMob(TypeFrog, 1, 0.5, 64, 0.5, 1)
	prey := NewMob(TypeSlime, 2, 1.5, 64, 0.5, 2)
	prey.SetSize(1, true)
	frog.OnGround, prey.OnGround = true, true
	return frog, prey, &tickView{w: w, others: []*Mob{frog, prey}}
}

func TestFrogTongueTimingAndPanic(t *testing.T) {
	m, o, v := frogScene(t)
	m.frog.scan = 0
	m.frogBrainStep(v)
	g := &frogTongueGoal{m: m}
	if !g.CanStart(v) {
		t.Fatal("nearby prey rejected")
	}
	if m.FrogTongueTarget() != 0 {
		t.Fatal("synched target set before starting")
	}
	g.Start(v)
	g.Tick(v)
	if !m.FrogUsingTongue() || o.VX != -0.75 {
		t.Fatalf("pose=%v pull=%v", m.FrogUsingTongue(), o.VX)
	}
	v.events = nil
	for range 6 {
		g.Tick(v)
	}
	if len(v.events) != 0 {
		t.Fatal("ate before catch animation finished")
	}
	g.Tick(v)
	if len(v.events) != 2 || v.events[1].Kind != MobAttackedPlayer || v.events[1].Damage != 10 {
		t.Fatalf("eat events=%+v", v.events)
	}
	for range 4 {
		g.Tick(v)
	}
	if m.FrogUsingTongue() || m.FrogTongueTarget() != 0 {
		t.Fatal("animation did not clear")
	}
	m.frog.target = o.ID
	g.Start(v)
	m.ai.running = append(m.ai.running, &panicGoal{m: m})
	g.Tick(v)
	if m.FrogTongueTarget() != 0 || m.FrogUsingTongue() {
		t.Fatal("panic retained tongue")
	}
}

func TestFrogTargetGates(t *testing.T) {
	for _, gate := range []string{"size", "love", "distance", "wall", "dead", "invulnerable"} {
		t.Run(gate, func(t *testing.T) {
			m, o, v := frogScene(t)
			switch gate {
			case "size":
				o.SetSize(2, true)
			case "love":
				m.InLove = 100
			case "distance":
				o.X = 10.5
			case "wall":
				v.w.(testWorld).set(1, 64, 0, defaultState(t, "minecraft:stone"))
			case "dead":
				o.Dead = true
			case "invulnerable":
				o.Invulnerable = true
			}
			m.frog.scan = 0
			m.frogBrainStep(v)
			if m.frog.target != 0 {
				t.Fatal("ineligible prey targeted")
			}
		})
	}
}

func TestFrogUnreachableMemory(t *testing.T) {
	m, o, v := frogScene(t)
	w := v.w.(testWorld)
	stone := defaultState(t, "minecraft:stone")
	// Visible above a solid column, but more than 1.75 blocks from any path endpoint.
	o.X, o.Y = 4.5, 68
	for y := int32(64); y < 68; y++ {
		floor(w, 3, -1, 5, 1, y, stone)
	}
	g := &frogTongueGoal{m: m}
	m.frog.target = o.ID
	if g.CanStart(v) {
		t.Fatal("unreachable prey accepted")
	}
	if len(m.frog.unreachable) != 1 || m.frog.expiry != 100 {
		t.Fatalf("memory=%+v", m.frog)
	}
	o.X, o.Y = 1.5, 64
	for range 99 {
		m.frogBrainStep(v)
	}
	if m.frog.target != 0 {
		t.Fatal("retried before memory expired")
	}
	m.frog.scan = 0
	m.frogBrainStep(v)
	if m.frog.target != o.ID {
		t.Fatal("did not retry expired memory")
	}
}

func TestFrogUnreachableMemoryEvictsOldest(t *testing.T) {
	m, o, v := frogScene(t)
	o.X, o.Y = 4.5, 68
	w := v.w.(testWorld)
	for y := int32(64); y < 68; y++ {
		floor(w, 3, -1, 5, 1, y, defaultState(t, "minecraft:stone"))
	}
	g := &frogTongueGoal{m: m}
	for id := int32(2); id <= 7; id++ {
		o.ID, m.frog.target = id, id
		if g.CanStart(v) {
			t.Fatal("unreachable target accepted")
		}
	}
	if len(m.frog.unreachable) != 5 || m.frog.unreachable[0] != 3 || m.frog.unreachable[4] != 7 {
		t.Fatalf("unreachable=%v", m.frog.unreachable)
	}
}

func TestFrogSelectsNearestVisiblePrey(t *testing.T) {
	m, o, v := frogScene(t)
	farther := NewMob(TypeMagmaCube, 3, 4.5, 64, 0.5, 3)
	farther.SetSize(1, true)
	v.others = []*Mob{m, farther, o}
	m.frog.scan = 0
	m.frogBrainStep(v)
	if m.frog.target != o.ID {
		t.Fatalf("target=%d, want %d", m.frog.target, o.ID)
	}
}

func TestFrogTongueTimeoutAndLostPrey(t *testing.T) {
	for _, lost := range []bool{false, true} {
		m, o, v := frogScene(t)
		o.X = 5.5
		m.frog.target = o.ID
		g := &frogTongueGoal{m: m}
		if !g.CanStart(v) {
			t.Fatal("reachable prey rejected")
		}
		g.Start(v)
		if lost {
			v.others = []*Mob{m}
		}
		for range 102 {
			g.Tick(v)
		}
		if m.FrogTongueTarget() != 0 || m.FrogUsingTongue() {
			t.Fatalf("lost=%v: attack retained", lost)
		}
		if len(v.events) != 0 {
			t.Fatal("ate absent or distant prey")
		}
	}
}

func TestFrogTonguePullAtSamePosition(t *testing.T) {
	m, o, v := frogScene(t)
	o.X, o.VX = m.X, 1
	m.frog.target = o.ID
	g := &frogTongueGoal{m: m}
	if !g.CanStart(v) {
		t.Fatal("prey in same cell rejected")
	}
	g.Start(v)
	g.Tick(v)
	if o.VX != 0 || o.VY != 0 || o.VZ != 0 {
		t.Fatalf("pull=(%v,%v,%v)", o.VX, o.VY, o.VZ)
	}
}

func TestPregnantFrogFindsShoreAndLaysOnce(t *testing.T) {
	m, _, v := frogScene(t)
	w := v.w.(testWorld)
	w.set(5, 63, 0, defaultState(t, "minecraft:water"))
	m.pregnant = true
	g := &frogLaySpawnGoal{m: m}
	if !g.CanStart(v) {
		t.Fatal("pregnant frog refused")
	}
	g.Start(v)
	g.Tick(v)
	if m.navDone() {
		t.Fatal("no walk to nearby shore")
	}
	if len(v.events) != 0 {
		t.Fatal("laid away from shore")
	}
	m.X, m.Y, m.Z = 4.5, 64, 0.5
	m.OnGround = false
	g.Tick(v)
	if len(v.events) != 0 {
		t.Fatal("laid while airborne")
	}
	m.OnGround = true
	g.Tick(v)
	if m.pregnant || len(v.events) != 1 || v.events[0].Kind != MobLaidFrogspawn || v.events[0].X != 5 || v.events[0].Y != 64 || v.events[0].Z != 0 {
		t.Fatalf("pregnant=%v events=%+v", m.pregnant, v.events)
	}
	if g.ShouldContinue(v) {
		t.Fatal("laying continued after pregnancy ended")
	}
}

func TestFrogspawnLayingRequiresOpenSourceWater(t *testing.T) {
	m, _, v := frogScene(t)
	w := v.w.(testWorld)
	for _, tc := range []struct {
		name         string
		below, above uint16
		want         bool
	}{
		{"source", defaultState(t, "minecraft:water"), 0, true},
		{"flowing", stateOf(t, "minecraft:water", "level=1"), 0, false},
		{"blocked", defaultState(t, "minecraft:water"), defaultState(t, "minecraft:stone"), false},
		{"solid top", stateOf(t, "minecraft:oak_slab", "type=top,waterlogged=true"), 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w.set(1, 63, 0, tc.below)
			w.set(1, 64, 0, tc.above)
			_, ok := frogSpawnCell(w, m.cell())
			if ok != tc.want {
				t.Fatalf("can lay=%v, want %v", ok, tc.want)
			}
		})
	}
}

// TestAFrogSpawnsOnFrogsSpawnableOn checks Frog.checkFrogSpawnRules: the
// ground is the frogs_spawnable_on tag, not just grass.
func TestAFrogSpawnsOnFrogsSpawnableOn(t *testing.T) {
	spawnGroundCases(t, TypeFrog, map[string]bool{
		"minecraft:mud":            true,
		"minecraft:mangrove_roots": true,
		"minecraft:grass_block":    true,
		"minecraft:sand":           false,
	})
}
