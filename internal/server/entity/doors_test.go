package entity

import (
	"fmt"
	"testing"

	"github.com/golem-mc/golem/registry"
)

// doorState is a door half of the block named, facing south and closed or open.
func doorState(t *testing.T, name, half string, open bool) uint16 {
	t.Helper()
	state, ok := registryDoor(name, half, open)
	if !ok {
		t.Fatalf("no state for %s %s", name, half)
	}
	return state
}

// doorWall walls the line x = 5 across z -25..25 with stone, two high,
// leaving a one-cell doorway at z = 0 that holds the door given.
func doorWall(t *testing.T, name string, open bool) testWorld {
	w := newTestWorld()
	stone := defaultState(t, "minecraft:stone")
	floor(w, -25, -25, 25, 25, 63, stone)
	wall(w, stone, 5, -25, 25, 0, 0, 64)
	w.set(5, 64, 0, doorState(t, name, "lower", open))
	w.set(5, 65, 0, doorState(t, name, "upper", open))
	return w
}

// TestAPathRunsThroughDoorsOnlyAsTheBodyAllows plans across a wall whose one
// gap holds a door: an open door passes every body, a closed wooden one only
// a body that opens doors, and an iron one never.
// vanilla: WalkNodeEvaluator.getPathTypeWithinMobBB
func TestAPathRunsThroughDoorsOnlyAsTheBodyAllows(t *testing.T) {
	for _, tc := range []struct {
		name  string
		open  bool
		opens bool
		want  bool
	}{
		{"minecraft:oak_door", false, false, false},
		{"minecraft:oak_door", false, true, true},
		{"minecraft:oak_door", true, false, true},
		{"minecraft:iron_door", false, true, false},
		{"minecraft:iron_door", true, false, true},
	} {
		w := doorWall(t, tc.name, tc.open)
		f := strideOf(t, 0.6, 1.95)
		f.openDoors = tc.opens
		// The doorway's only way through is the door; a wall this long is
		// beyond the search's range around it.
		path, found := findPath(w, f, Waypoint{2, 64, 0}, Waypoint{8, 64, 0}, 8, 0)
		reached := found && len(path) > 0 && path[len(path)-1] == (Waypoint{8, 64, 0})
		if reached != tc.want {
			t.Errorf("%s open=%v opensDoors=%v: reached=%v, want %v", tc.name, tc.open, tc.opens, reached, tc.want)
		}
	}
}

// TestWhoOpensDoors pins the bodies whose navigation opens doors.
func TestWhoOpensDoors(t *testing.T) {
	zombie := NewMob(TypeZombie, 1, 0.5, 64, 0.5, 1)
	if zombie.canOpenDoors() {
		t.Error("a zombie that cannot break doors opens them")
	}
	zombie.CanBreakDoors = true
	if !zombie.canOpenDoors() {
		t.Error("a zombie that breaks doors does not open them")
	}
	if !NewMob(TypeVillager, 2, 0.5, 64, 0.5, 1).canOpenDoors() {
		t.Error("a villager does not open doors")
	}
	if !NewMob(TypeCopperGolem, 5, 0.5, 64, 0.5, 1).canOpenDoors() {
		t.Error("a copper golem does not open doors")
	}
	if NewMob(TypeCow, 3, 0.5, 64, 0.5, 1).canOpenDoors() {
		t.Error("a cow opens doors")
	}
	vindicator := NewMob(TypeVindicator, 4, 0.5, 64, 0.5, 1)
	if vindicator.canOpenDoors() {
		t.Error("a vindicator outside a raid opens doors")
	}
	vindicator.InRaid = true
	if !vindicator.canOpenDoors() {
		t.Error("a vindicator in a raid does not open doors")
	}
}

// doorMob stands a mob at the door's side facing a path through it, having
// run into the door.
func doorMob(m *Mob, door Waypoint) {
	m.X, m.Y, m.Z = float64(door.X)-0.3, float64(door.Y), float64(door.Z)+0.5
	m.horizontalCollision = true
	m.nav.path = []Waypoint{{door.X, door.Y, door.Z}}
}

// TestAnOpenDoorGoalOpensTheDoorAndShutsItBehind runs the open-door goal across
// a closed door: it starts on the collision, queues the opening, and queues
// the closing once the body is through.
// vanilla: OpenDoorGoal.start, DoorInteractGoal.tick, OpenDoorGoal.stop
func TestAnOpenDoorGoalOpensTheDoorAndShutsItBehind(t *testing.T) {
	w := doorWall(t, "minecraft:oak_door", false)
	tv := &tickView{w: w}
	m := NewMob(TypeVindicator, 1, 0, 64, 0, 1)
	doorMob(m, Waypoint{5, 64, 0})
	g := newOpenDoorGoal(m, 6, true, nil)

	m.horizontalCollision = false
	if g.CanStart(tv) {
		t.Fatal("the goal starts without a collision")
	}
	m.horizontalCollision = true
	if !g.CanStart(tv) {
		t.Fatal("the goal does not start at a closed door in the path")
	}
	g.Start(tv)
	ops := m.TakeDoorOps()
	if len(ops) != 1 || ops[0].Kind != DoorOpOpen || ops[0].Y != 65 {
		t.Fatalf("start queued %+v, want one open of the head cell", ops)
	}
	if !g.ShouldContinue(tv) {
		t.Fatal("the goal ends before the body passes")
	}
	m.X = 5.9
	g.Tick(tv)
	if g.ShouldContinue(tv) {
		t.Fatal("the goal goes on after the body passed the door")
	}
	g.Stop(tv)
	if ops := m.TakeDoorOps(); len(ops) != 1 || ops[0].Kind != DoorOpClose {
		t.Fatalf("stop queued %+v, want one close", ops)
	}
}

func hardGriefingWorld(w testWorld) griefingWorld {
	w.diffUp, w.diff = true, 3
	return griefingWorld{w}
}

// TestAZombieBreaksADoorOnHard runs the break goal the full 240 ticks: the
// crack stages climb from 0 to 10, and the door is queued for removal at the
// last tick, but only on hard, with mob griefing on, and for a zombie that
// can break doors.
// vanilla: BreakDoorGoal.tick
func TestAZombieBreaksADoorOnHard(t *testing.T) {
	base := doorWall(t, "minecraft:oak_door", false)
	door := Waypoint{5, 64, 0}

	run := func(w World, canBreak bool) (started bool, ops []DoorOp) {
		m := NewMob(TypeZombie, 1, 0, 64, 0, 1)
		m.CanBreakDoors = canBreak
		doorMob(m, door)
		g := newZombieBreakDoorGoal(m)
		tv := &tickView{w: w}
		if !g.CanStart(tv) {
			return false, nil
		}
		g.Start(tv)
		for i := 0; i < doorBreakTime && g.ShouldContinue(tv); i++ {
			g.Tick(tv)
		}
		return true, m.TakeDoorOps()
	}

	started, ops := run(hardGriefingWorld(base), true)
	if !started {
		t.Fatal("the goal does not start on hard")
	}
	var stages []int32
	broke := 0
	for _, op := range ops {
		switch op.Kind {
		case DoorOpProgress:
			stages = append(stages, op.Stage)
		case DoorOpBreak:
			broke++
		}
	}
	if broke != 1 {
		t.Errorf("the door was broken %d times, want once", broke)
	}
	if len(stages) != 11 || stages[0] != 0 || stages[10] != 10 {
		t.Errorf("crack stages %v, want 0 through 10", stages)
	}

	if ok, _ := run(hardGriefingWorld(base), false); ok {
		t.Error("a zombie that cannot break doors starts the goal")
	}
	normal := griefingWorld{base}
	normal.diffUp, normal.diff = true, 2
	if ok, _ := run(normal, true); ok {
		t.Error("the goal starts on normal")
	}
	if ok, _ := run(base, true); ok {
		t.Error("the goal starts with mob griefing off")
	}
	open := doorWall(t, "minecraft:oak_door", true)
	if ok, _ := run(hardGriefingWorld(open), true); ok {
		t.Error("the goal starts at an open door")
	}
}

// TestAVillagerOpensDoorsOnItsPathAndShutsThemBehind runs InteractWithDoor
// along a path through a closed door: it opens the door at the next node
// without a collision, and shuts it once the villager has moved on.
// vanilla: InteractWithDoor.create
func TestAVillagerOpensDoorsOnItsPathAndShutsThemBehind(t *testing.T) {
	w := doorWall(t, "minecraft:oak_door", false)
	tv := &tickView{w: w}
	m := NewMob(TypeVillager, 1, 3, 64, 0.5, 1)
	m.nav.origin = Waypoint{3, 64, 0}
	m.nav.path = []Waypoint{{4, 64, 0}, {5, 64, 0}, {6, 64, 0}}
	g := newInteractWithDoor(m)

	g.CanStart(tv)
	if ops := m.TakeDoorOps(); len(ops) != 0 {
		t.Fatalf("queued %+v before any door is next", ops)
	}
	m.nav.advance()
	g.CanStart(tv)
	ops := m.TakeDoorOps()
	if len(ops) != 1 || ops[0].Kind != DoorOpOpen || ops[0].Y != 64 {
		t.Fatalf("queued %+v with the door next, want one open", ops)
	}
	// The door is open now and the villager steps through it.
	w.set(5, 64, 0, doorState(t, "minecraft:oak_door", "lower", true))
	m.X = 5.5
	m.nav.advance()
	m.nav.advance()
	for range doorRerunCooldown {
		g.CanStart(tv)
		m.TakeDoorOps()
	}
	m.X = 7.5
	m.nav.path = []Waypoint{{9, 64, 0}}
	m.nav.passed = append(m.nav.passed, Waypoint{8, 64, 0})
	g.hasLast = false
	g.cooldown = 0
	g.CanStart(tv)
	ops = m.TakeDoorOps()
	if len(ops) != 1 || ops[0].Kind != DoorOpClose {
		t.Fatalf("queued %+v after moving on, want one close", ops)
	}
}

// lyingDownWithDoor lies a villager down in its bed at x = 5 while it
// remembers an open oak door dist blocks west, and returns the queued ops.
// vanilla: SleepInBed.start
func lyingDownWithDoor(t *testing.T, dist int32) (*Mob, []DoorOp) {
	t.Helper()
	w := newTestWorld()
	x := 5 - dist
	w.set(x, 64, 0, doorState(t, "minecraft:oak_door", "lower", true))
	w.set(x, 65, 0, doorState(t, "minecraft:oak_door", "upper", true))
	m := NewMob(TypeVillager, 1, 5.5, 64, 0.5, 1)
	m.vb.doors = newInteractWithDoor(m)
	m.vb.doors.doors = []Waypoint{{x, 64, 0}}
	m.vb.home = &GlobalPos{X: 5, Y: 64, Z: 0}
	(&sleepInBedGoal{m: m}).Start(&tickView{w: w})
	if !m.Sleeping {
		t.Fatal("the villager did not lie down")
	}
	return m, m.TakeDoorOps()
}

func registryDoor(name, half string, open bool) (uint16, bool) {
	return registry.FindState(name, map[string]string{
		"facing": "south", "half": half, "hinge": "left", "open": fmt.Sprint(open), "powered": "false",
	})
}
