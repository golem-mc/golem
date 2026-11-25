package entity

import (
	"math"
	"slices"

	"github.com/golem-mc/golem/registry"
)

// TypeGoat is minecraft:goat.
const TypeGoat = registry.EntityTypeGoat

// The goat's numbers.
// vanilla: Goat, GoatAi
const (
	goatScreamingOdds  = 0.02 // Goat.GOAT_SCREAMING_CHANCE
	goatUnihornOdds    = float64(float32(0.1))
	goatFallReduction  = 10                     // Goat.GOAT_FALL_DAMAGE_REDUCTION
	goatRamMinDistance = 4                      // GoatAi.RAM_MIN_DISTANCE
	goatRamMaxDistance = 7                      // GoatAi.RAM_MAX_DISTANCE
	goatRamPrepare     = 20                     // GoatAi.RAM_PREPARE_TIME
	goatRamPrepareTime = 160                    // PrepareRamNearestTarget.TIME_OUT_DURATION
	goatRamTimeout     = 200                    // RamTarget.TIME_OUT_DURATION
	goatSenseRange     = 16                     // follow_range, the nearest-entities sensor's box
	goatSenseInterval  = 20                     // Sensor.DEFAULT_SCAN_RATE
	goatPreparePace    = 1.25                   // GoatAi.SPEED_MULTIPLIER_WHEN_PREPARING_TO_RAM
	goatRamPace        = 3.0                    // GoatAi.SPEED_MULTIPLIER_WHEN_RAMMING
	goatRamSpeedFactor = float64(float32(1.65)) // RamTarget.RAM_SPEED_FORCE_FACTOR
	goatAdultKnockback = 2.5                    // GoatAi.ADULT_RAM_KNOCKBACK_FORCE
	goatBabyKnockback  = 1.0                    // GoatAi.BABY_RAM_KNOCKBACK_FORCE
	goatAdultDamage    = 2
	goatBabyDamage     = 1
)

// The cooldown ranges between two rams, in ticks.
// vanilla: GoatAi.TIME_BETWEEN_RAMS, TIME_BETWEEN_RAMS_SCREAMER
const (
	goatRamGapMin         = 600
	goatRamGapMax         = 6000
	goatScreamingRamGapMn = 100
	goatScreamingRamGapMx = 300
)

var (
	goatSnapsHorn = registry.BlockTagOf("minecraft:snaps_goat_horn")
	goatGround    = registry.BlockTagOf("minecraft:goats_spawnable_on")
)

var goatSounds = MobSounds{
	Ambient: registry.SoundEntityGoatAmbient,
	Hurt:    registry.SoundEntityGoatHurt,
	Death:   registry.SoundEntityGoatDeath,
	Step:    registry.SoundEntityGoatStep,
	Eat:     registry.SoundEntityGoatEat,
}

var screamingGoatSounds = MobSounds{
	Ambient: registry.SoundEntityGoatScreamingAmbient,
	Hurt:    registry.SoundEntityGoatScreamingHurt,
	Death:   registry.SoundEntityGoatScreamingDeath,
	Step:    registry.SoundEntityGoatStep,
	Eat:     registry.SoundEntityGoatScreamingEat,
}

func init() {
	// vanilla: EntityTypes.GOAT's builder and Goat.createAttributes. Box 0.9
	// by 1.3, ten of health, movement speed 0.2f, attack damage 2.
	kinds[TypeGoat] = &kind{
		maxHeadYRot:  15,
		name:         "Goat",
		fireAverse:   true,
		width:        0.9,
		height:       1.3,
		eyeHeight:    1.105,
		maxHealth:    10,
		speed:        0.20000000298023224,
		attackDamage: goatAdultDamage,
		// Goat.BABY_DIMENSIONS: scalable(0.45, 0.65), eyes 0.59375.
		babyWidth:  0.45,
		babyHeight: 0.65,
		babyEye:    0.59375,
		temptTag:   &registry.ItemTagIDGoatFood,
		sounds:     goatSounds,

		followPace:      1.25, // GoatAi.SPEED_MULTIPLIER_WHEN_FOLLOWING_ADULT
		panicPace:       2.0,  // GoatAi.SPEED_MULTIPLIER_WHEN_PANICKING
		temptPace:       1.25, // GoatAi.SPEED_MULTIPLIER_WHEN_TEMPTED
		strollPace:      1.0,  // GoatAi.SPEED_MULTIPLIER_WHEN_IDLING
		ambientInterval: 120,  // Animal.getAmbientSoundInterval's own
	}
}

// GoatData is a goat's own state.
type GoatData struct {
	// Screaming is Goat.DATA_IS_SCREAMING_GOAT.
	Screaming bool
	// NoLeftHorn and NoRightHorn are DATA_HAS_LEFT_HORN and
	// DATA_HAS_RIGHT_HORN gone false.
	NoLeftHorn, NoRightHorn bool

	ram   goatRam
	jump  goatJump
	sense goatSense
	look  goatLook
}

// goatRam is the brain's ram cooldown and the two ramming behaviours'
// shared state.
// vanilla: GoatAi, PrepareRamNearestTarget, RamTarget
type goatRam struct {
	// cooldown is RAM_COOLDOWN_TICKS: no ram starts while it is above 0.
	cooldown int

	phase      goatPhase
	targetID   int32
	targetCell Waypoint
	startCell  Waypoint
	elapsed    int
	atStart    int
	lowered    bool
	dirX, dirZ float64
	edgeX      float64
	edgeZ      float64
}

type goatPhase uint8

const (
	goatIdle goatPhase = iota
	goatPreparing
	goatCharging
)

// rollGoatGap is UniformInt.sample of the gap between two rams.
func rollGoatGap(rng Dice, screaming bool) int {
	if screaming {
		return goatScreamingRamGapMn + int(rng.NextIntN(goatScreamingRamGapMx-goatScreamingRamGapMn+1))
	}
	return goatRamGapMin + int(rng.NextIntN(goatRamGapMax-goatRamGapMin+1))
}

// goatFailGap is the cooldown a ram that found no place takes: the range's
// lower bound.
// vanilla: GoatAi.initRamActivity's PrepareRamNearestTarget getCooldownOnFail
func goatFailGap(screaming bool) int {
	if screaming {
		return goatScreamingRamGapMn
	}
	return goatRamGapMin
}

// finalizeGoat rolls the long jump and ram cooldowns, the screaming variant
// and the missing horn.
// vanilla: Goat.finalizeSpawn, GoatAi.initMemories
func (m *Mob) finalizeGoat(dice Dice) {
	m.longJump().cooldown = rollJumpGap(m, dice)
	m.Goat.ram.cooldown = rollGoatGap(dice, false)
	m.SetMetadataGoatScreaming(dice.NextDouble() < goatScreamingOdds)
	if !m.IsBaby() && float64(dice.NextFloat()) < goatUnihornOdds {
		if dice.NextBoolean() {
			m.SetMetadataGoatNoLeftHorn(true)
		} else {
			m.SetMetadataGoatNoRightHorn(true)
		}
	}
}

// breedGoat is Goat.getBreedOffspring: the calf is screaming if the parent
// it takes after is, or one time in fifty regardless.
// vanilla: Goat.getBreedOffspring
func (m *Mob) breedGoat(baby, partner *Mob) {
	parent := m
	if m.Rand.Intn(2) == 1 {
		parent = partner
	}
	baby.SetMetadataGoatScreaming((parent.Type == TypeGoat && parent.Goat.Screaming) || m.Rand.Float64() < goatScreamingOdds)
}

// HasHorns reports whether the goat can still lose a horn.
func (m *Mob) HasHorns() bool { return !m.Goat.NoLeftHorn || !m.Goat.NoRightHorn }

// dropHorn is Goat.dropHorn without the item: a grown goat with a horn left
// loses one, and the horn velocity is rolled.
// vanilla: Goat.dropHorn
func (m *Mob) dropHorn() (vx, vy, vz float64, ok bool) {
	if m.IsBaby() || !m.HasHorns() {
		return 0, 0, 0, false
	}
	switch {
	case m.Goat.NoLeftHorn:
		m.SetMetadataGoatNoRightHorn(true)
	case m.Goat.NoRightHorn:
		m.SetMetadataGoatNoLeftHorn(true)
	case m.Rand.Intn(2) == 0:
		m.SetMetadataGoatNoLeftHorn(true)
	default:
		m.SetMetadataGoatNoRightHorn(true)
	}
	between := func(lo, hi float32) float64 { return float64(m.Rand.Float32()*(hi-lo) + lo) }
	return between(-0.2, 0.2), between(0.3, 0.7), between(-0.2, 0.2), true
}

// goatSound selects one of the goat's own events: the screaming
// variant has its own set.
func (m *Mob) goatSound(plain, screaming registry.Sound) registry.Sound {
	if m.Goat.Screaming {
		return screaming
	}
	return plain
}

// RamImpactSound is the sound of a ram landing.
// vanilla: GoatAi.initRamActivity
func (m *Mob) RamImpactSound() registry.Sound {
	return m.goatSound(registry.SoundEntityGoatRamImpact, registry.SoundEntityGoatScreamingRamImpact)
}

// goatGoals wires the goat's mind: the passive animal's goals with the ram
// ahead of the stroll.
// vanilla: GoatAi
func goatGoals(m *Mob) []Goal {
	goals := passiveGoals(m)
	out := make([]Goal, 0, len(goals)+2)
	for _, g := range goals {
		switch g.(type) {
		case *panicGoal:
			g = &panicGoal{m: m, pace: 2, brain: true} // vanilla: GoatAi.initCoreActivity AnimalPanic(2.0f)
		case *randomStroll:
			out = append(out, &goatRamGoal{m: m}, &goatJumpGoal{m: m})
			g = newBrainIdle(m, g.Priority(), 1, m.lookTarget) // vanilla: GoatAi.initIdleActivity
		case *lookAtPlayer:
			// The brain looks through SetEntityLookTargetSometimes and has no
			// random look-around.
			out = append(out, &goatLookGoal{m: m})
			continue
		case *randomLookAround:
			continue
		}
		if f, follow := g.(*followParentGoal); follow {
			// vanilla: GoatAi.ADULT_FOLLOW_RANGE UniformInt.of(5, 16).
			// NEAREST_ADULT (AdultSensor) is not ported: a baby searches live.
			f.ranged = &followRange{min: 5, max: 16, scan: 16}
		}
		out = append(out, g)
	}
	return out
}

// goatRamGoal is PrepareRamNearestTarget then RamTarget: walk to a run-up
// cell in line with the nearest target, lower the head 20 ticks, charge.
// vanilla: PrepareRamNearestTarget, RamTarget
type goatRamGoal struct {
	m *Mob
}

// goatRamPriority puts the ram behind the tempt and the mating goats, which
// the brain's ram activity yields to, and ahead of the stroll.
const goatRamPriority = 4

func (g *goatRamGoal) Priority() int           { return goatRamPriority }
func (g *goatRamGoal) Flags() GoalFlags        { return GoalMove | GoalLook }
func (g *goatRamGoal) RequiresEveryTick() bool { return true }

// ramVictim is something the ram can hit: a player or another mob.
type ramVictim struct {
	id      int32
	x, y, z float64
	w, h    float64
	player  bool
	eye     float64
	vis     float64 // visibilityPercent of a player, 1 for a mob
	stand   bool
}

// StandTarget is a visible armor stand as the mob tick sees it: a goat's ram
// may target it. X, Y, Z is the feet; Eye the height of the eyes above them.
type StandTarget struct {
	ID      int32
	X, Y, Z float64
	W, H    float64
	Eye     float64
}

func (g *goatRamGoal) victims(t *tickView) []ramVictim {
	out := make([]ramVictim, 0, len(t.players)+len(t.others))
	for i := range t.players {
		p := &t.players[i]
		if t.canAttackPlayer(p) {
			pw, ph, eye := p.box()
			out = append(out, ramVictim{id: p.EntityID, x: p.X, y: p.Y, z: p.Z, w: pw, h: ph, player: true, eye: eye, vis: visibilityPercent(g.m, p)})
		}
	}
	for _, o := range t.others {
		if o != g.m && o.Alive() && o.Type != TypeGoat {
			out = append(out, ramVictim{id: o.ID, x: o.X, y: o.Y, z: o.Z, w: o.width(), h: o.height(), eye: o.eyeHeight(), vis: 1})
		}
	}
	// vanilla: GoatAi.RAM_TARGET_CONDITIONS (a stand only while mob_griefing is on)
	if t.w.MobGriefing() {
		for _, s := range t.stands {
			out = append(out, ramVictim{id: s.ID, x: s.X, y: s.Y, z: s.Z, w: s.W, h: s.H, eye: s.Eye, vis: 1, stand: true})
		}
	}
	return out
}

func (g *goatRamGoal) walkable(w World, cell Waypoint) bool {
	m := g.m
	foot := footprintOf(g.m.width(), g.m.height())
	return isStableDestination(w, m, cell) && !hasMalus(w, m, cell) && walkableAt(w, foot, cell.X, cell.Y, cell.Z)
}

// startCell is PrepareRamNearestTarget.calculateRammingStartPosition: the
// reachable cell furthest from the target along a walkable row, at least
// four away, nearest the goat first.
func (g *goatRamGoal) startCell(w World, target Waypoint) (Waypoint, bool) {
	if !g.walkable(w, target) {
		return Waypoint{}, false
	}
	here := g.m.cell()
	var spots []Waypoint
	for _, d := range [4][2]int32{{0, -1}, {1, 0}, {0, 1}, {-1, 0}} {
		cur := target
		for range goatRamMaxDistance {
			next := Waypoint{cur.X + d[0], cur.Y, cur.Z + d[1]}
			if !g.walkable(w, next) {
				break
			}
			cur = next
		}
		if abs32(cur.X-target.X)+abs32(cur.Z-target.Z) >= goatRamMinDistance {
			spots = append(spots, cur)
		}
	}
	slices.SortStableFunc(spots, func(a, b Waypoint) int {
		return int(distSq(a, here) - distSq(b, here))
	})
	for _, s := range spots {
		if s == here {
			return s, true
		}
		if path, _, ok := g.m.createPath(w, float64(s.X)+0.5, float64(s.Y), float64(s.Z)+0.5); ok && len(path) > 0 {
			return s, true
		}
	}
	return Waypoint{}, false
}

func abs32(v int32) int32 {
	if v < 0 {
		return -v
	}
	return v
}

func distSq(a, b Waypoint) int64 {
	dx, dy, dz := int64(a.X-b.X), int64(a.Y-b.Y), int64(a.Z-b.Z)
	return dx*dx + dy*dy + dz*dz
}

// CanStart runs PrepareRamNearestTarget.start once the cooldown is over and
// the sensor has scanned: one look for the nearest visible target, with the
// fail cooldown when there is none or no run-up cell. A charge cut short by
// its timeout resumes.
// vanilla: PrepareRamNearestTarget.start, PrepareRamNearestTarget.stop
func (g *goatRamGoal) CanStart(t *tickView) bool {
	r := &g.m.Goat.ram
	if r.phase == goatCharging {
		return true
	}
	if r.cooldown > 0 || g.m.InLove > 0 {
		return false
	}
	if !g.m.Goat.sense.scanned {
		return false
	}
	var best *ramVictim
	victims := g.victims(t)
	for _, id := range g.m.Goat.sense.nearby {
		i := slices.IndexFunc(victims, func(v ramVictim) bool { return v.id == id })
		if i < 0 {
			continue
		}
		if v := &victims[i]; g.m.goatSees(t, id, v.x, v.y, v.z, v.eye, v.vis) {
			best = v
			break
		}
	}
	if best == nil {
		g.failRam(t)
		return false
	}
	target := cellAt(best.x, best.y, best.z)
	start, ok := g.startCell(t.w, target)
	if !ok {
		g.failRam(t)
		return false
	}
	*r = goatRam{cooldown: r.cooldown, phase: goatPreparing, targetID: best.id, targetCell: target, startCell: start}
	return true
}

// failRam is PrepareRamNearestTarget.stop without a ram target: the raise
// head event goes out even though the head never lowered.
// vanilla: PrepareRamNearestTarget.stop
func (g *goatRamGoal) failRam(t *tickView) {
	g.m.Goat.ram.cooldown = goatFailGap(g.m.Goat.Screaming)
	t.events = append(t.events, MobEvent{Mob: g.m, Kind: MobGoatRaisedHead})
}

func (g *goatRamGoal) Start(t *tickView) {
	if g.m.Goat.ram.phase == goatCharging {
		g.beginCharge(t)
		return
	}
	g.m.seekTo(t.w, float64(g.m.Goat.ram.startCell.X)+0.5, float64(g.m.Goat.ram.startCell.Y), float64(g.m.Goat.ram.startCell.Z)+0.5, goatPreparePace, 0)
}

func (g *goatRamGoal) victimByID(t *tickView, id int32) (ramVictim, bool) {
	for _, v := range g.victims(t) {
		if v.id == id {
			return v, true
		}
	}
	return ramVictim{}, false
}

func (g *goatRamGoal) ShouldContinue(t *tickView) bool {
	r := &g.m.Goat.ram
	switch r.phase {
	case goatPreparing:
		_, ok := g.victimByID(t, r.targetID)
		return ok && r.elapsed <= goatRamPrepareTime
	case goatCharging:
		return r.elapsed <= goatRamTimeout
	}
	return false
}

func (g *goatRamGoal) Tick(t *tickView) {
	r := &g.m.Goat.ram
	r.elapsed++
	switch r.phase {
	case goatPreparing:
		g.prepareTick(t)
	case goatCharging:
		g.chargeTick(t)
	}
}

// prepareTick walks to the run-up cell, restarts the plan when the target
// moves to another cell, and starts the charge after twenty ticks there.
// vanilla: PrepareRamNearestTarget.tick
func (g *goatRamGoal) prepareTick(t *tickView) {
	r, m := &g.m.Goat.ram, g.m
	v, ok := g.victimByID(t, r.targetID)
	if !ok {
		return
	}
	dx, dz := v.x-m.X, v.z-m.Z
	m.HeadYaw = float32(yawToward(dx, dz))
	if cellAt(v.x, v.y, v.z) != r.targetCell {
		g.raiseHead(t)
		m.stopSeeking()
		next := cellAt(v.x, v.y, v.z)
		start, found := g.startCell(t.w, next)
		if !found {
			r.elapsed = goatRamPrepareTime + 1
			return
		}
		r.targetCell, r.startCell, r.atStart = next, start, 0
		m.seekTo(t.w, float64(start.X)+0.5, float64(start.Y), float64(start.Z)+0.5, goatPreparePace, 0)
		return
	}
	if m.cell() != r.startCell {
		if len(m.nav.path) == 0 && r.elapsed%10 == 0 {
			m.seekTo(t.w, float64(r.startCell.X)+0.5, float64(r.startCell.Y), float64(r.startCell.Z)+0.5, goatPreparePace, 0)
		}
		return
	}
	if !r.lowered {
		r.lowered = true
		t.events = append(t.events, MobEvent{Mob: m, Kind: MobGoatLoweredHead})
	}
	r.atStart++
	if r.atStart <= goatRamPrepare {
		return
	}
	// The ram aims at the far edge of the target's cell.
	// vanilla: PrepareRamNearestTarget.getEdgeOfBlock
	r.edgeX = float64(r.targetCell.X) + 0.5 + 0.5*float64(sign32(r.targetCell.X-r.startCell.X))
	r.edgeZ = float64(r.targetCell.Z) + 0.5 + 0.5*float64(sign32(r.targetCell.Z-r.startCell.Z))
	t.events = append(t.events, MobEvent{Mob: m, Kind: MobGoatSounded, Pitch: m.VoicePitch(),
		SoundID: m.goatSound(registry.SoundEntityGoatPrepareRam, registry.SoundEntityGoatScreamingPrepareRam)})
	r.phase = goatCharging
	g.beginCharge(t)
}

// beginCharge is RamTarget.start: aim along the line from the edge to the
// goat's cell and walk to the edge.
// vanilla: RamTarget.start
func (g *goatRamGoal) beginCharge(t *tickView) {
	r, m := &g.m.Goat.ram, g.m
	cell := m.cell()
	dirX, dirZ := float64(cell.X)-r.edgeX, float64(cell.Z)-r.edgeZ
	if length := math.Hypot(dirX, dirZ); length > 0 {
		dirX, dirZ = dirX/length, dirZ/length
	}
	r.dirX, r.dirZ = dirX, dirZ
	r.elapsed = 0
	m.seekTo(t.w, r.edgeX, float64(r.targetCell.Y), r.edgeZ, goatRamPace, 0)
}

func sign32(v int32) int32 {
	switch {
	case v > 0:
		return 1
	case v < 0:
		return -1
	}
	return 0
}

// chargeTick is RamTarget.tick: hurt and shove the first thing in the goat's
// box, or snap a horn on a block ahead, or give up once the walk is over.
// vanilla: RamTarget.tick
func (g *goatRamGoal) chargeTick(t *tickView) {
	r, m := &g.m.Goat.ram, g.m
	for _, v := range g.victims(t) {
		if math.Abs(v.x-m.X) >= (v.w+m.width())/2 || math.Abs(v.z-m.Z) >= (v.w+m.width())/2 ||
			v.y >= m.Y+m.height() || v.y+v.h <= m.Y {
			continue
		}
		damage := float64(goatAdultDamage)
		force := goatAdultKnockback
		if m.IsBaby() {
			damage, force = goatBabyDamage, goatBabyKnockback
		}
		// vanilla: clamp(getSpeed() * 1.65, 0.2, 3.0), getSpeed being the
		// ram's pace times the movement speed
		speed := math.Min(math.Max(float64(float32(goatRamPace*m.pace()))*goatRamSpeedFactor, 0.2), 3.0)
		// vanilla: RamTarget.tick speedBoostPower, 0.25 a level of speed less slowness
		speed += 0.25 * float64(m.effectLevel(mobEffectSpeed)-m.effectLevel(mobEffectSlowness))
		if v.stand {
			// vanilla: ArmorStand.hurtServer refuses a mob_attack_no_aggro blow,
			// then RamTarget.tick shoves it all the same.
			t.events = append(t.events, MobEvent{Mob: m, Kind: MobGoatRammedStand, Player: Player{EntityID: v.id},
				RamYaw: yawToward(-r.dirX, -r.dirZ), RamPush: speed * force})
			g.impact(t)
			g.finish(t)
			return
		}
		// The player the strike names is the victim whichever kind it is.
		t.events = append(t.events, MobEvent{Mob: m, Kind: MobAttackedPlayer, Ram: true, Damage: damage,
			Player: Player{EntityID: v.id}, RamYaw: yawToward(-r.dirX, -r.dirZ), RamPush: speed * force})
		g.impact(t)
		g.finish(t)
		return
	}
	if g.hornSnaps(t.w) {
		g.impact(t)
		if vx, vy, vz, ok := m.dropHorn(); ok {
			t.events = append(t.events, MobEvent{Mob: m, Kind: MobGoatDroppedHorn, X: m.X, Y: m.Y, Z: m.Z, VX: vx, VY: vy, VZ: vz})
			t.events = append(t.events, MobEvent{Mob: m, Kind: MobGoatSounded, Pitch: 1, SoundID: registry.SoundEntityGoatHornBreak})
		}
		g.finish(t)
		return
	}
	if len(m.nav.path) == 0 {
		g.finish(t)
	}
}

func (g *goatRamGoal) impact(t *tickView) {
	t.events = append(t.events, MobEvent{Mob: g.m, Kind: MobGoatSounded, Pitch: 1, SoundID: g.m.RamImpactSound()})
}

// hornSnaps is RamTarget.hasRammedHornBreakingBlock: the block a unit step
// along the horizontal motion reaches, or the one above, is in
// snaps_goat_horn.
// vanilla: RamTarget.hasRammedHornBreakingBlock
func (g *goatRamGoal) hornSnaps(w World) bool {
	m := g.m
	x, z := m.X, m.Z
	if length := math.Hypot(m.VX, m.VZ); length > 0 {
		x, z = x+m.VX/length, z+m.VZ/length
	}
	cell := cellAt(x, m.Y, z)
	return registry.BlockOf(w.BlockAt(cell.X, cell.Y, cell.Z)).InTag(goatSnapsHorn) ||
		registry.BlockOf(w.BlockAt(cell.X, cell.Y+1, cell.Z)).InTag(goatSnapsHorn)
}

func (g *goatRamGoal) raiseHead(t *tickView) {
	r := &g.m.Goat.ram
	if r.lowered {
		r.lowered = false
		t.events = append(t.events, MobEvent{Mob: g.m, Kind: MobGoatRaisedHead})
	}
}

// finish is RamTarget.finishRam.
func (g *goatRamGoal) finish(t *tickView) {
	r := &g.m.Goat.ram
	t.events = append(t.events, MobEvent{Mob: g.m, Kind: MobGoatRaisedHead})
	r.cooldown = rollGoatGap(GoDice{g.m.Rand}, g.m.Goat.Screaming)
	r.phase, r.lowered = goatIdle, false
	g.m.stopSeeking()
}

func (g *goatRamGoal) Stop(t *tickView) {
	r := &g.m.Goat.ram
	switch r.phase {
	case goatPreparing:
		t.events = append(t.events, MobEvent{Mob: g.m, Kind: MobGoatRaisedHead})
		r.cooldown = goatFailGap(g.m.Goat.Screaming)
		r.phase, r.lowered = goatIdle, false
	case goatCharging:
		// RamTarget has no stop: the ram target stays and the charge restarts.
	}
	g.m.stopSeeking()
}

// SetGoatRamCooldown sets the RAM_COOLDOWN_TICKS memory.
func (m *Mob) SetGoatRamCooldown(ticks int) { m.Goat.ram.cooldown = ticks }

// RamCooldown reports the RAM_COOLDOWN_TICKS memory, 0 when absent.
func (m *Mob) RamCooldown() int { return m.Goat.ram.cooldown }

// SetLoadedYaw is Entity.load's rotation: head yaw is set before body yaw,
// and a goat's head stays within 15 degrees of the body it had before, 0 on
// a fresh mob.
// vanilla: Entity.load, Goat.setYHeadRot
func (m *Mob) SetLoadedYaw(yaw float32) {
	m.Yaw, m.HeadYaw = yaw, yaw
	if m.Type == TypeGoat {
		var body float32
		if m.look.bodyYawSet {
			body = m.look.bodyYaw
		}
		diff := float32(wrapDegrees(float64(yaw - body)))
		lim := m.maxHeadYRot()
		m.HeadYaw = body + max(-lim, min(lim, diff))
	}
	m.look.bodyYaw, m.look.bodyYawSet = yaw, true
}

// SeedBodyYaw sets the body yaw a fresh mob spawns with, ahead of any
// SetLoadedYaw.
// vanilla: EntityType.create
func (m *Mob) SeedBodyYaw(yaw float32) { m.look.bodyYaw, m.look.bodyYawSet = yaw, true }
