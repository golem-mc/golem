package entity

import (
	"math"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// CopperGolemState is the synched COPPER_GOLEM_STATE, in its enum order.
// vanilla: CopperGolemState
const (
	copperStateIdle = iota
	copperStateGettingItem
	copperStateGettingNoItem
	copperStateDroppingItem
	copperStateDroppingNoItem
)

// The transport's numbers.
// vanilla: CopperGolemAi, TransportItemsBetweenContainers
const (
	transportSearchXZ       = 32
	transportSearchY        = 8
	transportInteractTicks  = 60
	transportStackMax       = 16
	transportMemoryTicks    = 6000
	transportVisitedMax     = 10
	transportUnreachableMax = 50
	transportCooldownTicks  = 140
	// transportOpenTick, transportSoundTick: the interaction tick that opens
	// the chest and sets the state, and the one that plays the sound.
	transportOpenTick  = 1
	transportSoundTick = 9
)

// priorityTransport ranks the transport above the stroll and glances and
// below the panic.
const priorityTransport = 2

// ChestAccess is what the copper golem's transport reads and does at chests.
// The server implements it over the world's block entities, and a mob only
// ever holds one for the tick it runs in. A cell is a chest block's.
type ChestAccess interface {
	// ChestsIn appends to into the chest cells with a block entity in the
	// inclusive box.
	ChestsIn(x0, y0, z0, x1, y1, z1 int32, into [][3]int32) [][3]int32
	// ChestBlocked is ChestBlock.isChestBlockedAt.
	ChestBlocked(x, y, z int32) bool
	// ChestLocked is BaseContainerBlockEntity.isLocked on the cell's own block
	// entity, not its other half's.
	ChestLocked(x, y, z int32) bool
	// ChestInUse reports whether a player or golem has the chest, or its other
	// half, open.
	ChestInUse(x, y, z int32) bool
	// ChestPartner is the other half of a double chest.
	ChestPartner(x, y, z int32) (cell [3]int32, ok bool)
	// ChestEmpty reports whether the container, a double chest's both halves,
	// holds nothing; ok is false where it has no block entity.
	ChestEmpty(x, y, z int32) (empty, ok bool)
	// ChestHolds reports whether a slot holds the same item as the stack.
	ChestHolds(x, y, z int32, item protocol.ItemStack) bool
	// ChestTake removes up to transportStackMax of the first non-empty slot.
	ChestTake(x, y, z int32) protocol.ItemStack
	// ChestPut adds the stack in slot order and returns what did not fit.
	ChestPut(x, y, z int32, held protocol.ItemStack) protocol.ItemStack
}

type transportPhase uint8

const (
	transportTravelling transportPhase = iota
	transportQueuing
	transportInteracting
)

// transportAction is what the golem does at the chest it reached.
type transportAction uint8

const (
	transportNone transportAction = iota
	transportPickup
	transportPickupNothing
	transportPlace
	transportPlaceNothing
)

// copperTransport is TransportItemsBetweenContainers as the golem runs it:
// take a stack from a copper chest, carry it to a chest that holds the item
// or is empty, and put it in.
// vanilla: CopperGolemAi.initIdleActivity
type copperTransport struct {
	m      *Mob
	phase  transportPhase
	action transportAction
	target Waypoint
	has    bool
	ticks  int
	// pathed is whether the walk to the target has a path, which the
	// interaction range reads: a finished path reaches further.
	pathed bool
	// pathEnd is the last node of the planned path, the body's own cell for an
	// empty one; valid while pathed.
	pathEnd Waypoint
	// visited and unreachable are VISITED_BLOCK_POSITIONS and
	// UNREACHABLE_TRANSPORT_BLOCK_POSITIONS, each expiring as a whole.
	visited, unreachable           map[Waypoint]struct{}
	visitedUntil, unreachableUntil int64
}

func (g *copperTransport) Priority() int    { return priorityTransport }
func (g *copperTransport) Flags() GoalFlags { return GoalMove }

// CanStart is the behaviour's start conditions.
func (g *copperTransport) CanStart(*tickView) bool { return g.canRun() }

func (g *copperTransport) ShouldContinue(*tickView) bool { return g.canRun() }

func (g *copperTransport) canRun() bool {
	m := g.m
	return m.copper.cooldown == 0 && m.Chests != nil && !m.panicking() && m.LeashHolderID == 0
}

func (g *copperTransport) Start(*tickView) {}

func (g *copperTransport) Stop(t *tickView) { g.onStartTravelling(t) }

// RequiresEveryTick is true: a behaviour ticks every tick.
func (g *copperTransport) RequiresEveryTick() bool { return true }

// Tick is Behavior.tick.
func (g *copperTransport) Tick(t *tickView) {
	updated := g.updateInvalidTarget(t)
	if !g.has {
		g.onStartTravelling(t)
		return
	}
	if updated {
		return
	}
	if g.phase == transportQueuing && !g.otherInteracting(t) {
		g.phase = transportTravelling
		g.walkTowardsTarget(t)
	}
	if g.phase == transportTravelling {
		g.travel(t)
	}
	if g.phase == transportInteracting {
		g.reached(t)
	}
}

func (g *copperTransport) picking() bool { return g.m.MainHand.Empty() }

// wantedBlock is isWantedBlock: a copper chest to take from, a chest or
// trapped chest to put into.
func (g *copperTransport) wantedBlock(t *tickView, c Waypoint) bool {
	b := registry.BlockOf(t.w.BlockAt(c.X, c.Y, c.Z))
	if g.picking() {
		return b.InTag(registry.TagIDCopperChests)
	}
	return b == registry.BlockChest || b == registry.BlockTrappedChest
}

// updateInvalidTarget replaces a target that no longer holds and reports
// whether it did anything.
func (g *copperTransport) updateInvalidTarget(t *tickView) bool {
	if g.hasValidTarget(t) {
		return false
	}
	g.stopTargeting()
	if c, ok := g.findTarget(t); ok {
		g.target, g.has = c, true
		g.onStartTravelling(t)
		g.markVisited(c)
		return true
	}
	g.enterCooldown()
	return true
}

func (g *copperTransport) hasValidTarget(t *tickView) bool {
	if !g.has || !g.wantedBlock(t, g.target) {
		return false
	}
	c := g.target
	if _, exists := g.m.Chests.ChestEmpty(c.X, c.Y, c.Z); !exists || g.m.Chests.ChestBlocked(c.X, c.Y, c.Z) {
		return false
	}
	if g.phase != transportTravelling || g.validTravellingPath(t) {
		return true
	}
	g.markUnreachable(c)
	return false
}

// validTravellingPath is hasValidTravellingPath: a walk not yet planned is
// fine, a planned one must end within reach of the chest and see one of its
// sides.
func (g *copperTransport) validTravellingPath(t *tickView) bool {
	m := g.m
	fx, fy, fz := m.X, m.Y, m.Z
	if g.pathed {
		end := g.pathEnd
		fx, fy, fz = float64(end.X)+0.5, float64(end.Y), float64(end.Z)+0.5
	}
	fy += m.height() / 2
	canReach := g.within(t, g.interactionRange(), g.target, fx, fy, fz)
	return (!g.pathed && !canReach) || (canReach && g.seesTargetSide(t, fx, fy, fz))
}

// seesTargetSide is canSeeAnyTargetSide: a ray from the point to the middle
// of any of the chest cell's six faces whose first block collision is the
// chest cell.
func (g *copperTransport) seesTargetSide(t *tickView, fx, fy, fz float64) bool {
	c := g.target
	for _, d := range [...][3]float64{{0, -1, 0}, {0, 1, 0}, {0, 0, -1}, {0, 0, 1}, {-1, 0, 0}, {1, 0, 0}} {
		tx, ty, tz := float64(c.X)+0.5+d[0]*0.5, float64(c.Y)+0.5+d[1]*0.5, float64(c.Z)+0.5+d[2]*0.5
		if x, y, z, ok := world.ClipCollider(t.w, fx, fy, fz, tx-fx, ty-fy, tz-fz); ok && x == c.X && y == c.Y && z == c.Z {
			return true
		}
	}
	return false
}

// interactionRange is getInteractionRange: further once the path is done.
func (g *copperTransport) interactionRange() float64 {
	if g.pathed && g.m.navDone() {
		return 1.0
	}
	return 0.5
}

// within is isWithinTargetDistance: the chest's collision bounds grown by
// dist (0.5 vertically) meet the body's box centred on (cx, cy, cz).
func (g *copperTransport) within(t *tickView, dist float64, c Waypoint, cx, cy, cz float64) bool {
	boxes := world.CollisionBoxes(t.w.BlockAt(c.X, c.Y, c.Z))
	if len(boxes) == 0 {
		return false
	}
	b := boxes[0]
	for _, o := range boxes[1:] {
		b.MinX, b.MinY, b.MinZ = min(b.MinX, o.MinX), min(b.MinY, o.MinY), min(b.MinZ, o.MinZ)
		b.MaxX, b.MaxY, b.MaxZ = max(b.MaxX, o.MaxX), max(b.MaxY, o.MaxY), max(b.MaxZ, o.MaxZ)
	}
	hw, hh := g.m.width()/2, g.m.height()/2
	x, y, z := float64(c.X), float64(c.Y), float64(c.Z)
	return x+b.MinX-dist < cx+hw && x+b.MaxX+dist > cx-hw &&
		y+b.MinY-0.5 < cy+hh && y+b.MaxY+0.5 > cy-hh &&
		z+b.MinZ-dist < cz+hw && z+b.MaxZ+dist > cz-hw
}

func (g *copperTransport) centre() (x, y, z float64) {
	return g.m.X, g.m.Y + g.m.height()/2, g.m.Z
}

// findTarget is getTransportTarget: the nearest wanted, unlocked, unblocked
// chest in the search box that was neither visited nor found unreachable.
func (g *copperTransport) findTarget(t *tickView) (Waypoint, bool) {
	m := g.m
	c := m.cell()
	rx, ry := int32(transportSearchXZ), int32(transportSearchY)
	cells := m.Chests.ChestsIn(c.X-rx, c.Y-ry, c.Z-rx, c.X+rx, c.Y+ry, c.Z+rx, nil)
	best, bestDist := Waypoint{}, math.MaxFloat64
	for _, cell := range cells {
		p := Waypoint{cell[0], cell[1], cell[2]}
		dx, dy, dz := float64(p.X)+0.5-m.X, float64(p.Y)+0.5-m.Y, float64(p.Z)+0.5-m.Z
		d := dx*dx + dy*dy + dz*dz
		if d >= bestDist || !g.wantedBlock(t, p) || g.seen(p) || g.blocked(p) ||
			m.Chests.ChestLocked(p.X, p.Y, p.Z) {
			continue
		}
		best, bestDist = p, d
	}
	return best, bestDist != math.MaxFloat64
}

// blocked reports whether either half of the chest is blocked, which leaves
// it no container to pick.
// vanilla: TransportItemTarget.tryCreatePossibleTarget, ChestBlock.getContainer
func (g *copperTransport) blocked(p Waypoint) bool {
	if g.m.Chests.ChestBlocked(p.X, p.Y, p.Z) {
		return true
	}
	other, ok := g.m.Chests.ChestPartner(p.X, p.Y, p.Z)
	return ok && g.m.Chests.ChestBlocked(other[0], other[1], other[2])
}

// seen is isPositionAlreadyVisited over the chest and its other half.
func (g *copperTransport) seen(p Waypoint) bool {
	if g.listed(p) {
		return true
	}
	if other, ok := g.m.Chests.ChestPartner(p.X, p.Y, p.Z); ok {
		return g.listed(Waypoint{other[0], other[1], other[2]})
	}
	return false
}

func (g *copperTransport) listed(p Waypoint) bool {
	now := g.m.GameTime
	if _, ok := g.visited[p]; ok && now < g.visitedUntil {
		return true
	}
	_, ok := g.unreachable[p]
	return ok && now < g.unreachableUntil
}

// markVisited is setVisitedBlockPos.
func (g *copperTransport) markVisited(p Waypoint) {
	g.expire()
	if len(g.visited) >= transportVisitedMax {
		if _, again := g.visited[p]; !again {
			g.enterCooldown()
			return
		}
	}
	if g.visited == nil {
		g.visited = map[Waypoint]struct{}{}
	}
	g.visited[p] = struct{}{}
	g.visitedUntil = g.m.GameTime + transportMemoryTicks
}

// markUnreachable is markVisitedBlockPosAsUnreachable.
func (g *copperTransport) markUnreachable(p Waypoint) {
	g.expire()
	delete(g.visited, p)
	if len(g.unreachable) >= transportUnreachableMax {
		if _, again := g.unreachable[p]; !again {
			g.enterCooldown()
			return
		}
	}
	if g.unreachable == nil {
		g.unreachable = map[Waypoint]struct{}{}
	}
	g.unreachable[p] = struct{}{}
	g.visitedUntil = g.m.GameTime + transportMemoryTicks
	g.unreachableUntil = g.visitedUntil
}

// expire drops a memory past its time, as a memory with expiry is.
func (g *copperTransport) expire() {
	if g.m.GameTime >= g.visitedUntil {
		clear(g.visited)
	}
	if g.m.GameTime >= g.unreachableUntil {
		clear(g.unreachable)
	}
}

func (g *copperTransport) clearMemories() {
	clear(g.visited)
	clear(g.unreachable)
}

// stopTargeting is stopTargetingCurrentTarget.
func (g *copperTransport) stopTargeting() {
	g.ticks = 0
	g.has = false
	g.pathed = false
	g.pathEnd = Waypoint{}
	g.m.stopSeeking()
}

// enterCooldown is enterCooldownAfterNoMatchingTargetFound.
func (g *copperTransport) enterCooldown() {
	g.stopTargeting()
	g.m.copper.cooldown = transportCooldownTicks
	g.clearMemories()
}

// onStartTravelling closes the chest and returns the golem to idle.
func (g *copperTransport) onStartTravelling(t *tickView) {
	g.m.copperCloseChest(t)
	g.m.SetMetadataCopperState(copperStateIdle)
	g.phase = transportTravelling
	g.action = transportNone
	g.ticks = 0
}

func (g *copperTransport) walkTowardsTarget(t *tickView) {
	c := g.target
	if !g.m.seekTo(t.w, float64(c.X)+0.5, float64(c.Y), float64(c.Z)+0.5, 1.0, navReach) {
		return
	}
	g.pathed = true
	if path := g.m.navOf().nav.path; len(path) > 0 {
		g.pathEnd = path[len(path)-1]
	} else {
		g.pathEnd = g.m.cell()
	}
}

// otherInteracting is isAnotherMobInteractingWithTarget.
func (g *copperTransport) otherInteracting(*tickView) bool {
	c := g.target
	return g.m.Chests.ChestInUse(c.X, c.Y, c.Z)
}

// travel is onTravelToTarget.
func (g *copperTransport) travel(t *tickView) {
	x, y, z := g.centre()
	switch {
	case g.within(t, 3.0, g.target, x, y, z) && g.otherInteracting(t):
		g.m.stopSeeking()
		g.m.walk.speed = 0
		g.phase = transportQueuing
	case g.within(t, g.interactionRange(), g.target, x, y, z):
		g.startInteraction(t)
	default:
		g.walkTowardsTarget(t)
	}
}

// startInteraction is startOnReachedTargetInteraction.
func (g *copperTransport) startInteraction(t *tickView) {
	c := g.target
	empty, _ := g.m.Chests.ChestEmpty(c.X, c.Y, c.Z)
	switch {
	case g.picking() && !empty:
		g.action = transportPickup
	case g.picking():
		g.action = transportPickupNothing
	case empty || g.m.Chests.ChestHolds(c.X, c.Y, c.Z, g.m.MainHand):
		g.action = transportPlace
	default:
		g.action = transportPlaceNothing
	}
	g.phase = transportInteracting
}

// reached is onReachedTarget.
func (g *copperTransport) reached(t *tickView) {
	m := g.m
	x, y, z := g.centre()
	if !g.within(t, 2.0, g.target, x, y, z) {
		g.onStartTravelling(t)
		return
	}
	g.ticks++
	c := g.target
	// vanilla: TransportItemsBetweenContainers.onTargetInteraction
	m.stopSeeking()
	m.walk.speed = 0
	m.VX, m.VZ = 0, 0
	m.lookAtPoint(float64(c.X)+0.5, float64(c.Y)+0.5, float64(c.Z)+0.5)
	g.interact(t)
	if g.ticks < transportInteractTicks {
		return
	}
	g.finish(t)
	g.onStartTravelling(t)
}

// copperActions is CopperGolemAi.getTargetReachedInteractions: the state each
// action shows and its sound. The ids are vanilla's: its COPPER_GOLEM_ITEM_GET
// is registered as no_item_get and ITEM_NO_GET as no_item_no_get.
var copperActions = [...]struct {
	state int
	sound registry.Sound
}{
	transportPickup:        {copperStateGettingItem, registry.SoundEntityCopperGolemNoItemGet},
	transportPickupNothing: {copperStateGettingNoItem, registry.SoundEntityCopperGolemNoItemNoGet},
	transportPlace:         {copperStateDroppingItem, registry.SoundEntityCopperGolemItemDrop},
	transportPlaceNothing:  {copperStateDroppingNoItem, registry.SoundEntityCopperGolemItemNoDrop},
}

// interact is CopperGolemAi.onReachedTargetInteraction at the given tick.
func (g *copperTransport) interact(t *tickView) {
	if g.action == transportNone {
		return
	}
	m := g.m
	a := copperActions[g.action]
	switch g.ticks {
	case transportOpenTick:
		m.copperOpenChest(t, g.target)
		m.SetMetadataCopperState(a.state)
	case transportSoundTick:
		t.events = append(t.events, MobEvent{Mob: m, Kind: MobSounded, SoundID: a.sound, Volume: 1, Pitch: 1})
	case transportInteractTicks:
		m.copperCloseChest(t)
	}
}

// finish is doReachedTargetInteraction at the end of the interaction: the
// move itself, judged again against the chest as it is now.
func (g *copperTransport) finish(t *tickView) {
	m := g.m
	c := g.target
	chests := m.Chests
	empty, _ := chests.ChestEmpty(c.X, c.Y, c.Z)
	if g.picking() {
		if empty {
			g.stopTargeting()
			return
		}
		m.SetEquipment(protocol.EquipMainHand, chests.ChestTake(c.X, c.Y, c.Z))
		if !m.MainHand.Empty() {
			copperChestChanged(t, m, c)
		}
		m.DropChances[0] = 2 // vanilla: Mob.setGuaranteedDrop
		g.stopTargeting()
		g.clearMemories()
		return
	}
	if !empty && !chests.ChestHolds(c.X, c.Y, c.Z, m.MainHand) {
		g.stopTargeting()
		return
	}
	held := m.MainHand
	m.SetEquipment(protocol.EquipMainHand, chests.ChestPut(c.X, c.Y, c.Z, held))
	if m.MainHand.Count != held.Count {
		copperChestChanged(t, m, c)
	}
	g.stopTargeting()
	if m.MainHand.Empty() {
		g.clearMemories()
	}
}

// copperOpenChest is Container.startOpen with setOpenedChestPos.
func (m *Mob) copperOpenChest(t *tickView, c Waypoint) {
	m.copper.opened, m.copper.openedAt = true, c
	t.events = append(t.events, MobEvent{Mob: m, Kind: MobChestOpened, X: float64(c.X), Y: float64(c.Y), Z: float64(c.Z)})
}

// copperChestChanged is the container's setChanged, which its viewers'
// broadcastChanges sees.
func copperChestChanged(t *tickView, m *Mob, c Waypoint) {
	t.events = append(t.events, MobEvent{Mob: m, Kind: MobChestChanged, X: float64(c.X), Y: float64(c.Y), Z: float64(c.Z)})
}

// copperCloseChest is clearOpenedChestPos, which also ends the opener count.
func (m *Mob) copperCloseChest(t *tickView) {
	if !m.copper.opened {
		return
	}
	m.copper.opened = false
	c := m.copper.openedAt
	t.events = append(t.events, MobEvent{Mob: m, Kind: MobChestClosed, X: float64(c.X), Y: float64(c.Y), Z: float64(c.Z)})
}

// CopperState is the synched COPPER_GOLEM_STATE.
func (m *Mob) CopperState() int { return m.copper.state }
