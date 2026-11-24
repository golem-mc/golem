package entity

import (
	"math"
	"slices"

	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/registry"
)

// The goat's long jump and sensor numbers.
// vanilla: GoatAi, LongJumpToRandomPos, LongJumpMidJump, LongJumpUtil
const (
	goatJumpGapMin     = 600  // GoatAi.TIME_BETWEEN_LONG_JUMPS
	goatJumpGapMax     = 1200 // GoatAi.TIME_BETWEEN_LONG_JUMPS
	goatJumpReach      = 5    // GoatAi.MAX_LONG_JUMP_HEIGHT, MAX_LONG_JUMP_WIDTH
	goatJumpTries      = 20   // LongJumpToRandomPos.FIND_JUMP_TRIES
	goatJumpPrepare    = 40   // LongJumpToRandomPos.PREPARE_JUMP_DURATION
	goatJumpTimeout    = 200  // LongJumpToRandomPos.TIME_OUT_DURATION
	goatMidJumpTimeout = 100  // LongJumpMidJump.TIME_OUT_DURATION
	goatWalkReach      = 8    // the path length createPath(targetPos, 0, 8) allows
	goatPoseScale      = 0.7  // Goat.LONG_JUMPING_DIMENSION_SCALE_FACTOR
	goatLookRange      = 6.0  // GoatAi's SetEntityLookTargetSometimes
	goatLookGapMin     = 30   // GoatAi's UniformInt.of(30, 60)
	goatLookGapMax     = 60
	goatLookHoldMin    = 45 // GoatAi's LookAtTargetSink(45, 90)
	goatLookHoldMax    = 90
)

// goatJumpSpeed is GoatAi.MAX_JUMP_VELOCITY_MULTIPLIER.
const goatJumpSpeed = float64(float32(3.5714288))

// goatJumpStrength is the JUMP_STRENGTH attribute's 0.42f.
const goatJumpStrength = float64(float32(0.42))

var goatJumpAngles = [4]int{65, 70, 75, 80} // LongJumpToRandomPos.ALLOWED_ANGLES

// goatSense is the NEAREST_LIVING_ENTITIES memory: the last scan's list,
// nearest first, with cached line-of-sight answers. Positions are live;
// membership and order are up to 20 ticks old.
// vanilla: NearestLivingEntitySensor, NearestVisibleLivingEntities
type goatSense struct {
	delay   int
	scanned bool
	nearby  []int32
	visible map[int32]bool
}

// goatLook is SetEntityLookTargetSometimes's ticker and the LOOK_TARGET it
// sets, held for LookAtTargetSink's duration.
type goatLook struct {
	ticker  int
	target  int32
	remains int
}

// goatJump is the brain's LONG_JUMP_COOLDOWN_TICKS and LONG_JUMP_MID_JUMP
// memories and LongJumpToRandomPos's own fields.
type goatJump struct {
	cooldown int

	running    bool
	done       bool
	elapsed    int
	start      [3]float64
	candidates []jumpSpot
	tries      int
	chosen     bool
	velocity   [3]float64
	prepared   int
	// wantPreferred and fallback are LongJumpToPreferredBlock's roll for this
	// run and the first non-preferred spot set aside; only the first is ever
	// read because pick and usable stop once candidates is empty.
	wantPreferred bool
	hasFallback   bool
	fallback      jumpSpot
	totalWeight   int
	lookX         float64
	lookY         float64
	lookZ         float64

	// mid is LONG_JUMP_MID_JUMP; posed and noFriction are what
	// LongJumpMidJump.start set (Pose.LONG_JUMPING, setDiscardFriction).
	mid      bool
	posed    bool
	midTicks int
	// noFriction is also set on the launch tick, ahead of the behaviour.
	noFriction bool
}

type jumpSpot struct {
	cell   Waypoint
	weight int32
}

// jumpSpec is what differs between the goat's and the frog's long jump.
type jumpSpec struct {
	gapMin, gapMax int // the cooldown's UniformInt
	height, width  int // the candidate box's half-extents
	poseScale      float64
}

var (
	goatJumpSpec = jumpSpec{goatJumpGapMin, goatJumpGapMax, goatJumpReach, goatJumpReach, goatPoseScale}
	frogJumpSpec = jumpSpec{frogJumpGapMin, frogJumpGapMax, frogJumpHeight, frogJumpWidth, 1}
)

func (m *Mob) jumpSpec() jumpSpec {
	if m.Type == TypeFrog {
		return frogJumpSpec
	}
	return goatJumpSpec
}

// longJump is the long jump state of a goat or frog.
func (m *Mob) longJump() *goatJump {
	if m.Type == TypeFrog {
		return &m.frog.jump
	}
	return &m.Goat.jump
}

// rollJumpGap is TIME_BETWEEN_LONG_JUMPS.sample of the goat's or frog's
// long jump cooldown.
func rollJumpGap(m *Mob, rng Dice) int {
	s := m.jumpSpec()
	return s.gapMin + int(rng.NextIntN(int32(s.gapMax-s.gapMin+1)))
}

// initGoatMemories is GoatAi.initMemories (long jump cooldown, then ram)
// plus Brain.addSensor's random first-scan delay.
// vanilla: GoatAi.initMemories, Sensor.randomlyDelayStart
func (m *Mob) initGoatMemories() {
	m.longJump().cooldown = rollJumpGap(m, GoDice{m.Rand})
	m.Goat.ram.cooldown = rollGoatGap(GoDice{m.Rand}, false)
	m.Goat.sense.delay = m.Rand.Intn(goatSenseInterval)
}

// LongJumping reports whether the goat is drawn in Pose.LONG_JUMPING.
func (m *Mob) LongJumping() bool {
	return (m.Type == TypeGoat || m.Type == TypeFrog) && m.longJump().posed
}

func (m *Mob) setLongJumpPose(posed bool) {
	if m.Type == TypeFrog {
		m.SetMetadataFrogJumpPosed(posed)
	} else {
		m.SetMetadataGoatJumpPosed(posed)
	}
}

func (m *Mob) replaceLongJump(state goatJump) {
	if m.Type == TypeFrog {
		m.frog.jump = state
		m.InvalidateMetadataFrogJump()
	} else {
		m.Goat.jump = state
		m.InvalidateMetadataGoatJump()
	}
}

// poseScale is the long-jumping pose's scale on the box and the eyes.
// vanilla: Goat.getDefaultDimensions
func (m *Mob) poseScale() float64 {
	if m.LongJumping() {
		return m.jumpSpec().poseScale
	}
	return 1
}

// SetGoatLongJumpCooldown sets the LONG_JUMP_COOLDOWN_TICKS memory.
func (m *Mob) SetGoatLongJumpCooldown(ticks int) { m.longJump().cooldown = ticks }

// LongJumpCooldown reports the LONG_JUMP_COOLDOWN_TICKS memory, 0 when absent.
func (m *Mob) LongJumpCooldown() int { return m.longJump().cooldown }

// goatBrainStep is the goat's brain before its goals: the sensor, the
// cooldown countdowns, the mid-jump behaviour and the look.
// vanilla: Brain.tick, CountDownCooldownTicks
func (m *Mob) goatBrainStep(t *tickView) {
	if m.Goat.ram.cooldown > 0 {
		m.Goat.ram.cooldown--
	}
	j := m.longJump()
	if j.cooldown > 0 {
		j.cooldown--
	}
	m.senseGoat(t)
	m.midJumpStep(t)
	m.lookStep(t)
}

type seen struct {
	id int32
	d  float64
}

// senseGoat is Sensor.tick of the nearest-living-entities sensor.
// vanilla: Sensor.tick, NearestLivingEntitySensor.doTick
func (m *Mob) senseGoat(t *tickView) {
	s := &m.Goat.sense
	s.delay--
	if s.delay > 0 {
		return
	}
	s.delay = goatSenseInterval
	var found []seen
	// The box is the goat's inflated by the follow range, tested against the
	// other's box.
	inReach := func(x, y, z, w, h float64) bool {
		return math.Abs(x-m.X) < goatSenseRange+(m.width()+w)/2 &&
			math.Abs(z-m.Z) < goatSenseRange+(m.width()+w)/2 &&
			y < m.Y+m.height()+goatSenseRange && y+h > m.Y-goatSenseRange
	}
	for i := range t.players {
		p := &t.players[i]
		w, h, _ := p.box()
		if inReach(p.X, p.Y, p.Z, w, h) {
			found = append(found, seen{p.EntityID, Dist2(p.X, p.Y, p.Z, m.X, m.Y, m.Z)})
		}
	}
	for _, o := range t.others {
		if o != m && o.Alive() && inReach(o.X, o.Y, o.Z, o.width(), o.height()) {
			found = append(found, seen{o.ID, Dist2(o.X, o.Y, o.Z, m.X, m.Y, m.Z)})
		}
	}
	if t.w.MobGriefing() {
		for _, st := range t.stands {
			if inReach(st.X, st.Y, st.Z, st.W, st.H) {
				found = append(found, seen{st.ID, Dist2(st.X, st.Y, st.Z, m.X, m.Y, m.Z)})
			}
		}
	}
	slices.SortStableFunc(found, func(a, b seen) int {
		switch {
		case a.d < b.d:
			return -1
		case a.d > b.d:
			return 1
		}
		return 0
	})
	s.nearby = s.nearby[:0]
	for _, f := range found {
		s.nearby = append(s.nearby, f.id)
	}
	s.visible = map[int32]bool{}
	s.scanned = true
}

// goatSees is the memory's visibility test: Sensor.isEntityTargetable once per
// entity per scan, kept until the next one. vis scales the 16-block range.
// vanilla: NearestVisibleLivingEntities
func (m *Mob) goatSees(t *tickView, id int32, x, y, z, eye, vis float64) bool {
	s := &m.Goat.sense
	if v, ok := s.visible[id]; ok {
		return v
	}
	reach := max(goatSenseRange*vis, minVisibilityDistance)
	v := Dist2(x, y, z, m.X, m.Y, m.Z) <= reach*reach && m.seesPoint(t.w, x, y+eye, z)
	s.visible[id] = v
	return v
}

// lookStep is SetEntityLookTargetSometimes with LookAtTargetSink's timer:
// without a look target, the nearest visible player within six blocks
// becomes one after a 30-60 tick countdown that runs only while such a
// player is there.
// vanilla: SetEntityLookTargetSometimes.create, Ticker.tickDownAndCheck
func (m *Mob) lookStep(t *tickView) {
	l := &m.Goat.look
	if l.remains > 0 {
		if m.lookTarget(t) == nil {
			l.target, l.remains = 0, 0
			return
		}
		l.remains--
		if l.remains == 0 {
			l.target = 0
		}
		return
	}
	s := &m.Goat.sense
	j := m.longJump()
	if !s.scanned || m.Goat.ram.phase != goatIdle || j.running || j.mid || m.longJumpWanted() {
		return
	}
	var p *Player
	for _, id := range s.nearby {
		for i := range t.players {
			q := &t.players[i]
			if q.EntityID != id || q.Spectator || q.Dead {
				continue
			}
			if Dist2(q.X, q.Y, q.Z, m.X, m.Y, m.Z) <= goatLookRange*goatLookRange && m.goatSees(t, id, q.X, q.Y, q.Z, q.eyeHeight(), visibilityPercent(m, q)) {
				p = q
			}
		}
		if p != nil {
			break
		}
	}
	if p == nil {
		return
	}
	if l.ticker == 0 {
		l.ticker = goatLookGapMin + m.Rand.Intn(goatLookGapMax-goatLookGapMin+1) - 1
		return
	}
	if l.ticker--; l.ticker == 0 {
		l.target = p.EntityID
		l.remains = goatLookHoldMin + m.Rand.Intn(goatLookHoldMax-goatLookHoldMin+1)
	}
}

// longJumpWanted says the brain would pick the long jump activity over the
// idle one: its cooldown is gone and nothing walks.
// vanilla: GoatAi.initLongJumpActivity's memory requirements
func (m *Mob) longJumpWanted() bool {
	return m.longJump().cooldown == 0 && len(m.nav.path) == 0 && m.InLove == 0
}

// midJumpStep is LongJumpMidJump: the pose from the tick after the launch,
// and the landing that ends it.
// vanilla: LongJumpMidJump
func (m *Mob) midJumpStep(t *tickView) {
	j := m.longJump()
	if !j.mid {
		return
	}
	m.setLongJumpPose(true)
	j.noFriction = true
	j.midTicks++
	if !m.OnGround && j.midTicks <= goatMidJumpTimeout+1 {
		return
	}
	if m.OnGround {
		const tenth = float64(float32(0.1))
		m.VX *= tenth
		m.VZ *= tenth
		if m.Type == TypeFrog {
			t.events = append(t.events, MobEvent{Mob: m, Kind: MobSounded, Pitch: 1, Volume: 2, SoundID: registry.SoundEntityFrogStep})
		} else {
			t.events = append(t.events, MobEvent{Mob: m, Kind: MobGoatSounded, Pitch: 1, Volume: 2, SoundID: registry.SoundEntityGoatStep})
		}
	}
	m.replaceLongJump(goatJump{cooldown: rollJumpGap(m, GoDice{m.Rand})})
}

// goatLookGoal holds the head on the look target the sensor-driven timer set.
// vanilla: LookAtTargetSink
type goatLookGoal struct{ m *Mob }

func (g *goatLookGoal) Priority() int           { return priorityLookAt }
func (g *goatLookGoal) Flags() GoalFlags        { return GoalLook }
func (g *goatLookGoal) RequiresEveryTick() bool { return true }
func (g *goatLookGoal) CanStart(t *tickView) bool {
	return g.m.Goat.look.target != 0 && g.player(t) != nil
}
func (g *goatLookGoal) Start(*tickView) {}
func (g *goatLookGoal) ShouldContinue(t *tickView) bool {
	return g.m.Goat.look.target != 0 && g.player(t) != nil
}
func (g *goatLookGoal) Stop(*tickView) { g.m.Goat.look.target, g.m.Goat.look.remains = 0, 0 }

func (g *goatLookGoal) player(t *tickView) *Player { return g.m.lookTarget(t) }

// lookTarget is the look target while it stays alive and in the sensor's
// visible list.
// vanilla: EntityTracker.isVisibleBy
func (m *Mob) lookTarget(t *tickView) *Player {
	id := m.Goat.look.target
	if id == 0 || !slices.Contains(m.Goat.sense.nearby, id) {
		return nil
	}
	for i := range t.players {
		if p := &t.players[i]; p.EntityID == id && !p.Dead &&
			m.goatSees(t, id, p.X, p.Y, p.Z, p.eyeHeight(), visibilityPercent(m, p)) {
			return p
		}
	}
	return nil
}

func (g *goatLookGoal) Tick(t *tickView) {
	if p := g.player(t); p != nil {
		g.m.goatLookAt(p.X, p.Y+p.eyeHeight(), p.Z)
	}
}

// goatLookAt turns the head toward a point.
func (m *Mob) goatLookAt(x, y, z float64) {
	dx, dz := x-m.X, z-m.Z
	m.HeadYaw = float32(yawToward(dx, dz))
	m.Pitch = float32(pitchToward(dx, y-(m.Y+m.eyeHeight()), dz))
}

// goatJumpGoal is LongJumpToRandomPos.
// vanilla: LongJumpToRandomPos
type goatJumpGoal struct {
	m *Mob
	// tempt is the frog's tempt goal, whose lure the LONG_JUMP activity yields to.
	tempt *temptGoal
}

// goatJumpPriority puts the jump behind the ram and ahead of the stroll.
const goatJumpPriority = goatRamPriority + 1

func (g *goatJumpGoal) Priority() int {
	if g.m.Type == TypeFrog {
		return priorityBreed // the LONG_JUMP activity sits under tongue and lay spawn
	}
	return goatJumpPriority
}
func (g *goatJumpGoal) Flags() GoalFlags        { return GoalMove | GoalLook }
func (g *goatJumpGoal) RequiresEveryTick() bool { return true }

// CanStart is the LONG_JUMP activity's memory requirements and
// checkExtraStartConditions, whose failure costs half a cooldown.
// vanilla: LongJumpToRandomPos.checkExtraStartConditions
func (g *goatJumpGoal) CanStart(t *tickView) bool {
	m := g.m
	j := m.longJump()
	if j.mid || !m.longJumpWanted() || m.Type == TypeGoat && m.Goat.ram.phase != goatIdle {
		return false
	}
	if m.Type == TypeFrog && (m.frogJumpBlocked(g.tempt, t) || m.InWater()) {
		return false
	}
	cell := m.cell()
	honey := registry.BlockOf(t.w.BlockAt(cell.X, cell.Y, cell.Z)) == registry.BlockHoneyBlock
	if !m.OnGround || m.InWater() || m.InLava() || honey {
		j.cooldown = rollJumpGap(m, GoDice{m.Rand}) / 2
		return false
	}
	return true
}

func (g *goatJumpGoal) Start(*tickView) {
	m := g.m
	here := m.cell()
	spec := m.jumpSpec()
	spots := make([]jumpSpot, 0, (2*spec.width+1)*(2*spec.width+1)*(2*spec.height+1))
	totalWeight := 0
	// vanilla: BlockPos.betweenClosedStream runs x fastest, z outermost.
	w, h := int32(spec.width), int32(spec.height)
	for z := here.Z - w; z <= here.Z+w; z++ {
		for y := here.Y - h; y <= here.Y+h; y++ {
			for x := here.X - w; x <= here.X+w; x++ {
				if c := (Waypoint{x, y, z}); c != here {
					weight := int32(distSq(c, here))
					spots = append(spots, jumpSpot{c, weight})
					totalWeight += int(weight)
				}
			}
		}
	}
	m.replaceLongJump(goatJump{cooldown: m.longJump().cooldown, running: true, start: [3]float64{m.X, m.Y, m.Z},
		candidates: spots, totalWeight: totalWeight, tries: goatJumpTries,
		wantPreferred: m.Type == TypeFrog && m.Rand.Float32() < frogPreferChance})
}

func (g *goatJumpGoal) ShouldContinue(*tickView) bool {
	j := g.m.longJump()
	return j.running && !j.done
}

func (g *goatJumpGoal) Stop(*tickView) {
	j := g.m.longJump()
	j.running, j.candidates, j.hasFallback, j.totalWeight = false, nil, false, 0
}

// usable is canStillUse, and the timeout that ends the behaviour ahead of it.
// vanilla: Behavior.tickOrStop, LongJumpToRandomPos.canStillUse
func (g *goatJumpGoal) usable() bool {
	m, j := g.m, g.m.longJump()
	if j.elapsed > goatJumpTimeout {
		return false
	}
	valid := j.start == [3]float64{m.X, m.Y, m.Z} && j.tries > 0 && !m.InWater() && (j.chosen || len(j.candidates) > 0)
	if !valid && !j.mid {
		j.cooldown = rollJumpGap(m, GoDice{m.Rand}) / 2
	}
	return valid
}

func (g *goatJumpGoal) Tick(t *tickView) {
	m, j := g.m, g.m.longJump()
	if !j.running || j.done {
		return
	}
	j.elapsed++
	if !g.usable() {
		j.done = true
		return
	}
	if !j.chosen {
		j.tries--
		g.pick(t)
		return
	}
	m.goatLookAt(j.lookX, j.lookY, j.lookZ)
	j.prepared++
	if j.prepared < goatJumpPrepare {
		return
	}
	// A jump boost adds to the launch's length.
	boost := rules.JumpBoostPower(m.effects.All())
	length := math.Sqrt(j.velocity[0]*j.velocity[0] + j.velocity[1]*j.velocity[1] + j.velocity[2]*j.velocity[2])
	scale := (length + boost) / length
	m.VX, m.VY, m.VZ = j.velocity[0]*scale, j.velocity[1]*scale, j.velocity[2]*scale
	j.noFriction, j.mid = true, true
	if m.Type == TypeFrog {
		t.events = append(t.events, MobEvent{Mob: m, Kind: MobSounded, Pitch: 1, Volume: 1, SoundID: registry.SoundEntityFrogLongJump})
		return
	}
	sound := m.goatSound(registry.SoundEntityGoatLongJump, registry.SoundEntityGoatScreamingLongJump)
	t.events = append(t.events, MobEvent{Mob: m, Kind: MobGoatSounded, Pitch: 1, SoundID: sound})
}

// pick is pickCandidate: the first accepted candidate becomes the jump.
// vanilla: LongJumpToRandomPos.pickCandidate
func (g *goatJumpGoal) pick(t *tickView) {
	j := g.m.longJump()
	for len(j.candidates) > 0 {
		spot, ok := g.nextSpot(t)
		if !ok || !g.landing(t.w, spot.cell) {
			continue
		}
		target := [3]float64{float64(spot.cell.X) + 0.5, float64(spot.cell.Y) + 0.5, float64(spot.cell.Z) + 0.5}
		v, ok := g.vector(t.w, target)
		if !ok {
			continue
		}
		if g.walkable(t.w, spot.cell) {
			continue
		}
		j.lookX, j.lookY, j.lookZ = target[0], target[1], target[2]
		j.chosen, j.velocity, j.prepared = true, v, 0
		return
	}
}

// preferred reports a block of the frog's preferred-landing tag.
func (g *goatJumpGoal) preferred(w World, x, y, z int32) bool {
	return g.m.Type == TypeFrog && registry.BlockOf(w.BlockAt(x, y, z)).InTag(frogPreferJumpTag)
}

// drawSpot is LongJumpToRandomPos.getJumpCandidate: one weighted draw without
// replacement.
// vanilla: LongJumpToRandomPos.getJumpCandidate
func (g *goatJumpGoal) drawSpot() (jumpSpot, bool) {
	m, j := g.m, g.m.longJump()
	if len(j.candidates) == 0 {
		return jumpSpot{}, false
	}
	if j.totalWeight <= 0 {
		for _, c := range j.candidates {
			j.totalWeight += int(c.weight)
		}
	}
	roll := m.Rand.Intn(j.totalWeight)
	idx := 0
	for i, c := range j.candidates {
		if roll -= int(c.weight); roll < 0 {
			idx = i
			break
		}
	}
	spot := j.candidates[idx]
	j.candidates = slices.Delete(j.candidates, idx, idx+1)
	j.totalWeight -= int(spot.weight)
	return spot, true
}

// nextSpot is LongJumpToPreferredBlock.getJumpCandidate: when the frog wants a
// preferred block, draw until one sits above a preferred block, setting the
// first other aside as the fallback when candidates run out.
// vanilla: LongJumpToPreferredBlock.getJumpCandidate
func (g *goatJumpGoal) nextSpot(t *tickView) (jumpSpot, bool) {
	j := g.m.longJump()
	if !j.wantPreferred {
		return g.drawSpot()
	}
	for len(j.candidates) > 0 {
		spot, ok := g.drawSpot()
		if !ok {
			continue
		}
		if g.preferred(t.w, spot.cell.X, spot.cell.Y-1, spot.cell.Z) {
			return spot, true
		}
		if !j.hasFallback {
			j.fallback, j.hasFallback = spot, true
		}
	}
	if j.hasFallback {
		j.hasFallback = false
		return j.fallback, true
	}
	return jumpSpot{}, false
}

// landing is isAcceptableLandingPosition with defaultAcceptableLandingSpot:
// not the goat's column, solid below, and a standing cell with no path
// malus (GoalUtils.hasMalus).
// vanilla: LongJumpToRandomPos.isAcceptableLandingPosition
func (g *goatJumpGoal) landing(w World, cell Waypoint) bool {
	m := g.m
	if c := m.cell(); c.X == cell.X && c.Z == cell.Z {
		return false
	}
	if m.Type == TypeFrog {
		if ok, decided := frogAcceptsLanding(w, cell); decided || ok {
			return ok
		}
	}
	return isStableDestination(w, m, cell) && !hasMalus(w, m, cell) &&
		walkableAt(w, footprintOf(m.width(), m.height()), cell.X, cell.Y, cell.Z)
}

// walkable is the createPath(targetPos, 0, 8) test: a way of at most eight
// blocks that reaches the cell makes the jump pointless.
// vanilla: LongJumpToRandomPos.pickCandidate
func (g *goatJumpGoal) walkable(w World, cell Waypoint) bool {
	m := g.m
	if !m.canUpdatePath() {
		return false
	}
	foot := m.pathFootprint(w)
	target, ok := walkableNear(w, foot, float64(cell.X)+0.5, float64(cell.Y), float64(cell.Z)+0.5)
	if !ok {
		return false
	}
	_, reached := m.tickPaths.findPath(w, foot, m.cell(), target, goatWalkReach, 0)
	return reached
}

// vector is calculateOptimalJumpVector: the allowed angles in a random order,
// the first that has a vector.
// vanilla: LongJumpToRandomPos.calculateOptimalJumpVector
func (g *goatJumpGoal) vector(w World, target [3]float64) ([3]float64, bool) {
	m := g.m
	angles := goatJumpAngles
	m.Rand.Shuffle(len(angles), func(a, b int) { angles[a], angles[b] = angles[b], angles[a] })
	maxV := float64(float32(goatJumpStrength * goatJumpSpeed))
	for _, a := range angles {
		if v, ok := jumpVectorForAngle(w, m, target, maxV, a, true); ok {
			return v, true
		}
	}
	return [3]float64{}, false
}

// jumpVectorForAngle is LongJumpUtil.calculateJumpVectorForAngle; checkCollision
// is the goat's true argument and the breeze's false one.
// vanilla: LongJumpUtil.calculateJumpVectorForAngle
func jumpVectorForAngle(w World, m *Mob, target [3]float64, maxV float64, angle int, checkCollision bool) ([3]float64, bool) {
	pos := [3]float64{m.X, m.Y, m.Z}
	px, pz := target[0]-pos[0], target[2]-pos[2]
	if l := math.Sqrt(px*px + pz*pz); l >= 1e-5 {
		px, pz = px/l*0.5, pz/l*0.5
	} else {
		px, pz = 0, 0
	}
	dx, dy, dz := target[0]-px-pos[0], target[1]-pos[1], target[2]-pz-pos[2]
	ang := float64(float32(angle) * float32(math.Pi) / 180)
	xz := math.Atan2(dz, dx)
	r2 := dx*dx + dz*dz
	r := math.Sqrt(r2)
	g := m.gravity()
	sin2 := math.Sin(float64(float32(2) * float32(ang)))
	cos2 := math.Pow(math.Cos(ang), 2)
	sinA, cosA := math.Sin(ang), math.Cos(ang)
	sinXZ, cosXZ := math.Sin(xz), math.Cos(xz)
	v0sq := r2 * g / (r*sin2 - 2*dy*cos2)
	if v0sq < 0 || math.IsNaN(v0sq) {
		return [3]float64{}, false
	}
	v0 := math.Sqrt(v0sq)
	if v0 > maxV {
		return [3]float64{}, false
	}
	v0r, v0y := v0*cosA, v0*sinA
	if checkCollision {
		samples := int(math.Ceil(r/v0r)) * 2
		scale := m.jumpSpec().poseScale
		dimW, dimH := m.width()*scale, m.height()*scale
		ri := 0.0
		var prev *[3]float64
		for i := 0; i < samples-1; i++ {
			ri += r / float64(samples)
			yi := sinA/cosA*ri - ri*ri*g/(2*v0sq*cosA*cosA)
			cur := [3]float64{pos[0] + ri*cosXZ, pos[1] + yi, pos[2] + ri*sinXZ}
			if prev != nil && !clearTransition(w, m, dimW, dimH, *prev, cur) {
				return [3]float64{}, false
			}
			prev = &cur
		}
	}
	const keep = float64(float32(0.95))
	return [3]float64{v0r * cosXZ * keep, v0y * keep, v0r * sinXZ * keep}, true
}

// clearTransition is LongJumpUtil.isClearTransition: boxes along the segment,
// spaced a bit under the box's narrowest side apart, touch no collision.
// vanilla: LongJumpUtil.isClearTransition
func clearTransition(w World, m *Mob, width, height float64, a, b [3]float64) bool {
	d := [3]float64{b[0] - a[0], b[1] - a[1], b[2] - a[2]}
	length := math.Sqrt(d[0]*d[0] + d[1]*d[1] + d[2]*d[2])
	minDim := math.Min(width, height)
	checks := int(math.Ceil(length / minDim))
	step := minDim * float64(float32(0.9))
	cur := a
	for i := range checks {
		if i == checks-1 {
			cur = b
		} else if length > 0 {
			cur = [3]float64{cur[0] + d[0]/length*step, cur[1] + d[1]/length*step, cur[2] + d[2]/length*step}
		}
		box := AABB{cur[0] - width/2, cur[1], cur[2] - width/2, cur[0] + width/2, cur[1] + height, cur[2] + width/2}
		if overlapsSolid(w, m.collideCtx(), box) {
			return false
		}
	}
	return true
}
