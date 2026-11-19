package entity

import (
	"math"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/registry"
)

// The pillager: an illager whose crossbow charges, waits a beat and shoots
// from up to eight blocks. Not modelled: the five-slot inventory, and ammo
// held in a hand (the load is always arrows).
// vanilla: Pillager, RangedCrossbowAttackGoal, CrossbowAttackMob

const TypePillager = registry.EntityTypePillager

const (
	crossbowAttackRadius = 8.0 // Pillager.registerGoals
	crossbowWalkFactor   = 1.0
	// crossbowMobPower is CrossbowItem.MOB_ARROW_POWER.
	crossbowMobPower = 1.6
	// crossbowInaccuracy is performCrossbowAttack's 14 less 4 per difficulty.
	crossbowInaccuracy           = 14.0
	crossbowInaccuracyPerLevel   = 4.0
	crossbowAimLift              = 0.20000000298023224
	crossbowPathDelayMinTicks    = 20 // RangedCrossbowAttackGoal.PATHFINDING_DELAY_RANGE, 1 to 2 s
	crossbowPathDelaySpread      = 21
	crossbowAttackDelayMinTicks  = 20
	crossbowAttackDelaySpread    = 20
	crossbowSeeTicksBeforeAttack = 5
)

func init() {
	// vanilla: EntityTypes.PILLAGER and Pillager.createAttributes
	kinds[TypePillager] = &kind{
		name: "Pillager", width: 0.6, height: 1.95, eyeHeight: 1.6575,
		maxHealth: 24, speed: float64(float32(0.35)), hostile: true, followRange: 32, attackDamage: 5,
		sounds: MobSounds{
			Ambient: registry.SoundEntityPillagerAmbient,
			Hurt:    registry.SoundEntityPillagerHurt,
			Death:   registry.SoundEntityPillagerDeath,
		},
	}
	mobTracking[TypePillager] = tracking{rangeChunks: 8, updateInterval: 3}
}

// finalizePillager is Pillager.finalizeSpawn without a raid: a crossbow in
// the main hand and the weapon enchantment roll.
func (m *Mob) finalizePillager(local rules.LocalDifficulty) {
	m.SetEquipment(protocol.EquipMainHand, protocol.ItemStack{ItemID: int32(registry.ItemCrossbow), Count: 1})
	dice := m.equipmentRand()
	m.rollSpawnEnchantments(GoDice{dice}, local)
	// vanilla: Pillager.enchantSpawnedWeapon, PILLAGER_SPAWN_CROSSBOW
	if dice.Intn(300) == 0 && m.MainHand.ItemID == int32(registry.ItemCrossbow) {
		stack := m.MainHand
		enchantByKey("minecraft:pillager_spawn_crossbow", diceEnchantRandom{GoDice{dice}}, &stack, local.Special)
		m.SetEquipment(protocol.EquipMainHand, stack)
	}
}

// pillagerGoals wires Pillager.registerGoals: FloatGoal 0, creaking avoidance
// 1, hold ground 2, the crossbow 3, the illager targets, stroll 8 at 0.6, looks 9 (every
// tick, 15 blocks) and 10.
func pillagerGoals(m *Mob) []Goal {
	goals := append([]Goal{newFloatGoal(m), newCreakingAvoidGoal(m, 1, illagerCreakingWalk, illagerCreakingSprint),
		newObtainBannerGoal(m)}, illagerTargets(m, true)...)
	return withRaidGoals(m, append(goals,
		&holdGroundGoal{m: m, priority: 2},
		&crossbowAttackGoal{m: m, priority: 3},
		&randomStroll{m: m, priority: 8, pace: 0.6},
		&lookAtPlayer{m: m, priority: 9, reach: 15, chance: 1},
		&lookAtMob{m: m, priority: 10, reach: 15},
	))
}

type crossbowState uint8

const (
	crossbowUncharged crossbowState = iota
	crossbowCharging
	crossbowChargedWaiting
	crossbowReady
)

// crossbowAttackGoal is RangedCrossbowAttackGoal: walk to within the attack
// radius with a line of sight, charge, wait a random 20 to 39 ticks, shoot.
type crossbowAttackGoal struct {
	m          *Mob
	priority   int
	state      crossbowState
	seeTime    int
	attackWait int
	pathDelay  int
	// drawn is LivingEntity.getTicksUsingItem.
	drawn int
}

func (g *crossbowAttackGoal) Priority() int    { return g.priority }
func (g *crossbowAttackGoal) Flags() GoalFlags { return GoalMove | GoalLook }

func (g *crossbowAttackGoal) holdsCrossbow() bool {
	return g.m.MainHand.ItemID == int32(registry.ItemCrossbow) && !g.m.MainHand.Empty()
}

func (g *crossbowAttackGoal) validTarget(t *tickView) bool { return g.m.targetIn(t) != nil }

func (g *crossbowAttackGoal) CanStart(t *tickView) bool {
	return g.validTarget(t) && g.holdsCrossbow()
}

func (g *crossbowAttackGoal) Start(*tickView) { g.m.SetMetadataAggressive(true) }

func (g *crossbowAttackGoal) ShouldContinue(t *tickView) bool {
	return g.validTarget(t) && (g.CanStart(t) || len(g.m.navOf().nav.path) > 0) && g.holdsCrossbow()
}

// Stop clears the target, and mid-charge the use and loaded arrow. The
// state and wait keep running, so a target regained mid-wait still gets
// shot.
func (g *crossbowAttackGoal) Stop(*tickView) {
	m := g.m
	m.SetMetadataAggressive(false)
	m.SetMetadataTargetID(0)
	g.seeTime = 0
	if m.UsingItem {
		m.SetMetadataUsingItem(false)
		m.SetMetadataChargingCrossbow(false)
		stack := m.MainHand
		stack.SetChargedProjectiles(nil)
		m.SetEquipment(protocol.EquipMainHand, stack)
	}
	m.stopSeeking()
}

func (g *crossbowAttackGoal) RequiresEveryTick() bool { return true }

func (g *crossbowAttackGoal) Tick(t *tickView) {
	m := g.m
	p := m.targetIn(t)
	if p == nil {
		return
	}
	sees := m.seesTarget(t.w, p)
	if sees != (g.seeTime > 0) {
		g.seeTime = 0
	}
	if sees {
		g.seeTime++
	} else {
		g.seeTime--
	}
	dist2 := Dist2(m.X, m.Y, m.Z, p.X, p.Y, p.Z)
	needsToMove := (dist2 > crossbowAttackRadius*crossbowAttackRadius || g.seeTime < crossbowSeeTicksBeforeAttack) && g.attackWait == 0
	if needsToMove {
		g.pathDelay--
		if g.pathDelay <= 0 {
			pace := crossbowWalkFactor
			if g.state != crossbowUncharged {
				pace *= 0.5
			}
			m.seekTo(t.w, p.X, p.Y, p.Z, pace, navReach)
			g.pathDelay = crossbowPathDelayMinTicks + m.Rand.Intn(crossbowPathDelaySpread)
		}
	} else {
		g.pathDelay = 0
		m.stopSeeking()
	}
	dx, dz := p.X-m.X, p.Z-m.Z
	if dx != 0 || dz != 0 {
		m.HeadYaw = turnToward(m.HeadYaw, float32(yawToward(dx, dz)), headTurnSpeed)
	}
	m.Pitch = turnToward(m.Pitch,
		float32(pitchToward(dx, p.Y+p.eyeHeight()-(m.Y+m.eyeHeight()), dz)), headTurnSpeed)

	g.stepCrossbow(t, p, sees, !needsToMove)
}

// stepCrossbow advances the charge, wait and shoot cycle one tick; a draw
// begins only when mayDraw.
// vanilla: CrossbowAttack.crossbowAttack
func (g *crossbowAttackGoal) stepCrossbow(t *tickView, p *Player, sees, mayDraw bool) {
	m := g.m
	switch g.state {
	case crossbowUncharged:
		if mayDraw {
			m.startUsingWeapon(registry.ItemCrossbow)
			m.SetMetadataChargingCrossbow(true)
			g.drawn = 0
			g.state = crossbowCharging
		}
	case crossbowCharging:
		if !m.UsingItem {
			g.state = crossbowUncharged
		}
		g.drawn++
		g.chargeSounds(t)
		if duration := rules.CrossbowChargeTicks(m.MainHand, &rules.EnchContext{Rand: m.Rand}); int32(g.drawn) >= duration {
			m.SetMetadataUsingItem(false)
			m.SetMetadataChargingCrossbow(false)
			g.state = crossbowChargedWaiting
			g.attackWait = crossbowAttackDelayMinTicks + m.Rand.Intn(crossbowAttackDelaySpread)
		}
	case crossbowChargedWaiting:
		g.attackWait--
		if g.attackWait == 0 {
			g.state = crossbowReady
		}
	case crossbowReady:
		if sees {
			g.shoot(t, p)
			g.state = crossbowUncharged
		}
	}
}

// chargeSounds is CrossbowItem.onUseTick: the start sound at a fifth of the
// charge, the middle at half, then the load (an arrow only if none is in).
// Vanilla plays the first two on the players' channel.
func (g *crossbowAttackGoal) chargeSounds(t *tickView) {
	m := g.m
	start, mid, load := rules.CrossbowChargeStep(int32(g.drawn), rules.CrossbowChargeTicks(m.MainHand, &rules.EnchContext{Rand: m.Rand}))
	play := func(sound protocol.SoundHolder, volume, pitch float32) {
		if sound.ID >= 0 || sound.Name != "" {
			t.events = append(t.events, m.consumeSoundEvent(sound, volume, pitch))
		}
	}
	if start {
		play(rules.ChargingSounds(m.MainHand).Start, 0.5, 1)
	}
	if mid {
		play(rules.ChargingSounds(m.MainHand).Mid, 0.5, 1)
	}
	if load && !m.CrossbowLoaded() {
		m.loadCrossbow(t.w)
		play(rules.ChargingSounds(m.MainHand).End, 1, 1/(m.Rand.Float32()*0.5+1)+0.2)
	}
}

// shoot is CrossbowAttackMob.performCrossbowAttack: an empty crossbow fires
// nothing; a loaded one fires its arrows (three for multishot) at a third
// of the target's height, lifted by a fifth of the horizontal distance.
// vanilla: CrossbowItem.performShooting, shootProjectile
func (g *crossbowAttackGoal) shoot(t *tickView, p *Player) {
	m := g.m
	m.NoActionTicks = 0
	loaded := m.MainHand.ChargedProjectiles()
	if len(loaded) == 0 {
		return
	}
	stack := m.MainHand
	stack.SetChargedProjectiles(nil)
	m.SetEquipment(protocol.EquipMainHand, stack)
	originX, originY, originZ, aimX, aimY, aimZ := arrowAim(m, p, crossbowAimLift)
	inaccuracy := crossbowInaccuracy - crossbowInaccuracyPerLevel*float64(t.w.Difficulty())
	angles := rules.CrossbowAngles(len(loaded), rules.CrossbowMaxSpread(m.MainHand, m.crossbowEnchContext(t.w)))
	for i, angle := range angles {
		ax, ay, az := spreadAim(aimX, aimY, aimZ, angle)
		vx, vy, vz := ShootVelocity(ax, ay, az, crossbowMobPower, inaccuracy, m.Rand)
		t.events = append(t.events, MobEvent{
			Mob: m, Kind: MobShotArrow, Crossbow: true,
			X: originX, Y: originY, Z: originZ, VX: vx, VY: vy, VZ: vz,
			Damage: ArrowBaseDamage, Player: *p,
		})
		pitch := float32(1)
		if i > 0 {
			rangeDecider := float32(0.43)
			if i&1 == 1 {
				rangeDecider = 0.63
			}
			pitch = 1/(m.Rand.Float32()*0.5+1.8) + rangeDecider
		}
		t.events = append(t.events, MobEvent{Mob: m, Kind: MobSounded, SoundID: registry.SoundItemCrossbowShoot, Volume: 1, Pitch: pitch})
		// vanilla: ProjectileWeaponItem.shoot hurts the weapon per projectile and stops once it is gone
		if g.wearCrossbow(t) {
			break
		}
	}
}

// loadCrossbow loads the main-hand crossbow with one arrow per projectile
// its multishot fires; the load is saved with the crossbow.
// vanilla: CrossbowItem.tryLoadProjectiles, ProjectileWeaponItem.draw, Monster.getProjectile
func (m *Mob) loadCrossbow(w ...World) {
	arrows := make([]protocol.ItemStack, rules.CrossbowProjectileCount(m.MainHand, m.crossbowEnchContext(w...)))
	for i := range arrows {
		arrows[i] = protocol.ItemStack{ItemID: int32(registry.ItemArrow), Count: 1}
	}
	stack := m.MainHand
	stack.SetChargedProjectiles(arrows)
	m.SetEquipment(protocol.EquipMainHand, stack)
}

// CrossbowLoaded reports whether the main hand holds charged projectiles.
// vanilla: CrossbowItem.isCharged
func (m *Mob) CrossbowLoaded() bool { return len(m.MainHand.ChargedProjectiles()) > 0 }

// wearCrossbow takes one arrow's wear off the crossbow, clearing the hand
// and reporting true when it wore through.
func (g *crossbowAttackGoal) wearCrossbow(t *tickView) bool {
	m := g.m
	worn, broke := rules.Wear(m.MainHand, rules.ProcessDurabilityChange(m.MainHand, rules.CrossbowWearPerShot(false), m.Rand))
	m.SetEquipment(protocol.EquipMainHand, worn)
	if broke {
		t.events = append(t.events, MobEvent{Mob: m, Kind: MobHandBroke})
	}
	return broke
}

// spreadAim turns the aim angle degrees sideways about the axis the aim and
// its quarter-turn-up make.
// vanilla: CrossbowItem.getProjectileShotVector
func spreadAim(x, y, z float64, angle float32) (float64, float64, float64) {
	if angle == 0 {
		return x, y, z
	}
	n := math.Sqrt(x*x + y*y + z*z)
	vx, vy, vz := x/n, y/n, z/n
	// right = view x up
	rx, ry, rz := -vz, 0.0, vx
	if rx*rx+ry*ry+rz*rz <= 1e-7 {
		// Straight up or down: the cross with the body's up vector.
		rx, ry, rz = 1, 0, 0
	}
	// The view turned a quarter about right is the spread's axis.
	ax, ay, az := rotateAxis(vx, vy, vz, math.Pi/2, rx, ry, rz)
	return rotateAxis(vx, vy, vz, float64(angle)*math.Pi/180, ax, ay, az)
}

// rotateAxis is Rodrigues' rotation of v by theta about the axis a, which
// need not be a unit vector.
func rotateAxis(vx, vy, vz, theta, ax, ay, az float64) (float64, float64, float64) {
	n := math.Sqrt(ax*ax + ay*ay + az*az)
	if n == 0 {
		return vx, vy, vz
	}
	ax, ay, az = ax/n, ay/n, az/n
	cos, sin := math.Cos(theta), math.Sin(theta)
	dot := ax*vx + ay*vy + az*vz
	cx, cy, cz := ay*vz-az*vy, az*vx-ax*vz, ax*vy-ay*vx
	return vx*cos + cx*sin + ax*dot*(1-cos),
		vy*cos + cy*sin + ay*dot*(1-cos),
		vz*cos + cz*sin + az*dot*(1-cos)
}

// crossbowEnchContext is the shooter's ENCHANTED_ENTITY loot context.
func (m *Mob) crossbowEnchContext(worlds ...World) *rules.EnchContext {
	ctx := &rules.EnchContext{Victim: m.typeID(), Attacker: -1, DirectAttacker: -1, Rand: m.Rand,
		User: rules.EnchUser{Riding: m.RidesVehicle, InWater: m.InWater()}}
	if len(worlds) > 0 {
		w := worlds[0]
		ctx.Thundering = w.IsThundering()
		ctx.CanSeeSky = m.seesSkyAbove(w)
		ctx.Block = registry.BlockOf(w.BlockAt(rules.BlockOf(m.X), rules.BlockOf(m.Y), rules.BlockOf(m.Z)))
	}
	return ctx
}
