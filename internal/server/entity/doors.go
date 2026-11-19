package entity

import (
	"slices"

	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/registry"
)

// doorBlocks marks every door block: wooden, copper and iron.
var doorBlocks = registry.BlocksInGroup(registry.GroupDoor)

// doorKind is the door half of the pathfinder's PathType.
type doorKind uint8

const (
	doorNone doorKind = iota
	// doorOpen is PathType.DOOR_OPEN.
	doorOpen
	// doorWoodClosed is PathType.DOOR_WOOD_CLOSED: a closed door a hand opens.
	doorWoodClosed
	// doorIronClosed is PathType.DOOR_IRON_CLOSED.
	doorIronClosed
)

// doorTable holds, per block state, its door kind and whether a hand opens
// it, so a pathfinder cell costs one lookup rather than a property parse.
var doorTable = func() (t struct {
	kind   []doorKind
	wooden []bool
}) {
	t.kind = make([]doorKind, registry.StateCount)
	t.wooden = make([]bool, registry.StateCount)
	for state := range uint16(registry.StateCount) {
		block := registry.BlockOf(state)
		if !doorBlocks[block] {
			continue
		}
		t.wooden[state] = block != registry.BlockIronDoor
		switch {
		case registry.StateBool(state, registry.PropOpen):
			t.kind[state] = doorOpen
		case !t.wooden[state]:
			t.kind[state] = doorIronClosed
		default:
			t.kind[state] = doorWoodClosed
		}
	}
	return t
}()

// doorKindOf classifies a block state as a door the way the node evaluator
// does; the iron door is the only one BlockSetType.canOpenByHand refuses.
// vanilla: WalkNodeEvaluator.getPathTypeFromState
func doorKindOf(state uint16) doorKind {
	if int(state) >= len(doorTable.kind) {
		return doorNone
	}
	return doorTable.kind[state]
}

// isWoodenDoor reports whether the state is a door a hand opens, open or not.
// vanilla: DoorBlock.isWoodenDoor
func isWoodenDoor(state uint16) bool {
	return int(state) < len(doorTable.wooden) && doorTable.wooden[state]
}

// blocksBody reports whether the block stops the path: doors and leaves
// depend on the body's permissions and malus.
// vanilla: WalkNodeEvaluator.getPathTypeWithinMobBB
func (f footprint) blocksBody(state uint16) bool {
	if blockExtent(state) == 0 || pathfindableOpen(state) {
		return false
	}
	if f.leavesMalus >= 0 && blockPathType(state) == pathLeaves {
		return false
	}
	switch doorKindOf(state) {
	case doorOpen:
		return false
	case doorWoodClosed:
		return !f.openDoors
	}
	return true
}

// doorInFootprint reports whether a closed wooden door the body can open is
// in its footprint at the cell.
func doorInFootprint(w World, f footprint, x, y, z int32) bool {
	for dx := int32(0); dx < f.fw; dx++ {
		for dz := int32(0); dz < f.fw; dz++ {
			for dy := int32(0); dy < f.fh; dy++ {
				if doorKindOf(w.BlockAt(x+dx, y+dy, z+dz)) == doorWoodClosed {
					return true
				}
			}
		}
	}
	return false
}

// canOpenDoors is the navigation's canOpenDoors: a door-breaking zombie, a
// vindicator in a raid, and every villager, piglin and copper golem.
// vanilla: Zombie.setCanBreakDoors, Vindicator.customServerAiStep, Villager.<init>,
// AbstractPiglin.applyOpenDoorsAbility, CopperGolem.<init>
func (m *Mob) canOpenDoors() bool {
	switch {
	case m.CanBreakDoors:
		return true
	case m.Type == TypeVindicator:
		return m.InRaid
	}
	return m.Type == TypeVillager || m.Type == TypeCopperGolem || m.isPiglin()
}

// DoorOpKind is one change a door goal asks of the world.
type DoorOpKind uint8

const (
	// DoorOpOpen and DoorOpClose set a door's open state.
	DoorOpOpen DoorOpKind = iota
	DoorOpClose
	// DoorOpCrack is a swing at the door: level event 1019 and the arm swing.
	DoorOpCrack
	// DoorOpProgress sets the door's crack stage, -1 clearing it.
	DoorOpProgress
	// DoorOpBreak removes the door and plays level events 1021 and 2001.
	DoorOpBreak
)

// DoorOp is a door change the server applies after the tick.
type DoorOp struct {
	Mob     *Mob
	Kind    DoorOpKind
	X, Y, Z int32
	// Stage is the crack stage of a DoorOpProgress, 0 to 10 or -1.
	Stage int32
	// Swing says a DoorOpCrack also swings the mob's arm; a mob already
	// swinging does not start another.
	Swing bool
}

// TakeDoorOps returns and clears the door changes the mob's goals queued.
func (m *Mob) TakeDoorOps() []DoorOp {
	o := m.nav.doorOps
	m.nav.doorOps = nil
	return o
}

func (m *Mob) queueDoor(kind DoorOpKind, p Waypoint, stage int32) {
	m.nav.doorOps = append(m.nav.doorOps, DoorOp{Mob: m, Kind: kind, X: p.X, Y: p.Y, Z: p.Z, Stage: stage})
}

// doorInteract is DoorInteractGoal: once the body bumps into something it
// finds the wooden door on the path ahead, and notes when it has passed.
// vanilla: DoorInteractGoal
type doorInteract struct {
	m       *Mob
	pos     Waypoint
	hasDoor bool
	passed  bool
	// dirX and dirZ are the float32 offset from the body to the door's
	// centre at the start; a sign flip of their dot product is the pass.
	dirX, dirZ float32
}

// doorReach is the squared distance within which a path node's door counts.
const doorReach = 2.25

func (d *doorInteract) isOpen(w World) bool {
	if !d.hasDoor {
		return false
	}
	state := w.BlockAt(d.pos.X, d.pos.Y, d.pos.Z)
	block := registry.BlockOf(state)
	if !doorBlocks[block] {
		d.hasDoor = false
		return false
	}
	return doorKindOf(state) == doorOpen
}

func (d *doorInteract) setOpen(w World, open bool) {
	if !d.hasDoor || !doorBlocks[registry.BlockOf(w.BlockAt(d.pos.X, d.pos.Y, d.pos.Z))] {
		return
	}
	kind := DoorOpClose
	if open {
		kind = DoorOpOpen
	}
	d.m.queueDoor(kind, d.pos, 0)
}

// canUse looks for a wooden door among the path nodes up to two past the
// next, then at the head cell. Without a path, or at its end, it never
// starts.
// vanilla: DoorInteractGoal.canUse
func (d *doorInteract) canUse(t *tickView) bool {
	m := d.m
	if !m.horizontalCollision {
		return false
	}
	nav := &m.navOf().nav
	if len(nav.path) == 0 {
		return false
	}
	// The node list is the origin, the waypoints reached, then the two
	// after the next.
	reached := len(nav.passed)
	for i := 0; i < 1+reached+min(2, len(nav.path)); i++ {
		node := nav.origin
		switch {
		case i == 0:
		case i <= reached:
			node = nav.passed[i-1]
		default:
			node = nav.path[i-1-reached]
		}
		d.pos = Waypoint{node.X, node.Y + 1, node.Z}
		dx, dz := float64(d.pos.X)-m.X, float64(d.pos.Z)-m.Z
		if dx*dx+dz*dz > doorReach {
			continue
		}
		d.hasDoor = isWoodenDoor(t.w.BlockAt(d.pos.X, d.pos.Y, d.pos.Z))
		if d.hasDoor {
			return true
		}
	}
	c := m.cell()
	d.pos = Waypoint{c.X, c.Y + 1, c.Z}
	d.hasDoor = isWoodenDoor(t.w.BlockAt(d.pos.X, d.pos.Y, d.pos.Z))
	return d.hasDoor
}

func (d *doorInteract) start() {
	d.passed = false
	d.dirX = float32(float64(d.pos.X) + 0.5 - d.m.X)
	d.dirZ = float32(float64(d.pos.Z) + 0.5 - d.m.Z)
}

func (d *doorInteract) tick() {
	x := float32(float64(d.pos.X) + 0.5 - d.m.X)
	z := float32(float64(d.pos.Z) + 0.5 - d.m.Z)
	if d.dirX*x+d.dirZ*z < 0 {
		d.passed = true
	}
}

// openDoorGoal is OpenDoorGoal: it opens the door in the way and, when
// closeAfter is set, shuts it once the body is through or a second is up.
// vanilla: OpenDoorGoal
type openDoorGoal struct {
	doorInteract
	priority   int
	closeAfter bool
	// gate is the extra condition of a subclass's canUse.
	gate       func() bool
	forgetTime int
}

func newOpenDoorGoal(m *Mob, priority int, closeAfter bool, gate func() bool) *openDoorGoal {
	return &openDoorGoal{m: m, priority: priority, closeAfter: closeAfter, gate: gate}
}

func (g *openDoorGoal) Priority() int           { return g.priority }
func (g *openDoorGoal) Flags() GoalFlags        { return 0 }
func (g *openDoorGoal) RequiresEveryTick() bool { return true }

func (g *openDoorGoal) CanStart(t *tickView) bool {
	return g.canUse(t) && (g.gate == nil || g.gate())
}

func (g *openDoorGoal) ShouldContinue(*tickView) bool {
	return g.closeAfter && g.forgetTime > 0 && !g.passed
}

func (g *openDoorGoal) Start(t *tickView) {
	g.start()
	g.forgetTime = 20
	g.setOpen(t.w, true)
}

func (g *openDoorGoal) Stop(t *tickView) { g.setOpen(t.w, false) }

func (g *openDoorGoal) Tick(*tickView) {
	g.forgetTime--
	g.tick()
}

// The door-breaking figures: the break takes 240 ticks, the crack sound
// comes once in twenty ticks, and the progress has ten stages.
// vanilla: BreakDoorGoal.getDoorBreakTime, tick
const (
	doorBreakTime  = 240
	doorCrackOdds  = 20
	doorBreakSteps = 10
	// doorSwingTicks is LivingEntity.getCurrentSwingDuration without effects.
	doorSwingTicks = 6
)

// breakDoorGoal is BreakDoorGoal: the body cracks the closed wooden door in
// its way over doorBreakTime ticks, then removes it.
// vanilla: BreakDoorGoal
type breakDoorGoal struct {
	doorInteract
	priority int
	flags    GoalFlags
	// valid says whether the difficulty lets the door break.
	valid func(difficulty uint8) bool
	// gate is the extra condition of a subclass's canUse and canContinueToUse.
	gate func() bool
	// startRoll, if set, is the one-in-N roll a subclass's canUse makes first;
	// noAction zeroes the no-action time at the start.
	startRoll int
	noAction  bool
	breakTime int
	// lastProgress is the last stage sent; the goal's restart keeps it.
	lastProgress int32
	// swingEnd is the game tick the goal's own arm swing ends at.
	swingEnd int64
}

// zombieBreaksDoors is Zombie.DOOR_BREAKING_PREDICATE: hard only.
func zombieBreaksDoors(difficulty uint8) bool { return difficulty == rules.DifficultyHard }

// newZombieBreakDoorGoal is the goal Zombie.setCanBreakDoors adds at 1; it
// only runs while the flag is up.
func newZombieBreakDoorGoal(m *Mob) *breakDoorGoal {
	return &breakDoorGoal{m: m, priority: 1, lastProgress: -1, valid: zombieBreaksDoors,
		gate: func() bool { return m.CanBreakDoors }}
}

// newVindicatorBreakDoorGoal is Vindicator.VindicatorBreakDoorGoal: only
// inside a raid, on normal and hard, one try in ten ticks.
// vanilla: Vindicator.VindicatorBreakDoorGoal
func newVindicatorBreakDoorGoal(m *Mob) *breakDoorGoal {
	return &breakDoorGoal{m: m, priority: 2, flags: GoalMove, lastProgress: -1,
		valid: func(d uint8) bool { return d == rules.DifficultyNormal || d == rules.DifficultyHard },
		gate:  func() bool { return m.InRaid }, startRoll: 10, noAction: true}
}

func (g *breakDoorGoal) Priority() int           { return g.priority }
func (g *breakDoorGoal) Flags() GoalFlags        { return g.flags }
func (g *breakDoorGoal) RequiresEveryTick() bool { return true }

func (g *breakDoorGoal) CanStart(t *tickView) bool {
	if g.gate != nil && !g.gate() {
		return false
	}
	if g.startRoll > 0 && g.m.Rand.Intn(reducedTickDelay(g.startRoll)) != 0 {
		return false
	}
	if !g.canUse(t) || !t.w.MobGriefing() {
		return false
	}
	return g.valid(t.w.Difficulty()) && !g.isOpen(t.w)
}

func (g *breakDoorGoal) Start(*tickView) {
	g.start()
	g.breakTime = 0
	if g.noAction {
		g.m.NoActionTicks = 0
	}
}

func (g *breakDoorGoal) ShouldContinue(t *tickView) bool {
	if g.gate != nil && !g.gate() {
		return false
	}
	m := g.m
	dx, dy, dz := float64(g.pos.X)+0.5-m.X, float64(g.pos.Y)+0.5-m.Y, float64(g.pos.Z)+0.5-m.Z
	return g.breakTime <= doorBreakTime && !g.isOpen(t.w) && dx*dx+dy*dy+dz*dz < 4 &&
		g.valid(t.w.Difficulty())
}

func (g *breakDoorGoal) Stop(*tickView) { g.m.queueDoor(DoorOpProgress, g.pos, -1) }

func (g *breakDoorGoal) Tick(t *tickView) {
	g.tick()
	m := g.m
	if m.Rand.Intn(doorCrackOdds) == 0 {
		swing := m.GameTime >= g.swingEnd
		if swing {
			g.swingEnd = m.GameTime + doorSwingTicks
		}
		m.nav.doorOps = append(m.nav.doorOps, DoorOp{Mob: m, Kind: DoorOpCrack, X: g.pos.X, Y: g.pos.Y, Z: g.pos.Z, Swing: swing})
	}
	g.breakTime++
	progress := int32(float32(g.breakTime) / float32(doorBreakTime) * doorBreakSteps)
	if progress != g.lastProgress {
		m.queueDoor(DoorOpProgress, g.pos, progress)
		g.lastProgress = progress
	}
	if g.breakTime == doorBreakTime && g.valid(t.w.Difficulty()) {
		m.queueDoor(DoorOpBreak, g.pos, 0)
	}
}

// DoorIsWooden reports whether the state is a door a hand opens.
func DoorIsWooden(state uint16) bool { return isWoodenDoor(state) }

// DoorIsOpen reports whether the state is an open door.
func DoorIsOpen(state uint16) bool { return doorKindOf(state) == doorOpen }

// interactWithDoor is InteractWithDoor: the villager opens the doors at the
// path's previous and next nodes and closes those it opened once past,
// unless another villager is about to use them.
// vanilla: InteractWithDoor.create
type interactWithDoor struct {
	m        *Mob
	last     Waypoint
	hasLast  bool
	cooldown int
	// doors are the doors to close, DOORS_TO_CLOSE.
	doors []Waypoint
}

// The behaviour's figures.
// vanilla: InteractWithDoor
const (
	doorRerunCooldown = 20
	doorCloseReach    = 3.0
	doorHoldReach     = 2.0
)

func newInteractWithDoor(m *Mob) *interactWithDoor { return &interactWithDoor{m: m} }

func (g *interactWithDoor) Priority() int           { return 5 }
func (g *interactWithDoor) Flags() GoalFlags        { return 0 }
func (g *interactWithDoor) RequiresEveryTick() bool { return true }

// CanStart runs the whole behaviour: it answers true on a tick it ran.
func (g *interactWithDoor) CanStart(t *tickView) bool {
	nav := &g.m.navOf().nav
	if len(nav.path) == 0 {
		return false
	}
	next := nav.path[0]
	if g.hasLast && g.last == next {
		g.cooldown = doorRerunCooldown
	} else {
		g.cooldown--
		if g.cooldown > 0 {
			return false
		}
	}
	g.last, g.hasLast = next, true
	from := nav.prevNode()
	if g.interactable(t.w, from) {
		if !g.isOpen(t.w, from) {
			g.m.queueDoor(DoorOpOpen, from, 0)
		}
		g.remember(from)
	}
	if g.interactable(t.w, next) && !g.isOpen(t.w, next) {
		g.m.queueDoor(DoorOpOpen, next, 0)
		g.remember(next)
	}
	g.closeBehind(t, &from, &next)
	return true
}

func (g *interactWithDoor) ShouldContinue(*tickView) bool { return false }
func (g *interactWithDoor) Start(*tickView)               {}
func (g *interactWithDoor) Tick(*tickView)                {}
func (g *interactWithDoor) Stop(*tickView)                {}

// interactable is the MOB_INTERACTABLE_DOORS door check.
func (g *interactWithDoor) interactable(w World, p Waypoint) bool {
	return isWoodenDoor(w.BlockAt(p.X, p.Y, p.Z))
}

func (g *interactWithDoor) isOpen(w World, p Waypoint) bool {
	return doorKindOf(w.BlockAt(p.X, p.Y, p.Z)) == doorOpen
}

func (g *interactWithDoor) remember(p Waypoint) {
	if !slices.Contains(g.doors, p) {
		g.doors = append(g.doors, p)
	}
}

// closeBehind shuts the remembered doors the villager has left, forgetting
// each it visits. A nil from or to matches no door.
// vanilla: InteractWithDoor.closeDoorsThatIHaveOpenedOrPassedThrough
func (g *interactWithDoor) closeBehind(t *tickView, from, to *Waypoint) {
	kept := g.doors[:0]
	for _, p := range g.doors {
		if from != nil && p == *from || to != nil && p == *to {
			kept = append(kept, p)
			continue
		}
		if !closerToCenter(p, g.m.X, g.m.Y, g.m.Z, doorCloseReach) ||
			!g.interactable(t.w, p) || !g.isOpen(t.w, p) || g.othersComing(t, p) {
			continue
		}
		g.m.queueDoor(DoorOpClose, p, 0)
	}
	g.doors = kept
}

// othersComing reports whether a villager near the door has it on its path.
// vanilla: InteractWithDoor.areOtherMobsComingThroughDoor
func (g *interactWithDoor) othersComing(t *tickView, door Waypoint) bool {
	for _, o := range t.others {
		if o == g.m || o.Type != g.m.Type || !closerToCenter(door, o.X, o.Y, o.Z, doorHoldReach) {
			continue
		}
		nav := &o.navOf().nav
		if len(nav.path) > 0 && (door == nav.prevNode() || door == nav.path[0]) {
			return true
		}
	}
	return false
}

// closerToCenter is BlockPos.closerToCenterThan.
func closerToCenter(p Waypoint, x, y, z, dist float64) bool {
	dx, dy, dz := float64(p.X)+0.5-x, float64(p.Y)+0.5-y, float64(p.Z)+0.5-z
	return dx*dx+dy*dy+dz*dz < dist*dist
}
