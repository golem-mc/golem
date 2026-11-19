package entity

import (
	"math"

	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
)

// TypeDolphin is minecraft:dolphin.
const TypeDolphin = registry.EntityTypeDolphin

// vanilla: Dolphin.TOTAL_AIR_SUPPLY, TOTAL_MOISTNESS_LEVEL
const (
	dolphinMaxAir       = 4800
	dolphinMaxMoistness = 2400
)

func init() {
	// vanilla: EntityTypes.DOLPHIN, Dolphin.createAttributes
	kinds[TypeDolphin] = &kind{
		maxHeadXRot: 1, maxHeadYRot: 1,
		name: "Dolphin", width: float64(float32(0.9)), height: float64(float32(0.6)), eyeHeight: float64(float32(0.3)),
		babyWidth: float64(float32(0.9) * float32(0.65)), babyHeight: float64(float32(0.6) * float32(0.65)), babyEye: 0.09375,
		babyable: true, water: true, maxHealth: 10, speed: float64(float32(1.2)), attackDamage: 3,
		ambientInterval: 120,
		sounds:          MobSounds{Ambient: registry.SoundEntityDolphinAmbient, Hurt: registry.SoundEntityDolphinHurt, Death: registry.SoundEntityDolphinDeath},
	}
	mobTracking[TypeDolphin] = tracking{rangeChunks: 5, updateInterval: 3}
}

type dolphinState struct {
	gotFish                      bool
	moistness                    int
	speed, up                    float64
	treasure                     Waypoint
	treasureReady, treasureFound bool
}

// DolphinData is Dolphin's synched fish and moistness state.
func (m *Mob) DolphinData() (gotFish bool, moistness int) {
	return m.dolphin.gotFish, m.dolphin.moistness
}

// vanilla: Dolphin.tick
func (m *Mob) dolphinStep(w World, events []MobEvent) []MobEvent {
	if m.Type != TypeDolphin {
		return events
	}
	if m.NoAI {
		m.SetMetadataAirSupply(dolphinMaxAir)
		return events
	}
	if m.InWater() || m.inRain(w) {
		m.SetMetadataDolphinMoistness(dolphinMaxMoistness)
		return events
	}
	m.SetMetadataDolphinMoistness(m.dolphin.moistness - 1)
	if m.dolphin.moistness <= 0 {
		if landed, soft := m.plainHurt(1, registry.DamageTypeDryOut, false); landed {
			m.noteLastDamage(registry.DamageTypeDryOut)
			events = append(events, MobEvent{Mob: m, Kind: MobBurned, DamageType: registry.DamageTypeDryOut, Softened: soft, NoBubble: true})
		}
	}
	if m.OnGround {
		m.VX += float64((m.Rand.Float32()*2 - 1) * 0.2)
		m.VY += 0.5
		m.VZ += float64((m.Rand.Float32()*2 - 1) * 0.2)
		m.Yaw = m.Rand.Float32() * 360
		m.OnGround = false
		m.Impulse = true
	}
	return events
}

func dolphinGoals(m *Mob) []Goal {
	return []Goal{
		&breathAirGoal{m: m}, &tryFindWaterGoal{m: m},
		&dolphinTreasureGoal{m: m},
		&hurtByGoal{m: m, priority: 1, alertOthers: true, ignore: func(t MobType) bool { return kindOf(t).family == TypeGuardian || t == TypeGuardian }, accept: func(*tickView, *Player) bool { return !m.IsBaby() }},
		&dolphinSwimWithPlayerGoal{m: m},
		&swimStrollGoal{m: m, interval: 10}, &randomLookAround{m: m, priority: 4},
		&lookAtPlayer{m: m, priority: 5, reach: 6}, &dolphinJumpGoal{m: m},
		&meleeAttackGoal{m: m, priority: 6, pace: float64(float32(1.2)), followUnseen: true, refuse: m.IsBaby, holdSwing: m.IsBaby, swingSound: registry.SoundEntityDolphinAttack},
		&dolphinPlayGoal{m: m}, &dolphinRideGoal{m: m, boat: true}, &dolphinRideGoal{m: m},
		&avoidGoal{m: m, priority: 9, active: func() bool { return true }, maxDist: 8, walk: 1, sprint: 1, mobs: []MobType{TypeGuardian, TypeElderGuardian}},
	}
}

func (m *Mob) dolphinMoveControl(w World) {
	// vanilla: SmoothSwimmingMoveControl(85,10,0.02,0.1,true)
	if m.InWater() {
		m.VY += 0.005
	}
	m.dolphin.speed, m.dolphin.up = m.smoothSwimMoveControl(w, float64(float32(0.02)), float64(float32(0.1)))
}

// breathAirGoal is BreathAirGoal, which cannot be interrupted until refilled.
type breathAirGoal struct{ m *Mob }

func (g *breathAirGoal) Priority() int                   { return 0 }
func (g *breathAirGoal) Flags() GoalFlags                { return GoalMove | GoalLook }
func (g *breathAirGoal) Uninterruptible() bool           { return true }
func (g *breathAirGoal) CanStart(*tickView) bool         { return g.m.AirSupply < 140 }
func (g *breathAirGoal) ShouldContinue(t *tickView) bool { return g.CanStart(t) }
func (g *breathAirGoal) Start(t *tickView)               { g.m.stopSeeking(); g.findAir(t.w) }
func (g *breathAirGoal) Stop(*tickView)                  {}
func (g *breathAirGoal) RequiresEveryTick() bool         { return true }
func (g *breathAirGoal) Tick(t *tickView) {
	m := g.m
	g.findAir(t.w)
	// BreathAirGoal.tick moves once in addition to ordinary travel.
	m.smoothSwimTravelInput(0.02, m.dolphin.up)
	m.move(t.w, m.VX, m.VY, m.VZ)
}
func (g *breathAirGoal) findAir(w World) {
	m := g.m
	at := m.cell()
	dest := Waypoint{X: at.X, Y: at.Y + 8, Z: at.Z}
	// vanilla: BlockPos.neighborColumn, centre then N/E/S/W at each height.
search:
	for y := at.Y; y <= at.Y+8; y++ {
		for _, d := range [5][2]int32{{0, 0}, {0, -1}, {1, 0}, {0, 1}, {-1, 0}} {
			x, z := at.X+d[0], at.Z+d[1]
			state := w.BlockAt(x, y, z)
			if rules.IsAir(state) || registry.BlockOf(state) == registry.BlockBubbleColumn {
				dest = Waypoint{X: x, Y: y, Z: z}
				break search
			}
		}
	}
	m.swimTo(w, float64(dest.X), float64(dest.Y+1), float64(dest.Z), 1)
}

// tryFindWaterGoal is TryFindWaterGoal's direct move-control request on land.
type tryFindWaterGoal struct{ m *Mob }

func (g *tryFindWaterGoal) Priority() int    { return 0 }
func (g *tryFindWaterGoal) Flags() GoalFlags { return 0 }
func (g *tryFindWaterGoal) CanStart(t *tickView) bool {
	at := g.m.cell()
	return g.m.OnGround && !rules.IsWater(t.w.BlockAt(at.X, at.Y, at.Z))
}
func (g *tryFindWaterGoal) ShouldContinue(t *tickView) bool { return g.CanStart(t) }
func (g *tryFindWaterGoal) Start(t *tickView) {
	m := g.m
	at := m.cell()
	for z := at.Z - 2; z <= at.Z+2; z++ {
		for y := at.Y - 2; y <= at.Y; y++ {
			for x := at.X - 2; x <= at.X+2; x++ {
				if rules.IsWater(t.w.BlockAt(x, y, z)) {
					m.moveTo([]Waypoint{{X: x, Y: y, Z: z}}, Waypoint{X: x, Y: y, Z: z}, 1)
					return
				}
			}
		}
	}
}
func (g *tryFindWaterGoal) Tick(*tickView)          {}
func (g *tryFindWaterGoal) Stop(*tickView)          {}
func (g *tryFindWaterGoal) RequiresEveryTick() bool { return false }

func dolphinSpawnOk(w SpawnWorld, x, y, z int32) bool {
	// vanilla: AgeableWaterCreature.checkSurfaceAgeableWaterCreatureSpawnRules
	sea := w.SeaLevel()
	return y >= sea-13 && y <= sea && rules.IsWater(w.BlockAt(x, y, z)) && rules.IsWater(w.BlockAt(x, y-1, z)) && registry.BlockOf(w.BlockAt(x, y+1, z)) == registry.BlockWater
}

// waterSplash queues the splash of a body first touching water: the kind's
// low-speed sound below a volume of 0.25, the generic one above. The volume
// is 0.2 of the body's own speed, or 0.9 of its controlling passenger's.
// vanilla: Entity.doWaterSplashEffect, Entity.getSwimSplashSound
func (m *Mob) waterSplash(events []MobEvent) []MobEvent {
	vx, vy, vz, scale := m.VX, m.VY, m.VZ, float32(0.2)
	switch {
	case m.Steered():
		// A riding player's movement is zero on the server: rideTick clears
		// it and the client moves the player.
		vx, vy, vz, scale = 0, 0, 0, 0.9
	case m.Horse.Rider == uuid.Nil && !m.NoAI && len(m.Passengers) > 0 &&
		!m.Passengers[0].inTag(registry.EntityTypeTagIDNonControllingRider):
		// vanilla: Mob.getControllingPassenger
		p := m.Passengers[0]
		vx, vy, vz, scale = p.VX, p.VY, p.VZ, 0.9
	}
	volume := min(float32(1), float32(math.Sqrt(vx*vx*0.2+vy*vy+vz*vz*0.2))*scale)
	sound := registry.SoundEntityGenericSplash
	if volume < 0.25 {
		switch {
		case m.Type == TypeDolphin:
			sound = registry.SoundEntityDolphinSplash
		case m.Type == TypeAxolotl:
			sound = registry.SoundEntityAxolotlSplash
		case m.hostileSounding():
			sound = registry.SoundEntityHostileSplash
		}
	}
	return append(events, MobEvent{Mob: m, Kind: MobSplashed, SoundID: sound, Volume: volume, Pitch: 1 + (m.Rand.Float32()-m.Rand.Float32())*0.4})
}
