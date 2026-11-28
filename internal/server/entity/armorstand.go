package entity

import (
	"math"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// The armor stand: a living body that stays where placed. A plain value like
// primed TNT, returning events. X, Y, Z is the box's bottom centre; the box
// is ARMOR_STAND's sized(0.5F, 1.975F), (0.25F, 0.9875F) when small, and
// none for a marker, which keeps markers out of every sweep.

// Armor stand numbers: the box and its small half, default health,
// LivingEntity gravity, the drag (0.98 vertically, the block's friction
// times the 0.91 air drag horizontally), the window in which a second punch
// breaks it, and the two fire burns.
// vanilla: EntityTypes.ARMOR_STAND, LivingEntity.travelInAir, ArmorStand
// .hurtServer
const (
	ArmorStandWidth   = 0.5
	ArmorStandHeight  = 1.975
	ArmorStandHealth  = 20
	ArmorStandGravity = 0.08
	// ArmorStandDrag is LivingEntity.travelInAir's vertical 0.98, the
	// ArmorStandAirDrag its horizontal 0.91.
	ArmorStandDrag    = 0.98
	ArmorStandAirDrag = 0.91
	// punchWindowTicks is the 5-tick window a second punch breaks within.
	// vanilla: ArmorStand.WOBBLE_TIME
	punchWindowTicks = 5
	// burnDamage is the on-fire bite of 4.0 while health is above 0.5;
	// ignitingDamage is what a fire source does to a stand already alight.
	burnDamage     = 4.0
	ignitingDamage = 0.15
	// lavaShallowDepth is getFluidJumpThreshold for a stand's eye height:
	// lava no deeper than this drags the shallow branch.
	lavaShallowDepth = 0.4
)

// The wire's Set Equipment slot numbers, which the stand's DisabledSlots
// mask shares: 0 main hand, 1 off hand, 2 feet, 3 legs, 4 chest, 5 head.
const (
	StandMainHand = 0
	StandOffHand  = 1
	StandFeet     = 2
	StandLegs     = 3
	StandChest    = 4
	StandHead     = 5
)

// standSlotNames names the six wire slots, in wire order, the way the
// equipment compound of a save does.
var standSlotNames = [...]string{"mainhand", "offhand", "feet", "legs", "chest", "head"}

// ArmorStand is one armor stand of a world.
type ArmorStand struct {
	LivingState
	LivingEffects
	ID int32
	StandBody
	// Yaw is the whole of the stand's rotation, snapped to the 45 degrees
	// the placement leaves it at.
	Yaw float32

	// The stand's item tag and summon state: the four flags, the base flags'
	// invisibility, the six Pose rotations (Head, Body, LeftArm, RightArm,
	// LeftLeg, RightLeg) and the locked-slot mask.
	Small, ShowArms, NoBasePlate, Marker, Invisible bool
	DisabledSlots                                   int32
	Pose                                            [6][3]float32

	// NoGravity is Entity.isNoGravity: no travel at all (no drag, gravity or
	// move), though a current still pushes it. It also skips block effects and
	// extinguishing, since the flag turns off hasPhysics and so
	// isAffectedByBlocks.
	// vanilla: ArmorStand.travel, Entity.isNoGravity, ArmorStand.hasPhysics
	NoGravity bool

	// The equipment, in the wire slots: what the stand holds and wears.
	MainHand, OffHand protocol.ItemStack
	Worn              [4]protocol.ItemStack // feet, legs, chest, head

	attrs   *player.Attributes
	scale   float64
	located standLocated
	age     int32

	Health    float64
	FireTicks int
	Dead      bool

	// Invulnerable is the Invulnerable tag: every hurt is refused but a creative
	// player's blow.
	// vanilla: LivingEntity.isInvulnerableTo
	Invulnerable bool

	// LastHit is the loop tick of the last shaking punch, 0 before any; the
	// second-punch window is measured from it.
	LastHit int64
}

// NewArmorStand places a stand at full health with no equipment and client
// defaults for the rest.
func NewArmorStand(id int32, x, y, z float64) *ArmorStand {
	s := &ArmorStand{ID: id, X: x, Y: y, Z: z, Health: ArmorStandHealth, scale: 1, attrs: player.NewAttributes(armorStandAttributeSupplier)}
	s.Pose[2] = [3]float32{-10, 0, -10} // LeftArm
	s.Pose[3] = [3]float32{-15, 0, 10}  // RightArm
	s.Pose[4] = [3]float32{-1, 0, -1}   // LeftLeg
	s.Pose[5] = [3]float32{1, 0, 1}     // RightLeg
	return s
}

// ArmorStandEventKind is one thing an armor stand's tick produced that the
// server may have to act on.
type ArmorStandEventKind uint8

const (
	// ArmorStandBroke: health ran out (fire or an explosion) and the body is
	// dead, for the server to remove with its drops.
	ArmorStandBroke ArmorStandEventKind = iota
	// ArmorStandVoided: the stand fell 64 below the world and is dead, for
	// the server to remove with no sound and no drops.
	ArmorStandVoided
	// ArmorStandLocated: location_changed entries are due; Level holds a bit
	// per EnchEquipment piece whose stack changed, and LocatedMoved for a new
	// cell or a landing.
	ArmorStandLocated
	// ArmorStandSlid: the stand slid down a honey block's side, owed the
	// slide's sound and particles.
	ArmorStandSlid
)

// ArmorStandEvent is one event out of an armor stand's tick.
type ArmorStandEvent struct {
	Kind    ArmorStandEventKind
	X, Y, Z float64
	Level   int32
}

// HalfWidth is half the stand's box width.
func (s *ArmorStand) HalfWidth() float64 { return ArmorStandWidth / 2 * s.dimensionScale() }

// vanilla: LivingEntity.getScale, ArmorStand.getDimensionsMarker
func (s *ArmorStand) dimensionScale() float64 {
	if s.Small {
		return s.scale * 0.5
	}
	return s.scale
}

func (s *ArmorStand) boxHeight() float64 {
	return ArmorStandHeight * s.dimensionScale()
}

// Box is the stand's box about its feet, for the fluid walk and inside-block
// check. A marker has none, keeping it out of every sweep.
func (s *ArmorStand) Box() AABB {
	if s.Marker {
		return AABB{minX: s.X, minY: s.Y, minZ: s.Z, maxX: s.X, maxY: s.Y, maxZ: s.Z}
	}
	return s.boxAtScale(s.dimensionScale())
}

func (s *ArmorStand) boxAtScale(scale float64) AABB {
	hw := ArmorStandWidth / 2 * scale
	return AABB{
		minX: s.X - hw, minY: s.Y, minZ: s.Z - hw,
		maxX: s.X + hw, maxY: s.Y + ArmorStandHeight*scale, maxZ: s.Z + hw,
	}
}

// BlastBox is the bounding box the exposure of a blast is measured over.
// vanilla: ServerExplosion.getSeenPercent
func (s *ArmorStand) BlastBox() (minX, minY, minZ, maxX, maxY, maxZ float64) {
	b := s.Box()
	return b.minX, b.minY, b.minZ, b.maxX, b.maxY, b.maxZ
}

// eyeHeight is EntityDimensions.eyeHeight: 0.9 of the full box, the whole
// small box.
// vanilla: Entity.getEyeHeight
func (s *ArmorStand) eyeHeight(scale float64) float64 {
	if s.Small {
		return ArmorStandHeight * scale
	}
	return ArmorStandHeight * 0.9 * scale
}

// EyePosition is Entity.getEyePosition, where an explosion's knockback is
// directed from.
// vanilla: Entity.getEyePosition
func (s *ArmorStand) EyePosition() (x, y, z float64) {
	return s.X, s.Y + s.eyeHeight(s.dimensionScale()), s.Z
}

// Step advances the stand one tick: fire cells light it, the burn bites,
// currents push it, and it falls through fluid or air drag. A marker
// neither burns nor moves (setInvisible's noPhysics), nor does a dead stand
// awaiting removal. A no-gravity stand does not move and is not lit by cells
// (isAffectedByBlocks is false), though an existing burn still bites.
// vanilla: ArmorStand.tick, LivingEntity.aiStep
func (s *ArmorStand) Step(w World) []ArmorStandEvent {
	if s.Dead {
		return nil
	}
	var events []ArmorStandEvent
	if mask := s.locatedStep(); mask != 0 {
		events = append(events, ArmorStandEvent{Kind: ArmorStandLocated, X: s.X, Y: s.Y, Z: s.Z, Level: mask})
	}
	s.TickTimers()
	// Even a marker or an invulnerable stand goes.
	// vanilla: Entity.checkBelowWorld, ArmorStand.hurtServer (BYPASSES_INVULNERABILITY kills with no drops)
	if s.Y < float64(w.MinY())-64 {
		s.Dead = true
		return append(events, ArmorStandEvent{Kind: ArmorStandVoided})
	}
	s.tickEffects()
	if s.Marker {
		// A marker takes no damage but its fire still counts down.
		// vanilla: Entity.baseTick
		if s.FireTicks > 0 {
			s.FireTicks--
		}
		return events
	}

	// The fluids about the box first: the burn below skips while in lava,
	// and the current's push is part of the tick's movement.
	scale := s.dimensionScale()
	box := s.boxAtScale(scale)
	s.updateFluids(w, box, s.eyeHeight(scale))
	inLava := s.lava.height > 0

	// The burn: 4 damage on every 20th fire tick while health is above 0.5,
	// the count dropping by one a tick. A fire cell relights it every tick,
	// keeping the full eight seconds and a bite every tick. An invulnerable
	// stand still counts down but takes no damage.
	// vanilla: Entity.baseTick, DamageTypeTags.BURNS_ARMOR_STANDS
	if s.FireTicks > 0 {
		if s.FireTicks%20 == 0 && !inLava && !s.Invisible && s.Health > 0.5 &&
			!s.InvulnerableTo(rules.DamageTypeOnFire, false) && s.causeDamage(burnDamage) {
			return append(events, ArmorStandEvent{Kind: ArmorStandBroke})
		}
		s.FireTicks--
	}

	if s.NoGravity {
		s.settleVelocity()
	} else {
		wasOnGround, fell := s.OnGround, s.FallDistance
		s.LandedFall = s.travel(w, box, ArmorStandWidth/2*scale, ArmorStandGravity)
		if !wasOnGround && fell > 0 && s.OnGround && s.locatedActive() {
			events = append(events, ArmorStandEvent{Kind: ArmorStandLocated, X: s.X, Y: s.Y, Z: s.Z,
				Level: LocatedMoved | LocatedLanded})
		}
	}

	// Inside-block effects after the move (applyEffectsFromBlocks): fire lights
	// the stand and deals the source's 0.15, taken while alight; lava lights it
	// longer and hurts nothing (it is in neither armor-stand damage tag). An
	// invisible stand takes neither bite, an invulnerable one neither hurt,
	// though the fire still catches. A no-gravity stand skips all of it,
	// including the extinguish, since isAffectedByBlocks is false.
	// vanilla: BaseFireBlock.entityInside, LavaFluid.entityInside,
	// ArmorStand.hurtServer, Entity.applyEffectsFromBlocks
	if !s.NoGravity {
		s.VY, _ = bubbleColumnDrift(w, s.Box(), s.VY)
		var broke bool
		s.Box().insideCells(func(x, y, z int32) {
			switch {
			case rules.IsFire(w.BlockAt(x, y, z)):
				s.igniteFor(fireTicksFire)
				if !s.Invisible && !s.InvulnerableTo(rules.DamageTypeInFire, false) &&
					s.causeDamage(ignitingDamage) {
					broke = true
				}
			case rules.IsLava(w.BlockAt(x, y, z)), rules.IsLavaCauldron(w.BlockAt(x, y, z)):
				s.igniteFor(fireTicksLava)
			case honeySlideCell(w.BlockAt(x, y, z), x, y, z, s.X, s.Y, s.Z, 2*s.HalfWidth(), s.OnGround, &s.VX, &s.VY, &s.VZ, &s.FallDistance):
				events = append(events, ArmorStandEvent{Kind: ArmorStandSlid})
			}
		})
		if broke {
			return []ArmorStandEvent{{Kind: ArmorStandBroke}}
		}

		// Rain (only where the sky reaches the stand) and water extinguish it,
		// after the move and the inside-block effects.
		// vanilla: Entity.applyEffectsFromBlocks
		if s.inRain(w) || rules.IsWater(w.BlockAt(int32(math.Floor(s.X)), int32(math.Floor(s.Y)), int32(math.Floor(s.Z)))) {
			s.FireTicks = 0
		}
	}
	return events
}

// inRain reports whether rain lands on the stand, which a roof keeps dry.
func (s *ArmorStand) inRain(w World) bool {
	return rainLandsOn(w, s.X, s.Y, s.Z, s.boxHeight())
}

// igniteFor raises the fire count to ticks, vanilla's igniteForTicks
// leaving a longer burn alone.
func (s *ArmorStand) igniteFor(ticks int) {
	if s.FireTicks < ticks {
		s.FireTicks = ticks
	}
}

// PunchOutcome says what one player's punch on the stand did.
type PunchOutcome uint8

const (
	// PunchShaken: the first punch, which shakes the stand and writes its
	// window — entity event 32 on the wire.
	PunchShaken PunchOutcome = iota
	// PunchBroke: the second punch within the window, or a creative hand's
	// alone; the body is dead and the server takes it out with its drops.
	PunchBroke
	// PunchRefused: an invulnerable stand's punch from a hand that is not
	// creative's, which the stand shrugs off whole — no shake, no window.
	PunchRefused
)

// InvulnerableTo is LivingEntity.isInvulnerableTo for a stand: an
// invulnerable stand refuses all but a creative player's blow.
// vanilla: LivingEntity.isInvulnerableTo
func (s *ArmorStand) InvulnerableTo(damageType registry.DamageType, creative bool) bool {
	return s.Invulnerable && !creative && !rules.InvulnerabilityBypassed(damageType)
}

// Punch is hurtServer's CAN_BREAK_ARMOR_STAND branch: a creative hand breaks
// the stand at once; another punch breaks it if one landed in the last five
// ticks (LastHit), else shakes it and records the time. now is the loop's
// tick counter; vanilla's lastHit starts at 0, so a punch in the world's
// first ticks finds the window open. An invulnerable stand refuses a
// survival punch before anything else, as isInvulnerableTo leads
// hurtServer.
// vanilla: ArmorStand.hurtServer, DamageTypeTags.CAN_BREAK_ARMOR_STAND
func (s *ArmorStand) Punch(now int64, creative bool) PunchOutcome {
	if s.InvulnerableTo(rules.DamageTypePlayerAttack, creative) {
		return PunchRefused
	}
	if creative {
		s.Dead = true
		return PunchBroke
	}
	if now-s.LastHit <= punchWindowTicks {
		s.Dead = true
		return PunchBroke
	}
	s.LastHit = now
	return PunchShaken
}

// StandHurt is the feedback and drops a stand's typed hurt produces.
type StandHurt struct {
	Landed, Shake, Item, Equipment, Particles bool
}

// HurtByCommand runs the damage-type branches, including hurts that change
// the stand but return false to DamageCommand.
// vanilla: ArmorStand.hurtServer
func (s *ArmorStand) HurtByCommand(now int64, damageType registry.DamageType, creative, noBuild, griefingOff bool) (hit StandHurt) {
	if s.Dead || griefingOff {
		return hit
	}
	if rules.InvulnerabilityBypassed(damageType) {
		s.Dead = true
		return hit
	}
	if s.InvulnerableTo(damageType, creative) || s.Invisible || s.Marker {
		return hit
	}
	switch {
	case damageType.InTag(registry.DamageTypeTagIDIsExplosion):
		s.Dead, hit.Equipment = true, true
		return hit
	case damageType.InTag(registry.DamageTypeTagIDIgnitesArmorStands):
		if s.FireTicks > 0 {
			hit.Equipment = s.causeDamage(ignitingDamage)
		} else {
			s.igniteFor(100) // vanilla: ArmorStand.hurtServer, five seconds
		}
		return hit
	case damageType.InTag(registry.DamageTypeTagIDBurnsArmorStands) && s.Health > 0.5:
		hit.Equipment = s.causeDamage(burnDamage)
		return hit
	}
	alwaysKills := damageType.InTag(registry.DamageTypeTagIDAlwaysKillsArmorStands)
	if noBuild || (!alwaysKills && !damageType.InTag(registry.DamageTypeTagIDCanBreakArmorStand)) {
		return hit
	}
	hit.Landed = true
	if creative || alwaysKills {
		s.Dead = true
	} else {
		s.Punch(now, false)
	}
	if s.Dead {
		hit.Particles, hit.Item, hit.Equipment = true, !creative, !creative
	} else {
		hit.Shake = true
	}
	return hit
}

// causeDamage subtracts dmg from health, breaking the stand at 0.5 or less,
// and reports the death. It is ArmorStand.causeDamage's direct subtraction,
// with no invulnerability window.
// vanilla: ArmorStand.causeDamage
func (s *ArmorStand) causeDamage(dmg float64) bool {
	health := s.Health - dmg
	if health <= 0.5 {
		s.Dead = true
		return true
	}
	s.Health = health
	return false
}

// standFilterBit is EquipmentSlot.getFilterBit(0) for a wire slot: the mask
// bits are numbered by EquipmentSlot id, not wire index (the off hand comes
// after the head). Offsets 8 and 16 are added to it.
// vanilla: EquipmentSlot.getFilterBit
func standFilterBit(slot int) uint {
	switch slot {
	case StandMainHand:
		return 0
	case StandFeet:
		return 1
	case StandLegs:
		return 2
	case StandChest:
		return 3
	case StandHead:
		return 4
	case StandOffHand:
		return 5
	}
	return 0
}

// SlotLocked is ArmorStand.isDisabled for a wire slot: the +0 bit disables
// the slot entirely, and hand slots are disabled while the stand shows no
// arms.
// vanilla: ArmorStand.isDisabled
func (s *ArmorStand) SlotLocked(slot int) bool {
	return s.DisabledSlots&(int32(1)<<standFilterBit(slot)) != 0 || (s.handSlot(slot) && !s.ShowArms)
}

// takeLocked is the +8 bit, refusing taking an item out; putLocked the +16
// bit, refusing putting one in.
// vanilla: ArmorStand.swapItem
func (s *ArmorStand) takeLocked(slot int) bool {
	return s.DisabledSlots&(int32(1)<<(standFilterBit(slot)+8)) != 0
}

func (s *ArmorStand) putLocked(slot int) bool {
	return s.DisabledSlots&(int32(1)<<(standFilterBit(slot)+16)) != 0
}

// handSlot reports whether a wire slot is one of the two hands.
func (s *ArmorStand) handSlot(slot int) bool {
	return slot == StandMainHand || slot == StandOffHand
}

// HasItemInSlot reports whether one wire slot holds anything.
func (s *ArmorStand) HasItemInSlot(slot int) bool {
	return !s.StackOf(slot).Empty()
}

// SlotForItem is the slot the stand puts a held stack in: the one its
// equippable component names if the stand can use it, else the main hand.
// vanilla: LivingEntity.getEquipmentSlotForItem, ArmorStand.canUseSlot
func (s *ArmorStand) SlotForItem(stack protocol.ItemStack) int {
	item, ok := registry.StackEquippable(stack)
	if !ok {
		return StandMainHand
	}
	slot := StandMainHand
	switch item.Slot {
	case "head":
		slot = StandHead
	case "chest":
		slot = StandChest
	case "legs":
		slot = StandLegs
	case "feet":
		slot = StandFeet
	case "offhand":
		slot = StandOffHand
	}
	if s.SlotLocked(slot) {
		return StandMainHand
	}
	return slot
}

// SwapItem is ArmorStand.swapItem: held and slot stacks trade places, except
// a creative hand copies one item into an empty slot and keeps its own, and
// a survival stack of several puts down one. It returns the hand's stack and
// whether the slot changed; a locked take or put bit leaves it alone.
// vanilla: ArmorStand.swapItem
func (s *ArmorStand) SwapItem(slot int, held protocol.ItemStack, creative bool) (protocol.ItemStack, bool) {
	onStand := s.StackOf(slot)
	if !onStand.Empty() && s.takeLocked(slot) {
		return held, false
	}
	if onStand.Empty() && s.putLocked(slot) {
		return held, false
	}
	if creative && onStand.Empty() && !held.Empty() {
		one := held
		one.Count = 1
		s.SetStack(slot, one)
		return held, true
	}
	if !held.Empty() && held.Count > 1 {
		if !onStand.Empty() {
			return held, false
		}
		one := held
		one.Count = 1
		s.SetStack(slot, one)
		held.Count--
		return held, true
	}
	s.SetStack(slot, held)
	return onStand, true
}

// ClickedSlot is ArmorStand.getClickedSlot: the click height over the
// stand's scale (bands halved when small) picks a body part, returned only
// if that slot holds an item, so a click at the legs of a stand wearing only
// a helmet falls through to a hand. The main hand answers unless only the
// off hand holds something.
// vanilla: ArmorStand.getClickedSlot
func (s *ArmorStand) ClickedSlot(ox, oy, oz float64) int {
	y := oy / s.dimensionScale()
	if y >= 0.1 {
		d := 0.45
		if s.Small {
			d = 0.8
		}
		if y < 0.1+d && s.HasItemInSlot(StandFeet) {
			return StandFeet
		}
	}
	d := 0.0
	if s.Small {
		d = 0.3
	}
	if y >= 0.9+d {
		d2 := 0.7
		if s.Small {
			d2 = 1.0
		}
		if y < 0.9+d2 && s.HasItemInSlot(StandChest) {
			return StandChest
		}
	}
	if y >= 0.4 {
		d3 := 0.8
		if s.Small {
			d3 = 1.0
		}
		if y < 0.4+d3 && s.HasItemInSlot(StandLegs) {
			return StandLegs
		}
	}
	if y >= 1.6 && s.HasItemInSlot(StandHead) {
		return StandHead
	}
	if s.HasItemInSlot(StandMainHand) {
		return StandMainHand
	}
	if !s.HasItemInSlot(StandOffHand) {
		return StandMainHand
	}
	return StandOffHand
}

// StackOf reads one wire slot's stack, empty where it holds nothing.
func (s *ArmorStand) StackOf(slot int) protocol.ItemStack {
	switch slot {
	case StandMainHand:
		return s.MainHand
	case StandOffHand:
		return s.OffHand
	case StandFeet, StandLegs, StandChest, StandHead:
		return s.Worn[slot-StandFeet]
	}
	return protocol.ItemStack{}
}

// SetStack puts stack in one wire slot.
func (s *ArmorStand) SetStack(slot int, stack protocol.ItemStack) {
	switch slot {
	case StandMainHand:
		s.MainHand = stack
	case StandOffHand:
		s.OffHand = stack
	case StandFeet, StandLegs, StandChest, StandHead:
		s.Worn[slot-StandFeet] = stack
	}
}

// StandSlotNames names the six wire slots the way the equipment compound
// of a save does, in wire order.
func StandSlotNames() [6]string { return standSlotNames }

// ArmorStandItem is the item a broken stand drops.
func ArmorStandItem() (int32, bool) {
	return int32(registry.ItemArmorStand), true
}

// ArmorStandPlacementYaw is the yaw a placed stand takes: the placer's own
// turned about to face them, snapped to 45 degrees.
// vanilla: ArmorStandItem.useOn
func ArmorStandPlacementYaw(yaw float32) float32 {
	wrapped := world.WrapDegrees(yaw + 180)
	step := math.Floor(float64(wrapped)*8/360 + 0.5)
	return float32(step * 45)
}

// ArmorStandPlacementBox is the box a stand placed in the cell would fill,
// centred on the cell over its floor.
// vanilla: ArmorStandItem.useOn
func ArmorStandPlacementBox(x, y, z int32) AABB {
	half := ArmorStandWidth / 2
	minX, minY, minZ := float64(x)+0.5-half, float64(y), float64(z)+0.5-half
	return AABB{minX, minY, minZ, minX + ArmorStandWidth, minY + ArmorStandHeight, minZ + ArmorStandWidth}
}

// IgnoresPunch reports whether a player's swing passes the stand by: an
// invisible stand and a marker take no hit, and a hand that may not build
// (adventure) breaks none.
// vanilla: Player.attack, ArmorStand.hurtServer
func (s *ArmorStand) IgnoresPunch(adventure bool) bool {
	return s.Invisible || s.Marker || adventure
}

// RefusesProjectile reports whether a projectile's hurt is refused: an
// invisible stand takes none, a non-building shooter and a mob's shot with
// mob_griefing off are refused, and an invulnerable stand refuses all but a
// creative shooter.
// vanilla: ArmorStand.hurtServer
func (s *ArmorStand) RefusesProjectile(damageType registry.DamageType, creative, noBuild, griefingOff bool) bool {
	return s.Invisible || noBuild || griefingOff || s.InvulnerableTo(damageType, creative)
}

// StandUse is what a right-click on a stand comes to before the swap.
type StandUse uint8

const (
	// StandUsePass falls through to the plain entity interact.
	StandUsePass StandUse = iota
	// StandUseRefused consumes the click with nothing swapped.
	StandUseRefused
	// StandUseSwap trades the held stack with the slot UseSlot names.
	StandUseSwap
)

// UseSlot is ArmorStand.interact's slot choice: a held item goes to its own
// slot, only an empty hand uses the clicked one, falling back to the held
// item's slot if the clicked one is locked. A marker and a name tag fall
// through; an empty hand on an empty slot does nothing; a locked slot, or a
// hand slot without arms, refuses a held item.
// vanilla: ArmorStand.interact
func (s *ArmorStand) UseSlot(held protocol.ItemStack, ox, oy, oz float64) (int, StandUse) {
	if s.Marker || registry.Item(held.ItemID) == registry.ItemNameTag {
		return 0, StandUsePass
	}
	heldSlot := s.SlotForItem(held)
	if held.Empty() {
		slot := s.ClickedSlot(ox, oy, oz)
		if s.SlotLocked(slot) {
			slot = heldSlot
		}
		if !s.HasItemInSlot(slot) {
			return 0, StandUsePass
		}
		return slot, StandUseSwap
	}
	if s.SlotLocked(heldSlot) {
		return 0, StandUseRefused
	}
	return heldSlot, StandUseSwap
}

// EquipmentDrops is what the stand held and wore, in wire-slot order.
// vanilla: ArmorStand.brokenByAnything (PREVENT_EQUIPMENT_DROP)
func (s *ArmorStand) EquipmentDrops() []protocol.ItemStack {
	stacks := make([]protocol.ItemStack, 0, 6)
	for _, slot := range [...]int{
		StandMainHand, StandOffHand,
		StandFeet, StandLegs, StandChest, StandHead,
	} {
		stack := s.StackOf(slot)
		if stack.Empty() || rules.PreventsEquipmentDrop(stack) {
			continue
		}
		stacks = append(stacks, stack)
	}
	return stacks
}

// BreakingParticles is where the plank burst of a stand's end goes: risen
// two thirds of its box, spread a quarter of the box either way.
// vanilla: ArmorStand.showBreakingParticles
func (s *ArmorStand) BreakingParticles() (x, y, z float64, dx, dy, dz float32) {
	minX, minY, _, maxX, maxY, _ := s.BlastBox()
	width, height := maxX-minX, maxY-minY
	return s.X, s.Y + height*2.0/3.0, s.Z, float32(width / 4), float32(height / 4), float32(width / 4)
}

// DispenserEquip puts one piece a dispenser fires at the stand in the slot
// its equippable component names, and reports whether it fit: a bare slot
// the stand's disabled bits and arms allow. The take and put locks guard
// only a player's swap, and no item for the slots a stand has names an
// allowed entity.
// vanilla: LivingEntity.canEquipWithDispenser, ArmorStand.canUseSlot
func (s *ArmorStand) DispenserEquip(stack protocol.ItemStack) bool {
	return s.dispenserEquip(stack, true)
}

// CanDispenserEquip reports whether DispenserEquip would fit the piece, changing nothing.
func (s *ArmorStand) CanDispenserEquip(stack protocol.ItemStack) bool {
	return s.dispenserEquip(stack, false)
}

func (s *ArmorStand) dispenserEquip(stack protocol.ItemStack, commit bool) bool {
	if s.Dead {
		return false
	}
	item, ok := registry.StackEquippable(stack)
	if !ok {
		return false
	}
	slot := -1
	for i, slotName := range standSlotNames {
		if slotName == item.Slot {
			slot = i
		}
	}
	if slot < 0 || s.SlotLocked(slot) || s.HasItemInSlot(slot) {
		return false
	}
	if commit {
		stack.Count = 1
		s.SetStack(slot, stack)
	}
	return true
}

// CapFall is Entity.checkFallDistanceAccumulation.
func (s *ArmorStand) CapFall() { CapFallAccumulation(s.VY, &s.FallDistance) }
