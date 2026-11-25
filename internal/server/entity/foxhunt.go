package entity

import (
	"math"
	"slices"

	"github.com/google/uuid"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/registry"
)

// The fox's hunting numbers.
// vanilla: Fox
const (
	foxStalkNearSq    = 36.0 // StalkPreyGoal: 6 blocks squared
	foxStalkPace      = 1.5  // StalkPreyGoal's moveTo speed
	foxMeleePace      = 1.2  // FoxMeleeAttackGoal(this, (double)1.2f, true)
	foxCrouchStep     = 0.2  // Fox.tick's crouchAmount increment
	foxCrouchFull     = 5.0  // Fox.MAX_CROUCH_AMOUNT
	foxPounceReach    = 2.0  // FoxPounceGoal.tick's distanceTo
	foxPounceForwd    = 0.8  // FoxPounceGoal.start's horizontal push
	foxPounceUp       = 0.9  // FoxPounceGoal.start's upward push
	foxPounceRest     = 0.05 // FoxPounceGoal.canContinueToUse's vertical speed squared
	foxPounceLevel    = 15.0 // FoxPounceGoal.canContinueToUse: pitch below which the fox counts as level
	foxPounceFlat     = 0.03 // FoxPounceGoal.tick: vertical speed squared under which the head levels out
	foxPounceJumpBias = 6.5  // FoxPounceGoal.tick: weight of a rising jump in the pitch
	foxFaceplantPitch = 60   // FoxPounceGoal.tick: head-down pitch of the faceplant
	foxFaceplantDust  = 0.2  // Fox.tick: chance per tick of the snow bursting around a faceplanted fox
	foxSniffChance    = 0.05 // FoxEatBerriesGoal.tick
	foxFaceplantTicks = 40   // FaceplantGoal.start
	foxAvoidPlayers   = 16.0 // AvoidEntityGoal<Player> distance
	foxAvoidMobs      = 8.0  // AvoidEntityGoal<Wolf> and <PolarBear> distance
	foxAvoidWalk      = 1.6  // the avoid goals' walk speed
	foxAvoidSprint    = 1.4  // the avoid goals' sprint speed
	foxLandScan       = 10   // the land and turtle target goals' randomInterval
	foxFishScan       = 20   // the fish target goal's randomInterval
	foxDefendScan     = 10   // DefendTrustedTargetGoal's randomInterval
	foxPerchChance    = 0.02 // PerchAndSearchGoal.canUse
	foxEatChewFrom    = 560  // Fox.aiStep: chewing sounds start past this
)

// The goal priorities of the fox's hunting side.
// vanilla: Fox.registerGoals
const (
	foxPriorityFaceplant = 1
	foxPriorityBreed     = 3
	foxPriorityAvoid     = 4
	foxPriorityStalk     = 5
	foxPriorityPounce    = 6
	foxPriorityMelee     = 7
	foxPriorityFollow    = 8
	foxPriorityLeap      = 10
	foxPriorityPerch     = 13
	foxPriorityDefend    = 3
)

// FoxFlags is the fox's flag byte as the client reads it.
// vanilla: Fox.DATA_FLAGS_ID
func (m *Mob) FoxFlags() uint8 {
	var bits uint8
	for _, f := range []struct {
		on  bool
		bit uint8
	}{
		{m.Fox.Sitting, protocol.FoxSitting},
		{m.Fox.Crouching, protocol.FoxCrouching},
		{m.Fox.Interested, protocol.FoxInterest},
		{m.Fox.Pouncing, protocol.FoxPouncing},
		{m.Fox.Sleeping, protocol.FoxSleeping},
		{m.Fox.Faceplanted, protocol.FoxFaceplant},
		{m.Fox.Defending, protocol.FoxDefending},
	} {
		if f.on {
			bits |= f.bit
		}
	}
	return bits
}

// clearFoxStates is Fox.clearStates.
func (m *Mob) clearFoxStates() {
	m.SetMetadataFoxInterested(false)
	m.SetMetadataFoxCrouching(false)
	m.SetMetadataFoxSitting(false)
	m.SetMetadataFoxSleeping(false)
	m.SetMetadataFoxDefending(false)
	m.SetMetadataFoxFaceplanted(false)
}

// FoxTrust adds a player the fox trusts. Two are kept; a third replaces the
// second.
// vanilla: Fox.addTrustedEntity
func (m *Mob) FoxTrust(id uuid.UUID) {
	if id == uuid.Nil {
		return
	}
	if len(m.Fox.Trusted) < 2 {
		m.Fox.Trusted = append(m.Fox.Trusted, id)
		return
	}
	m.Fox.Trusted[1] = id
}

// foxTrusts is Fox.trusts.
func (m *Mob) foxTrusts(id uuid.UUID) bool {
	for _, t := range m.Fox.Trusted {
		if t == id && id != uuid.Nil {
			return true
		}
	}
	return false
}

// foxFullyCrouched is Fox.isFullyCrouched.
func (m *Mob) foxFullyCrouched() bool { return m.Fox.CrouchAmount == foxCrouchFull }

// vanilla: Fox.STALKABLE_PREY
var foxStalkableKinds = []MobType{TypeChicken, TypeRabbit}

// foxPathClear is Fox.isPathClear: three cells above the line to the target
// are replaceable at six points along it.
// vanilla: Fox.isPathClear
func foxPathClear(w World, m *Mob, tx, tz float64) bool {
	zdiff, xdiff := tz-m.Z, tx-m.X
	slope := zdiff / xdiff
	for i := range 6 {
		frac := float64(float32(i) / 6.0)
		var x, z float64
		if slope == 0 {
			x = xdiff * frac
		} else {
			z = zdiff * frac
			x = z / slope
		}
		if math.IsNaN(x) {
			x = 0
		}
		for j := 1; j < 4; j++ {
			state := w.BlockAt(int32(math.Floor(m.X+x)), int32(math.Floor(m.Y+float64(j))), int32(math.Floor(m.Z+z)))
			if !rules.IsReplaceableState(state) {
				return false
			}
		}
	}
	return true
}

// foxPrey is the mob the fox hunts, if it still lives.
func (m *Mob) foxPrey(t *tickView) *Mob {
	if m.TargetID == 0 {
		return nil
	}
	if o := t.mobByID(m.TargetID); o != nil && o.Alive() {
		return o
	}
	return nil
}

// lookAtMob eases the head onto the mob's eyes.
func (m *Mob) lookAtMob(o *Mob) {
	dx, dz := o.X-m.X, o.Z-m.Z
	m.HeadYaw = turnToward(m.HeadYaw, float32(yawToward(dx, dz)), headTurnSpeed)
	m.Pitch = turnToward(m.Pitch, float32(pitchToward(dx, (o.Y+o.eyeHeight())-(m.Y+m.eyeHeight()), dz)), headTurnSpeed)
}

// foxGuard adds a fox's own conditions and start-up to a shared goal.
type foxGuard struct {
	Goal
	allow func() bool
	// keep applies allow to the running goal too.
	keep    bool
	onStart func()
}

func (g *foxGuard) CanStart(t *tickView) bool { return g.allow() && g.Goal.CanStart(t) }
func (g *foxGuard) ShouldContinue(t *tickView) bool {
	return (!g.keep || g.allow()) && g.Goal.ShouldContinue(t)
}
func (g *foxGuard) Start(t *tickView) {
	if g.onStart != nil {
		g.onStart()
	}
	g.Goal.Start(t)
}

// foxPreyGoals are the three NearestAttackableTargetGoals of Fox.registerGoals;
// the variant decides which ranks first.
// vanilla: Fox.setTargetGoals
func foxPreyGoals(m *Mob) []Goal {
	return []Goal{
		&foxPreyGoal{mobTargetGoal{m: m, unsighted: true, interval: foxLandScan, kinds: foxStalkableKinds}, 4, 6},
		&foxPreyGoal{mobTargetGoal{m: m, unsighted: true, interval: foxLandScan,
			kinds: babyTurtleKinds, matches: isBabyTurtleAshore}, 4, 6},
		&foxPreyGoal{mobTargetGoal{m: m, unsighted: true, interval: foxFishScan, kinds: schoolingFishKinds}, 6, 4},
	}
}

// foxPreyGoal ranks a prey goal by the fox's variant: red foxes put the land
// prey first, snow foxes the fish.
type foxPreyGoal struct {
	mobTargetGoal
	red, snow int
}

func (g *foxPreyGoal) Priority() int {
	if g.m.Fox.Variant == foxSnow {
		return g.snow
	}
	return g.red
}

// foxAlertable is FoxBehaviorGoal.alertable: something within the alert
// range that the fox would rather not sleep or sit beside.
// vanilla: Fox.FoxAlertableEntitiesSelector
func (m *Mob) foxAlertable(t *tickView) bool {
	near := func(x, y, z float64) bool {
		return math.Abs(x-m.X) <= foxAlertRange && math.Abs(z-m.Z) <= foxAlertRange &&
			math.Abs(y-m.Y) <= foxAlertHeight && Dist2(x, y, z, m.X, m.Y, m.Z) <= foxAlertRange*foxAlertRange
	}
	for i := range t.players {
		p := &t.players[i]
		if p.Huntable && !p.Sneaking && !p.Sleeping && !m.foxTrusts(p.UUID) && near(p.X, p.Y, p.Z) {
			return true
		}
	}
	for _, o := range t.others {
		if o == m || !o.Alive() || o.Type == TypeFox || o.Tamable.Tame {
			continue
		}
		if near(o.X, o.Y, o.Z) {
			return true
		}
	}
	return false
}

// foxStalkGoal is Fox.StalkPreyGoal: far from a chicken or rabbit target the
// fox runs at it, and within six blocks it crouches.
// vanilla: Fox.StalkPreyGoal
type foxStalkGoal struct{ m *Mob }

func (g *foxStalkGoal) Priority() int           { return foxPriorityStalk }
func (g *foxStalkGoal) Flags() GoalFlags        { return GoalMove | GoalLook }
func (g *foxStalkGoal) RequiresEveryTick() bool { return false }

func (g *foxStalkGoal) CanStart(t *tickView) bool {
	m := g.m
	if m.Fox.Sleeping {
		return false
	}
	o := m.foxPrey(t)
	return o != nil && slices.Contains(foxStalkableKinds, o.Type) && Dist2(m.X, m.Y, m.Z, o.X, o.Y, o.Z) > foxStalkNearSq &&
		!m.Fox.Crouching && !m.Fox.Interested && !m.jump
}

func (g *foxStalkGoal) ShouldContinue(t *tickView) bool { return g.CanStart(t) }

func (g *foxStalkGoal) Start(*tickView) {
	g.m.SetMetadataFoxSitting(false)
	g.m.SetMetadataFoxFaceplanted(false)
}

func (g *foxStalkGoal) Stop(t *tickView) {
	m := g.m
	if o := m.foxPrey(t); o != nil && foxPathClear(t.w, m, o.X, o.Z) {
		m.SetMetadataFoxInterested(true)
		m.SetMetadataFoxCrouching(true)
		m.stopSeeking()
		m.lookAtMob(o)
		return
	}
	m.SetMetadataFoxInterested(false)
	m.SetMetadataFoxCrouching(false)
}

func (g *foxStalkGoal) Tick(t *tickView) {
	m := g.m
	o := m.foxPrey(t)
	if o == nil {
		return
	}
	m.lookAtMob(o)
	if Dist2(m.X, m.Y, m.Z, o.X, o.Y, o.Z) <= foxStalkNearSq {
		m.SetMetadataFoxInterested(true)
		m.SetMetadataFoxCrouching(true)
		m.stopSeeking()
		return
	}
	m.seekTo(t.w, o.X, o.Y, o.Z, foxStalkPace, navReach)
}

// foxPounceGoal is Fox.FoxPounceGoal: a fully crouched fox with a clear line
// leaps at its target, biting within two blocks and tilting its head with
// the flight; landing nose-first in snow faceplants it. The leap cannot be
// pre-empted. canUse's discarded path for a blocked target is not
// simulated.
// vanilla: Fox.FoxPounceGoal
type foxPounceGoal struct {
	m *Mob
	// jumping is Mob.jumping on the tick the goal started: the jump control
	// clears it after that tick's goals have run.
	jumping bool
}

func (g *foxPounceGoal) Priority() int           { return foxPriorityPounce }
func (g *foxPounceGoal) Flags() GoalFlags        { return GoalMove | GoalJump }
func (g *foxPounceGoal) RequiresEveryTick() bool { return false }

// Uninterruptible is FoxPounceGoal.isInterruptable.
func (g *foxPounceGoal) Uninterruptible() bool { return true }

func (g *foxPounceGoal) CanStart(t *tickView) bool {
	m := g.m
	if !m.foxFullyCrouched() {
		return false
	}
	o := m.foxPrey(t)
	if o == nil {
		return false
	}
	clear := foxPathClear(t.w, m, o.X, o.Z)
	if !clear {
		m.SetMetadataFoxCrouching(false)
		m.SetMetadataFoxInterested(false)
	}
	return clear
}

func (g *foxPounceGoal) ShouldContinue(t *tickView) bool {
	m := g.m
	if m.foxPrey(t) == nil {
		return false
	}
	landed := m.VY*m.VY < foxPounceRest && math.Abs(float64(m.Pitch)) < foxPounceLevel && m.OnGround
	return !landed && !m.Fox.Faceplanted
}

func (g *foxPounceGoal) Start(t *tickView) {
	m := g.m
	g.jumping = true
	m.SetMetadataFoxPouncing(true)
	m.SetMetadataFoxInterested(false)
	if o := m.foxPrey(t); o != nil {
		dx, dy, dz := o.X-m.X, o.Y-m.Y, o.Z-m.Z
		if l := math.Sqrt(dx*dx + dy*dy + dz*dz); l > 0 {
			m.VX += dx / l * foxPounceForwd
			m.VZ += dz / l * foxPounceForwd
		}
		m.VY += foxPounceUp
		m.OnGround = false
		m.lookAtMob(o)
	}
	m.stopSeeking()
}

func (g *foxPounceGoal) Tick(t *tickView) {
	m := g.m
	o := m.foxPrey(t)
	jumping := g.jumping
	g.jumping = false
	if !m.Fox.Faceplanted {
		if m.VY*m.VY < foxPounceFlat && m.Pitch != 0 {
			m.Pitch += 0.2 * float32(wrapDegrees(float64(-m.Pitch)))
		} else {
			bias := 1.0
			if jumping && m.VY > 0 {
				bias = foxPounceJumpBias
			}
			biasedY := m.VY * bias
			flat := math.Hypot(m.VX, m.VZ)
			if l := math.Hypot(flat, biasedY); l > 1.0e-5 {
				m.Pitch = float32(math.Copysign(1, -biasedY) * math.Acos(flat/l) * 57.2957763671875)
			}
		}
	}
	switch {
	case o != nil && Dist2(m.X, m.Y, m.Z, o.X, o.Y, o.Z) <= foxPounceReach*foxPounceReach:
		t.events = append(t.events, MobEvent{Mob: m, Kind: MobAttackedPlayer, Damage: m.attackDamage(), Player: o.asTarget()})
	case m.Pitch > 0 && m.OnGround && float32(m.VY) != 0 && foxInSnow(t.w, m):
		m.Pitch = foxFaceplantPitch
		m.SetMetadataTargetID(0)
		m.SetMetadataFoxFaceplanted(true)
	}
	// The look control turns the head after the goals have run, toward the
	// target the tick began with even when a faceplant just dropped it.
	if o != nil {
		m.lookAtMob(o)
	}
}

// foxInSnow says whether the cell the fox stands in holds a snow layer.
func foxInSnow(w World, m *Mob) bool {
	return registry.BlockOf(w.BlockAt(int32(math.Floor(m.X)), int32(math.Floor(m.Y)), int32(math.Floor(m.Z)))) == registry.BlockSnow
}

func (g *foxPounceGoal) Stop(*tickView) {
	f := &g.m.Fox
	g.m.SetMetadataFoxCrouching(false)
	f.CrouchAmount = 0
	g.m.SetMetadataFoxInterested(false)
	g.m.SetMetadataFoxPouncing(false)
}

// foxFaceplantGoal is Fox.FaceplantGoal: a fox that dived into snow stays put
// for two seconds.
// vanilla: Fox.FaceplantGoal
type foxFaceplantGoal struct {
	m         *Mob
	countdown int
}

func (g *foxFaceplantGoal) Priority() int           { return foxPriorityFaceplant }
func (g *foxFaceplantGoal) Flags() GoalFlags        { return GoalMove | GoalLook | GoalJump }
func (g *foxFaceplantGoal) RequiresEveryTick() bool { return false }
func (g *foxFaceplantGoal) CanStart(*tickView) bool { return g.m.Fox.Faceplanted }
func (g *foxFaceplantGoal) ShouldContinue(*tickView) bool {
	return g.m.Fox.Faceplanted && g.countdown > 0
}
func (g *foxFaceplantGoal) Start(*tickView) { g.countdown = reducedTickDelay(foxFaceplantTicks) }
func (g *foxFaceplantGoal) Tick(*tickView)  { g.countdown-- }
func (g *foxFaceplantGoal) Stop(*tickView)  { g.m.SetMetadataFoxFaceplanted(false) }

// foxDefendGoal is Fox.DefendTrustedTargetGoal: whoever struck a trusted
// player last becomes the target, once per blow.
// vanilla: Fox.DefendTrustedTargetGoal
type foxDefendGoal struct {
	m                 *Mob
	attacker          int32
	pending, answered int
}

func (g *foxDefendGoal) Priority() int           { return foxPriorityDefend }
func (g *foxDefendGoal) Flags() GoalFlags        { return GoalTarget }
func (g *foxDefendGoal) RequiresEveryTick() bool { return false }

func (g *foxDefendGoal) CanStart(t *tickView) bool {
	m := g.m
	if m.Rand.Intn(reducedTickDelay(foxDefendScan)) != 0 {
		return false
	}
	for _, id := range m.Fox.Trusted {
		for i := range t.players {
			p := &t.players[i]
			if p.UUID != id {
				continue
			}
			g.attacker, g.pending = p.HurtBy, p.HurtByStamp
			return g.pending != g.answered && g.canAttack(t, g.attacker)
		}
	}
	return false
}

// canAttack is the targeting conditions: alive, not trusted, in the follow
// range and seen.
func (g *foxDefendGoal) canAttack(t *tickView, id int32) bool {
	m := g.m
	target := m.targetByID(t, id)
	if target == nil || !target.Huntable || t.playerByID(id) != nil && !t.canAttackPlayer(target) || target.Spectator || id == m.ID || m.foxTrusts(target.UUID) {
		return false
	}
	follow := m.followRange()
	return Dist2(m.X, m.Y, m.Z, target.X, target.Y, target.Z) <= follow*follow &&
		m.seesTarget(t.w, target)
}

func (g *foxDefendGoal) Start(t *tickView) {
	m := g.m
	m.setTargetBy(g.attacker, TargetByDefend)
	g.answered = g.pending
	m.SetMetadataFoxDefending(true)
	m.SetMetadataFoxSleeping(false)
	t.events = append(t.events, MobEvent{Mob: m, Kind: MobSounded, SoundID: registry.SoundEntityFoxAggro, Volume: 1, Pitch: 1})
}

func (g *foxDefendGoal) ShouldContinue(t *tickView) bool {
	p := g.m.targetIn(t)
	if p == nil {
		return false
	}
	follow := g.m.followRange()
	return Dist2(g.m.X, g.m.Y, g.m.Z, p.X, p.Y, p.Z) <= follow*follow
}

func (g *foxDefendGoal) Tick(*tickView) {}
func (g *foxDefendGoal) Stop(*tickView) { g.m.SetMetadataTargetID(0) }

// foxHurtRecently is getLastHurtByMob() != null within foxHurtMemory ticks.
// The stamp is any hurt's, so a later fall keeps the memory alive.
// vanilla: LivingEntity.getLastHurtByMob
func (m *Mob) foxHurtRecently() bool {
	return m.retaliate != 0 && m.Age-m.lastDamageStamp <= foxHurtMemory
}

// foxPerchGoal is Fox.PerchAndSearchGoal: now and then an idle fox sits and
// looks about.
// vanilla: Fox.PerchAndSearchGoal
type foxPerchGoal struct {
	m               *Mob
	relX, relZ      float64
	lookTime, looks int
}

func (g *foxPerchGoal) Priority() int           { return foxPriorityPerch }
func (g *foxPerchGoal) Flags() GoalFlags        { return GoalMove | GoalLook }
func (g *foxPerchGoal) RequiresEveryTick() bool { return false }

func (g *foxPerchGoal) CanStart(t *tickView) bool {
	m := g.m
	return !m.foxHurtRecently() && m.Rand.Float32() < foxPerchChance && !m.Fox.Sleeping && m.TargetID == 0 &&
		len(m.navOf().nav.path) == 0 && !m.foxAlertable(t) && !m.Fox.Pouncing && !m.Fox.Crouching
}

func (g *foxPerchGoal) ShouldContinue(*tickView) bool { return g.looks > 0 }

func (g *foxPerchGoal) resetLook() {
	angle := math.Pi * 2 * g.m.Rand.Float64()
	g.relX, g.relZ = math.Cos(angle), math.Sin(angle)
	g.lookTime = reducedTickDelay(80 + g.m.Rand.Intn(20))
}

func (g *foxPerchGoal) Start(*tickView) {
	g.resetLook()
	g.looks = 2 + g.m.Rand.Intn(3)
	g.m.SetMetadataFoxSitting(true)
	g.m.stopSeeking()
}

func (g *foxPerchGoal) Tick(*tickView) {
	g.lookTime--
	if g.lookTime <= 0 {
		g.looks--
		g.resetLook()
	}
	g.m.HeadYaw = turnToward(g.m.HeadYaw, float32(yawToward(g.relX, g.relZ)), headTurnSpeed)
}

func (g *foxPerchGoal) Stop(*tickView) { g.m.SetMetadataFoxSitting(false) }

// foxFinishMeal is the swallow of Fox.aiStep: the consume sounds, the
// on-consume effects, one item gone and the use remainder in its place.
// vanilla: Consumable.onConsume, ItemStack.finishUsingItem
func (m *Mob) foxFinishMeal(events []MobEvent) []MobEvent {
	cons, _ := registry.StackConsumable(m.MainHand)
	volume, pitch := rules.ConsumableSound(m.Rand, cons.Drink)
	events = append(events, m.consumeSoundEvent(cons.Sound, volume, pitch))
	// foxConsumable requires food, whose listener also plays the consume sound.
	// vanilla: FoodProperties.onConsume
	foodSound := m.consumeSoundEvent(cons.Sound, 1, 1+0.4*(m.Rand.Float32()-m.Rand.Float32()))
	foodSound.Unmuted = true
	events = append(events, foodSound)
	meal := rules.ConsumeOn(m, cons.Effects, m.Rand)
	for _, change := range meal.Changes {
		events = append(events, MobEvent{Mob: m, Kind: MobEffectChanged, EffectChange: change})
	}
	for _, sound := range meal.Sounds {
		// vanilla: PlaySoundConsumeEffect.apply calls Level.playSound directly.
		effectSound := m.consumeSoundEvent(sound, 1, 1)
		effectSound.Unmuted = true
		// vanilla: PlaySoundConsumeEffect.apply passes user.blockPosition().
		effectSound.Centred = true
		events = append(events, effectSound)
	}
	// An extra remainder is discarded when food remains in the mouth:
	// vanilla: LivingEntity.handleExtraItemsCreatedOnUse is empty.
	before := m.MainHand.Count
	stack := m.MainHand
	stack.Count--
	m.SetEquipment(protocol.EquipMainHand, stack)
	if m.MainHand.Count <= 0 {
		m.SetEquipment(protocol.EquipMainHand, protocol.ItemStack{})
		if before == 1 {
			m.SetEquipment(protocol.EquipMainHand, cons.Remainder)
		}
	}
	return events
}

// consumeSoundEvent retains direct component holders across the tick phases.
func (m *Mob) consumeSoundEvent(sound protocol.SoundHolder, volume, pitch float32) MobEvent {
	event := MobEvent{Mob: m, Kind: MobSounded, Volume: volume, Pitch: pitch}
	if sound.Name == "" && sound.ID >= 0 {
		event.SoundID = registry.Sound(sound.ID + 1)
	} else {
		inline := sound
		event.SoundHolder = &inline
	}
	return event
}
