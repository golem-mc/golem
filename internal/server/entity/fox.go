package entity

import (
	"math"

	"github.com/google/uuid"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/registry"
)

// Fox.Variant ids.
// vanilla: Fox.Variant
const (
	foxRed  int32 = 0
	foxSnow int32 = 1
)

// The fox's numbers.
// vanilla: Fox
const (
	foxScreechChance   = 0.1  // Fox.getAmbientSound
	foxScreechReach    = 16.0 // Fox.getAmbientSound: the inflate around the box
	foxEatDelay        = 600  // Fox.aiStep: ticksSinceEaten past this finishes the meal
	foxSleepDelay      = 140  // SleepGoal.WAIT_TIME_BEFORE_SLEEP before reducedTickDelay
	foxBerryWait       = 40   // FoxEatBerriesGoal.WAIT_TICKS
	foxBerryPace       = 1.2  // FoxEatBerriesGoal's speed modifier, (double)1.2f
	foxItemPace        = 1.2  // FoxSearchForItemsGoal's moveTo speed, (double)1.2f
	foxItemRange       = 8    // FoxSearchForItemsGoal's box inflate
	foxBerrySearch     = 12   // FoxEatBerriesGoal's searchRange
	foxHurtMemory      = 100  // LivingEntity.getLastHurtByMob's 100-tick memory
	foxAlertRange      = 12   // FoxBehaviorGoal.alertableTargeting range
	foxAlertHeight     = 6    // FoxBehaviorGoal.alertable's box inflate on y
	foxLookRange       = 24   // FoxLookAtPlayerGoal's lookDistance
	foxPanicPace       = 2.2  // FoxPanicGoal(this, 2.2)
	foxBerryAcceptSq   = 4.0  // FoxEatBerriesGoal.acceptedDistance(), 2.0 squared
	foxBerryRecalc     = 100  // FoxEatBerriesGoal.shouldRecalculatePath
	foxPriorityShelter = 6
	foxPrioritySleep   = 7 // Fox.registerGoals priorities, in the order they are wired
	foxPriorityBerry   = 10
	foxPriorityVillage = 9
	foxPriorityStroll  = 11
	foxPriorityItems   = 11
	foxPriorityLook    = 12
)

// foxGround marks the foxes_spawnable_on tag.
var foxGround = registry.BlockTagOf("minecraft:foxes_spawnable_on")

// FoxItem is where a pickable item entity lies, and what it holds, as the
// server lists them.
type FoxItem struct {
	X, Y, Z float64
	ItemID  int32
}

// FoxState is what the fox keeps beyond the common mob.
type FoxState struct {
	// Variant is Fox.Variant's id: 0 red, 1 snow.
	Variant int32
	// Sleeping is Fox.FLAG_SLEEPING.
	Sleeping bool
	// Crouching, Interested, Pouncing, Defending, Sitting and Faceplanted are
	// the rest of the flag byte. Crouching needs 25 ticks to be full.
	// vanilla: Fox.DATA_FLAGS_ID
	Crouching, Interested, Pouncing, Defending, Sitting, Faceplanted bool
	// CrouchAmount is Fox.crouchAmount, 0 to 5.
	CrouchAmount float32
	// Trusted are the players the fox does not flee and defends.
	// vanilla: Fox.DATA_TRUSTED_ID_0, DATA_TRUSTED_ID_1
	Trusted []uuid.UUID
	// TicksSinceEaten counts up from the last pickup or meal.
	// vanilla: Fox.ticksSinceEaten
	TicksSinceEaten int
	// Items are the pickable items of the world, handed in each tick by the
	// server for the search goal.
	Items []FoxItem
}

// FoxVariantName is the save name of a variant id.
// vanilla: Fox.Variant.getSerializedName
func FoxVariantName(id int32) string {
	if id == foxSnow {
		return "snow"
	}
	return "red"
}

// FoxVariantByName is Fox.Variant.CODEC read back: an unknown name is red.
func FoxVariantByName(name string) int32 {
	if name == "snow" {
		return foxSnow
	}
	return foxRed
}

// foxKind is Fox's builder and createAttributes: box 0.6 by 0.7, ten of
// health, speed 0.3, bite 2, tempted by nothing.
var foxKind = &kind{
	name:            "Fox",
	pathLength:      32, // vanilla: Fox.<init> setRequiredPathLength(32)
	fireAverse:      true,
	width:           0.6,
	height:          0.7,
	eyeHeight:       0.4,
	maxHealth:       10,
	speed:           float64(float32(0.3)),
	attackDamage:    2,
	followRange:     32,
	panicPace:       foxPanicPace,
	followPace:      1.25, // FoxFollowParentGoal(this, 1.25)
	ambientInterval: 120,  // Animal.getAmbientSoundInterval's own
	babyWidth:       0.36, // Fox.BABY_DIMENSIONS: the box scaled by 0.6
	babyHeight:      0.42,
	babyEye:         0.34375,
	temptTag:        &registry.ItemTagIDFoxFood,
	sounds: MobSounds{
		Ambient: registry.SoundEntityFoxAmbient,
		Hurt:    registry.SoundEntityFoxHurt,
		Death:   registry.SoundEntityFoxDeath,
	},
}

// finalizeFox gives the fox its biome's variant, a pack sharing the first
// member's, and rolls the item it is born holding.
// vanilla: Fox.finalizeSpawn, Fox.populateDefaultEquipmentSlots
func (m *Mob) finalizeFox(dice Dice, biome registry.Biome, group *SpawnGroup) {
	if group != nil && group.foxSet {
		m.SetMetadataFoxVariant(group.foxVariant)
	} else {
		m.SetMetadataFoxVariant(foxRed)
		if biome.InTag(registry.BiomeTagIDSpawnsSnowFoxes) { // vanilla: Fox.Variant.byBiome
			m.SetMetadataFoxVariant(foxSnow)
		}
		if group != nil {
			group.foxSet, group.foxVariant = true, m.Fox.Variant
		}
	}
	if dice.NextFloat() >= 0.2 {
		return
	}
	var item int32
	switch odds := dice.NextFloat(); {
	case odds < 0.05:
		item = int32(registry.ItemEmerald)
	case odds < 0.2:
		item = int32(registry.ItemEgg)
	case odds < 0.4:
		item = int32(registry.ItemRabbitHide)
		if dice.NextBoolean() {
			item = int32(registry.ItemRabbitFoot)
		}
	case odds < 0.6:
		item = int32(registry.ItemWheat)
	case odds < 0.8:
		item = int32(registry.ItemLeather)
	default:
		item = int32(registry.ItemFeather)
	}
	m.SetEquipment(protocol.EquipMainHand, protocol.ItemStack{ItemID: item, Count: 1})
	m.DropChances[0] = guaranteedDropChance
}

// foxCanHold is Fox.canHoldItem: an empty mouth takes anything, a full one
// swaps its non-food for a meal once the fox has not just eaten.
// vanilla: Fox.canHoldItem
func foxCanHold(held, stack protocol.ItemStack, sinceEaten int) bool {
	return held.Empty() || sinceEaten > 0 && foxConsumable(stack) && !foxConsumable(held)
}

// foxConsumable is Fox.isConsumableFood: an item with both a food and a
// consumable component.
func foxConsumable(stack protocol.ItemStack) bool {
	if stack.Empty() {
		return false
	}
	_, food := registry.StackFood(stack)
	_, consumable := registry.StackConsumable(stack)
	return food && consumable
}

// takeFox is Fox.pickUpItem asking its question of one found stack: one
// item goes in the mouth, the rest stays where it lies.
func (s *stagedSlots) takeFox(m *Mob, stack protocol.ItemStack) (slot string, count int32, ok bool) {
	if !foxCanHold(s.main, stack, m.Fox.TicksSinceEaten) {
		return "", 0, false
	}
	taken := stack
	taken.Count = 1
	s.main = taken
	return "mainhand", 1, true
}

// FoxPocketBerries puts one berry of a picked bush in the mouth when it is
// empty, and returns how many of the count remain to be dropped.
// vanilla: Fox.FoxEatBerriesGoal.pickSweetBerries
func (m *Mob) FoxPocketBerries(count int32) int32 {
	if m.Type == TypeFox && m.MainHand.Empty() && count > 0 {
		m.SetEquipment(protocol.EquipMainHand, protocol.ItemStack{ItemID: int32(registry.ItemSweetBerries), Count: 1})
		m.DropChances[0] = guaranteedDropChance
		return count - 1
	}
	return count
}

// foxStep is the part of Fox.aiStep and Fox.tick the goals do not own: the
// meal in the mouth, the waking and the crouch.
// vanilla: Fox.aiStep, Fox.tick
func (m *Mob) foxStep(w World, events []MobEvent) []MobEvent {
	f := &m.Fox
	if m.NoAI {
		return events
	}
	f.TicksSinceEaten++
	if foxConsumable(m.MainHand) && m.TargetID == 0 && m.OnGround && !f.Sleeping {
		if f.TicksSinceEaten > foxEatDelay {
			events = m.foxFinishMeal(events)
			f.TicksSinceEaten = 0
		} else if f.TicksSinceEaten > foxEatChewFrom && m.Rand.Float32() < 0.1 {
			events = append(events,
				MobEvent{Mob: m, Kind: MobSounded, SoundID: registry.SoundEntityFoxEat, Volume: 1, Pitch: 1},
				MobEvent{Mob: m, Kind: MobStatus, Status: protocol.EntityStatusFoxEat})
		}
	}
	if m.TargetID == 0 {
		m.SetMetadataFoxCrouching(false)
		m.SetMetadataFoxInterested(false)
		m.SetMetadataFoxDefending(false)
	}
	if f.Defending && m.Rand.Float32() < 0.05 {
		events = append(events, MobEvent{Mob: m, Kind: MobSounded, SoundID: registry.SoundEntityFoxAggro, Volume: 1, Pitch: 1})
	}
	inWater := m.InWater()
	if inWater || m.TargetID != 0 || w.IsThundering() {
		m.SetMetadataFoxSleeping(false)
	}
	if inWater || f.Sleeping {
		m.SetMetadataFoxSitting(false)
	}
	if f.Faceplanted && m.Rand.Float32() < foxFaceplantDust {
		cell := Waypoint{int32(math.Floor(m.X)), int32(math.Floor(m.Y)), int32(math.Floor(m.Z))}
		events = append(events, MobEvent{Mob: m, Kind: MobFoxFaceplantDust,
			X: float64(cell.X), Y: float64(cell.Y), Z: float64(cell.Z), Status: int32(w.BlockAt(cell.X, cell.Y, cell.Z))})
	}
	if f.Crouching {
		f.CrouchAmount = min(f.CrouchAmount+foxCrouchStep, foxCrouchFull)
	} else {
		f.CrouchAmount = 0
	}
	return events
}

// foxGoals wires the fox's mind.
// vanilla: Fox.registerGoals
func foxGoals(m *Mob) []Goal {
	f := &m.Fox
	avoidMobs := func(typ MobType, ok func(*Mob) bool) Goal {
		return &avoidGoal{m: m, priority: foxPriorityAvoid, active: func() bool { return !f.Defending },
			maxDist: foxAvoidMobs, walk: foxAvoidWalk, sprint: foxAvoidSprint, mobs: []MobType{typ}, mobOK: ok}
	}
	prey := foxPreyGoals(m)
	return []Goal{
		newFloatGoal(m),
		&foxFaceplantGoal{m: m},
		&climbPowderSnowGoal{m: m},
		&panicGoal{m: m, pace: foxPanicPace, refuse: func() bool { return f.Defending }},
		&foxGuard{Goal: &breedGoal{m: m, priority: foxPriorityBreed}, allow: func() bool { return true }, onStart: func() {
			m.clearFoxStates()
			if p := foxPartner(m); p != nil {
				p.clearFoxStates()
			}
		}},
		&foxDefendGoal{m: m},
		&avoidGoal{m: m, priority: foxPriorityAvoid, active: func() bool { return !f.Defending }, maxDist: foxAvoidPlayers,
			walk: foxAvoidWalk, sprint: foxAvoidSprint,
			playerOK: func(p *Player) bool { return !p.Sneaking && !m.foxTrusts(p.UUID) }},
		avoidMobs(TypeWolf, func(o *Mob) bool { return !o.Tamable.Tame }),
		avoidMobs(TypePolarBear, nil),
		prey[0], prey[1],
		&foxStalkGoal{m: m},
		&foxShelterGoal{m: m, interval: reducedTickDelay(foxShelterInterval)},
		&foxPounceGoal{m: m},
		prey[2],
		&foxGuard{Goal: &meleeAttackGoal{m: m, priority: foxPriorityMelee, followUnseen: true, pace: foxMeleePace,
			swingSound: registry.SoundEntityFoxBite},
			allow:   func() bool { return !f.Sitting && !f.Sleeping && !f.Crouching && !f.Faceplanted },
			onStart: func() { m.SetMetadataFoxInterested(false) }},
		newFoxSleepGoal(m),
		&foxGuard{Goal: &followParentGoal{m: m, priority: foxPriorityFollow}, allow: func() bool { return !f.Defending },
			keep: true, onStart: m.clearFoxStates},
		&foxGuard{Goal: &strollThroughVillage{m: m, interval: reducedTickDelay(foxVillageInterval)},
			allow: m.foxCanMove, keep: true, onStart: m.clearFoxStates},
		newFoxBerriesGoal(m),
		&leapAtTargetGoal{m: m, priority: foxPriorityLeap},
		&randomStroll{m: m, priority: foxPriorityStroll},
		&foxItemsGoal{m: m},
		&foxGuard{Goal: &lookAtPlayer{m: m, priority: foxPriorityLook, reach: foxLookRange},
			allow: func() bool { return !f.Faceplanted && !f.Interested }, keep: true},
		&foxPerchGoal{m: m},
	}
}

// foxPartner is the fox the breed goal is courting.
func foxPartner(m *Mob) *Mob {
	for _, g := range m.ai.goals {
		if gg, ok := g.(*foxGuard); ok {
			if b, ok := gg.Goal.(*breedGoal); ok {
				return b.partner
			}
		}
	}
	return nil
}

// foxShelterGoal is Fox.SeekShelterGoal: by day a fox under open sky walks
// to a roofed cell, checking every five seconds (at once in a
// thunderstorm), and not inside a village.
// vanilla: Fox.SeekShelterGoal, FleeSunGoal
type foxShelterGoal struct {
	m                         *Mob
	interval                  int
	wantedX, wantedY, wantedZ float64
}

const (
	foxShelterInterval = 100  // SeekShelterGoal's interval in ticks
	foxShelterPace     = 1.25 // SeekShelterGoal(this, 1.25)
)

func (g *foxShelterGoal) Priority() int           { return foxPriorityShelter }
func (g *foxShelterGoal) Flags() GoalFlags        { return GoalMove }
func (g *foxShelterGoal) RequiresEveryTick() bool { return false }

func (g *foxShelterGoal) CanStart(t *tickView) bool {
	m := g.m
	if m.Fox.Sleeping || m.TargetID != 0 {
		return false
	}
	if t.w.IsThundering() && m.seesSkyAbove(t.w) {
		return g.findHidePos(t.w)
	}
	if g.interval > 0 {
		g.interval--
		return false
	}
	g.interval = foxShelterInterval
	return t.w.BrightOutside() && m.seesSkyAbove(t.w) && !m.inVillage() && g.findHidePos(t.w)
}

// findHidePos is FleeSunGoal.getHidePos: ten throws of the dice around the
// feet, kept when the cell has no sky over it and the fox does not mind it.
func (g *foxShelterGoal) findHidePos(w World) bool {
	feet := g.m.cell()
	for range 10 {
		cand := Waypoint{
			X: feet.X + int32(g.m.Rand.Intn(20)) - 10,
			Y: feet.Y + int32(g.m.Rand.Intn(6)) - 3,
			Z: feet.Z + int32(g.m.Rand.Intn(20)) - 10,
		}
		if canSeeSkyAt(w, cand.X, cand.Y, cand.Z) || g.m.walkTargetValue(w, cand) < 0 {
			continue
		}
		g.wantedX, g.wantedY, g.wantedZ = float64(cand.X)+0.5, float64(cand.Y), float64(cand.Z)+0.5
		return true
	}
	return false
}

func (g *foxShelterGoal) Start(t *tickView) {
	g.m.clearFoxStates()
	g.m.seekTo(t.w, g.wantedX, g.wantedY, g.wantedZ, foxShelterPace, navReach)
}

func (g *foxShelterGoal) ShouldContinue(*tickView) bool { return len(g.m.navOf().nav.path) > 0 }
func (g *foxShelterGoal) Tick(*tickView)                {}
func (g *foxShelterGoal) Stop(*tickView)                {}

// foxSleepGoal is Fox.SleepGoal: by day, under a roof and with nobody about
// to startle it, the fox lies down.
// vanilla: Fox.SleepGoal
type foxSleepGoal struct {
	m         *Mob
	countdown int
}

func newFoxSleepGoal(m *Mob) *foxSleepGoal {
	return &foxSleepGoal{m: m, countdown: m.Rand.Intn(reducedTickDelay(foxSleepDelay))}
}

func (g *foxSleepGoal) Priority() int           { return foxPrioritySleep }
func (g *foxSleepGoal) Flags() GoalFlags        { return GoalMove | GoalLook | GoalJump }
func (g *foxSleepGoal) RequiresEveryTick() bool { return false }

func (g *foxSleepGoal) CanStart(t *tickView) bool {
	if len(g.m.navOf().nav.path) > 0 {
		return false
	}
	return g.canSleep(t) || g.m.Fox.Sleeping
}

func (g *foxSleepGoal) ShouldContinue(t *tickView) bool { return g.canSleep(t) }

func (g *foxSleepGoal) canSleep(t *tickView) bool {
	if g.countdown > 0 {
		g.countdown--
		return false
	}
	return t.w.BrightOutside() && g.hasShelter(t.w) && !g.m.foxAlertable(t) && !g.m.inPowderSnow(t.w)
}

// hasShelter is FoxBehaviorGoal.hasShelter: no sky above the top of the box
// and a cell the fox does not mind standing in.
func (g *foxSleepGoal) hasShelter(w World) bool {
	m := g.m
	cell := Waypoint{int32(math.Floor(m.X)), int32(math.Floor(m.Y + m.height())), int32(math.Floor(m.Z))}
	return !canSeeSkyAt(w, cell.X, cell.Y, cell.Z) && m.walkTargetValue(w, cell) >= 0
}

func (g *foxSleepGoal) Start(*tickView) {
	m := g.m
	m.stopSeeking()
	m.jump = false
	m.SetMetadataFoxSitting(false)
	m.SetMetadataFoxCrouching(false)
	m.SetMetadataFoxInterested(false)
	m.SetMetadataFoxSleeping(true)
}

func (g *foxSleepGoal) Tick(*tickView) {}

func (g *foxSleepGoal) Stop(*tickView) {
	g.countdown = g.m.Rand.Intn(reducedTickDelay(foxSleepDelay))
	g.m.clearFoxStates()
}

// foxBerriesGoal is Fox.FoxEatBerriesGoal: walk to a ripe sweet berry bush or
// a berried cave vine and pick it.
// vanilla: Fox.FoxEatBerriesGoal
type foxBerriesGoal struct {
	*moveToBlock
	waited int
}

func newFoxBerriesGoal(m *Mob) *foxBerriesGoal {
	g := &foxBerriesGoal{}
	g.moveToBlock = newMoveToBlock(m, foxBerryPace, foxBerrySearch, foxBerryValid)
	g.acceptedSq = foxBerryAcceptSq
	g.recalcEvery = foxBerryRecalc
	return g
}

// foxBerryValid is FoxEatBerriesGoal.isValidTarget.
func foxBerryValid(w World, x, y, z int32) bool {
	state := w.BlockAt(x, y, z)
	block := registry.BlockOf(state)
	if block != registry.BlockSweetBerryBush && block != registry.BlockCaveVines && block != registry.BlockCaveVinesPlant {
		return false
	}
	if _, _, ok := registry.BlockStateName(state); !ok {
		return false
	}
	if block == registry.BlockSweetBerryBush {
		age, _ := registry.StateInt(state, registry.PropAge)
		return age >= 2
	}
	return registry.StateBool(state, registry.PropBerries)
}

func (g *foxBerriesGoal) Priority() int { return foxPriorityBerry }

func (g *foxBerriesGoal) CanStart(t *tickView) bool { return !g.m.Fox.Sleeping && g.canUse(t) }

func (g *foxBerriesGoal) Start(t *tickView) {
	g.waited = 0
	g.m.SetMetadataFoxSitting(false)
	g.start(t)
}

func (g *foxBerriesGoal) ShouldContinue(t *tickView) bool { return g.cont(t) }

func (g *foxBerriesGoal) Tick(t *tickView) {
	if g.reached {
		if g.waited >= foxBerryWait {
			if t.w.MobGriefing() {
				t.events = append(t.events, MobEvent{Mob: g.m, Kind: MobPickedBerries,
					X: float64(g.target.X), Y: float64(g.target.Y), Z: float64(g.target.Z)})
			}
		} else {
			g.waited++
		}
	} else if g.m.Rand.Float32() < foxSniffChance {
		t.events = append(t.events, MobEvent{Mob: g.m, Kind: MobSounded, SoundID: registry.SoundEntityFoxSniff, Volume: 1, Pitch: 1})
	}
	g.tick(t)
}

// foxItemsGoal is Fox.FoxSearchForItemsGoal: with an empty mouth and nothing
// to fear, walk to a pickable item nearby.
// vanilla: Fox.FoxSearchForItemsGoal
type foxItemsGoal struct{ m *Mob }

func (g *foxItemsGoal) Priority() int           { return foxPriorityItems }
func (g *foxItemsGoal) Flags() GoalFlags        { return GoalMove }
func (g *foxItemsGoal) RequiresEveryTick() bool { return false }

// nearest is the item the fox walks to: the closest within the box grown by
// foxItemRange.
func (g *foxItemsGoal) nearest() (FoxItem, bool) {
	m := g.m
	best, found := FoxItem{}, false
	bestD := math.Inf(1)
	for _, it := range m.Fox.Items {
		if math.Abs(it.X-m.X) > foxItemRange+m.width()/2 || math.Abs(it.Z-m.Z) > foxItemRange+m.width()/2 ||
			it.Y > m.Y+m.height()+foxItemRange || it.Y < m.Y-foxItemRange {
			continue
		}
		if d := Dist2(it.X, it.Y, it.Z, m.X, m.Y, m.Z); d < bestD {
			best, bestD, found = it, d, true
		}
	}
	return best, found
}

func (g *foxItemsGoal) CanStart(*tickView) bool {
	m := g.m
	if !m.MainHand.Empty() || m.TargetID != 0 || m.foxHurtRecently() || m.Fox.Sleeping || m.Fox.Sitting || m.Fox.Faceplanted ||
		m.Rand.Intn(reducedTickDelay(10)) != 0 {
		return false
	}
	_, ok := g.nearest()
	return ok
}

func (g *foxItemsGoal) walk(t *tickView) {
	if it, ok := g.nearest(); ok && g.m.MainHand.Empty() {
		g.m.seekTo(t.w, it.X, it.Y, it.Z, foxItemPace, navReach)
	}
}

func (g *foxItemsGoal) Start(t *tickView) { g.walk(t) }

func (g *foxItemsGoal) ShouldContinue(t *tickView) bool { return g.CanStart(t) }

func (g *foxItemsGoal) Tick(t *tickView) { g.walk(t) }

func (g *foxItemsGoal) Stop(*tickView) {}

// foxScreech makes the idle sound the night screech at double volume when no
// player is within sixteen blocks of the box.
// vanilla: Fox.getAmbientSound, Fox.playAmbientSound
func (m *Mob) foxScreech(w World, players []Player, moan *MobEvent) {
	if m.Fox.Sleeping || w.BrightOutside() || m.Rand.Float32() >= foxScreechChance {
		return
	}
	for i := range players {
		p := &players[i]
		if p.Spectator {
			continue
		}
		pw, ph, _ := p.box()
		if math.Abs(p.X-m.X) <= foxScreechReach+(m.width()+pw)/2 &&
			math.Abs(p.Z-m.Z) <= foxScreechReach+(m.width()+pw)/2 &&
			p.Y < m.Y+m.height()+foxScreechReach && p.Y+ph > m.Y-foxScreechReach {
			return
		}
	}
	moan.SoundID, moan.Volume, moan.Pitch = registry.SoundEntityFoxScreech, 2, m.VoicePitch()
}
