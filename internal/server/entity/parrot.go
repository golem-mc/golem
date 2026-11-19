package entity

import (
	"math"
	"math/rand"

	"github.com/google/uuid"

	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// The parrot: a tameable flyer that is never a baby, takes no fall damage and
// imitates the monsters near it and lands on its owner's shoulder.
// vanilla: Parrot

// TypeParrot is minecraft:parrot.
const TypeParrot = registry.EntityTypeParrot

// parrotVariants is Parrot.Variant's count: red_blue, blue, green,
// yellow_blue, gray.
const parrotVariants = 5

// The parrot's numbers.
const (
	// parrotFlyingSpeed is the FLYING_SPEED attribute (0.4f).
	parrotFlyingSpeed = float64(float32(0.4))
	// parrotPitchTurn is the maxTurn of FlyingMoveControl(this, 10, false).
	parrotPitchTurn = 10
	// parrotFlap is the share of a fall kept each tick while airborne.
	// vanilla: Parrot.calculateFlapping
	parrotFlap = 0.6
	// parrotPriorityFollowMob is the registration of FollowMobGoal(1.0, 3, 7).
	parrotPriorityFollowMob = 3
	parrotFollowStop        = 3.0
	parrotFollowArea        = 7.0
	// parrotImitateRange is imitateNearbyMobs' inflate(20).
	parrotImitateRange = 20.0
	parrotImitateVol   = float32(0.7)
	// parrotWander* are getTreePos' box half sizes and HoverRandomPos'
	// 8 blocks across and 7 up or down.
	parrotTreeXZ, parrotTreeY = 3, 6
	parrotWanderRange         = 8.0
	parrotWanderRise          = 7
	parrotWanderTries         = 10
	// parrotRideCooldown is ShoulderRidingEntity.canSitOnShoulder's 100 ticks.
	parrotRideCooldown = 100
	// parrotPriorityShoulder is the registration of LandOnOwnersShoulderGoal.
	parrotPriorityShoulder = 3
)

func init() {
	// vanilla: EntityTypes.PARROT, Parrot.createAttributes; the eyes are 0.54
	kinds[TypeParrot] = &kind{
		name: "Parrot", width: 0.5, height: 0.9, eyeHeight: float64(float32(0.54)),
		maxHealth: 6, speed: float64(float32(0.2)), attackDamage: 3, flying: true, flyPath: &parrotFlight,
		sounds: MobSounds{
			Ambient: registry.SoundEntityParrotAmbient, Hurt: registry.SoundEntityParrotHurt,
			Death: registry.SoundEntityParrotDeath, Step: registry.SoundEntityParrotStep,
		},
		ambientInterval: 120, // vanilla: Animal.getAmbientSoundInterval
	}
	mobTracking[TypeParrot] = tracking{rangeChunks: 8, updateInterval: 3}
}

// parrotState is the parrot's own state.
type parrotState struct {
	rng     *rand.Rand
	variant int
	// quiet is true in Peaceful, where the ambient sound never imitates.
	quiet bool
	// rideCooldown counts ticks lived; a parrot lands on a shoulder after
	// parrotRideCooldown. Not saved. vanilla: ShoulderRidingEntity.rideCooldownCounter
	rideCooldown int
}

// SetParrotSoundRandom binds the level stream used for imitation and ambient selection.
func (m *Mob) SetParrotSoundRandom(rng *rand.Rand) {
	if m.Type == TypeParrot {
		m.parrot.rng = rng
	}
}

// ParrotVariant is the synched Parrot.Variant id.
func (m *Mob) ParrotVariant() int { return m.parrot.variant }

// SetParrotVariant applies Variant.byId's clamp.
func (m *Mob) SetParrotVariant(v int) {
	m.SetMetadataParrotVariant(max(0, min(v, parrotVariants-1)))
}

// TameParrot is the seed's one-in-ten roll: the parrot becomes the player's,
// standing. It reports success.
// vanilla: Parrot.mobInteract, TamableAnimal.tame
func (m *Mob) TameParrot(owner uuid.UUID) bool {
	if m.Rand.Intn(10) != 0 {
		return false
	}
	m.SetMetadataTamableTame(true)
	m.SetMetadataTamableOwner(owner)
	return true
}

// parrotImitations maps the monsters a parrot copies to its sound; the empty
// sound is the happy ghast's.
// vanilla: Parrot.MOB_SOUND_MAP
var parrotImitations = [registry.EntityTypeCount]registry.Sound{
	registry.EntityTypeBlaze: registry.SoundEntityParrotImitateBlaze, registry.EntityTypeBogged: registry.SoundEntityParrotImitateBogged, registry.EntityTypeBreeze: registry.SoundEntityParrotImitateBreeze,
	registry.EntityTypeCamelHusk: registry.SoundEntityParrotImitateCamelHusk, registry.EntityTypeCaveSpider: registry.SoundEntityParrotImitateSpider, registry.EntityTypeCreaking: registry.SoundEntityParrotImitateCreaking,
	registry.EntityTypeCreeper: registry.SoundEntityParrotImitateCreeper, registry.EntityTypeDrowned: registry.SoundEntityParrotImitateDrowned, registry.EntityTypeElderGuardian: registry.SoundEntityParrotImitateElderGuardian,
	registry.EntityTypeEnderDragon: registry.SoundEntityParrotImitateEnderDragon, registry.EntityTypeEndermite: registry.SoundEntityParrotImitateEndermite, registry.EntityTypeEvoker: registry.SoundEntityParrotImitateEvoker,
	registry.EntityTypeGhast: registry.SoundEntityParrotImitateGhast, registry.EntityTypeHappyGhast: registry.SoundIntentionallyEmpty, registry.EntityTypeGuardian: registry.SoundEntityParrotImitateGuardian,
	registry.EntityTypeHoglin: registry.SoundEntityParrotImitateHoglin, registry.EntityTypeHusk: registry.SoundEntityParrotImitateHusk, registry.EntityTypeIllusioner: registry.SoundEntityParrotImitateIllusioner,
	registry.EntityTypeMagmaCube: registry.SoundEntityParrotImitateMagmaCube, registry.EntityTypeParched: registry.SoundEntityParrotImitateParched, registry.EntityTypePhantom: registry.SoundEntityParrotImitatePhantom,
	registry.EntityTypePiglin: registry.SoundEntityParrotImitatePiglin, registry.EntityTypePiglinBrute: registry.SoundEntityParrotImitatePiglinBrute, registry.EntityTypePillager: registry.SoundEntityParrotImitatePillager,
	registry.EntityTypeRavager: registry.SoundEntityParrotImitateRavager, registry.EntityTypeShulker: registry.SoundEntityParrotImitateShulker, registry.EntityTypeSilverfish: registry.SoundEntityParrotImitateSilverfish,
	registry.EntityTypeSkeleton: registry.SoundEntityParrotImitateSkeleton, registry.EntityTypeSlime: registry.SoundEntityParrotImitateSlime, registry.EntityTypeSpider: registry.SoundEntityParrotImitateSpider,
	registry.EntityTypeStray: registry.SoundEntityParrotImitateStray, registry.EntityTypeVex: registry.SoundEntityParrotImitateVex, registry.EntityTypeVindicator: registry.SoundEntityParrotImitateVindicator,
	registry.EntityTypeWarden: registry.SoundEntityParrotImitateWarden, registry.EntityTypeWitch: registry.SoundEntityParrotImitateWitch, registry.EntityTypeWither: registry.SoundEntityParrotImitateWither,
	registry.EntityTypeWitherSkeleton: registry.SoundEntityParrotImitateWitherSkeleton, registry.EntityTypeZoglin: registry.SoundEntityParrotImitateZoglin, registry.EntityTypeZombie: registry.SoundEntityParrotImitateZombie,
	registry.EntityTypeZombieHorse: registry.SoundEntityParrotImitateZombieHorse, registry.EntityTypeZombieNautilus: registry.SoundEntityParrotImitateZombieNautilus,
	registry.EntityTypeZombieVillager: registry.SoundEntityParrotImitateZombieVillager,
}

// parrotImitationSounds is parrotImitations' sounds in a fixed order, for the
// ambient roll.
var parrotImitationSounds = func() []registry.Sound {
	var out []registry.Sound
	// Ascending type IDs preserve the ambient sound selection order.
	for t, name := range parrotImitations {
		if _, ok := parrotImitation(MobType(t)); ok {
			out = append(out, name)
		}
	}
	return out
}()

// ParrotImitationSound reports the sound copied from kind, including a happy
// ghast's empty sound.
func ParrotImitationSound(kind MobType) (registry.Sound, bool) {
	name, ok := parrotImitation(kind)
	return name, ok
}

func parrotImitation(kind MobType) (registry.Sound, bool) {
	if uint32(kind) >= uint32(len(parrotImitations)) {
		return registry.SoundNone, false
	}
	name := parrotImitations[kind]
	return name, name != registry.SoundNone
}

// ParrotAmbientSound rolls a parrot's idle voice, occasionally an imitation
// outside Peaceful. vanilla: Parrot.getAmbient
func ParrotAmbientSound(rng *rand.Rand, peaceful bool) registry.Sound {
	if !peaceful && rng.Intn(1000) == 0 {
		return parrotImitationSounds[rng.Intn(len(parrotImitationSounds))]
	}
	return registry.SoundEntityParrotAmbient
}

// parrotAmbient is the ambient sound, a monster's imitation one time in a
// thousand outside Peaceful.
// vanilla: Parrot.getAmbient
func (m *Mob) parrotAmbient() registry.Sound {
	return ParrotAmbientSound(m.parrot.rng, m.parrot.quiet)
}

// parrotStep is the part of Parrot.aiStep that makes sound: one tick in 400
// the parrot may copy a monster within 20 blocks.
// vanilla: Parrot.aiStep, imitateNearbyMobs
func (m *Mob) parrotStep(t *tickView) {
	m.parrot.rideCooldown++
	m.parrot.quiet = t.w.Difficulty() == rules.DifficultyPeaceful
	rng := m.parrot.rng
	if rng.Intn(400) != 0 || !m.Alive() || m.Silent || rng.Intn(2) != 0 {
		return
	}
	var near []*Mob
	for _, o := range t.others {
		if _, ok := parrotImitation(o.Type); ok && o != m && boxesWithin(m, o, parrotImitateRange) {
			near = append(near, o)
		}
	}
	if len(near) == 0 {
		return
	}
	o := near[rng.Intn(len(near))]
	if o.Silent {
		return
	}
	if sound := parrotImitations[o.Type]; sound != registry.SoundNone {
		t.events = append(t.events, MobEvent{Mob: m, Kind: MobSounded, SoundID: sound,
			Volume: parrotImitateVol, Pitch: (rng.Float32()-rng.Float32())*0.2 + 1})
	}
}

// boxesWithin reports whether b's box meets a's box grown by r on every side.
// vanilla: AABB.inflate, intersects
func boxesWithin(a, b *Mob, r float64) bool {
	return math.Abs(a.X-b.X) < r+(a.width()+b.width())/2 &&
		b.Y < a.Y+a.height()+r && b.Y+b.height() > a.Y-r &&
		math.Abs(a.Z-b.Z) < r+(a.width()+b.width())/2
}

// parrotMoveStep is the parrot's travel: the flying control's order drives it
// through the air with gravity off; with no order it falls and walks like any
// body, and a fall is slowed to 0.6.
// vanilla: FlyingMoveControl.tick, Parrot.calculateFlapping
func (m *Mob) parrotMoveStep(t *tickView, events []MobEvent) []MobEvent {
	m.flyFollowPath()
	m.SetMetadataNoGravity(false)
	m.flyBabyControl(&m.navOf().nav)
	fromX, fromY, fromZ := m.X, m.Y, m.Z
	events = m.step(t.w, events)
	// vanilla: Parrot.playStepSound
	events = m.stepSoundStep(t.w, m.X-fromX, m.Y-fromY, m.Z-fromZ, events)
	if !m.OnGround && m.VY < 0 {
		m.VY *= parrotFlap
	}
	return events
}

func parrotGoals(m *Mob) []Goal {
	return []Goal{
		newFloatGoal(m),
		// Vanilla registers the panic at 0; it holds only the move control, which the float goal does not.
		&panicGoal{m: m, tame: true, pace: 1.25},
		&lookAtPlayer{m: m, priority: 1},
		&sitGoal{m: m},
		&followOwnerGoal{m: m, priority: 2, start: 5, stop: 1},
		&randomStroll{m: m, priority: 2, pick: m.parrotWanderPos},
		&followMobGoal{m: m},
		&landOnShoulderGoal{m: m},
	}
}

// parrotWanderPos is ParrotWanderGoal.getPosition: a free cell beside a tree
// to perch on, else a point in the air ahead.
// vanilla: Parrot.ParrotWanderGoal.getTreePos, WaterAvoidingRandomFlyingGoal.getPosition
func (m *Mob) parrotWanderPos(t *tickView) (Waypoint, bool) {
	// The 0.001 chance of skipping the trees is WaterAvoidingRandomStrollGoal's probability.
	if m.Rand.Float32() >= 0.001 {
		if c, ok := m.parrotTreeCell(t.w); ok {
			return c, true
		}
	}
	return m.airWanderPos(t, parrotWanderRange)
}

// airWanderPos is a free cell within reach blocks ahead and seven up or down,
// tried ten times.
// vanilla: HoverRandomPos, AirAndWaterRandomPos
func (m *Mob) airWanderPos(t *tickView, reach float64) (Waypoint, bool) {
	sin, cos := math.Sincos(float64(m.Yaw) * math.Pi / 180)
	base := math.Atan2(cos, -sin)
	for range parrotWanderTries {
		angle := base + (m.Rand.Float64()*2-1)*math.Pi/2
		dist := math.Sqrt(m.Rand.Float64()) * reach
		y := m.Y + float64(m.Rand.Intn(2*parrotWanderRise+1)-parrotWanderRise)
		c := cellAt(m.X+math.Cos(angle)*dist, y, m.Z+math.Sin(angle)*dist)
		if flyCellPassable(t.w, c.X, c.Y, c.Z, false, false, true) {
			return c, true
		}
	}
	return Waypoint{}, false
}

var (
	logBlocks    = registry.BlockTagOf("minecraft:logs")
	parrotGround = registry.BlockTagOf("minecraft:parrots_spawnable_on")
)

func (m *Mob) parrotTreeCell(w World) (Waypoint, bool) {
	own := cellAt(m.X, m.Y, m.Z)
	x0, y0, z0 := int32(math.Floor(m.X-parrotTreeXZ)), int32(math.Floor(m.Y-parrotTreeY)), int32(math.Floor(m.Z-parrotTreeXZ))
	x1, y1, z1 := int32(math.Floor(m.X+parrotTreeXZ)), int32(math.Floor(m.Y+parrotTreeY)), int32(math.Floor(m.Z+parrotTreeXZ))
	for z := z0; z <= z1; z++ {
		for y := y0; y <= y1; y++ {
			for x := x0; x <= x1; x++ {
				if (Waypoint{x, y, z}) == own {
					continue
				}
				below := registry.BlockOf(w.BlockAt(x, y-1, z))
				if (below.InTag(leavesBlocks) || below.InTag(logBlocks)) &&
					world.IsAirState(w.BlockAt(x, y, z)) && world.IsAirState(w.BlockAt(x, y+1, z)) {
					return Waypoint{x, y, z}, true
				}
			}
		}
	}
	return Waypoint{}, false
}

// followMobGoal keeps a parrot near the first visible mob of another kind
// within seven blocks, backing off inside three.
// vanilla: FollowMobGoal
type followMobGoal struct {
	m      *Mob
	target *Mob
	recalc int
}

func (g *followMobGoal) Priority() int    { return parrotPriorityFollowMob }
func (g *followMobGoal) Flags() GoalFlags { return GoalMove | GoalLook }

func (g *followMobGoal) CanStart(t *tickView) bool {
	for _, o := range t.others {
		if o.Type != g.m.Type && !o.hasEffect(mobEffectInvisibility) && o.Alive() && boxesWithin(g.m, o, parrotFollowArea) {
			g.target = o
			return true
		}
	}
	return false
}

func (g *followMobGoal) Start(*tickView) { g.recalc, g.m.nav.waterFree = 0, true }

func (g *followMobGoal) ShouldContinue(*tickView) bool {
	o := g.target
	return o != nil && g.m.flightOn() &&
		Dist2(g.m.X, g.m.Y, g.m.Z, o.X, o.Y, o.Z) > parrotFollowStop*parrotFollowStop
}

func (g *followMobGoal) Tick(t *tickView) {
	o := g.target
	if o == nil || g.m.LeashHolderID != 0 {
		return
	}
	dx, dy, dz := o.X-g.m.X, o.Y-g.m.Y, o.Z-g.m.Z
	g.m.HeadYaw = float32(yawToward(dx, dz))
	g.m.Pitch = float32(pitchToward(dx, dy, dz))
	if g.recalc--; g.recalc > 0 {
		return
	}
	g.recalc = reducedTickDelay(10)
	if dx*dx+dy*dy+dz*dz > parrotFollowStop*parrotFollowStop {
		g.m.seekTo(t.w, o.X, o.Y, o.Z, 1.0, navReach)
		return
	}
	g.m.stopSeeking()
	if dx*dx+dy*dy+dz*dz <= parrotFollowStop {
		g.m.seekTo(t.w, g.m.X-dx, g.m.Y, g.m.Z-dz, 1.0, navReach)
	}
}

func (g *followMobGoal) Stop(*tickView) {
	g.target, g.m.nav.waterFree = nil, false
	g.m.stopSeeking()
}
func (g *followMobGoal) RequiresEveryTick() bool { return false }

// landOnShoulderGoal sends a tame, standing parrot onto its owner's shoulder
// when their boxes meet.
// vanilla: LandOnOwnersShoulderGoal
type landOnShoulderGoal struct{ m *Mob }

func (g *landOnShoulderGoal) Priority() int                   { return parrotPriorityShoulder }
func (g *landOnShoulderGoal) Flags() GoalFlags                { return 0 }
func (g *landOnShoulderGoal) RequiresEveryTick() bool         { return false }
func (g *landOnShoulderGoal) Start(*tickView)                 {}
func (g *landOnShoulderGoal) Stop(*tickView)                  {}
func (g *landOnShoulderGoal) ShouldContinue(t *tickView) bool { return g.CanStart(t) }

func (g *landOnShoulderGoal) CanStart(t *tickView) bool {
	owner := g.m.ownerIn(t)
	return owner != nil && !g.m.Tamable.OrderedToSit && !owner.Spectator && !owner.Flying &&
		!owner.InWater && !owner.InPowderSnow && g.m.parrot.rideCooldown > parrotRideCooldown
}

func (g *landOnShoulderGoal) Tick(t *tickView) {
	m := g.m
	owner := m.ownerIn(t)
	if owner == nil || m.Tamable.SittingPose || m.LeashHolderID != 0 {
		return
	}
	// A body is Entity.setEntityOnShoulder's: grounded, dry, not riding, a free side.
	if owner.Passenger || !owner.OnGround || owner.InWater || owner.InPowderSnow || owner.ShoulderSlots <= 0 {
		return
	}
	w, h := owner.Width, owner.Height
	if w == 0 {
		w, h = 0.6, 1.8
	}
	if math.Abs(m.X-owner.X) < (m.width()+w)/2 && m.Y < owner.Y+h && m.Y+m.height() > owner.Y &&
		math.Abs(m.Z-owner.Z) < (m.width()+w)/2 {
		t.events = append(t.events, MobEvent{Mob: m, Kind: MobLandedOnShoulder, Player: *owner})
	}
}
