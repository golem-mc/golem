package entity

import (
	"math"

	"github.com/google/uuid"

	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// TypeCamel is minecraft:camel.
const TypeCamel = registry.EntityTypeCamel

const (
	// vanilla: Camel.DASH_COOLDOWN_TICKS, SITDOWN_DURATION_TICKS,
	// STANDUP_DURATION_TICKS, IDLE_MINIMAL_DURATION_TICKS (20 s)
	camelDashCooldown   = 55
	camelSitTicks       = 40
	camelStandTicks     = 52
	camelMinPoseTicks   = 400
	camelSitHeightDrop  = 1.43
	camelBabyAnchor     = 0.09375
	camelAdultAnchor    = 0.375
	camelJump           = float64(float32(0.42))
	camelSittingEye     = 0.845
	camelBabySittingH   = 0.425
	camelBabySittingEye = 0.41
)

func init() {
	// vanilla: EntityTypes.CAMEL, Camel.createAttributes, Camel.BABY_*_DIMENSIONS
	kinds[TypeCamel] = &kind{
		fireAverse:  true,
		maxHeadYRot: 30,
		name:        "Camel", width: float64(float32(1.7)), height: 2.375, eyeHeight: float64(float32(2.275)),
		babyWidth: float64(float32(0.95)), babyHeight: float64(float32(1.4)), babyEye: float64(float32(1.38)),
		babyable: true, maxHealth: 32, speed: float64(float32(0.09)), stepHeight: 1.5,
		temptTag: &registry.ItemTagIDCamelFood, temptPace: 2.5, followPace: 2.5, panicPace: 4, strollPace: 2,
		volume: 0.8, ambientInterval: 400,
		sounds: MobSounds{Ambient: registry.SoundEntityCamelAmbient, Hurt: registry.SoundEntityCamelHurt, Death: registry.SoundEntityCamelDeath, Eat: registry.SoundEntityCamelEat, Step: registry.SoundEntityCamelStep},
	}
	mobTracking[TypeCamel] = tracking{rangeChunks: 10, updateInterval: 3}
}

// camelState is Camel's own synched and counted fields.
type camelState struct {
	// lastPose is LAST_POSE_CHANGE_TICK: the game tick of the last pose change,
	// negative while sitting.
	lastPose int64
	// pending holds the entity_action of a stand-up that had no tick view (hurt, panic), sent by camelStep.
	pending []MobEvent
	dashing bool
	// dashCooldown counts down from camelDashCooldown after a dash.
	dashCooldown int
	// forward is the controlling rider's forward key, Player.zza > 0.
	forward bool
}

// SetCamelForward records whether the controlling rider holds forward.
func (m *Mob) SetCamelForward(v bool) { m.camel.forward = v }

// CamelSitting is Camel.isCamelSitting.
func (m *Mob) CamelSitting() bool { return m.camel.lastPose < 0 }

// CamelDashing is Camel.DASH.
func (m *Mob) CamelDashing() bool { return m.camel.dashing }

// CamelPoseTick is the synched LAST_POSE_CHANGE_TICK.
func (m *Mob) CamelPoseTick() int64 { return m.camel.lastPose }

func (m *Mob) camelPoseTime() int64 {
	t := m.camel.lastPose
	if t < 0 {
		t = -t
	}
	return m.GameTime - t
}

func (m *Mob) camelInTransition() bool {
	if m.CamelSitting() {
		return m.camelPoseTime() < camelSitTicks
	}
	return m.camelPoseTime() < camelStandTicks
}

// camelRefusesToMove is Camel.refuseToMove.
func (m *Mob) camelRefusesToMove() bool {
	return IsCamel(m.Type) && (m.CamelSitting() || m.camelInTransition())
}

// camelPoseHeight is the box height of the camel's sitting or standing pose.
// vanilla: Camel.ADULT_SITTING_DIMENSIONS, BABY_SITTING_DIMENSIONS
func (m *Mob) camelPoseHeight(sitting bool) float64 {
	k := m.kind()
	switch {
	case sitting && m.IsBaby():
		return camelBabySittingH
	case sitting:
		return k.height - camelSitHeightDrop
	case m.IsBaby():
		return k.babyHeight
	}
	return k.height
}

// camelCanChangePose is Camel.canCamelChangePose: the box of the pose to
// change into overlaps no block.
// vanilla: LivingEntity.wouldNotSuffocateAtTargetPose
func (m *Mob) camelCanChangePose(w World) bool {
	return boxFits(w, m.collideCtx(), m.X, m.Z, m.Y, m.camelPoseHeight(!m.CamelSitting()), m.width()/2)
}

func (m *Mob) camelSound(t *tickView, sound registry.Sound) {
	t.events = append(t.events, MobEvent{Mob: m, Kind: MobSounded, SoundID: sound, Volume: m.SoundVolume(), Pitch: m.VoicePitch()})
}

// camelSitDown is Camel.sitDown.
func (m *Mob) camelSitDown(t *tickView) {
	if m.CamelSitting() {
		return
	}
	m.camelSound(t, m.CamelSound(CamelSit))
	t.events = append(t.events, MobEvent{Mob: m, Kind: MobEntityAction})
	m.SetMetadataCamelLastPose(-m.GameTime)
}

// camelStandUp is Camel.standUp.
func (m *Mob) camelStandUp(t *tickView) {
	if !m.CamelSitting() {
		return
	}
	m.camelSound(t, m.CamelSound(CamelStand))
	t.events = append(t.events, MobEvent{Mob: m, Kind: MobEntityAction})
	m.SetMetadataCamelLastPose(m.GameTime)
}

// CamelStandUpInstantly is Camel.standUpInstantly; its entity_action is sent
// by the next camelStep.
func (m *Mob) CamelStandUpInstantly() {
	m.camelResetFullStand()
	m.camel.pending = append(m.camel.pending, MobEvent{Mob: m, Kind: MobEntityAction})
}

// camelResetFullStand is Camel.resetLastPoseChangeTickToFullStand.
func (m *Mob) camelResetFullStand() {
	m.SetMetadataCamelLastPose(max(0, m.GameTime-camelStandTicks-1))
}

// CamelJump is the server's half of a rider's dash: the client supplies the
// impulse. It reports whether the camel dashed (then the dash sound plays).
// vanilla: Camel.handleStartJump, canJump, onSyncedDataUpdated
func (m *Mob) CamelJump(power int32) bool {
	if power <= 0 || m.Horse.Saddle.Empty() || m.camelRefusesToMove() {
		return false
	}
	m.camelSetDashing(true)
	return true
}

// camelSetDashing sets DASH; a change starts the cooldown if none runs.
func (m *Mob) camelSetDashing(v bool) {
	if m.camel.dashing == v {
		return
	}
	m.SetMetadataCamelDashing(v)
	if m.camel.dashCooldown == 0 {
		m.camel.dashCooldown = camelDashCooldown
	}
}

// camelStep is Camel.tick and the brain's pose rules.
// vanilla: Camel.tick, CamelMoveControl.tick
func (m *Mob) camelStep(t *tickView) {
	t.events = append(t.events, m.camel.pending...)
	m.camel.pending = m.camel.pending[:0]
	if m.camel.dashing && m.camel.dashCooldown < camelDashCooldown-5 && (m.OnGround || m.InWater() || m.Vehicle != nil) {
		m.camelSetDashing(false)
	}
	if m.camel.dashCooldown > 0 {
		m.camel.dashCooldown--
		if m.camel.dashCooldown == 0 {
			t.events = append(t.events, MobEvent{Mob: m, Kind: MobSounded, SoundID: m.CamelSound(CamelDashReady), Volume: 1, Pitch: 1})
		}
	}
	if m.Horse.Rider == uuid.Nil {
		m.camel.forward = false
	}
	if m.camelRefusesToMove() && m.OnGround {
		m.VX, m.VZ = 0, 0
	}
	if m.CamelSitting() && m.InWater() {
		m.CamelStandUpInstantly()
		return
	}
	if !m.CamelSitting() || !m.Alive() || m.NoAI {
		return
	}
	// Forward on a rider's keys stands a settled camel up without the room
	// test, as Camel.tickRidden does.
	if m.camel.forward && !m.camelInTransition() {
		m.camelStandUp(t)
		return
	}
	// A walk target stands a settled camel up.
	if !m.camelInTransition() && m.LeashHolderID == 0 && len(m.navOf().nav.path) > 0 && m.camelCanChangePose(t.w) {
		m.camelStandUp(t)
	}
}

// camelAnchor is the passenger point's height above the feet of the front or
// back seat, which follows the sit and stand animation.
// vanilla: Camel.getBodyAnchorAnimationYOffset
func (m *Mob) camelAnchor(front bool) float64 {
	sitOffset := camelAdultAnchor
	if m.IsBaby() {
		sitOffset = camelBabyAnchor
	}
	base := m.height() - sitOffset
	diff := float64(float32(camelSitHeightDrop))
	drop := diff - 0.2
	bottom := diff - drop
	sitting, transition := m.CamelSitting(), m.camelInTransition()
	if transition {
		var dur, half int
		var flex float64
		switch {
		case sitting:
			dur, half = camelSitTicks, 28
			flex = 0.1
			if front {
				flex = 0.5
			}
		default:
			dur, half = camelStandTicks, 32
			flex = 0.35
			if front {
				half, flex = 24, 0.6
			}
		}
		pose := math.Max(0, math.Min(float64(m.camelPoseTime()), float64(dur)))
		first := pose < float64(half)
		part := (pose - float64(half)) / float64(dur-half)
		if first {
			part = pose / float64(half)
		}
		flexPoint := diff - flex*drop
		lerp := func(a, b float64) float64 { return a + part*(b-a) }
		switch {
		case sitting && first:
			base += lerp(diff, flexPoint)
		case sitting:
			base += lerp(flexPoint, bottom)
		case first:
			base += lerp(bottom-diff, bottom-flexPoint)
		default:
			base += lerp(bottom-flexPoint, 0)
		}
	}
	if sitting && !transition {
		base += bottom
	}
	return base
}

// camelSeat is the passenger attachment of the seat at index in the camel's
// own frame.
// vanilla: Camel.getPassengerAttachmentPoint
func (m *Mob) camelSeat(index int) (x, y, z float64) {
	offset := 0.5
	if len(m.PassengerIDs()) > 1 && index > 0 {
		offset = -0.7
	}
	return 0, m.camelAnchor(index <= 0), offset
}

// camelCanBeRidden is Camel.mobInteract's room test: a grown camel with a
// free seat among two.
func (m *Mob) camelCanBeRidden() bool {
	return m.Alive() && !m.IsBaby() && len(m.Passengers) == 0 && len(m.PassengerIDs()) < 2
}

// camelStepVoice is the sound of Camel.playStepSound and CamelHusk.playStepSound.
func (m *Mob) camelStepVoice(state uint16) registry.Sound {
	sand := registry.BlockOf(state).InTag(registry.TagIDCamelSandStepSoundBlocks)
	if m.Type == TypeCamelHusk {
		if sand {
			return registry.SoundEntityCamelHuskStepSand
		}
		return registry.SoundEntityCamelHuskStep
	}
	if sand {
		return registry.SoundEntityCamelStepSand
	}
	return registry.SoundEntityCamelStep
}

func camelGoals(m *Mob) []Goal {
	// vanilla: CamelAi.initCoreActivity, initIdleActivity
	idle := func() bool { return !m.camelRefusesToMove() }
	look := &lookAtPlayer{m: m, reach: 6}
	wander := &runOneGoal{m: m, priority: priorityStroll, flags: GoalMove, weights: []int{1, 1, 1, 1}, choices: []Goal{
		&gatedGoal{Goal: &randomStroll{m: m, interval: 1, pace: 2, ignoreIdle: true, emptyPick: true,
			pick: func(t *tickView) (Waypoint, bool) { return landRandomPos(t.w, m, strollSpreadXZ, strollSpreadY) }}, enabled: idle},
		&gatedGoal{Goal: &lookWalkChoice{m: m, pace: 2, look: look.player}, enabled: idle},
		&camelSitGoal{m: m},
		&doNothingGoal{m: m, min: 30, max: 60},
	}}
	return []Goal{newFloatGoal(m),
		&panicGoal{m: m, pace: 4, brain: true, refuse: func() bool { return m.Horse.Rider != uuid.Nil }, onStart: m.CamelStandUpInstantly}, // vanilla: CamelAi.CamelPanic
		&breedGoal{m: m}, &temptGoal{m: m, stopAt: 3.5},
		&gatedGoal{Goal: &followParentGoal{m: m, ranged: &followRange{min: 5, max: 16, scan: 16}}, enabled: idle},
		wander, look, &randomLookAround{m: m}}
}

// camelSitGoal is CamelAi.RandomSitting, a one-shot RunOne choice that
// toggles the pose once it has held for twenty seconds.
type camelSitGoal struct{ m *Mob }

func (g *camelSitGoal) Priority() int    { return priorityStroll }
func (g *camelSitGoal) Flags() GoalFlags { return 0 }

func (g *camelSitGoal) CanStart(t *tickView) bool {
	m := g.m
	return !m.InWater() && m.camelPoseTime() >= camelMinPoseTicks && m.LeashHolderID == 0 && m.OnGround &&
		m.Horse.Rider == uuid.Nil && m.camelCanChangePose(t.w)
}

func (g *camelSitGoal) Start(t *tickView) {
	if g.m.CamelSitting() {
		g.m.camelStandUp(t)
	} else if !g.m.panicking() {
		g.m.camelSitDown(t)
	}
}

func (g *camelSitGoal) ShouldContinue(*tickView) bool { return false }
func (g *camelSitGoal) Tick(*tickView)                {}
func (g *camelSitGoal) Stop(*tickView)                {}
func (g *camelSitGoal) RequiresEveryTick() bool       { return false }

// camelFields reads and writes LastPoseTick.
// vanilla: Camel.addAdditionalSaveData, readAdditionalSaveData
func (m *Mob) camelFields(c *mobCodec) {
	if !c.load {
		c.set("LastPoseTick", nbt.Long(m.camel.lastPose))
		return
	}
	var tick int64
	if v, ok := c.tag["LastPoseTick"].(nbt.Long); ok {
		tick = int64(v)
	}
	m.SetMetadataCamelLastPose(tick)
}
