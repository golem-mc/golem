package entity

import (
	"math"
	"slices"

	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// The enderman: it is provoked by a gaze on its eyes, teleports away from
// what hurts it, and is driven off by daylight and water.
// vanilla: EnderMan

// endermanState is what the enderman keeps beside the shared mob state.
type endermanState struct {
	// staredAt is EnderMan.DATA_STARED_AT, the flag that makes the client
	// open the mouth; it falls with the target.
	staredAt bool
	// w is the world of the last tick: a hurt teleports on the spot and has
	// no world of its own to do it in.
	w World
	// hurtTeleports are the teleports a hurt made since the last tick, told
	// with the next tick's events.
	hurtTeleports []MobEvent
	// dodged is set when a projectile's hit was answered by a teleport, and
	// read once by the projectile through TookDodge; every hit clears it first.
	dodged bool
	// lastTarget and targetChangeAge are EnderMan.targetChangeTime: the age the
	// target last changed, which holds off the daylight teleport for 600 ticks.
	lastTarget      int32
	targetChangeAge int
	// carried is the block state EnderMan.DATA_CARRY_STATE holds, 0 for none.
	carried uint16
}

// The vanilla numbers of the enderman's gaze and teleports.
// vanilla: EnderMan.isBeingStaredBy, EnderMan.teleport, EnderMan.hurtServer
const (
	// stareCone is the cone size isLookingAtMe is asked with; the dot of the
	// view vector and the direction to the eyes must pass 1 - cone/distance.
	stareCone = 0.025
	// stareFreezeRange2 is the squared distance within which a stared-at
	// enderman stands still.
	stareFreezeRange2 = 256.0
	// stareBlinkRange2 is the squared distance under which a stared-at
	// enderman teleports away from its target.
	stareBlinkRange2 = 16.0
	// stareApproachDelay is the ticks a target farther than sixteen blocks
	// waits before the enderman teleports towards it.
	stareApproachDelay = 30
	// stareAggroDelay is the ticks a stare must last before it becomes a
	// target.
	stareAggroDelay = 5
	// endermanDaylightDelay is the ticks since the last target change before
	// daylight sends the enderman away.
	endermanDaylightDelay = 600
	// projectileTeleportTries is how many random teleports a projectile's
	// hit tries until one lands.
	projectileTeleportTries = 64
	// sneakingEyeHeight is a sneaking player's eye height.
	sneakingEyeHeight = 1.27
)

// Creepy is EnderMan.isCreepy, the screaming flag: the enderman has a target.
func (m *Mob) Creepy() bool { return m.Type == TypeEnderman && m.TargetID != 0 }

// StaredAt is EnderMan.hasBeenStaredAt.
func (m *Mob) StaredAt() bool { return m.Type == TypeEnderman && m.enderman.staredAt }

// endermanGoals is EnderMan.registerGoals.
func endermanGoals(m *Mob) []Goal {
	return []Goal{
		newFloatGoal(m),
		&freezeWhenLookedAtGoal{m: m},
		&endermanLookGoal{m: m},
		&meleeAttackGoal{m: m, priority: 2},
		&hurtByGoal{m: m, priority: 2},
		&mobTargetGoal{m: m, victim: TypeEndermite, priority: 3},
		&resetAngerGoal{m: m},
		&randomStroll{m: m, priority: hostileStroll},
		&lookAtPlayer{m: m, priority: hostileLookAt},
		&randomLookAround{m: m, priority: hostileLookAround},
		&endermanLeaveBlockGoal{m: m},
		&endermanTakeBlockGoal{m: m},
	}
}

// playerEye is the y of a player's eyes.
func playerEye(p *Player) float64 {
	if p.Sneaking && p.Width == 0 {
		return p.Y + sneakingEyeHeight
	}
	return p.Y + p.eyeHeight()
}

// beingStaredBy is EnderMan.isBeingStaredBy: a player without a gaze
// disguise looks at the eyes with a clear line.
// vanilla: EnderMan.isBeingStaredBy, LivingEntity.isLookingAtMe
func (m *Mob) beingStaredBy(w World, p *Player) bool {
	if m.gazeDisguised(p) {
		return false
	}
	gazeY := m.Y + m.eyeHeight()
	eyeY := playerEye(p)
	dx, dy, dz := m.X-p.X, gazeY-eyeY, m.Z-p.Z
	dist := math.Sqrt(dx*dx + dy*dy + dz*dz)
	if dist == 0 {
		return false
	}
	yaw, pitch := p.Yaw*math.Pi/180, p.Pitch*math.Pi/180
	lx := -math.Sin(yaw) * math.Cos(pitch)
	ly := -math.Sin(pitch)
	lz := math.Cos(yaw) * math.Cos(pitch)
	dot := (lx*dx + ly*dy + lz*dz) / dist
	if !(dot > 1-stareCone/dist) {
		return false
	}
	return rayClear(w, p.X, eyeY, p.Z, m.X, gazeY, m.Z)
}

// gazeDisguised reports whether the player wears a head piece that hides
// them from a gaze.
// vanilla: LivingEntity.PLAYER_NOT_WEARING_DISGUISE_ITEM
func (m *Mob) gazeDisguised(p *Player) bool {
	head := p.Worn[wornHead]
	return !head.Empty() && registry.Item(head.ItemID).InTag(registry.ItemTagIDGazeDisguiseEquipment)
}

// rayClear reports whether the segment between the two points misses all
// block collision boxes.
// vanilla: LivingEntity.hasLineOfSight with ClipContext.Block.COLLIDER
func rayClear(w World, ox, oy, oz, tx, ty, tz float64) bool {
	return rayClearSkipping(w, ox, oy, oz, tx, ty, tz, nil)
}

// rayClearVisual is rayClear seeing through the blocks with an empty visual
// shape.
// vanilla: LivingEntity.hasLineOfSight with ClipContext.Block.VISUAL
func rayClearVisual(w World, ox, oy, oz, tx, ty, tz float64) bool {
	return rayClearSkipping(w, ox, oy, oz, tx, ty, tz, visuallyEmptyBlocks)
}

// rayClearSkipping is rayClear ignoring the blocks marked in skip, if any.
func rayClearSkipping(w World, ox, oy, oz, tx, ty, tz float64, skip *[registry.BlockCount]bool) bool {
	return !clipBlockShapes(w, ox, oy, oz, tx-ox, ty-oy, tz-oz, skip).ok
}

// playerByID finds a player of the tick by entity ID.
func (t *tickView) playerByID(id int32) *Player {
	for i := range t.players {
		if t.players[i].EntityID == id {
			return &t.players[i]
		}
	}
	return nil
}

// freezeWhenLookedAtGoal is EnderMan$EndermanFreezeWhenLookedAt: a target
// player looking at the enderman within sixteen blocks stops it. It holds
// the head too, which vanilla's look control does on its own.
type freezeWhenLookedAtGoal struct {
	m *Mob
}

func (g *freezeWhenLookedAtGoal) Priority() int    { return 1 }
func (g *freezeWhenLookedAtGoal) Flags() GoalFlags { return GoalMove | GoalJump | GoalLook }

func (g *freezeWhenLookedAtGoal) CanStart(t *tickView) bool {
	if g.m.TargetID == 0 {
		return false
	}
	p := t.playerByID(g.m.TargetID)
	if p == nil || !p.Huntable {
		return false
	}
	if Dist2(g.m.X, g.m.Y, g.m.Z, p.X, p.Y, p.Z) > stareFreezeRange2 {
		return false
	}
	return g.m.beingStaredBy(t.w, p)
}

func (g *freezeWhenLookedAtGoal) Start(*tickView) { g.m.stopSeeking() }

func (g *freezeWhenLookedAtGoal) ShouldContinue(t *tickView) bool { return g.CanStart(t) }

func (g *freezeWhenLookedAtGoal) Tick(t *tickView) {
	if p := t.playerByID(g.m.TargetID); p != nil {
		g.m.lookAtPoint(p.X, playerEye(p), p.Z)
	}
}

func (g *freezeWhenLookedAtGoal) Stop(*tickView) {}

func (g *freezeWhenLookedAtGoal) RequiresEveryTick() bool { return false }

// lookAtPoint turns the head towards a point at the usual head speed.
func (m *Mob) lookAtPoint(x, y, z float64) {
	dx, dz := x-m.X, z-m.Z
	m.HeadYaw = turnToward(m.HeadYaw, float32(yawToward(dx, dz)), headTurnSpeed)
	m.Pitch = turnToward(m.Pitch, float32(pitchToward(dx, y-(m.Y+m.eyeHeight()), dz)), headTurnSpeed)
}

// angerInducing is EndermanLookForPlayerGoal.isAngerInducing: a stare or an
// anger at the player.
func (m *Mob) angerInducing(t *tickView, p *Player) bool {
	return m.beingStaredBy(t.w, p) || m.angryAt(t.w, p)
}

// endermanLookGoal is EnderMan$EndermanLookForPlayerGoal: a player who
// stares, or whom it is angry at, for five ticks becomes the target; during
// the hunt a stared-at enderman blinks away from a near target and toward a
// far one. Unlike the nearest-player goal it checks every selection and
// never drops a target for distance or lost sight.
type endermanLookGoal struct {
	m *Mob
	// pendingID is the anger-inducing player not yet taken for the target.
	pendingID    int32
	aggroTime    int
	teleportTime int
}

func (g *endermanLookGoal) Priority() int    { return 1 }
func (g *endermanLookGoal) Flags() GoalFlags { return GoalTarget }

func (g *endermanLookGoal) CanStart(t *tickView) bool {
	p := g.m.nearestHuntable(t, g.m.angerInducing, true)
	if p == nil {
		g.pendingID = 0
		return false
	}
	g.pendingID = p.EntityID
	return true
}

func (g *endermanLookGoal) Start(*tickView) {
	g.aggroTime = reducedTickDelay(stareAggroDelay)
	g.teleportTime = 0
	g.m.SetMetadataEndermanStaredAt(true)
}

func (g *endermanLookGoal) ShouldContinue(t *tickView) bool {
	if g.pendingID != 0 {
		p := t.playerByID(g.pendingID)
		if p == nil || !p.Huntable || !g.m.angerInducing(t, p) {
			return false
		}
		g.m.lookAtPoint(p.X, playerEye(p), p.Z)
		return true
	}
	return g.m.targetIn(t) != nil
}

func (g *endermanLookGoal) Tick(t *tickView) {
	if g.pendingID != 0 {
		g.aggroTime--
		if g.aggroTime <= 0 {
			g.m.SetMetadataTargetID(g.pendingID)
			g.pendingID = 0
		}
		return
	}
	p := t.playerByID(g.m.TargetID)
	if p == nil || g.m.isPassenger() {
		return
	}
	d2 := Dist2(g.m.X, g.m.Y, g.m.Z, p.X, p.Y, p.Z)
	if g.m.beingStaredBy(t.w, p) {
		if d2 < stareBlinkRange2 {
			t.events = g.m.teleportRandom(t.w, t.events)
		}
		g.teleportTime = 0
		return
	}
	if d2 > stareFreezeRange2 {
		g.teleportTime++
		if g.teleportTime > reducedTickDelay(stareApproachDelay) {
			var ok bool
			if t.events, ok = g.m.teleportTowards(t.w, p, t.events); ok {
				g.teleportTime = 0
			}
		}
	}
}

func (g *endermanLookGoal) Stop(*tickView) {
	g.pendingID = 0
	g.m.SetMetadataEndermanStaredAt(false)
	g.m.SetMetadataTargetID(0)
}

func (g *endermanLookGoal) RequiresEveryTick() bool { return false }

// endermanStep runs the enderman's own tick: the target bookkeeping, the
// water's bite, the daylight, and the teleports a hurt asked for.
// vanilla: EnderMan.setTarget, LivingEntity.aiStep, EnderMan.customServerAiStep
func (m *Mob) endermanStep(w World, events []MobEvent) []MobEvent {
	e := &m.enderman
	// The view is shared and refiltered for each mover; keep this one's own.
	if b, ok := w.(*Bodies); ok {
		own := *b
		own.Skip = slices.Clone(b.Skip)
		w = &own
	}
	e.w = w
	events = append(events, e.hurtTeleports...)
	e.hurtTeleports = e.hurtTeleports[:0]
	if m.TargetID != e.lastTarget {
		if m.TargetID == 0 {
			m.SetMetadataEndermanStaredAt(false)
			e.targetChangeAge = 0
		} else {
			e.targetChangeAge = m.Age
		}
		e.lastTarget = m.TargetID
	}
	if m.Dead {
		return events
	}
	if !m.NoAI && w.BrightOutside() && m.Age >= e.targetChangeAge+endermanDaylightDelay {
		c := m.cell()
		brightness := lightMagicValue(w.MaxLocalRawLightAt(c.X, int32(math.Floor(m.Y+m.eyeHeight())), c.Z))
		if brightness > 0.5 && canSeeSkyAt(w, c.X, c.Y, c.Z) && float32(m.Rand.Float64())*30 < (brightness-0.4)*2 {
			m.SetMetadataTargetID(0)
			events = m.teleportRandom(w, events)
		}
	}
	return events
}

// endermanHurtRoll is the teleport EnderMan.hurtServer rolls after a hurt
// that has no living entity behind it: nine times in ten, at once.
func (m *Mob) endermanHurtRoll(damageType registry.DamageType, creative bool) {
	if m.Type != TypeEnderman || m.Dead || m.isPassenger() || m.InvulnerableTo(damageType, creative) {
		return
	}
	if m.Rand.Intn(10) != 0 {
		m.hurtTeleport()
	}
}

// hurtTeleport is a teleport made inside a hurt; its event rides the next
// tick. It reports whether the enderman landed anywhere.
func (m *Mob) hurtTeleport() bool {
	e := &m.enderman
	if e.w == nil {
		return false
	}
	before := len(e.hurtTeleports)
	e.hurtTeleports = m.teleportRandom(e.w, e.hurtTeleports)
	return len(e.hurtTeleports) > before
}

// dodgesProjectile reports whether the hit is answered by a teleport instead
// of damage, and whether one landed within 64 tries (only then does the hit
// count as landed).
// vanilla: EnderMan.hurtServer (IS_PROJECTILE)
func (m *Mob) dodgesProjectile(damageType registry.DamageType, creative bool) (dodges, teleported bool) {
	m.enderman.dodged = false
	if m.Type != TypeEnderman || m.Dead || m.isPassenger() || m.InvulnerableTo(damageType, creative) {
		return false, false
	}
	for range projectileTeleportTries {
		if m.hurtTeleport() {
			m.enderman.dodged = true
			return true, true
		}
	}
	return true, false
}

// TookDodge reports, once, whether the last projectile hit was dodged; such
// a projectile flies on without sound or effect.
// vanilla: AbstractArrow.onHitEntity, ThrownTrident.onHitEntity
func (m *Mob) TookDodge() bool {
	d := m.enderman.dodged
	m.enderman.dodged = false
	return d
}

// teleportRandom is EnderMan.teleport(): a random spot within thirty-two
// blocks sideways and thirty-two up or down.
func (m *Mob) teleportRandom(w World, events []MobEvent) []MobEvent {
	if m.Dead || m.isPassenger() {
		return events
	}
	x := m.X + (m.Rand.Float64()-0.5)*64
	y := m.Y + float64(m.Rand.Intn(64)-32)
	z := m.Z + (m.Rand.Float64()-0.5)*64
	return m.teleportTo(w, x, y, z, registry.TagIDEndermanDoesNotTeleportTo, events)
}

// teleportTowards is EnderMan.teleportTowards: sixteen blocks along the
// line to the player, with a spread.
func (m *Mob) teleportTowards(w World, p *Player, events []MobEvent) ([]MobEvent, bool) {
	if m.isPassenger() {
		return events, false
	}
	dx, dy, dz := m.X-p.X, m.Y+m.height()/2-playerEye(p), m.Z-p.Z
	n := math.Sqrt(dx*dx + dy*dy + dz*dz)
	if n > 0 {
		dx, dy, dz = dx/n, dy/n, dz/n
	}
	x := m.X + (m.Rand.Float64()-0.5)*8 - dx*16
	y := m.Y + float64(m.Rand.Intn(16)-8) - dy*16
	z := m.Z + (m.Rand.Float64()-0.5)*8 - dz*16
	before := len(events)
	events = m.teleportTo(w, x, y, z, registry.TagIDEndermanDoesNotTeleportTo, events)
	return events, len(events) > before
}

// borderClamper is a world that moves a point into its border; a test
// world without one has no border.
// vanilla: WorldBorder.clampVec3ToBound
type borderClamper interface {
	ClampToBorder(x, z float64) (float64, float64)
}

// teleportTo is LivingEntity.randomTeleport: the body drops to the first
// block below the spot it can land on, and stays put if that floor or a
// block in its box is in the avoid tag, the box is in a block or fluid, or,
// for an enderman, the floor holds water. The spot is first clamped into
// the border when the world has one. A landing is reported as
// MobTeleported from the old place. An unloaded column reads as air and has
// no ground.
func (m *Mob) teleportTo(w World, x, y, z float64, avoidTag registry.BlockTagID, events []MobEvent) []MobEvent {
	if b, ok := w.(borderClamper); ok {
		x, z = b.ClampToBorder(x, z)
	}
	bx, by, bz := int32(math.Floor(x)), int32(math.Floor(y)), int32(math.Floor(z))
	minY := w.MinY()
	landed := false
	for !landed && by > minY {
		if rules.CanTeleportTo(w.BlockAt(bx, by-1, bz)) {
			landed = true
			continue
		}
		y--
		by--
	}
	if !landed {
		return events
	}
	floor := w.BlockAt(bx, by-1, bz)
	avoid := func(state uint16) bool {
		return registry.BlockOf(state).InTag(avoidTag)
	}
	// vanilla: LivingEntity.checkPositionAndTeleport, EnderMan.canRandomlyTeleportTo
	if avoid(floor) || m.Type == TypeEnderman && rules.IsWater(floor) {
		return events
	}
	oldX, oldY, oldZ := m.X, m.Y, m.Z
	m.SetPos(x, y, z)
	b := m.mobBox()
	if m.BoxBlocked(w) || rules.AnyBlockIn(w, b.minX, b.minY, b.minZ, b.maxX, b.maxY, b.maxZ,
		func(s uint16) bool { return world.HoldsFluid(s) || avoid(s) }) {
		m.SetPos(oldX, oldY, oldZ)
		return events
	}
	m.stopSeeking()
	return append(events, MobEvent{Mob: m, Kind: MobTeleported, X: oldX, Y: oldY, Z: oldZ, VX: x, VY: y, VZ: z})
}
