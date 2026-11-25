package entity

import (
	"math"
	"math/rand"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
)

// Powder snow freezing: vanilla's FREEZE inside-block counter and
// LivingEntity.aiStep's hurt, which the server turns into frozen metadata
// and damage events.

// freezeStep runs a tick of freezing: +1 while the box shares a cell with
// powder snow and nothing prevents it (capped at full), -2 outside (floored
// at 0). A fully frozen body is hurt every 40th tick of its age: 1, or 5 for
// freeze_hurts_extra_types (here only the magma cube). It runs after the
// block pass, so a fire lit this tick has already cleared the counter.
// vanilla: InsideBlockEffectType.FREEZE, LivingEntity.aiStep
func (m *Mob) freezeStep(w World, scan *contactScan, events []MobEvent) []MobEvent {
	inSnow := m.touches(w, scan, contactPowderSnow) && m.inPowderSnow(w)
	m.SetMetadataTicksFrozen(rules.FreezeTicksStep(m.TicksFrozen, inSnow, inSnow && m.canFreeze()))
	m.wasInPowderSnow, m.lastInPowderSnow = m.lastInPowderSnow, inSnow
	// The frost slow's ground check is only read while frozen.
	if m.TicksFrozen > 0 {
		m.underfootAir = rules.FrostSlowAir(w, m.X, m.Y, m.Z, m.width()/2, m.OnGround)
	}
	if m.TicksFrozen < rules.FullyFrozenTicks || m.Age%rules.FreezeHurtTicks != 0 {
		return events
	}
	if landed, soft := m.plainHurt(rules.FreezeHurt(false), rules.DamageTypeFreeze, false); landed {
		m.noteLastDamage(rules.DamageTypeFreeze)
		if !soft {
			events = append(events, MobEvent{Mob: m, Kind: MobBurned,
				DamageType: rules.DamageTypeFreeze, Softened: soft})
		}
	}
	return events
}

// Skeleton.tick's powder snow conversion: 140 ticks in the snow to start,
// 300 more to finish.
const (
	strayInSnowTicks       = 140
	strayConversionTicks   = 300
	strayNotConvertingTime = -1
)

// freezeConversionStep is Skeleton.tick, reading the last tick's powder-snow
// result, so leaving the snow cancels at once. It returns the kind the body
// becomes this tick, or empty.
// vanilla: Skeleton.tick
func (m *Mob) freezeConversionStep() MobType {
	if m.Type != TypeSkeleton {
		return NoMobType
	}
	if !m.lastInPowderSnow {
		m.inPowderSnowTime = strayNotConvertingTime
		m.SetMetadataStrayConverting(false)
		return NoMobType
	}
	if m.StrayConverting {
		m.StrayConversionTime--
		if m.StrayConversionTime < 0 {
			return TypeStray
		}
		return NoMobType
	}
	m.inPowderSnowTime++
	if m.inPowderSnowTime >= strayInSnowTicks {
		m.SetMetadataStrayConverting(true)
		m.StrayConversionTime = strayConversionTicks
	}
	return NoMobType
}

// Combuster is an entity that set a body alight, carried through
// CombustSource without importing package plugin.
type Combuster interface {
	UUID() uuid.UUID
}

// CombustSource identifies the block or entity that set a body alight.
type CombustSource struct {
	HasBlock bool
	X, Y, Z  int32
	By       Combuster
}

// IgniteForTicks raises the burn to at least ticks, scaled by its
// burning_time, and thaws the body.
// vanilla: LivingEntity.igniteForTicks
func (m *Mob) IgniteForTicks(ticks int) {
	m.IgniteForTicksBy(ticks, CombustSource{})
}

// IgniteForTicksBy is IgniteForTicks with the block or entity that caused it.
// vanilla: LivingEntity.igniteForTicks
func (m *Mob) IgniteForTicksBy(ticks int, src CombustSource) {
	burning := 0.0
	if m.attrs != nil {
		burning = m.attrs.Value(rules.BurningTimeAttribute)
	} else {
		burning = rules.EquipmentAttributeValue(m.EnchEquipment(), rules.BurningTimeAttribute)
	}
	ticks = rules.ScaledIgniteTicks(ticks, burning)
	if ticks > m.FireTicks && m.Combust != nil {
		ticks = m.Combust(m, ticks, src)
	}
	if ticks > m.FireTicks {
		m.SetMetadataFireTicks(ticks)
	}
	m.SetMetadataTicksFrozen(0)
}

// canFreeze reports whether powder snow can freeze the body: not a
// freeze_immune_entity_types kind (the stray and the snow golem), not the plain skeleton, and not wearing freeze_immune_wearables in an armour slot (hands do
// not count).
// vanilla: Entity.canFreeze, LivingEntity.canFreeze, Skeleton.canFreeze
func (m *Mob) canFreeze() bool {
	if m.Type == TypeSkeleton { // not is(): bogged, parched and wither skeleton freeze
		return false
	}
	if m.SulfurCubeFilled() || rules.FreezeImmuneWearables(m.Worn[:]...) {
		return false
	}
	_, ok := m.typeIDOK()
	return !ok || !m.inTag(registry.EntityTypeTagIDFreezeImmuneEntityTypes)
}

// inPowderSnow reports whether the box shares a cell with powder snow,
// which vanilla's block pass turns into isInPowderSnow.
// vanilla: PowderSnowBlock.entityInside
func (m *Mob) inPowderSnow(w World) bool {
	inSnow := false
	m.mobBox().insideCells(func(x, y, z int32) {
		if rules.IsPowderSnow(w.BlockAt(x, y, z)) {
			inSnow = true
		}
	})
	return inSnow
}

// EnchEquipment is every piece whose modifiers act on the mob: the four
// armour slots and both hands.
// vanilla: LivingEntity.collectEquipmentChanges
func (m *Mob) EnchEquipment() []rules.EnchPiece {
	return enchEquipment(m.Worn, m.MainHand, m.OffHand)
}

// enchEquipment lists the armor and hand pieces in their shared wear order.
func enchEquipment(worn [4]protocol.ItemStack, main, off protocol.ItemStack) []rules.EnchPiece {
	pieces := make([]rules.EnchPiece, 0, len(worn)+2)
	for i, name := range ArmorSlotNames {
		pieces = append(pieces, rules.EnchPiece{Stack: worn[i], Slot: name})
	}
	return append(pieces,
		rules.EnchPiece{Stack: main, Slot: "mainhand"},
		rules.EnchPiece{Stack: off, Slot: "offhand"})
}

// enchPieceBreak is the break Entity Event of each EnchEquipment piece.
// vanilla: LivingEntity.entityEventForEquipmentBreak
var enchPieceBreak = [...]uint8{
	protocol.EntityStatusBreakFeet, protocol.EntityStatusBreakLegs,
	protocol.EntityStatusBreakChest, protocol.EntityStatusBreakHead,
	protocol.EntityStatusBreakMainHand, protocol.EntityStatusBreakOffHand,
}

// WearEnchPiece wears EnchEquipment piece i by n points before Unbreaking,
// clearing it when it wears through, and returns that break's Entity Event
// status, 0 while it holds.
// vanilla: ChangeItemDamage.apply, ItemStack.hurtAndBreak
func (m *Mob) WearEnchPiece(i int, n int32) uint8 {
	m.equipmentRevision++
	return wearEnchPiece(&m.Worn, &m.MainHand, &m.OffHand, i, n, m.Rand)
}

// wearEnchPiece applies durability loss to the armor or hand piece.
func wearEnchPiece(worn *[4]protocol.ItemStack, main, off *protocol.ItemStack, i int, n int32, rng *rand.Rand) uint8 {
	slot := main
	switch {
	case i < len(worn):
		slot = &worn[i]
	case i == len(worn)+1:
		slot = off
	}
	updated, broke := rules.Wear(*slot, rules.ProcessDurabilityChange(*slot, n, rng))
	*slot = updated
	if !broke {
		return 0
	}
	return enchPieceBreak[i]
}

// inRain reports whether rain lands on the body.
func (m *Mob) inRain(w World) bool { return rainLandsOn(w, m.X, m.Y, m.Z, m.height()) }

// rainLandsOn reports rain at the body's feet or box top (open sky and a
// raining biome).
// vanilla: Entity.isInRain, Level.isRainingAt
func rainLandsOn(w World, bx, by, bz, height float64) bool {
	x, z := int32(math.Floor(bx)), int32(math.Floor(bz))
	feetY := int32(math.Floor(by))
	if w.RainingAt(x, feetY, z) {
		return true
	}
	topY := int32(math.Floor(by + height))
	return topY != feetY && w.RainingAt(x, topY, z)
}

// collideCtx is what powder snow's collision reads of this body: its fall,
// and whether it walks on snow (a powder_snow_walkable_mobs kind or leather
// boots).
// vanilla: PowderSnowBlock.canEntityWalkOnPowderSnow
func (m *Mob) collideCtx() collideCtx {
	walker := false
	if m.walksOnPowderSnowByKind() {
		walker = true
	} else {
		walker = registry.Item(m.Worn[wornFeet].ItemID) == registry.ItemLeatherBoots && m.Worn[wornFeet].Count > 0
	}
	return collideCtx{fallDistance: m.FallDistance, walker: walker, lavaWalker: m.kind().walksOnLava}
}
