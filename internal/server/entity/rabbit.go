package entity

import (
	"math"

	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/registry"
)

// Rabbit.Variant ids.
// vanilla: Rabbit.Variant
const (
	rabbitBrown          int32 = 0
	rabbitWhite          int32 = 1
	rabbitBlack          int32 = 2
	rabbitWhiteSplotched int32 = 3
	rabbitGold           int32 = 4
	rabbitSalt           int32 = 5
	rabbitEvil           int32 = 99
)

// rabbitGround marks the rabbits_spawnable_on tag.
var rabbitGround = registry.BlockTagOf("minecraft:rabbits_spawnable_on")

// ValidRabbitType reports whether id is a Rabbit.Variant id; any other id
// reads as brown.
func ValidRabbitType(id int32) bool {
	switch id {
	case rabbitBrown, rabbitWhite, rabbitBlack, rabbitWhiteSplotched, rabbitGold, rabbitSalt, rabbitEvil:
		return true
	}
	return false
}

// rabbitTypeFor rolls the variant a biome spawns.
// vanilla: Rabbit.getRandomRabbitVariant
func rabbitTypeFor(biome registry.Biome, rng Dice) int32 {
	roll := rng.NextIntN(100)
	switch {
	case biome.InTag(registry.BiomeTagIDSpawnsWhiteRabbits):
		if roll < 80 {
			return rabbitWhite
		}
		return rabbitWhiteSplotched
	case biome.InTag(registry.BiomeTagIDSpawnsGoldRabbits):
		return rabbitGold
	case roll < 50:
		return rabbitBrown
	case roll < 90:
		return rabbitSalt
	}
	return rabbitBlack
}

// finalizeRabbit gives the rabbit its biome's variant, a pack sharing the
// first member's. Every member rolls, even one that takes the pack's.
// vanilla: Rabbit.finalizeSpawn
func (m *Mob) finalizeRabbit(dice Dice, biome registry.Biome, group *SpawnGroup) {
	m.SetMetadataRabbitType(rabbitTypeFor(biome, dice))
	if group != nil && group.rabbitSet {
		m.SetMetadataRabbitType(group.rabbitType)
	} else if group != nil {
		group.rabbitSet, group.rabbitType = true, m.RabbitType
	}
}

// The rabbit's pace modifiers and hop timings.
// vanilla: Rabbit
const (
	rabbitFleePace       = 2.2 // Rabbit.FLEE_SPEED_MOD
	rabbitAttackPace     = 1.4 // Rabbit.ATTACK_SPEED_MOD
	rabbitRaidPace       = 0.7 // RaidGardenGoal's MoveToBlockGoal speed
	rabbitJumpDelay      = 10  // Rabbit.JUMP_DELAY_TICKS
	rabbitPanicJumpDelay = 3   // Rabbit.PANIC_JUMP_DELAY_TICKS
	rabbitJumpDuration   = 15  // Rabbit.JUMP_DURATION_IN_TICKS
	rabbitMoreCarrots    = 40  // Rabbit.MORE_CARROTS_DELAY
	rabbitEvilArmor      = 8   // Rabbit.EVIL_ARMOR_VALUE
	rabbitEvilAttack     = 5   // Rabbit.EVIL_ATTACK_POWER_INCREMENT
	rabbitRaidSearch     = 16  // RaidGardenGoal's search range
	rabbitLookRange      = 10  // Rabbit.registerGoals LookAtPlayerGoal distance
)

// rabbitState is the hop machinery of Rabbit, RabbitJumpControl and
// RabbitMoveControl.
type rabbitState struct {
	// jumping is LivingEntity.jumping: set by a hop's start, cleared on the
	// landing or when jumpDuration runs out.
	jumping                 bool
	jumpTicks, jumpDuration int
	wasOnGround             bool
	jumpDelay               int
	// speedMod is RabbitMoveControl's current speed modifier, nextJumpSpeed
	// the last positive one it was asked for.
	speedMod, nextJumpSpeed float64
	// hopped marks a jumpFromGround the tick's end tells the watchers of.
	hopped bool
}

// SetRabbitVariant is Rabbit.applyImplicitComponent for rabbit/variant: an id
// Rabbit.Variant does not know reads as brown.
// vanilla: Rabbit.Variant.byId
func (m *Mob) SetRabbitVariant(id int32) {
	if m.Type != TypeRabbit {
		return
	}
	if !ValidRabbitType(id) {
		id = rabbitBrown
	}
	m.SetMetadataRabbitType(id)
}

// KillerBunny reports whether the rabbit is the Rabbit.Variant.EVIL one.
func (m *Mob) KillerBunny() bool { return m.Type == TypeRabbit && m.RabbitType == rabbitEvil }

// evilGoal is a goal only the killer bunny runs: Rabbit.setVariant adds
// these when the variant becomes EVIL.
// vanilla: Rabbit.setVariant
type evilGoal struct {
	Goal
	m *Mob
}

func (g *evilGoal) CanStart(t *tickView) bool { return g.m.KillerBunny() && g.Goal.CanStart(t) }

// rabbitGoals wires the rabbit's mind. The melee and target goals sit behind
// evilGoal.
// vanilla: Rabbit.registerGoals, Rabbit.setVariant
func rabbitGoals(m *Mob) []Goal {
	return []Goal{
		newFloatGoal(m),
		&climbPowderSnowGoal{m: m},
		&panicGoal{m: m},
		&evilGoal{&hurtByGoal{m: m, priority: priorityHurtBy, alertOthers: true}, m},
		&breedGoal{m: m},
		&evilGoal{&nearestTargetGoal{m: m, priority: priorityNearest}, m},
		&evilGoal{&mobTargetGoal{m: m, victim: TypeWolf, priority: priorityNearest}, m},
		&temptGoal{m: m},
		&rabbitAvoidGoal{m: m, avoid: avoidPlayers, maxDist: 8},
		&rabbitAvoidGoal{m: m, avoid: avoidWolves, maxDist: 10},
		&rabbitAvoidGoal{m: m, avoid: avoidMonsters, maxDist: 4},
		&evilGoal{&meleeAttackGoal{m: m, priority: 4, followUnseen: true, pace: rabbitAttackPace}, m},
		newRaidGardenGoal(m),
		&randomStroll{m: m},
		&lookAtPlayer{m: m, priority: 11, reach: rabbitLookRange},
	}
}

type avoidKind int

const (
	avoidPlayers avoidKind = iota
	avoidWolves
	avoidMonsters
)

// rabbitAvoidGoal is Rabbit.RabbitAvoidEntityGoal: the nearest player, wolf
// or monster in reach sends the rabbit running to a spot away from it,
// unless the rabbit is the killer bunny.
// vanilla: AvoidEntityGoal, Rabbit.RabbitAvoidEntityGoal
type rabbitAvoidGoal struct {
	m       *Mob
	avoid   avoidKind
	maxDist float64
	path    []Waypoint
	end     Waypoint
}

func (g *rabbitAvoidGoal) Priority() int    { return 4 }
func (g *rabbitAvoidGoal) Flags() GoalFlags { return GoalMove }

func (g *rabbitAvoidGoal) accepts(o *Mob) bool {
	if !o.Alive() || o == g.m {
		return false
	}
	if g.avoid == avoidWolves {
		return o.Type == TypeWolf
	}
	return MonsterClassKind(o.Type)
}

// nearest is ServerEntityGetter.getNearestEntity over the box grown by the
// reach, with combat targeting conditions: within the visibility-scaled
// range and in sight.
// vanilla: AvoidEntityGoal.canUse, TargetingConditions.test
func (g *rabbitAvoidGoal) nearest(t *tickView) (x, y, z float64, found bool) {
	m := g.m
	best := math.Inf(1)
	consider := func(tx, ty, tz, width, height, vis float64) {
		if math.Abs(tx-m.X) > g.maxDist+(m.width()+width)/2 ||
			ty >= m.Y+m.height()+3 || ty+height <= m.Y-3 ||
			math.Abs(tz-m.Z) > g.maxDist+(m.width()+width)/2 {
			return
		}
		d := Dist2(tx, ty, tz, m.X, m.Y, m.Z)
		reach := max(g.maxDist*vis, minVisibilityDistance)
		if d > reach*reach || d >= best {
			return
		}
		if !m.seesPoint(t.w, tx, ty+0.85*height, tz) {
			return
		}
		best, x, y, z, found = d, tx, ty, tz, true
	}
	if g.avoid == avoidPlayers {
		if !playersThreaten(t) {
			return
		}
		for i := range t.players {
			p := &t.players[i]
			if p.Huntable {
				pw, ph, _ := p.box()
				consider(p.X, p.Y, p.Z, pw, ph, visibilityPercent(m, p))
			}
		}
		return
	}
	for _, o := range m.herdNear(t, max(g.maxDist, avoidHeight)) {
		if g.accepts(o) {
			consider(o.X, o.Y, o.Z, o.width(), o.height(), 1)
		}
	}
	return
}

// CanStart picks a spot away from the nearest threat and plans a path to
// it; the spot must not be nearer to the threat than the rabbit already is.
func (g *rabbitAvoidGoal) CanStart(t *tickView) bool {
	if g.m.KillerBunny() {
		return false
	}
	ax, ay, az, ok := g.nearest(t)
	if !ok {
		return false
	}
	spot, ok := posAway(t.w, g.m, 16, 7, ax, az)
	if !ok {
		return false
	}
	px, py, pz := float64(spot.X)+0.5, float64(spot.Y), float64(spot.Z)+0.5
	if Dist2(ax, ay, az, px, py, pz) < Dist2(ax, ay, az, g.m.X, g.m.Y, g.m.Z) {
		return false
	}
	path, end, ok := g.m.createPath(t.w, px, py, pz)
	if !ok || len(path) == 0 {
		return false
	}
	g.path, g.end = path, end
	return true
}

func (g *rabbitAvoidGoal) Start(*tickView) { g.m.moveTo(g.path, g.end, rabbitFleePace) }

func (g *rabbitAvoidGoal) ShouldContinue(*tickView) bool { return len(g.m.navOf().nav.path) > 0 }

// Tick runs at the sprint pace within seven blocks of the threat and the
// walk pace beyond; both are the flee pace for a rabbit.
func (g *rabbitAvoidGoal) Tick(*tickView) { g.m.navOf().nav.speedFactor = rabbitFleePace }

func (g *rabbitAvoidGoal) Stop(*tickView) {
	g.path = nil
	g.m.stopSeeking()
}

func (g *rabbitAvoidGoal) RequiresEveryTick() bool { return false }

// posAway is DefaultRandomPos.getPosAway: ten cells within the spreads,
// bearing within a quarter turn of away-from-the-point, best walk-target
// value winning.
// vanilla: DefaultRandomPos.getPosAway, RandomPos.generateRandomDirectionWithinRadians
func posAway(w World, m *Mob, xzSpread, ySpread int, fromX, fromZ float64) (Waypoint, bool) {
	return posAwayBetween(w, m, 0, float64(xzSpread), ySpread, fromX, fromZ)
}

// posAwayBetween is LandRandomPos.getPosAway with a minimum horizontal
// distance; the maximum also bounds each offset axis.
// vanilla: LandRandomPos.getPosAway
func posAwayBetween(w World, m *Mob, minXZ, maxXZ float64, ySpread int, fromX, fromZ float64) (Waypoint, bool) {
	foot := footprintOf(m.width(), m.height())
	at := m.cell()
	center := math.Atan2(m.Z-fromZ, m.X-fromX) - math.Pi/2
	best := math.Inf(-1)
	var cell Waypoint
	found := false
	for range defaultRandomTries {
		angle := center + float64(2*m.Rand.Float32()-1)*(math.Pi/2)
		dist := (minXZ + (maxXZ-minXZ)*math.Sqrt(m.Rand.Float64())) * math.Sqrt2
		dx, dz := -dist*math.Sin(angle), dist*math.Cos(angle)
		if math.Abs(dx) > maxXZ || math.Abs(dz) > maxXZ {
			continue
		}
		dy := m.Rand.Intn(2*ySpread+1) - ySpread
		spot := Waypoint{at.X + int32(math.Floor(dx)), at.Y + int32(dy), at.Z + int32(math.Floor(dz))}
		if !isStableDestination(w, m, spot) || hasMalus(w, m, spot) || !walkableAt(w, foot, spot.X, spot.Y, spot.Z) {
			continue
		}
		if value := m.walkTargetValue(w, spot); value > best {
			best, cell, found = value, spot, true
		}
	}
	return cell, found
}

// raidGardenGoal is Rabbit.RaidGardenGoal: walk to a farmland block under a
// full-grown carrot and eat a stage of it.
// vanilla: Rabbit.RaidGardenGoal
type raidGardenGoal struct {
	*moveToBlock
	wantsToRaid bool
	canRaid     bool
}

func newRaidGardenGoal(m *Mob) *raidGardenGoal {
	g := &raidGardenGoal{}
	g.moveToBlock = newMoveToBlock(m, rabbitRaidPace, rabbitRaidSearch, g.valid)
	return g
}

// valid is RaidGardenGoal.isValidTarget, which also arms canRaid.
func (g *raidGardenGoal) valid(w World, x, y, z int32) bool {
	if !g.wantsToRaid || g.canRaid || !registry.BlockOf(w.BlockAt(x, y, z)).InTag(supportsCropsBlocks) {
		return false
	}
	if age, isCarrot := carrotAge(w, x, y+1, z); isCarrot && age == carrotMaxAge {
		g.canRaid = true
		return true
	}
	return false
}

// supportsCropsBlocks marks the blocks of the supports_crops tag.
var supportsCropsBlocks = registry.BlockTagOf("minecraft:supports_crops")

// carrotMaxAge is the CarrotBlock age isMaxAge reads.
const carrotMaxAge = 7

func carrotAge(w World, x, y, z int32) (age int, isCarrot bool) {
	state := w.BlockAt(x, y, z)
	if registry.BlockOf(state) != registry.BlockCarrots {
		return 0, false
	}
	age, _ = registry.StateInt(state, registry.PropAge)
	return age, true
}

func (g *raidGardenGoal) Priority() int { return 5 }

func (g *raidGardenGoal) CanStart(t *tickView) bool {
	if g.nextStart <= 0 {
		if !t.w.MobGriefing() {
			return false
		}
		g.canRaid = false
		g.wantsToRaid = g.m.MoreCarrotTicks <= 0
	}
	return g.canUse(t)
}

func (g *raidGardenGoal) Start(t *tickView) { g.start(t) }

func (g *raidGardenGoal) ShouldContinue(t *tickView) bool { return g.canRaid && g.cont(t) }

// Tick eats a stage of the carrot once the rabbit is on it.
func (g *raidGardenGoal) Tick(t *tickView) {
	g.tick(t)
	m := g.m
	m.HeadYaw = turnToward(m.HeadYaw, float32(yawToward(float64(g.target.X)+0.5-m.X, float64(g.target.Z)+0.5-m.Z)), headTurnSpeed)
	if !g.reached {
		return
	}
	if _, isCarrot := carrotAge(t.w, g.target.X, g.target.Y+1, g.target.Z); g.canRaid && isCarrot {
		t.events = append(t.events, MobEvent{Mob: m, Kind: MobRaidedCrop,
			X: float64(g.target.X), Y: float64(g.target.Y + 1), Z: float64(g.target.Z)})
		m.MoreCarrotTicks = rabbitMoreCarrots
	}
	g.canRaid = false
	g.nextStart = 10
}

// rabbitMoveStep is Rabbit.customServerAiStep, RabbitMoveControl.tick and
// RabbitJumpControl.tick after goals and navigation: the rabbit walks only
// mid-hop, and hops once the landing delay is over.
// vanilla: Rabbit.customServerAiStep, RabbitMoveControl.tick, RabbitJumpControl.tick
func (m *Mob) rabbitMoveStep(t *tickView) {
	r := &m.rabbit
	if r.jumpDelay > 0 {
		r.jumpDelay--
	}
	if m.MoreCarrotTicks > 0 {
		m.MoreCarrotTicks = max(m.MoreCarrotTicks-m.Rand.Intn(3), 0)
	}
	wanted := len(m.nav.path) > 0
	if wanted {
		r.nextJumpSpeed = m.nav.speedFactor
		if m.InWater() {
			r.nextJumpSpeed = 1.5
		}
	}
	chase := false
	if m.OnGround {
		if !r.wasOnGround {
			r.jumping = false
			r.jumpDelay = rabbitJumpDelay
			if r.speedMod >= rabbitFleePace {
				r.jumpDelay = rabbitPanicJumpDelay
			}
		}
		if m.KillerBunny() && r.jumpDelay == 0 {
			if p := m.targetIn(t); p != nil && Dist2(m.X, m.Y, m.Z, p.X, p.Y, p.Z) < 16 {
				m.walk.dirX, m.walk.dirZ = p.X-m.X, p.Z-m.Z
				m.faceHeading()
				m.rabbitStartJumping(t)
				r.wasOnGround, chase, wanted = true, true, true
			}
		}
		if wanted && !chase && r.jumpDelay == 0 && !m.jump {
			m.rabbitStartJumping(t)
		}
	}
	r.wasOnGround = m.OnGround

	if m.OnGround && !r.jumping && !m.jump {
		r.speedMod = 0
		m.walk.speed = 0
	} else if wanted || m.jump {
		r.speedMod = r.nextJumpSpeed
	}
	if chase {
		length := math.Hypot(m.walk.dirX, m.walk.dirZ)
		if length > 0 {
			m.walk.dirX, m.walk.dirZ = m.walk.dirX/length, m.walk.dirZ/length
		}
		m.walk.speed = m.pace() * r.nextJumpSpeed
	}
	if m.jump {
		m.rabbitStartJumping(t)
	}
	m.jump = r.jumping
}

// faceHeading turns the body to the walk direction.
// vanilla: Rabbit.facePoint
func (m *Mob) faceHeading() {
	m.Yaw = float32(yawToward(m.walk.dirX, m.walk.dirZ))
}

// rabbitStartJumping is Rabbit.startJumping: the jump flag goes up for the
// hop's fifteen ticks, with the jump sound.
// vanilla: Rabbit.startJumping, Rabbit.setJumping
func (m *Mob) rabbitStartJumping(t *tickView) {
	r := &m.rabbit
	r.jumping = true
	r.jumpDuration, r.jumpTicks = rabbitJumpDuration, 0
	t.events = append(t.events, MobEvent{Mob: m, Kind: MobJumped,
		Pitch: ((m.Rand.Float32()-m.Rand.Float32())*0.2 + 1) * 0.8})
}

// rabbitAfterStep is Rabbit.aiStep after the travel: the hop's timer, and
// entity event 1 for a hop that left the ground.
// vanilla: Rabbit.aiStep
func (m *Mob) rabbitAfterStep(t *tickView) {
	r := &m.rabbit
	if r.jumpTicks != r.jumpDuration {
		r.jumpTicks++
	} else if r.jumpDuration != 0 {
		r.jumpTicks, r.jumpDuration = 0, 0
		r.jumping = false
	}
	if r.hopped {
		r.hopped = false
		t.events = append(t.events, MobEvent{Mob: m, Kind: MobHopped})
	}
}

// rabbitJump is Rabbit.getJumpPower and jumpFromGround: a low hop when
// strolling, a high one at a block or wall, plus moveRelative's forward
// push when nearly still.
// vanilla: Rabbit.getJumpPower, Rabbit.jumpFromGround
func (m *Mob) rabbitJump(factor float64) {
	r := &m.rabbit
	power := 0.3
	if r.speedMod <= 0.6 {
		power = 0.2
	}
	if p := m.nav.path; len(p) > 0 && float64(p[0].Y) > m.Y+0.5 {
		power = 0.5
	}
	if m.horizontalCollision {
		power = 0.5
	}
	power = power*factor + rules.JumpBoostPower(m.effects.All())
	if power > m.VY {
		m.VY = power
	}
	if r.speedMod > 0 && m.VX*m.VX+m.VZ*m.VZ < 0.01 {
		up := 1.5
		if m.IsBaby() {
			up = 0.5
		}
		scale := 0.1 / math.Sqrt(up*up+1)
		yaw := float64(m.Yaw) * math.Pi / 180
		m.VY += up * scale
		m.VX += -math.Sin(yaw) * scale
		m.VZ += math.Cos(yaw) * scale
	}
	r.hopped = true
}
