package entity

import (
	"math"
	"sort"

	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// The shulker: stuck to a block face, it opens for a player in range and
// hops to another face when its own is gone. The client moves a player
// pushed by an opening shell; the server sends no position sync.
// vanilla: Shulker

const TypeShulker = registry.EntityTypeShulker

func init() {
	// vanilla: EntityTypes.SHULKER, Shulker.createAttributes. A sound of
	// its own plays for a hurt while the shell is shut.
	kinds[TypeShulker] = &kind{
		maxHeadXRot: 180, maxHeadYRot: 180,
		name: "Shulker", width: 1, height: 1, eyeHeight: 0.5,
		maxHealth: 30, hostile: true, livesInPeaceful: true, fireImmune: true,
		sounds: MobSounds{
			Ambient: registry.SoundEntityShulkerAmbient,
			Hurt:    registry.SoundEntityShulkerHurt,
			Death:   registry.SoundEntityShulkerDeath,
		},
		ambientInterval: 120, // vanilla: AbstractGolem.getAmbientSoundInterval
	}
	// vanilla: EntityTypes.SHULKER clientTrackingRange(10)
	mobTracking[TypeShulker] = tracking{rangeChunks: 10, updateInterval: 3}
}

// The shulker's numbers.
const (
	shulkerOpenPeek      = 100 // ShulkerAttackGoal.start
	shulkerIdlePeek      = 30  // ShulkerPeekGoal.start
	shulkerCoveredArmor  = 20.0
	shulkerTeleportTries = 5
	shulkerTeleportSpan  = 8
	shulkerShootRange2   = 400.0 // ShulkerAttackGoal.tick, 20 blocks squared
	shulkerSlabReach     = 4.0   // ShulkerNearestAttackGoal.getTargetSearchArea
	shulkerPeekChance    = 40    // ShulkerPeekGoal.canUse
	shulkerHurtTeleport  = 4     // Shulker.hurtServer's nextInt(4)
	shulkerDeflate       = 1e-6
	shulkerCopyReach     = 8.0 // Shulker.hitByShulkerBullet's inflate(8.0)
	shulkerFirstShot     = 20  // ShulkerAttackGoal.start's attackTime
	shulkerShotBase      = 20  // ShulkerAttackGoal.tick: 20 + nextInt(10) * 20 / 2
	shulkerShotSpread    = 10
)

// Direction ids of the attach face, in Direction's own order.
const (
	faceDown uint8 = iota
	faceUp
	faceNorth
	faceSouth
	faceWest
	faceEast
)

// faceStep is each direction's unit step, indexed by id.
var faceStep = [6][3]int32{{0, -1, 0}, {0, 1, 0}, {0, 0, -1}, {0, 0, 1}, {-1, 0, 0}, {1, 0, 0}}

// shulkerState is what a shulker keeps beyond the common mob.
type shulkerState struct {
	// face is DATA_ATTACH_FACE_ID: the side of the cell it clings to.
	face uint8
	// peek is DATA_PEEK_ID: 0 shut, up to 100 fully open.
	peek uint8
	// covered is the "covered" armour modifier, added the first time the shell
	// shuts and removed when it opens, so a never-opened shulker has no armour.
	// vanilla: Shulker.setRawPeekAmount
	covered bool
	// armorSet records the armour instance first accessed by a shell transition.
	armorSet bool
	// teleport is a hurt's roll that came up, run on the next step.
	teleport bool
	// split is a landed bullet's hit awaiting its teleport and copy, from
	// the box the shulker stood in.
	split     bool
	splitFrom AABB
	// peekNow is currentPeekAmount: the opening the box has reached, 0 to 1,
	// moving 0.05 a tick toward peek/100.
	peekNow float32
	// colorOff is DATA_COLOR_ID less its default 16, wrapping, so the zero
	// value is the undyed default; the byte is kept as given, past 15 too.
	colorOff uint8
}

// IsShulker reports whether the mob is a shulker.
func (m *Mob) IsShulker() bool { return m.Type == TypeShulker }

// CollidesWithAny is canBeCollidedWith(null): a live shulker, or a live
// adult happy ghast on its still timeout; every other mob is passed through.
// vanilla: Shulker.canBeCollidedWith, HappyGhast.canBeCollidedWith
func (m *Mob) CollidesWithAny() bool {
	switch m.Type {
	case TypeShulker:
		return m.Alive()
	case TypeHappyGhast:
		return m.Alive() && !m.IsBaby() && m.StaysStill()
	}
	return false
}

// ShulkerFace is the shulker's attach face direction id and ShulkerPeek its
// raw shell opening.
func (m *Mob) ShulkerFace() uint8 { return m.shulker.face }
func (m *Mob) ShulkerPeek() uint8 { return m.shulker.peek }

// SetShulkerPeek sets the raw shell opening, 0 to 100, as the Peek tag does.
func (m *Mob) SetShulkerPeek(raw uint8) { m.SetMetadataShulkerPeek(raw) }

// ShulkerClosed reports whether the shell is shut.
// vanilla: Shulker.isClosed
func (m *Mob) ShulkerClosed() bool { return m.IsShulker() && m.shulker.peek == 0 }

// snapShulker puts the shulker on the centre of its block cell.
// vanilla: Shulker.setPos
func (m *Mob) snapShulker() {
	m.SetPos(math.Floor(m.X)+0.5, math.Floor(m.Y+0.5), math.Floor(m.Z)+0.5)
}

// setPeek opens or shuts the shell, with the sound and game event that go
// with it.
// vanilla: Shulker.setRawPeekAmount
func (m *Mob) setPeek(amount uint8, t *tickView) {
	m.shulker.armorSet = true
	m.shulker.covered = amount == 0
	if m.attrs != nil {
		m.syncShulkerArmor(m.attrs)
	}
	m.SetMetadataShulkerPeek(amount)
	t.events = append(t.events, MobEvent{Mob: m, Kind: MobShulkerShell, Status: int32(amount)})
}

// noteShulkerHurt rolls the teleport a hurt below half health may start.
// vanilla: Shulker.hurtServer
func (m *Mob) noteShulkerHurt() {
	if m.IsShulker() && m.Health > 0 && m.Health < m.kind().maxHealth*0.5 && m.Rand.Intn(shulkerHurtTeleport) == 0 {
		m.shulker.teleport = true
	}
}

// HurtByShulkerBullet is a bullet's blow. An open shulker it hits may
// teleport and leave a copy behind.
// vanilla: Shulker.hurtServer
func (m *Mob) HurtByShulkerBullet(amount, flightVX, flightVZ float64, shooterID int32, shooterIsPlayer, shooterCreative bool, source ...*HitSource) (landed, softened bool) {
	from := m.mobBox()
	landed, softened = m.HurtByProjectile(amount, rules.DamageTypeMobProjectile, flightVX, flightVZ, shooterID, shooterIsPlayer, shooterCreative, source...)
	if landed && m.IsShulker() && !m.shulker.teleport {
		m.shulker.split, m.shulker.splitFrom = true, from
	}
	return landed, softened
}

// shulkerGoals is Shulker.registerGoals.
func shulkerGoals(m *Mob) []Goal {
	return []Goal{
		&lookAtPlayer{m: m, priority: 1},
		&hurtByGoal{m: m, priority: 1, alertOthers: true, ignore: func(k MobType) bool { return k == TypeShulker }},
		&nearestTargetGoal{m: m, priority: 2, accept: shulkerInSlab(m),
			gate: func(t *tickView) bool { return t.w.Difficulty() != rules.DifficultyPeaceful }},
		&mobTargetGoal{m: m, priority: 3, matches: shulkerDefends(m), area: shulkerInMobSlab(m), gate: func() bool { return m.Team != "" }},
		&shulkerAttackGoal{m: m},
		&shulkerPeekGoal{m: m},
		&randomLookAround{m: m, priority: 8},
	}
}

// shulkerSlabReaches is how far the hunt reaches along each axis.
func shulkerSlabReaches(m *Mob) [3]float64 {
	reach := [3]float64{m.followRange(), m.followRange(), m.followRange()}
	for i, s := range faceStep[m.shulker.face] {
		if s != 0 {
			reach[i] = shulkerSlabReach
		}
	}
	return reach
}

// shulkerInSlab limits the hunt to the shulker's box grown four blocks along
// its attach axis and by the follow range on the other two.
// vanilla: Shulker.ShulkerNearestAttackGoal.getTargetSearchArea
func shulkerInSlab(m *Mob) func(*tickView, *Player) bool {
	return func(_ *tickView, p *Player) bool {
		reach := shulkerSlabReaches(m)
		pw, ph, _ := p.box()
		b, hw := m.mobBox(), pw/2
		return p.X+hw > b.minX-reach[0] && p.X-hw < b.maxX+reach[0] &&
			p.Y+ph > b.minY-reach[1] && p.Y < b.maxY+reach[1] &&
			p.Z+hw > b.minZ-reach[2] && p.Z-hw < b.maxZ+reach[2]
	}
}

// shulkerDefends is ShulkerDefenseAttackGoal's selector: an Enemy that is
// not on the shulker's own team.
func shulkerDefends(m *Mob) func(*Mob) bool {
	return func(o *Mob) bool { return Enemy(o.Type) && (m.Team == "" || o.Team != m.Team) }
}

// shulkerInMobSlab is shulkerInSlab for a mob's box.
// vanilla: Shulker.ShulkerDefenseAttackGoal.getTargetSearchArea
func shulkerInMobSlab(m *Mob) func(*Mob) bool {
	return func(o *Mob) bool {
		b, mb, reach := o.mobBox(), m.mobBox(), shulkerSlabReaches(m)
		return b.maxX > mb.minX-reach[0] && b.minX < mb.maxX+reach[0] &&
			b.maxY > mb.minY-reach[1] && b.minY < mb.maxY+reach[1] &&
			b.maxZ > mb.minZ-reach[2] && b.minZ < mb.maxZ+reach[2]
	}
}

// shulkerAttackGoal is Shulker.ShulkerAttackGoal: the shell opens wide
// while a target is held, and a target beyond 20 blocks is let go.
type shulkerAttackGoal struct {
	m          *Mob
	attackTime int
}

func (g *shulkerAttackGoal) Priority() int    { return 4 }
func (g *shulkerAttackGoal) Flags() GoalFlags { return GoalMove | GoalLook }

func (g *shulkerAttackGoal) CanStart(t *tickView) bool {
	return g.m.targetIn(t) != nil && t.w.Difficulty() != rules.DifficultyPeaceful
}

func (g *shulkerAttackGoal) ShouldContinue(t *tickView) bool { return g.CanStart(t) }
func (g *shulkerAttackGoal) Start(t *tickView) {
	g.attackTime = shulkerFirstShot
	g.m.setPeek(shulkerOpenPeek, t)
}

func (g *shulkerAttackGoal) Stop(t *tickView)        { g.m.setPeek(0, t) }
func (g *shulkerAttackGoal) RequiresEveryTick() bool { return true }

func (g *shulkerAttackGoal) Tick(t *tickView) {
	p := g.m.targetIn(t)
	if p == nil {
		return
	}
	if t.w.Difficulty() == rules.DifficultyPeaceful {
		return
	}
	g.attackTime--
	g.m.lookAtTarget(p)
	if Dist2(g.m.X, g.m.Y, g.m.Z, p.X, p.Y, p.Z) >= shulkerShootRange2 {
		g.m.SetMetadataTargetID(0)
		return
	}
	if g.attackTime > 0 {
		return
	}
	m := g.m
	g.attackTime = shulkerShotBase + m.Rand.Intn(shulkerShotSpread)*20/2
	box := m.mobBox()
	t.events = append(t.events,
		MobEvent{Mob: m, Kind: MobShulkerShot, Player: *p, Status: int32(bulletAxis[m.shulker.face]),
			X: (box.minX + box.maxX) / 2, Y: (box.minY + box.maxY) / 2, Z: (box.minZ + box.maxZ) / 2,
			Seed: m.Rand.Int63()})
	if !m.Silent {
		t.events = append(t.events, MobEvent{Mob: m, Kind: MobSounded, SoundID: registry.SoundEntityShulkerShoot,
			Volume: 2, Pitch: (m.Rand.Float32()-m.Rand.Float32())*0.2 + 1})
	}
}

// shulkerPeekGoal is Shulker.ShulkerPeekGoal: an idle shulker cracks its
// shell for one to three seconds now and then.
type shulkerPeekGoal struct {
	m        *Mob
	peekTime int
}

func (g *shulkerPeekGoal) Priority() int    { return 7 }
func (g *shulkerPeekGoal) Flags() GoalFlags { return 0 }

func (g *shulkerPeekGoal) CanStart(t *tickView) bool {
	m := g.m
	return m.targetIn(t) == nil && m.Rand.Intn(reducedTickDelay(shulkerPeekChance)) == 0 &&
		m.canStayAt(m.shulkerWorld(t), m.cell(), m.shulker.face)
}

func (g *shulkerPeekGoal) ShouldContinue(t *tickView) bool {
	return g.m.targetIn(t) == nil && g.peekTime > 0
}

func (g *shulkerPeekGoal) Start(t *tickView) {
	g.peekTime = reducedTickDelay(20 * (1 + g.m.Rand.Intn(3)))
	g.m.setPeek(shulkerIdlePeek, t)
}

func (g *shulkerPeekGoal) Stop(t *tickView) {
	if g.m.targetIn(t) == nil {
		g.m.setPeek(0, t)
	}
}

func (g *shulkerPeekGoal) Tick(*tickView)          { g.peekTime-- }
func (g *shulkerPeekGoal) RequiresEveryTick() bool { return false }

// shulkerWorld is the world for a shulker's collision queries: the world's
// bodies plus other live shulkers, excluding its own ride chain.
// vanilla: Shulker.canBeCollidedWith, CollisionGetter.noCollision
type shulkerWorld struct {
	World
	self   *Mob
	others []*Mob
}

func (s shulkerWorld) BodyBoxes(box AABB) []AABB {
	var out []AABB
	if b, ok := s.World.(BodyWorld); ok {
		out = b.BodyBoxes(box)
	}
	for _, o := range s.others {
		if o.IsShulker() && o.Alive() && o.RootVehicle() != s.self.RootVehicle() {
			// Bound every attach face's fully open box before computing its peek.
			side, down, up := 1.5, 1.0, 2.0
			// Most candidates have no attribute edits; avoid an attribute read
			// for every distant pair in the herd.
			if o.attrs != nil {
				size := o.attrScale()
				side, down, up = side*size, down*size, up*size
			}
			if o.X+side < box.minX || o.X-side > box.maxX || o.Z+side < box.minZ || o.Z-side > box.maxZ ||
				o.Y+up < box.minY || o.Y-down > box.maxY {
				continue
			}
			if hull := o.mobBox(); box.Crosses(hull) {
				out = append(out, hull)
			}
		}
	}
	return out
}

func (m *Mob) shulkerWorld(t *tickView) shulkerWorld {
	return shulkerWorld{World: t.w, self: m, others: t.others}
}

// shulkerStep is Shulker.tick's server half: no velocity movement, a lost
// attachment replaced by another face or a teleport, a hurt's roll, and the
// box's opening.
func (m *Mob) shulkerStep(t *tickView) {
	m.VX, m.VY, m.VZ = 0, 0, 0
	w := m.shulkerWorld(t)
	if !m.NoAI {
		if m.shulker.teleport {
			m.shulker.teleport = false
			m.teleportShulker(w, t)
		}
		if m.shulker.split {
			m.shulker.split = false
			m.copyShulker(w, t)
		}
	}
	// A NoAI shulker still reattaches; teleportShulker refuses it.
	at := m.cell()
	if !m.RidesVehicle && !m.canStayAt(w, at, m.shulker.face) {
		if face, ok := m.attachableFace(w, at); ok {
			m.SetMetadataShulkerFace(face)
		} else {
			m.teleportShulker(w, t)
		}
	}
	m.stepShulkerPeek(w, t)
}

// copyShulker teleports an open shulker a bullet hit and, unless the
// neighbourhood is crowded, leaves a new shulker where it stood.
// vanilla: Shulker.hitByShulkerBullet
func (m *Mob) copyShulker(w World, t *tickView) {
	if m.shulker.peek == 0 || !m.teleportShulker(w, t) {
		return
	}
	from := m.shulker.splitFrom
	near := from.inflate(shulkerCopyReach, shulkerCopyReach, shulkerCopyReach)
	count := 1
	if !near.Crosses(m.mobBox()) {
		count = 0
	}
	for _, o := range t.others {
		if o != m && o.IsShulker() && o.Alive() && near.Crosses(o.mobBox()) {
			count++
		}
	}
	if m.Rand.Float32() < float32(count-1)/5 {
		return
	}
	t.events = append(t.events, MobEvent{Mob: m, Kind: MobShulkerCopied, Status: int32(m.dyeColor()),
		X: (from.minX + from.maxX) / 2, Y: from.minY, Z: (from.minZ + from.maxZ) / 2})
}

// attachableFace is the first face, in Direction order, the shulker can
// hold at the cell.
// vanilla: Shulker.findAttachableSurface
func (m *Mob) attachableFace(w World, at Waypoint) (uint8, bool) {
	for face := faceDown; face <= faceEast; face++ {
		if m.canStayAt(w, at, face) {
			return face, true
		}
	}
	return 0, false
}

// canStayAt reports whether the cell is free, the block on the face is full
// toward it and the fully opened shell has room.
// vanilla: Shulker.canStayAt
func (m *Mob) canStayAt(w World, at Waypoint, face uint8) bool {
	if state := w.BlockAt(at.X, at.Y, at.Z); !world.IsAirState(state) &&
		(!world.IsMovingPiston(state) || at != m.cell()) {
		return false
	}
	s := faceStep[face]
	if !faceFull(w.BlockAt(at.X+s[0], at.Y+s[1], at.Z+s[2]), face^1) {
		return false
	}
	// getProgressAabb at full peek, deflated.
	open := shulkerDeltaBox(m.attrScale(), face^1, -1, 1, float64(at.X)+0.5, float64(at.Y), float64(at.Z)+0.5)
	return !overlapsSolid(w, collideCtx{}, open.inflate(-shulkerDeflate, -shulkerDeflate, -shulkerDeflate))
}

// teleportShulker tries five random cells within eight blocks and takes the
// first free one that has a face to hold.
// vanilla: Shulker.teleportSomewhere
func (m *Mob) teleportShulker(w World, t *tickView) bool {
	if m.NoAI || !m.Alive() {
		return false
	}
	at := m.cell()
	span := 2*shulkerTeleportSpan + 1
	for range shulkerTeleportTries {
		to := Waypoint{
			X: at.X + int32(m.Rand.Intn(span)-shulkerTeleportSpan),
			Y: at.Y + int32(m.Rand.Intn(span)-shulkerTeleportSpan),
			Z: at.Z + int32(m.Rand.Intn(span)-shulkerTeleportSpan),
		}
		if to.Y <= w.MinY() || !world.IsAirState(w.BlockAt(to.X, to.Y, to.Z)) || !w.InBorder(to.X, to.Z) {
			continue
		}
		cell := AABB{float64(to.X), float64(to.Y), float64(to.Z), float64(to.X) + 1, float64(to.Y) + 1, float64(to.Z) + 1}
		if overlapsSolid(w, collideCtx{}, cell.inflate(-shulkerDeflate, -shulkerDeflate, -shulkerDeflate)) {
			continue
		}
		face, ok := m.attachableFace(w, to)
		if !ok {
			continue
		}
		step := faceStep[face]
		if registry.BlockOf(w.BlockAt(to.X+step[0], to.Y+step[1], to.Z+step[2])).InTag(registry.TagIDShulkerDoesNotTeleportTo) {
			continue
		}
		m.getOffMount(t)
		m.RidesVehicle = false
		ev := MobEvent{Mob: m, Kind: MobShulkerTeleported, X: m.X, Y: m.Y, Z: m.Z}
		m.SetMetadataShulkerFace(face)
		m.SetPos(float64(to.X)+0.5, float64(to.Y), float64(to.Z)+0.5)
		ev.VX, ev.VY, ev.VZ = m.X, m.Y, m.Z
		t.events = append(t.events, ev)
		m.SetMetadataShulkerPeek(0)
		m.SetMetadataTargetID(0)
		return true
	}
	return false
}

// faceFull reports whether the state's collision shape covers its whole
// face toward the direction id.
// vanilla: Block.isFaceFull
func faceFull(state uint16, face uint8) bool {
	axis, high := [6]int{1, 1, 2, 2, 0, 0}[face], face%2 == 1
	u, v := (axis+1)%3, (axis+2)%3
	var boxes [][2][3]float64
	cuts := [2][]float64{{0, 1}, {0, 1}}
	for _, b := range blockBoxes(state) {
		lo := [3]float64{b.MinX, b.MinY, b.MinZ}
		hi := [3]float64{b.MaxX, b.MaxY, b.MaxZ}
		if (high && hi[axis] != 1) || (!high && lo[axis] != 0) || lo[axis] == hi[axis] {
			continue
		}
		boxes = append(boxes, [2][3]float64{lo, hi})
		cuts[0] = append(cuts[0], lo[u], hi[u])
		cuts[1] = append(cuts[1], lo[v], hi[v])
	}
	if len(boxes) == 0 {
		return false
	}
	// Every cell the box edges cut the face into must lie inside a box.
	for i := range cuts {
		sort.Float64s(cuts[i])
	}
	for i := 0; i+1 < len(cuts[0]); i++ {
		for j := 0; j+1 < len(cuts[1]); j++ {
			cu, cv := (cuts[0][i]+cuts[0][i+1])/2, (cuts[1][j]+cuts[1][j+1])/2
			if cu <= 0 || cu >= 1 || cv <= 0 || cv >= 1 || cuts[0][i] == cuts[0][i+1] || cuts[1][j] == cuts[1][j+1] {
				continue
			}
			covered := false
			for _, b := range boxes {
				covered = covered || (cu > b[0][u] && cu < b[1][u] && cv > b[0][v] && cv < b[1][v])
			}
			if !covered {
				return false
			}
		}
	}
	return true
}
