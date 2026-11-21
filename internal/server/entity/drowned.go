package entity

import (
	"math"

	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// The drowned: a zombie at home in water. By day it hunts only players in
// water, it swims at a target in water instead of walking the bottom, and it
// keeps what it drowned holding.

// drownedGoals is Drowned.addBehaviourGoals with Zombie.registerGoals' looks.
func drownedGoals(m *Mob) []Goal {
	return withZombieTargets(m, []Goal{
		&drownedGoToWaterGoal{m: m},
		newZombieBreakDoorGoal(m),
		// vanilla: Drowned.registerGoals, HurtByTargetGoal(this,
		// Drowned.class).setAlertOthers — ignores other drowned's blows but
		// calls them to a stranger's.
		&hurtByGoal{m: m, priority: priorityHurtBy, alertOthers: true, ignore: func(k MobType) bool { return k == TypeDrowned }},
		&nearestTargetGoal{m: m, priority: priorityNearest, accept: drownedOkTarget(m)},
		&drownedTridentGoal{witchAttackGoal{m: m}},
		&drownedAttackGoal{meleeAttackGoal{m: m, priority: 2}},
		newZombieAttackTurtleEggGoal(m),
		newDrownedBeachGoal(m),
		&drownedSwimUpGoal{m: m},
		&randomStroll{m: m, priority: hostileStroll, pick: func(t *tickView) (Waypoint, bool) { return plainStrollPos(t.w, m) }},
		&lookAtPlayer{m: m, priority: hostileLookAt},
		&randomLookAround{m: m, priority: hostileLookAround},
	},
		// vanilla: Drowned.addBehaviourGoals, Axolotl mustSee at 3
		&mobTargetGoal{m: m, priority: 3, fromPrey: true, matches: func(o *Mob) bool { return o.Type == TypeAxolotl }})
}

// drownedOkTarget is Drowned.okTarget: by day only a player in water.
func drownedOkTarget(m *Mob) func(*tickView, *Player) bool {
	return func(t *tickView, p *Player) bool {
		return !t.w.BrightOutside() || playerInWater(t.w, p)
	}
}

// drownedAttackGoal is Drowned$DrownedAttackGoal: the zombie's melee,
// held only while the target is one okTarget allows.
type drownedAttackGoal struct {
	meleeAttackGoal
}

func (g *drownedAttackGoal) CanStart(t *tickView) bool {
	// vanilla: Drowned$DrownedAttackGoal.canUse — the melee check runs first,
	// so its 20-tick window is spent even when okTarget refuses.
	p := g.m.targetIn(t)
	if !g.canUse(t, p) {
		return false
	}
	return p != nil && drownedOkTarget(g.m)(t, p)
}

// ShouldContinue holds a swimming hunt past a spent path: the swim steers at
// the target without one.
func (g *drownedAttackGoal) ShouldContinue(t *tickView) bool {
	p := g.m.targetIn(t)
	if p == nil || !drownedOkTarget(g.m)(t, p) {
		return false
	}
	return g.m.InWater() && g.m.targetInWater(t) || g.meleeAttackGoal.ShouldContinue(t)
}

// playerInWater is Entity.isInWater for a player: water anywhere in the
// standing 0.6 by 1.8 box.
func playerInWater(w World, p *Player) bool {
	pw, ph, _ := p.box()
	for x := int32(math.Floor(p.X - pw/2)); x <= int32(math.Floor(p.X+pw/2)); x++ {
		for y := int32(math.Floor(p.Y)); y <= int32(math.Floor(p.Y+ph)); y++ {
			for z := int32(math.Floor(p.Z - pw/2)); z <= int32(math.Floor(p.Z+pw/2)); z++ {
				if rules.IsWater(w.BlockAt(x, y, z)) {
					return true
				}
			}
		}
	}
	return false
}

// wantsToSwim is Drowned.wantsToSwim.
func (m *Mob) wantsToSwim(t *tickView) bool {
	return m.searchingForLand || m.targetInWater(t)
}

func (m *Mob) targetInWater(t *tickView) bool {
	p := m.targetIn(t)
	return p != nil && playerInWater(t.w, p)
}

// drownedMoveStep is Drowned$DrownedMoveControl.tick for a swimming drowned
// in water: lift while the target is above and steer at the target (vanilla
// steers at the next node), easing speed an eighth toward the pace each
// tick. Not swimming, an airborne drowned sinks faster.
func (m *Mob) drownedMoveStep(t *tickView) {
	m.SetMetadataSwimming(m.waterFluid.eyesIn && m.wantsToSwim(t))
	if !m.InWater() || !m.wantsToSwim(t) {
		if !m.OnGround {
			m.VY -= 0.008
		}
		return
	}
	p := m.targetIn(t)
	if p != nil && p.Y > m.Y || m.searchingForLand {
		m.VY += 0.002
	}
	var dx, dy, dz float64
	switch {
	case m.nav.swim && len(m.nav.path) > 0:
		dx, dy, dz = m.nav.flyX-m.X, m.nav.flyY-m.Y, m.nav.flyZ-m.Z
	case m.targetInWater(t):
		dx, dy, dz = p.X-m.X, p.Y-m.Y, p.Z-m.Z
	default:
		m.swimSpeed = 0
		m.walk.speed = 0
		return
	}
	d := math.Sqrt(dx*dx + dy*dy + dz*dz)
	if d < 1e-7 {
		m.swimSpeed = 0
		return
	}
	dy /= d
	m.Yaw = turnToward(m.Yaw, float32(yawToward(dx, dz)), 90)
	m.swimSpeed += (m.pace()*meleePace - m.swimSpeed) * 0.125
	m.VX += m.swimSpeed * dx * 0.005
	m.VY += m.swimSpeed * dy * 0.1
	m.VZ += m.swimSpeed * dz * 0.005
	// The swim is the steering's alone; the walk stands down.
	m.walk.speed = 0
}

// drownedSwimTravel is Drowned.travelInWater's swimming branch: the swim's
// push, no gravity, drag 0.9.
func (m *Mob) drownedSwimTravel(w World) {
	// moveRelative(0.01, input), the input the move control's speed
	yaw := float64(m.Yaw) * math.Pi / 180
	m.VX += -math.Sin(yaw) * 0.01 * m.swimSpeed
	m.VZ += math.Cos(yaw) * 0.01 * m.swimSpeed
	m.move(w, m.VX, m.VY, m.VZ)
	m.VX *= 0.9
	m.VY *= 0.9
	m.VZ *= 0.9
}

// drownedSeaLevel is the overworld's sea level, the only dimension whose
// spawn lists include the drowned.
const drownedSeaLevel = 63

// drownedSpawnOk is the IN_WATER placement plus
// Drowned.checkDrownedSpawnRules: water at and under the cell, no full block
// above, dark enough, then 1 in 15 in a river
// (minecraft:more_frequent_drowned_spawns) or 1 in 40 elsewhere more than
// five blocks below sea level.
func drownedSpawnOk(w SpawnWorld, rng Dice, x, y, z int32) bool {
	if !rules.IsWater(w.BlockAt(x, y, z)) || blockExtent(w.BlockAt(x, y+1, z)) == 1 {
		return false
	}
	if !rules.IsWater(w.BlockAt(x, y-1, z)) {
		return false
	}
	if w.Difficulty() == rules.DifficultyPeaceful || !isDarkEnoughToSpawn(w, x, y, z, rng) {
		return false
	}
	if w.Biome(x, y, z).InTag(registry.BiomeTagIDMoreFrequentDrownedSpawns) {
		return rng.NextIntN(15) == 0
	}
	return rng.NextIntN(40) == 0 && y < drownedSeaLevel-5
}

// The underwater conversion's numbers, Zombie.tick: 600 ticks with the
// eyes in water before it starts, 300 more before it is done.
const (
	drownInWaterTicks   = 600
	drownConversionTime = 300
)

// convertsInWater is Zombie.convertsInWater: the zombie and the husk.
func (m *Mob) convertsInWater() bool {
	return m.Type == TypeZombie || m.Type == TypeHusk
}

// waterConversionStep is Zombie.tick's ConversionTracker, run after baseTick's
// fluid read so the eyes are this tick's. It returns the kind the body
// becomes this tick (Zombie and Husk.doUnderWaterConversion), or empty.
func (m *Mob) waterConversionStep() MobType {
	if !m.convertsInWater() || m.NoAI || !m.Alive() {
		return NoMobType
	}
	if !m.waterFluid.eyesIn {
		m.InWaterTime = -1
		m.SetMetadataDrownedConverting(false)
		return NoMobType
	}
	if m.DrownedConverting {
		m.DrownedConversionTime--
		if m.DrownedConversionTime >= 0 {
			return NoMobType
		}
		if m.Type == TypeHusk {
			return TypeZombie
		}
		return TypeDrowned
	}
	m.InWaterTime++
	if m.InWaterTime >= drownInWaterTicks {
		m.SetMetadataDrownedConverting(true)
		m.DrownedConversionTime = drownConversionTime
	}
	return NoMobType
}

// ConvertedFrom copies what Mob.convertTo keeps (ConversionType.SINGLE with
// equipment, plus convertCommon): position, motion, facing, babyhood, hands,
// fall and player memory. NoAI and persistence come through the caller's
// convertedEntity.
func (m *Mob) ConvertedFrom(old *Mob) {
	m.SetPos(old.X, old.Y, old.Z)
	m.VX, m.VY, m.VZ = old.VX, old.VY, old.VZ
	m.Yaw, m.Pitch, m.HeadYaw = old.Yaw, old.Pitch, old.HeadYaw
	m.OnGround, m.FallDistance = old.OnGround, old.FallDistance
	m.SetMetadataBabyAge(old.BabyAge)
	if Ageable(old.Type) && Ageable(m.Type) {
		m.forcedAge, m.forcedAgeTimer = old.forcedAge, old.forcedAgeTimer
	}
	// vanilla: ConversionType.convertCommon copies a slot's chance only with its item
	for slot := range 6 {
		if item := old.EquipmentSlot(slot); !item.Empty() {
			m.SetEquipment(slot, item)
			m.DropChances[slot] = old.DropChances[slot]
		}
	}
	// vanilla: ConversionType.convertCommon
	m.CanBreakDoors = old.CanBreakDoors
	m.playerMemoryTicks = old.playerMemoryTicks
	m.playerCreditID = old.playerCreditID
	// The fluid state carries over: the first tick rereads it, but a save
	// in between keeps InWaterTime by it.
	m.waterFluid, m.lavaFluid = old.waterFluid, old.lavaFluid
}

// drownedGoToWaterGoal is DrownedGoToWaterGoal: by day, out of the water,
// walk to a water block found within ten tries nearby.
type drownedGoToWaterGoal struct {
	m      *Mob
	wanted Waypoint
}

func (g *drownedGoToWaterGoal) Priority() int    { return 1 }
func (g *drownedGoToWaterGoal) Flags() GoalFlags { return GoalMove }

func (g *drownedGoToWaterGoal) CanStart(t *tickView) bool {
	if !t.w.BrightOutside() || g.m.InWater() {
		return false
	}
	from := g.m.cell()
	for range 10 {
		c := Waypoint{from.X + int32(g.m.Rand.Intn(20)-10), from.Y + int32(2-g.m.Rand.Intn(8)), from.Z + int32(g.m.Rand.Intn(20)-10)}
		if rules.IsWaterBlock(t.w.BlockAt(c.X, c.Y, c.Z)) {
			g.wanted = c
			return true
		}
	}
	return false
}

func (g *drownedGoToWaterGoal) Start(t *tickView) {
	g.m.seekTo(t.w, float64(g.wanted.X)+0.5, float64(g.wanted.Y), float64(g.wanted.Z)+0.5, 1.0, navReach)
}

func (g *drownedGoToWaterGoal) ShouldContinue(*tickView) bool { return !g.m.navDone() }
func (g *drownedGoToWaterGoal) Tick(*tickView)                {}
func (g *drownedGoToWaterGoal) Stop(*tickView)                {}
func (g *drownedGoToWaterGoal) RequiresEveryTick() bool       { return false }

// drownedSwimUpGoal is DrownedSwimUpGoal: at night, in water deeper than two
// blocks under the sea, swim to a cell towards one block under the surface;
// it has no goal flags, so it runs beside the hunt.
type drownedSwimUpGoal struct {
	m     *Mob
	stuck bool
}

func (g *drownedSwimUpGoal) Priority() int    { return 6 }
func (g *drownedSwimUpGoal) Flags() GoalFlags { return 0 }

func (g *drownedSwimUpGoal) CanStart(t *tickView) bool {
	return !t.w.BrightOutside() && g.m.InWater() && g.m.Y < float64(t.w.SeaLevel()-2)
}

func (g *drownedSwimUpGoal) Start(*tickView) { g.m.searchingForLand, g.stuck = true, false }
func (g *drownedSwimUpGoal) Stop(*tickView)  { g.m.searchingForLand = false }

func (g *drownedSwimUpGoal) ShouldContinue(t *tickView) bool { return g.CanStart(t) && !g.stuck }

func (g *drownedSwimUpGoal) Tick(t *tickView) {
	m := g.m
	sea := float64(t.w.SeaLevel() - 1)
	if m.Y >= sea || !m.navDone() && !g.closeToNextPos() {
		return
	}
	// vanilla: DefaultRandomPos.getPosTowards with the amphibious
	// navigation's stable-destination and malus tests.
	cell, ok := posTowardsWhere(t.w, m, 4, 8, [2]float64{m.X, m.Z}, math.Pi/2, func(c Waypoint) bool {
		state := t.w.BlockAt(c.X, c.Y, c.Z)
		return !registry.IsSolidRender(state) && !rules.IsLava(state) && !world.IsAirState(t.w.BlockAt(c.X, c.Y-1, c.Z))
	})
	if !ok {
		g.stuck = true
		return
	}
	m.swimTo(t.w, float64(cell.X)+0.5, float64(cell.Y), float64(cell.Z)+0.5, 1.0)
}

// closeToNextPos is Drowned.closeToNextPos: the path's end within two blocks.
func (g *drownedSwimUpGoal) closeToNextPos() bool {
	m := g.m
	if len(m.nav.path) == 0 {
		return false
	}
	return Dist2(m.X, m.Y, m.Z, float64(m.nav.end.X), float64(m.nav.end.Y), float64(m.nav.end.Z)) < 4
}

func (g *drownedSwimUpGoal) RequiresEveryTick() bool { return false }

// drownedTridentGoal is DrownedTridentAttackGoal, using RangedAttackGoal's
// ten-block, five-seen-tick hunt rather than the skeleton's bow draw.
type drownedTridentGoal struct{ witchAttackGoal }

func (g *drownedTridentGoal) CanStart(t *tickView) bool {
	return g.witchAttackGoal.CanStart(t) && !g.m.MainHand.Empty() && registry.Item(g.m.MainHand.ItemID) == registry.ItemTrident
}

func (g *drownedTridentGoal) ShouldContinue(t *tickView) bool {
	return g.CanStart(t) || g.m.targetIn(t) != nil && !g.m.navDone()
}

func (g *drownedTridentGoal) Start(t *tickView) {
	g.witchAttackGoal.Start(t)
	g.m.SetMetadataAggressive(true)
	g.m.SetMetadataUsingItem(true)
}

func (g *drownedTridentGoal) Stop(t *tickView) {
	g.witchAttackGoal.Stop(t)
	g.m.SetMetadataAggressive(false)
	g.m.SetMetadataUsingItem(false)
}

func (g *drownedTridentGoal) Tick(t *tickView) { g.tick(t, 40, g.m.throwTrident) }

// vanilla: Drowned.performRangedAttack
func (m *Mob) throwTrident(t *tickView, p *Player) {
	ox, oy, oz, ax, ay, az := arrowAim(m, p, rangedArrowAimLift)
	vx, vy, vz := ShootVelocity(ax, ay, az, rangedArrowPower,
		rangedArrowInaccuracy-rangedInaccuracyPerDifficultyLevel*float64(t.w.Difficulty()), m.Rand)
	t.events = append(t.events, MobEvent{Mob: m, Kind: MobThrewTrident,
		X: ox, Y: oy, Z: oz, VX: vx, VY: vy, VZ: vz,
		Pitch: 1 / (m.Rand.Float32()*0.4 + 0.8)})
}

// drownedBeachGoal is DrownedGoToBeachGoal's MoveToBlockGoal(1.0, 8, 2).
type drownedBeachGoal struct{ *moveToBlock }

func newDrownedBeachGoal(m *Mob) *drownedBeachGoal {
	g := &drownedBeachGoal{newMoveToBlock(m, 1.0, 8, func(w World, x, y, z int32) bool {
		return world.IsAirState(w.BlockAt(x, y+1, z)) && world.IsAirState(w.BlockAt(x, y+2, z)) && faceFull(w.BlockAt(x, y, z), 1)
	})}
	g.vertRange = 2
	return g
}

func (g *drownedBeachGoal) Priority() int { return 5 }
func (g *drownedBeachGoal) CanStart(t *tickView) bool {
	// The superclass spends its search cooldown even when the daylight or depth refuses.
	return g.canUse(t) && !t.w.BrightOutside() && g.m.InWater() && g.m.Y >= float64(t.w.SeaLevel()-3)
}
func (g *drownedBeachGoal) Start(t *tickView) {
	g.m.searchingForLand = false
	g.start(t)
}
func (g *drownedBeachGoal) ShouldContinue(t *tickView) bool { return g.cont(t) }
func (g *drownedBeachGoal) Tick(t *tickView)                { g.tick(t) }
