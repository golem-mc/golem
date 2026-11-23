package entity

import (
	"math"

	"github.com/google/uuid"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/registry"
)

const (
	// HorseMaxTemper is the temper at which riding tames a horse for certain.
	// vanilla: AbstractHorse.getMaxTemper
	HorseMaxTemper = 100
	// horseStandTicks is how long a horse rears after a jump or an outburst.
	// vanilla: AbstractHorse.standIfPossible
	horseStandTicks = 20
	// horseTemperStep is what each failed round of riding adds.
	// vanilla: RunAroundLikeCrazyGoal.tick
	horseTemperStep = 5
	horseColours    = 7
	// horseBaseJump is the jump_strength a horse starts with before a
	// finalize rolls it.
	// vanilla: AbstractHorse.createBaseHorseAttributes
	horseBaseJump = 0.7
	horseMarkings = 5
	// llamaMaxTemper, llamaVariants and llamaMaxStrength are a llama's
	// temper cap, its four colours and its strongest chest.
	// vanilla: Llama.getMaxTemper, Llama.Variant, Llama.MAX_STRENGTH
	llamaMaxTemper   = 30
	llamaVariants    = 4
	llamaMaxStrength = 5
	// chestedBaseJump is the donkey's and mule's jump_strength.
	// vanilla: AbstractChestedHorse.createBaseChestedHorseAttributes
	chestedBaseJump = 0.5

	// horseRunPace and horseStrollPace are the shares of the horse's walk
	// RunAroundLikeCrazyGoal(1.2) and WaterAvoidingRandomStrollGoal(0.7)
	// run at.
	horseRunPace    = 1.2
	horseStrollPace = 0.7

	// skeletonTrapRange is how near a player springs a trap horse, and
	// skeletonTrapLife the ticks an unsprung, unpersistent one waits.
	// vanilla: SkeletonTrapGoal.canUse, SkeletonHorse.TRAP_MAX_LIFE
	skeletonTrapRange = 10
	skeletonTrapLife  = 18000
)

// HorseState is what a horse carries beyond any other animal.
type HorseState struct {
	// Variant is the colour in the low byte and the markings in the next.
	// vanilla: Horse.setVariantAndMarkings
	Variant    int32
	Tame, Bred bool
	// Strength is a llama's strength, 1 to 5: its chest's columns.
	// vanilla: Llama.getStrength
	Strength int32
	// didSpit is set by a spit and read once by the llama's grudge, which
	// lets go of its target.
	// vanilla: Llama.didSpit
	didSpit bool
	// head and tail are the llamas ahead of and behind this one in a caravan.
	// vanilla: Llama.caravanHead, caravanTail
	head, tail *Mob
	// knotHeld says the lead this tick is on a fence knot.
	knotHeld bool
	// traderLead says the lead this tick is on a wandering trader, whose
	// despawn delay is traderDelay.
	traderLead  bool
	traderDelay int32
	// Chest says a donkey or mule carries a chest, and Items is what it
	// holds: HorseChestColumns columns of three rows while it does, else nil.
	// vanilla: AbstractChestedHorse.hasChest, AbstractHorse.inventory
	Chest  bool
	Items  []protocol.ItemStack
	Temper int32
	Owner  uuid.UUID
	Saddle protocol.ItemStack
	// Speed and Jump are the rolled movement_speed and jump_strength.
	Speed, Jump float64
	// Attribute instances created by rolls or loading, even at the supplier's base.
	// Movement speed is instantiated by LivingEntity.removeFrost.
	healthSet, jumpSet bool
	// Rider is the player riding the horse, uuid.Nil for none, and RiderID
	// their entity ID, which the passenger list names them by.
	Rider   uuid.UUID
	RiderID int32
	// ExtraRiderIDs are the players seated after the first, who ride
	// without steering; only a forced ride seats them.
	ExtraRiderIDs []int32
	// Standing counts the ticks the horse still rears.
	Standing int
	// gallopSoundCounter counts ridden hoof steps, including the silent ones.
	// vanilla: AbstractHorse.playStepSound
	gallopSoundCounter int
	// Eating is the horse grazing with its head down, which holds it still;
	// eatingTicks counts the graze to its end after 50 ticks.
	// vanilla: AbstractHorse.isEating, eatingCounter
	Eating      bool
	eatingTicks int
	// mouthTicks counts up from 1 while a fed horse's mouth is open, which
	// shuts after 30 ticks; 0 is shut. vanilla: AbstractHorse.mouthCounter
	mouthTicks int
	// NeedsStick requires the kind's steering stick, and StickHeld says
	// the rider holds it. vanilla: Pig/Strider.getControllingPassenger
	NeedsStick, StickHeld bool
	// Trap is a skeleton horse waiting for a player, and TrapTime the ticks
	// it has waited. vanilla: SkeletonHorse.isTrap, trapTime
	Trap     bool
	TrapTime int32
}

// Controlled reports whether the riding player steers the horse: the client
// moves it and the server accepts the position.
// vanilla: AbstractHorse.getControllingPassenger
func (h *HorseState) Controlled() bool {
	return h.Rider != uuid.Nil && !h.Saddle.Empty() && (!h.NeedsStick || h.StickHeld)
}

// IsEquine reports vanilla's AbstractHorse kinds that ride and tame like a
// horse.
func IsEquine(t MobType) bool {
	return IsCamel(t) || t == TypeHorse || t == TypeDonkey || t == TypeMule || t == TypeSkeletonHorse || t == TypeZombieHorse || IsLlama(t)
}

// IsUndeadHorse reports the skeleton horse and the zombie horse.
func IsUndeadHorse(t MobType) bool { return t == TypeSkeletonHorse || t == TypeZombieHorse }

// IsLlama reports the llama and the trader llama.
func IsLlama(t MobType) bool { return t == TypeLlama || t == TypeTraderLlama }

// Chested reports the AbstractChestedHorse kinds.
func (m *Mob) Chested() bool { return m.Type == TypeDonkey || m.Type == TypeMule || IsLlama(m.Type) }

// maxTemper is the temper at which riding tames for certain.
// vanilla: AbstractHorse.getMaxTemper, Llama.getMaxTemper
func (m *Mob) maxTemper() int32 {
	if IsLlama(m.Type) {
		return llamaMaxTemper
	}
	return HorseMaxTemper
}

// HorseChestColumns is the chest columns of a donkey's or mule's chest, of
// three rows each. vanilla: AbstractChestedHorse.getInventoryColumns
const HorseChestColumns = 5

// ChestColumns is the mount screen's chest columns while a chest is fitted:
// a llama's strength, else HorseChestColumns; 0 without a chest.
// vanilla: AbstractChestedHorse.getInventoryColumns, Llama.getInventoryColumns
func (m *Mob) ChestColumns() int32 {
	switch {
	case !m.Horse.Chest:
		return 0
	case IsLlama(m.Type):
		return m.Horse.Strength
	}
	return HorseChestColumns
}

// EquipChest fits an empty chest on a living, tame chested horse.
// vanilla: AbstractChestedHorse.getSlot(499), DispenseItemBehavior.bootStrap
func (m *Mob) EquipChest() bool {
	if !m.Alive() || !m.Chested() || !m.Horse.Tame || m.Horse.Chest {
		return false
	}
	m.fitChest()
	return true
}

// fitChest gives the mount its chest and empty slots.
// vanilla: AbstractChestedHorse.equipChest, AbstractHorse.createInventory
func (m *Mob) fitChest() {
	m.SetMetadataHorseChest(true)
	m.Horse.Items = make([]protocol.ItemStack, 3*m.ChestColumns())
}

// Wears reports whether the stack goes in the mount's "saddle" or "body"
// slot: an equippable piece for that slot its kind may wear, the saddle
// only on a living tame adult.
// vanilla: Mob.isEquippableInSlot, AbstractHorse.canUseSlot
func (m *Mob) Wears(slot string, stack protocol.ItemStack) bool {
	item, ok := registry.StackEquippable(stack)
	if !ok || item.Slot != slot || !item.AllowsEntity(m.Type.Name()) {
		return false
	}
	if IsNautilus(m.Type) && (slot == "saddle" || slot == "body") {
		return m.NautilusCanEquip()
	}
	return slot != "saddle" || m.Alive() && !m.IsBaby() && m.Horse.Tame
}

// HorseFlags is the AbstractHorse flag byte, given the bits of the tame, bred,
// eating, standing and open-mouth flags.
func (m *Mob) HorseFlags(tame, bred, eating, standing, mouth byte) byte {
	var b byte
	// vanilla: Camel.isTamed is true without the flag
	if m.Horse.Tame && !IsCamel(m.Type) {
		b |= tame
	}
	if m.Horse.Bred {
		b |= bred
	}
	if m.Horse.Eating {
		b |= eating
	}
	if m.Horse.Standing > 0 {
		b |= standing
	}
	if m.Horse.mouthTicks > 0 {
		b |= mouth
	}
	return b
}

// vanilla: AbstractHorse.generateMaxHealth
func generateHorseHealth(dice Dice) float64 {
	return 15 + float64(dice.NextIntN(8)) + float64(dice.NextIntN(9))
}

// vanilla: AbstractHorse.generateJumpStrength, whose 0.4f is a float
func generateHorseJump(dice Dice) float64 {
	return float64(float32(0.4)) + dice.NextDouble()*0.2 + dice.NextDouble()*0.2 + dice.NextDouble()*0.2
}

// vanilla: AbstractHorse.generateSpeed, whose 0.45f is a float
func generateHorseSpeed(dice Dice) float64 {
	return (float64(float32(0.45)) + dice.NextDouble()*0.3 + dice.NextDouble()*0.3 + dice.NextDouble()*0.3) * 0.25
}

// vanilla: ZombieHorse.generateZombieHorseJumpStrength and generateZombieHorseSpeed;
// SPEED_FACTOR is a float.
var (
	zombieHorseBaseJump    = 0.5
	zombieHorseJumpStep    = 0.06666666666666667
	zombieHorseBaseSpeed   = 9.0
	zombieHorseSpeedFactor = float64(float32(42.16))
)

// finalizeHorse rolls a horse's colour (shared by a pack), markings and three
// attributes; a donkey or mule rolls its health alone, a skeleton horse its
// jump alone, a zombie horse its jump and speed.
// vanilla: Horse.finalizeSpawn, Horse.randomizeAttributes, AbstractChestedHorse.randomizeAttributes,
// SkeletonHorse.randomizeAttributes
func (m *Mob) finalizeHorse(dice Dice, group *SpawnGroup) {
	if IsCamel(m.Type) {
		// vanilla: Camel.finalizeSpawn, resetLastPoseChangeTickToFullStand
		m.camelResetFullStand()
		return
	}
	if m.Type == TypeSkeletonHorse {
		m.Horse.jumpSet = true
		m.Horse.Jump = generateHorseJump(dice)
		return
	}
	if m.Type == TypeZombieHorse {
		m.Horse.jumpSet = true
		// vanilla: ZombieHorse.randomizeAttributes, jump then speed
		m.Horse.Jump = zombieHorseBaseJump + dice.NextDouble()*zombieHorseJumpStep + dice.NextDouble()*zombieHorseJumpStep + dice.NextDouble()*zombieHorseJumpStep
		m.Horse.Speed = (zombieHorseBaseSpeed + dice.NextDouble() + dice.NextDouble() + dice.NextDouble()) / zombieHorseSpeedFactor
		return
	}
	if IsLlama(m.Type) {
		// vanilla: Llama.setRandomStrength, Llama.finalizeSpawn
		m.SetMetadataHorseStrength(1 + dice.NextIntN(llamaStrengthRange(dice.NextFloat() < 0.04)))
		if group != nil && group.llamaSet {
			m.SetMetadataHorseVariant(group.llamaVariant)
		} else {
			m.SetMetadataHorseVariant(dice.NextIntN(llamaVariants))
			if group != nil {
				group.llamaSet, group.llamaVariant = true, m.Horse.Variant
			}
		}
	}
	if m.Chested() {
		m.Horse.healthSet = true
		m.MaxHealth = generateHorseHealth(dice)
		m.syncMaxHealth()
		m.SetMetadataHealth(m.MaxHealth)
		return
	}
	var colour int32
	if group != nil && group.horseSet {
		colour = group.horseColour
	} else {
		colour = dice.NextIntN(horseColours)
		if group != nil {
			group.horseSet, group.horseColour = true, colour
		}
	}
	m.SetMetadataHorseVariant(colour&0xFF | dice.NextIntN(horseMarkings)<<8&0xFF00)
	m.Horse.healthSet, m.Horse.jumpSet = true, true
	m.MaxHealth = generateHorseHealth(dice)
	m.syncMaxHealth()
	m.SetMetadataHealth(m.MaxHealth)
	m.Horse.Speed = generateHorseSpeed(dice)
	m.Horse.Jump = generateHorseJump(dice)
}

// llamaStrengthRange is the bound a new llama's strength is drawn from: 5
// for 1 in 25, else 3.
func llamaStrengthRange(strong bool) int32 {
	if strong {
		return llamaMaxStrength
	}
	return 3
}

// IsSaddle reports whether the stack is the saddle item.
func IsSaddle(stack protocol.ItemStack) bool {
	return registry.Item(stack.ItemID) == registry.ItemSaddle
}

// CanTakeSaddle reports whether a saddle can be fitted: a tame, grown,
// living horse with the slot free.
// vanilla: Equippable.equipOnTarget, AbstractHorse.canUseSlot
func (m *Mob) CanTakeSaddle() bool {
	return IsEquine(m.Type) && !IsLlama(m.Type) && m.Alive() && !m.IsBaby() && m.Horse.Tame && m.Horse.Saddle.Empty()
}

// CanBeRidden reports whether a click seats the player on an available mount.
// vanilla: AbstractHorse.mobInteract, HappyGhast.canAddPassenger
func (m *Mob) CanBeRidden() bool {
	if m.Type == TypeHappyGhast {
		return m.Alive() && !m.IsBaby() && m.WearsHarness() && len(m.PassengerIDs()) < len(happyGhastSeats)
	}
	if IsCamel(m.Type) {
		return m.camelCanBeRidden()
	}
	if len(m.Passengers) > 0 || m.Horse.Rider != uuid.Nil || !m.Alive() {
		return false
	}
	if IsNautilus(m.Type) {
		return m.NautilusCanEquip()
	}
	if m.IsStrider() || m.Type == TypePig {
		return !m.Horse.Saddle.Empty() && (m.Type == TypePig || !m.lavaFluid.eyesIn)
	}
	// vanilla: TraderLlama.doPlayerRide
	if m.Type == TypeTraderLlama && m.Horse.traderLead {
		return false
	}
	return IsEquine(m.Type) && !m.IsBaby()
}

// Steered reports whether the first rider steers the mob: the client moves
// it and the server accepts the position. A happy ghast obeys its harness
// unless it is told to stay still.
// vanilla: HappyGhast.getControllingPassenger
func (m *Mob) Steered() bool {
	if m.Type == TypeHappyGhast {
		return m.Horse.Rider != uuid.Nil && m.WearsHarness() && m.stillTimeout == 0
	}
	return m.Horse.Controlled()
}

// Board seats the player and calms the horse.
// vanilla: AbstractHorse.doPlayerRide
func (m *Mob) Board(rider uuid.UUID) {
	m.Horse.Rider = rider
	m.SetMetadataHorseEating(false)
	m.SetMetadataHorseStanding(0)
}

// Unboard clears the seat.
func (m *Mob) Unboard() { m.Horse.Rider, m.Horse.RiderID = uuid.Nil, 0 }

// MakeMad rears the horse, a llama only sounding off; it reports whether it
// began, which is when the angry sound plays.
// vanilla: AbstractHorse.makeMad, Llama.canPerformRearing
func (m *Mob) MakeMad() bool {
	if m.Horse.Standing > 0 {
		return false
	}
	if !IsLlama(m.Type) {
		m.stand()
	}
	return true
}

// RidingJump is the server's half of a ridden horse's client-simulated jump:
// the horse rears. It reports whether the horse may jump (then the jump
// sound plays).
// vanilla: AbstractHorse.handleStartJump, canJump
func (m *Mob) RidingJump(power int32) bool {
	if !IsEquine(m.Type) || m.Horse.Saddle.Empty() || power <= 0 {
		return false
	}
	m.SetMetadataHorseEating(false)
	m.SetMetadataHorseStanding(max(m.Horse.Standing, horseStandTicks))
	return true
}

// stand rears the horse, which ends a graze.
// vanilla: AbstractHorse.setStanding
func (m *Mob) stand() {
	m.SetMetadataHorseEating(false)
	m.SetMetadataHorseStanding(horseStandTicks)
}

// noteHorseHurt rears a hurt equine on 1 hit in 3.
// vanilla: AbstractHorse.hurtServer, standIfPossible
func (m *Mob) noteHorseHurt() {
	if IsCamel(m.Type) {
		// vanilla: Camel.actuallyHurt
		m.CamelStandUpInstantly()
		return
	}
	if IsEquine(m.Type) && m.Alive() && m.Rand.Intn(3) == 0 {
		m.MakeMad()
	}
}

const (
	horseHealOdds = 900
	// vanilla: AbstractHorse.aiStep
	horseGrazeOdds  = 300
	horseGrazeTicks = 50
	// vanilla: AbstractHorse.tick
	horseMouthTicks = 30
)

// horseStep shuts a fed horse's mouth, counts the rearing down, heals a
// point on 1 tick in 900, starts and ends a graze on grass, and leaves a trap
// horse nobody sprung in time.
// vanilla: AbstractHorse.tick, AbstractHorse.aiStep, SkeletonHorse.aiStep
func (m *Mob) horseStep(w World) {
	if m.Horse.mouthTicks > 0 {
		if m.SetMetadataHorseMouthTicks(m.Horse.mouthTicks + 1); m.Horse.mouthTicks > horseMouthTicks {
			m.SetMetadataHorseMouthTicks(0)
		}
	}
	if m.Alive() {
		if m.Rand.Intn(horseHealOdds) == 0 {
			m.heal(1)
		}
		// vanilla: Llama.canEatGrass
		if !IsLlama(m.Type) {
			m.grazeStep(w)
		}
	}
	if m.Horse.Standing > 0 {
		m.SetMetadataHorseStanding(m.Horse.Standing - 1)
	}
	if !m.Persistence && m.Horse.Trap {
		if m.Horse.TrapTime >= skeletonTrapLife {
			m.Discard()
		}
		m.Horse.TrapTime++
	}
}

// grazeStep lowers the head of a horse nobody rides on grass on 1 tick in
// 300 and raises it after 50. The odds are rolled before the block is read.
// vanilla: AbstractHorse.aiStep
func (m *Mob) grazeStep(w World) {
	ridden := m.Horse.Rider != uuid.Nil || len(m.Passengers) > 0 || len(m.Horse.ExtraRiderIDs) > 0
	if !m.Horse.Eating && !ridden && m.Rand.Intn(horseGrazeOdds) == 0 {
		at := m.cell()
		m.SetMetadataHorseEating(registry.BlockOf(w.BlockAt(at.X, at.Y-1, at.Z)) == registry.BlockGrassBlock)
	}
	if m.Horse.Eating {
		if m.Horse.eatingTicks++; m.Horse.eatingTicks > horseGrazeTicks {
			m.Horse.eatingTicks = 0
			m.SetMetadataHorseEating(false)
		}
	}
}

// skeletonTrapGoal springs a trap horse once a living player comes near:
// it is tamed and grown, and the server brings the bolt and the riders.
// vanilla: SkeletonTrapGoal
type skeletonTrapGoal struct{ m *Mob }

func (g *skeletonTrapGoal) Priority() int    { return priorityPanic }
func (g *skeletonTrapGoal) Flags() GoalFlags { return 0 }

// vanilla: EntityGetter.hasNearbyAlivePlayer
func (g *skeletonTrapGoal) CanStart(t *tickView) bool {
	if !g.m.Horse.Trap {
		return false
	}
	for _, p := range t.players {
		if !p.Spectator && !p.Dead && Dist2(p.X, p.Y, p.Z, g.m.X, g.m.Y, g.m.Z) < skeletonTrapRange*skeletonTrapRange {
			return true
		}
	}
	return false
}

func (g *skeletonTrapGoal) Start(*tickView)               {}
func (g *skeletonTrapGoal) ShouldContinue(*tickView) bool { return g.m.Horse.Trap }

func (g *skeletonTrapGoal) Tick(t *tickView) {
	m := g.m
	if !m.Horse.Trap {
		return
	}
	m.Horse.Trap = false
	m.SetMetadataHorseTame(true)
	m.SetMetadataBabyAge(0)
	t.events = append(t.events, MobEvent{Mob: m, Kind: MobSkeletonTrapSprung})
}

func (g *skeletonTrapGoal) Stop(*tickView)          {}
func (g *skeletonTrapGoal) RequiresEveryTick() bool { return false }

// runAroundGoal is the wild horse's bucking: with a player aboard it runs
// about, and now and then either gives in to the rider or throws them.
// vanilla: RunAroundLikeCrazyGoal
type runAroundGoal struct {
	m       *Mob
	x, y, z float64
}

func (g *runAroundGoal) Priority() int    { return priorityPanic }
func (g *runAroundGoal) Flags() GoalFlags { return GoalMove }

func (g *runAroundGoal) wild() bool {
	return !g.m.Horse.Tame && g.m.Horse.Rider != uuid.Nil
}

func (g *runAroundGoal) CanStart(t *tickView) bool {
	if !g.wild() {
		return false
	}
	cell, ok := defaultRandomPos(t.w, g.m, 5, 4)
	if !ok {
		return false
	}
	g.x, g.y, g.z = float64(cell.X)+0.5, float64(cell.Y), float64(cell.Z)+0.5
	return true
}

func (g *runAroundGoal) Start(t *tickView) {
	g.m.seekTo(t.w, g.x, g.y, g.z, horseRunPace, navReach)
}

func (g *runAroundGoal) ShouldContinue(*tickView) bool {
	return g.wild() && len(g.m.navOf().nav.path) > 0
}

// Tick is the one-in-fifty roll: a rider whose luck holds tames the horse,
// the rest add to its temper and are thrown.
// vanilla: RunAroundLikeCrazyGoal.tick
func (g *runAroundGoal) Tick(t *tickView) {
	m := g.m
	if !g.wild() || m.Rand.Intn(reducedTickDelay(50)) != 0 {
		return
	}
	if m.Rand.Intn(int(m.maxTemper())) < int(m.Horse.Temper) {
		m.SetMetadataHorseTame(true)
		m.Horse.Owner = m.Horse.Rider
		t.events = append(t.events, MobEvent{Mob: m, Kind: MobHorseTamed})
		return
	}
	m.Horse.Temper = min(m.maxTemper(), m.Horse.Temper+horseTemperStep)
	m.Unboard()
	m.MakeMad()
	t.events = append(t.events, MobEvent{Mob: m, Kind: MobHorseThrewRider})
}

func (g *runAroundGoal) Stop(*tickView)          { g.m.stopSeeking() }
func (g *runAroundGoal) RequiresEveryTick() bool { return false }

// randomStandGoal rears an idle horse now and then and sounds its ambient
// call; the counter climbs from -ambient interval and the roll fires once it
// is positive.
// vanilla: RandomStandGoal
type randomStandGoal struct {
	m         *Mob
	nextStand int
}

func newRandomStandGoal(m *Mob) *randomStandGoal {
	return &randomStandGoal{m: m, nextStand: -m.ambientSoundInterval()}
}

func (g *randomStandGoal) Priority() int    { return 9 }
func (g *randomStandGoal) Flags() GoalFlags { return 0 }

func (g *randomStandGoal) CanStart(*tickView) bool {
	g.nextStand++
	m := g.m
	if g.nextStand > 0 && m.Rand.Intn(1000) < g.nextStand {
		g.nextStand = -m.ambientSoundInterval()
		// vanilla: AbstractHorse.isImmobile, the part a standing horse meets
		return m.Horse.Standing == 0 && m.Rand.Intn(10) == 0
	}
	return false
}

// Start rears the horse and plays its ambient call as the stand sound.
// vanilla: RandomStandGoal.start, AbstractHorse.standIfPossible
func (g *randomStandGoal) Start(t *tickView) {
	m := g.m
	m.stand()
	if ev, vol, pitch := m.AmbientVoice(); ev != registry.SoundNone {
		t.events = append(t.events, MobEvent{Mob: m, Kind: MobSounded, SoundID: ev, Volume: vol, Pitch: pitch})
	}
}

func (g *randomStandGoal) ShouldContinue(*tickView) bool { return false }
func (g *randomStandGoal) Tick(*tickView)                {}
func (g *randomStandGoal) Stop(*tickView)                {}
func (g *randomStandGoal) RequiresEveryTick() bool       { return true }

// horseGoals is AbstractHorse.registerGoals with the kind's behaviour goals.
// vanilla: AbstractHorse.registerGoals, AbstractHorse.addBehaviourGoals
func horseGoals(m *Mob) []Goal {
	if IsCamel(m.Type) {
		return camelGoals(m)
	}
	if m.Type == TypeSkeletonHorse {
		// vanilla: SkeletonHorse.addBehaviourGoals adds none
		return []Goal{
			&skeletonTrapGoal{m: m},
			&runAroundGoal{m: m},
			&followParentGoal{m: m},
			&randomStroll{m: m, pace: horseStrollPace},
			&lookAtPlayer{m: m, reach: 6}, // vanilla: AbstractHorse/Llama.registerGoals
			&randomLookAround{m: m},
			newRandomStandGoal(m),
		}
	}
	if IsLlama(m.Type) {
		// vanilla: Llama.registerGoals; the target goals sit among the
		// others by priority, holding only GoalTarget.
		goals := []Goal{
			newFloatGoal(m),
			&runAroundGoal{m: m},
			&hurtByGoal{m: m, priority: 1, drop: m.takeDidSpit},
			&caravanGoal{m: m, speed: llamaCaravanSpeed},
			&mobTargetGoal{m: m, priority: 2, victim: TypeWolf, unsighted: true, interval: 16,
				followShare: llamaWolfFollow, matches: isWildWolf},
			newSpitGoal(m),
			&panicGoal{m: m, priority: 3},
			&breedGoal{m: m, priority: 4},
			&temptGoal{m: m, priority: 5},
			&followParentGoal{m: m, priority: 6},
			&randomStroll{m: m, priority: 7, pace: horseStrollPace},
			&lookAtPlayer{m: m, priority: 8, reach: 6},
			&randomLookAround{m: m, priority: 9},
		}
		if m.Type == TypeTraderLlama {
			goals = withTraderLlamaTargets(m, goals)
		}
		return goals
	}
	if m.Type == TypeZombieHorse {
		// vanilla: ZombieHorse.addBehaviourGoals: float and tempt, no panic
		return []Goal{
			newFloatGoal(m),
			&runAroundGoal{m: m},
			&temptGoal{m: m},
			&followParentGoal{m: m},
			&randomStroll{m: m, pace: horseStrollPace},
			&lookAtPlayer{m: m, reach: 6},
			&randomLookAround{m: m},
			newRandomStandGoal(m),
		}
	}
	return []Goal{
		newFloatGoal(m),
		&panicGoal{m: m},
		&runAroundGoal{m: m},
		&breedGoal{m: m},
		&temptGoal{m: m},
		&followParentGoal{m: m},
		&randomStroll{m: m, pace: horseStrollPace},
		&lookAtPlayer{m: m, reach: 6},
		&randomLookAround{m: m},
		newRandomStandGoal(m),
	}
}

// HorseClick is what a click on a horse does.
type HorseClick int

const (
	// HorseClickNone: nothing happens (a rider aboard, a foal, or food).
	HorseClickNone HorseClick = iota
	// HorseClickRear: an untamed horse reared at the item.
	HorseClickRear
	// HorseClickSaddle: the saddle is fitted.
	HorseClickSaddle
	// HorseClickChest: a chest is fitted to a donkey or mule.
	HorseClickChest
	// HorseClickMount: the click seats the player.
	HorseClickMount
	// HorseClickFeed: the item is horse food, for FeedHorse.
	HorseClickFeed
	// HorseClickInventory: a sneaking click on a tame adult opens the mount
	// screen.
	HorseClickInventory
	// HorseClickArmor: the held body piece is fitted.
	HorseClickArmor
)

// UseItem handles a click no lead or name tag took: food goes to FeedHorse,
// an untamed horse rears at any other item, a tame one takes a chest, a
// saddle or body armour, a sneaking click on a tame adult opens its screen,
// and a bare click mounts. mad reports that rearing began (the
// angry sound's cue).
// vanilla: Horse.mobInteract, AbstractChestedHorse.mobInteract, AbstractHorse.mobInteract,
// Equippable.equipOnTarget
func (m *Mob) UseItem(held protocol.ItemStack, sneaking bool) (click HorseClick, mad bool) {
	// vanilla: SkeletonHorse.mobInteract passes on an untamed one
	if !m.Alive() || m.Horse.Rider != uuid.Nil && !IsCamel(m.Type) || m.Type == TypeSkeletonHorse && !m.Horse.Tame {
		return HorseClickNone, false
	}
	if m.Horse.Tame && sneaking && !m.IsBaby() {
		return HorseClickInventory, false
	}
	// A golden dandelion goes to the age lock (server.lockAge) before any
	// food or rearing.
	// vanilla: Horse.mobInteract, AbstractChestedHorse.mobInteract
	if m.IsBaby() && held.ItemID == int32(registry.ItemGoldenDandelion) {
		return HorseClickNone, false
	}
	if !held.Empty() {
		// vanilla: SkeletonHorse skips Horse.mobInteract's feeding
		if m.Type != TypeSkeletonHorse && registry.Item(held.ItemID).InTag(registry.ItemTagOf(m.foodTag())) {
			return HorseClickFeed, false
		}
		if !m.Horse.Tame {
			return HorseClickRear, m.MakeMad()
		}
		// A tame foal takes a chest too: only a golden dandelion sends a baby
		// past AbstractChestedHorse.mobInteract.
		if held.ItemID == int32(registry.ItemChest) && m.EquipChest() {
			return HorseClickChest, false
		}
		if !m.IsBaby() && IsSaddle(held) && m.CanTakeSaddle() {
			held.Count = 1
			m.SetMetadataHorseSaddle(held)
			m.SaddleDropChance = guaranteedDropChance
			return HorseClickSaddle, false
		}
		// vanilla: AbstractHorse.mobInteract's equipBodyArmor
		if !m.IsBaby() && m.Body.Empty() && m.Wears("body", held) {
			held.Count = 1
			m.SetEquipment(6, held)
			m.BodyDropChance = guaranteedDropChance
			return HorseClickArmor, false
		}
	}
	if m.IsBaby() {
		return HorseClickNone, false
	}
	return HorseClickMount, false
}

// horseMeal is what one horse food does: the health it heals, the seconds a
// foal grows, the temper it adds, and whether a tame adult falls in love.
// vanilla: AbstractHorse.handleEating
type horseMeal struct {
	heal   float64
	ageUp  int
	temper int32
	love   bool
}

// foodTag is the tag of the items the kind eats.
// vanilla: Llama.isFood, AbstractHorse.isFood
func (m *Mob) foodTag() string {
	if m.Type == TypeZombieHorse {
		return "minecraft:zombie_horse_food"
	}
	if m.Type == TypeCamelHusk {
		return "minecraft:camel_husk_food"
	}
	if IsCamel(m.Type) {
		return "minecraft:camel_food"
	}
	if IsLlama(m.Type) {
		return "minecraft:llama_food"
	}
	return "minecraft:horse_food"
}

// llamaMeals is what a llama's two foods do.
// vanilla: Llama.handleEating
var llamaMeals = map[registry.Item]horseMeal{
	registry.ItemWheat:    {heal: 2, ageUp: 10, temper: 3},
	registry.ItemHayBlock: {heal: 10, ageUp: 90, temper: 6, love: true},
}

// camelMeals is what the camel's cactus does.
// vanilla: Camel.handleEating
var camelMeals = map[registry.Item]horseMeal{
	registry.ItemCactus: {heal: 2, ageUp: 10, love: true},
}

// camelHuskMeals is what the camel husk's rabbit foot does; it never falls in
// love.
// vanilla: Camel.handleEating, CamelHusk.canFallInLove
var camelHuskMeals = map[registry.Item]horseMeal{
	registry.ItemRabbitFoot: {heal: 2, ageUp: 10},
}

var horseMeals = map[registry.Item]horseMeal{
	registry.ItemWheat:                {heal: 2, ageUp: 20, temper: 3},
	registry.ItemSugar:                {heal: 1, ageUp: 30, temper: 3},
	registry.ItemHayBlock:             {heal: 20, ageUp: 180},
	registry.ItemApple:                {heal: 3, ageUp: 60, temper: 3},
	registry.ItemRedMushroom:          {heal: 3, temper: 3},
	registry.ItemCarrot:               {heal: 3, ageUp: 60, temper: 3},
	registry.ItemGoldenCarrot:         {heal: 4, ageUp: 60, temper: 5, love: true},
	registry.ItemGoldenApple:          {heal: 10, ageUp: 240, temper: 10, love: true},
	registry.ItemEnchantedGoldenApple: {heal: 10, ageUp: 240, temper: 10, love: true},
}

// FeedHorse feeds the horse one item: it heals, a foal grows, an untamed
// horse (or a tame one the meal did something for) gains temper, and a
// golden food sends a tame adult into love. ate says the item is spent,
// loved that love began (the hearts' cue).
// vanilla: AbstractHorse.handleEating
func (m *Mob) FeedHorse(w World, item int32) (ate, loved bool) {
	meals := horseMeals
	if IsLlama(m.Type) {
		meals = llamaMeals
	}
	switch m.Type {
	case TypeCamelHusk:
		meals = camelHuskMeals
	case TypeCamel:
		meals = camelMeals
	}
	meal, ok := meals[registry.Item(item)]
	if !ok {
		return false, false
	}
	// vanilla: ZombieHorse.canFallInLove
	if meal.love && m.Type != TypeZombieHorse && m.Horse.Tame && m.BabyAge == 0 && !m.IsInLove() {
		m.InLove = loveTicks
		ate, loved = true, true
	}
	if m.Health < m.MaxHealth && meal.heal > 0 {
		m.heal(meal.heal)
		ate = true
	}
	if m.IsBaby() && meal.ageUp > 0 && !m.AgeLocked {
		m.ageUp(w, meal.ageUp*20)
		ate = true
	}
	if meal.temper > 0 && (ate || !m.Horse.Tame) && m.Horse.Temper < m.maxTemper() {
		m.Horse.Temper = min(m.maxTemper(), m.Horse.Temper+meal.temper)
		ate = true
	}
	if ate && !IsLlama(m.Type) && !IsCamel(m.Type) {
		// vanilla: AbstractHorse.eating, openMouth; Llama and Camel override
		// handleEating without it.
		m.SetMetadataHorseMouthTicks(1)
	}
	return ate, loved
}

// canParent is AbstractHorse.canParent: a tame, grown, unhurt horse in love
// that nobody rides and that rides nothing.
func (m *Mob) canParent() bool {
	return m.Horse.Rider == uuid.Nil && len(m.Passengers) == 0 && m.Vehicle == nil &&
		m.Horse.Tame && !m.IsBaby() && m.Health >= m.MaxHealth && m.IsInLove()
}

// horseCanMate is the horse's and donkey's canMate: either of the two kinds,
// both able to parent; a mule or skeleton horse mates with nothing.
// vanilla: Horse.canMate, Donkey.canMate, Llama.canMate, AbstractHorse.canMate
func (m *Mob) horseCanMate(o *Mob) bool {
	if IsLlama(m.Type) {
		return o != m && IsLlama(o.Type) && m.canParent() && o.canParent()
	}
	if IsCamel(m.Type) {
		return o != m && m.Type == TypeCamel && o.Type == TypeCamel && m.canParent() && o.canParent()
	}
	breeds := func(t MobType) bool { return t == TypeHorse || t == TypeDonkey }
	return o != m && breeds(m.Type) && breeds(o.Type) && m.canParent() && o.canParent()
}

// OffspringType is the kind a breeding of the mother with the partner bears:
// a horse and a donkey make a mule, any other pair its own kind.
// vanilla: Horse.getBreedOffspring, Donkey.getBreedOffspring
func (m *Mob) OffspringType(partner *Mob) MobType {
	if m.Type == TypeHorse && partner.Type == TypeDonkey || m.Type == TypeDonkey && partner.Type == TypeHorse {
		return TypeMule
	}
	return m.Type
}

// The ranges a foal's inherited attributes stay in, each generator's extremes
// cast to a float.
// vanilla: AbstractHorse.MIN_HEALTH .. MAX_MOVEMENT_SPEED
var (
	horseHealthMin, horseHealthMax = 15.0, 30.0
	horseJumpMin                   = float64(float32(float64(float32(0.4))))
	horseJumpMax                   = float64(float32(float64(float32(0.4)) + 0.6))
	horseSpeedMin                  = float64(float32(float64(float32(0.45)) * 0.25))
	horseSpeedMax                  = float64(float32((float64(float32(0.45)) + 0.9) * 0.25))
)

// breedHorse gives a foal its parents' make: a horse foal one parent's colour
// and markings (or a fresh roll), and every foal health, jump and speed drawn
// around its parents' mean.
// vanilla: Horse.getBreedOffspring, AbstractHorse.setOffspringAttributes
func (m *Mob) breedHorse(baby, partner *Mob) {
	if IsCamel(m.Type) {
		// vanilla: Camel.getBreedOffspring rolls nothing
		return
	}
	if baby.Type == TypeHorse {
		colour, markings := m.Horse.Variant&0xFF, m.Horse.Variant>>8&0xFF
		switch pick := m.Rand.Intn(9); {
		case pick >= 8:
			colour = int32(m.Rand.Intn(horseColours))
		case pick >= 4:
			colour = partner.Horse.Variant & 0xFF
		}
		switch pick := m.Rand.Intn(5); {
		case pick >= 4:
			markings = int32(m.Rand.Intn(horseMarkings))
		case pick >= 2:
			markings = partner.Horse.Variant >> 8 & 0xFF
		}
		baby.SetMetadataHorseVariant(colour | markings<<8)
	}
	if IsLlama(baby.Type) {
		// vanilla: Llama.getBreedOffspring
		strength := int32(m.Rand.Intn(int(max(m.Horse.Strength, partner.Horse.Strength)))) + 1
		if m.Rand.Float32() < 0.03 {
			strength++
		}
		baby.SetMetadataHorseStrength(min(strength, llamaMaxStrength))
		baby.SetMetadataHorseVariant(partner.Horse.Variant)
		if m.Rand.Intn(2) == 0 {
			baby.SetMetadataHorseVariant(m.Horse.Variant)
		}
	}
	baby.Horse.healthSet, baby.Horse.jumpSet = true, true
	baby.MaxHealth = offspringAttribute(m.MaxHealth, partner.MaxHealth, horseHealthMin, horseHealthMax, m)
	baby.syncMaxHealth()
	baby.SetMetadataHealth(min(baby.Health, baby.MaxHealth))
	baby.Horse.Jump = offspringAttribute(m.Horse.Jump, partner.Horse.Jump, horseJumpMin, horseJumpMax, m)
	baby.Horse.Speed = offspringAttribute(m.Horse.Speed, partner.Horse.Speed, horseSpeedMin, horseSpeedMax, m)
}

// offspringAttribute draws a foal's value from its parents' mean, spread by
// their difference plus 15% of the range either way and reflected back into
// the range at its ends.
// vanilla: AbstractHorse.createOffspringAttribute
func offspringAttribute(a, b, lo, hi float64, m *Mob) float64 {
	a, b = max(lo, min(hi, a)), max(lo, min(hi, b))
	margin := 0.15 * (hi - lo)
	quality := (m.Rand.Float64()+m.Rand.Float64()+m.Rand.Float64())/3 - 0.5
	v := (a+b)/2 + (math.Abs(a-b)+margin*2)*quality
	switch {
	case v > hi:
		return hi - (v - hi)
	case v < lo:
		return lo + (lo - v)
	}
	return v
}

// BoxWorld is the collision query used to find a horse's dismount spot.
type BoxWorld interface {
	BoxCollides(minX, minY, minZ, maxX, maxY, maxZ float64) bool
}

// HorseDismountSpot is the free spot on the rider's right, else the horse's
// own position, for a rider of the given box size. Vanilla's getDismountLocationInDirection floor search is
// limited to the horse's height and one above.
// vanilla: AbstractHorse.getDismountLocationForPassenger
func HorseDismountSpot(w BoxWorld, m *Mob, boxWidth, boxHeight float64) player.Position {
	width, _ := KindDims(m.Type)
	gap := (width + boxWidth + 1e-5) / 2
	for _, turn := range [...]float64{90, -90} {
		rad := (float64(m.Yaw) + turn) * math.Pi / 180
		dx, dz := -math.Sin(rad), math.Cos(rad)
		if scale := math.Max(math.Abs(dx), math.Abs(dz)); scale != 0 {
			dx, dz = dx/scale, dz/scale
		}
		x, z := m.X+dx*gap, m.Z+dz*gap
		for _, dy := range [...]float64{0, 1} {
			y := m.Y + dy
			half := boxWidth / 2
			if !w.BoxCollides(x-half, y, z-half, x+half, y+boxHeight, z+half) {
				return player.Position{X: x, Y: y, Z: z}
			}
		}
	}
	return player.Position{X: m.X, Y: m.Y, Z: m.Z}
}

// MoveCollisionWorld compares a vehicle's current and reported collision boxes.
type MoveCollisionWorld interface {
	CollidesAnew(from, to [6]float64) bool
}

// RejectsMove reports whether a rider's report exceeds the tick's move budget
// or enters a collision shape the vehicle did not already intersect.
// vanilla: ServerGamePacketListenerImpl.handleMoveVehicle
func (m *Mob) RejectsMove(w MoveCollisionWorld, fromX, fromY, fromZ, x, y, z float64) bool {
	if MovedTooFar(fromX, fromY, fromZ, x, y, z, 0) {
		return true
	}
	width, height := KindDims(m.Type)
	half := width / 2
	// Collision starts at the last accepted position, rather than the tick start.
	return w.CollidesAnew(
		[6]float64{m.X - half, m.Y, m.Z - half, m.X + half, m.Y + height, m.Z + half},
		[6]float64{x - half, y, z - half, x + half, y + height, z + half})
}

// RideLanding is the fall check for an accepted ridden-horse move from
// (fromX, fromY, fromZ): the drop banks as for a walker, water and webs
// clear it, and a landing past the safe distance hurts. It returns the
// settled fall, the damage, whether it landed whole or softened, and the
// landing block's effect, which the rider shares.
// A happy ghast banks no fall at all.
// vanilla: Entity.doCheckFallDamage, AbstractHorse.causeFallDamage, HappyGhast.checkFallDamage
func (m *Mob) RideLanding(w World, fromX, fromY, fromZ float64, onGround bool) (landed, damage float64, hurt, soft bool, effect rules.FallEffect) {
	if m.Type != TypeHappyGhast {
		m.bankFall(w, fromX, fromY, fromZ)
	}
	if !onGround || m.Type == TypeHappyGhast {
		return 0, 0, false, false, rules.FallEffect{Scale: 1, Modifier: 1, DamageType: rules.DamageTypeFall}
	}
	landed = m.FallDistance
	m.FallDistance = 0
	damage, hurt, soft, effect = m.fallBlow(w, landed)
	m.climbedFrom = ""
	return landed, damage, hurt, soft, effect
}
