package entity

import (
	"math"
	"math/rand"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/registry"
)

// The mannequin's numbers.
// vanilla: EntityTypes.MANNEQUIN sized(0.6F, 1.8F), clientTrackingRange(32)
const (
	MannequinWidth         = 0.6
	MannequinHeight        = 1.8
	MannequinTrackingRange = 32
	// MannequinHealth is the LivingEntity default max health.
	MannequinHealth = 20
	// MannequinAllLayers is every PlayerModelPart bit, the layers a fresh
	// mannequin shows.
	MannequinAllLayers = 0x7F
)

// Mannequin is a living body with no AI, held by its immovable flag.
// vanilla: Mannequin
type Mannequin struct {
	ID int32
	LivingEffects
	LivingState
	Age, FireTicks, AirSupply, TicksFrozen int
	Attributes                             *player.Attributes
	attrWorn                               [7]protocol.ItemStack
	StandBody
	Yaw               float32
	Pitch             float32
	Health            float64
	MainHand, OffHand protocol.ItemStack
	Worn              [4]protocol.ItemStack // feet, legs, chest, head
	// Dead is the mannequin removed; a dying one has Health 0 and counts
	// DeathTime up until it is.
	Dead bool
	// InvulnerableTime counts down from 20 after a landed hit; above 10 a
	// hit lands only what it adds to LastHurt.
	InvulnerableTime int
	LastHurt         float64
	ArrowCount       int
	removeArrowTime  int
	// Pose is the protocol pose id; only standing, sneaking, swimming,
	// fall flying and sleeping are valid.
	Pose int32
	// LeftHanded is the main hand being the left.
	LeftHanded bool
	// Layers is the shown PlayerModelPart mask.
	Layers byte
	// Profile is the minecraft:profile wire form; empty bytes for none.
	Profile   []byte
	Immovable bool
	NoGravity bool
	// Invulnerable is the Invulnerable tag; see InvulnerableTo.
	Invulnerable bool
	// Label is the custom label, nil for the default; HideLabel drops it.
	Label     *protocol.Text
	HideLabel bool
	// Combust is asked before the body catches fire for ticks and answers the
	// ticks it burns, 0 when cancelled.
	Combust func(ticks int, src CombustSource) int
}

// NewMannequin is a mannequin standing at the given feet position.
func NewMannequin(id int32, x, y, z float64) *Mannequin {
	return &Mannequin{ID: id, X: x, Y: y, Z: z, Health: MannequinHealth, Layers: MannequinAllLayers, AirSupply: rules.MaxAirSupply, Attributes: player.NewAttributes(mannequinAttributeSupplier)}
}

// MannequinDimensions is the hit box width and height of a pose.
// vanilla: Avatar.POSES, LivingEntity.SLEEPING_DIMENSIONS
func MannequinDimensions(pose int32) (width, height float64) {
	switch pose {
	case protocol.PoseSneaking:
		return MannequinWidth, 1.5
	case protocol.PoseSwimming, protocol.PoseFallFlying:
		return MannequinWidth, 0.6
	case protocol.PoseSleeping:
		return 0.2, 0.2
	}
	return MannequinWidth, MannequinHeight
}

// CapFall is Entity.checkFallDistanceAccumulation.
func (m *Mannequin) CapFall() { CapFallAccumulation(m.VY, &m.FallDistance) }

// BlastBox is the hit box of the current pose.
func (m *Mannequin) BlastBox() (minX, minY, minZ, maxX, maxY, maxZ float64) {
	width, height := MannequinDimensions(m.Pose)
	half := width / 2
	return m.X - half, m.Y, m.Z - half, m.X + half, m.Y + height, m.Z + half
}

// Dying is Health run out: the body stays for its death animation.
func (m *Mannequin) Dying() bool { return m.Health <= 0 }

// ArrowKnockback applies a full hit's shove and Punch's extra push.
// vanilla: LivingEntity.dealDefaultKnockback, AbstractArrow.doKnockback
func (m *Mannequin) ArrowKnockback(vx, vz, punch float64, full bool, rng *rand.Rand) {
	scale := rules.KnockbackScale(m.Attributes)
	if scale == 0 {
		return
	}
	if full {
		kx, ky, kz := rules.ArrowDefaultKnockback(vx, vz, scale, m.OnGround, rng)
		m.VX, m.VZ = m.VX/2+kx, m.VZ/2+kz
		if m.OnGround {
			m.VY = math.Min(0.4, m.VY/2+ky)
		}
	}
	px, py, pz := rules.ArrowPunch(vx, vz, punch, scale)
	m.Push(px, py, pz)
}

// InvulnerableTo reports whether the Invulnerable tag refuses a hurt of
// damageType: all but a creative player's and bypasses_invulnerability.
// vanilla: Entity.isInvulnerableToBase
func (m *Mannequin) InvulnerableTo(damageType registry.DamageType, creative bool) bool {
	return m.Invulnerable && !creative && !rules.InvulnerabilityBypassed(damageType)
}

// Hurt lands a hit of amount through the hurt window. landed is false when
// nothing landed; softened when only the excess over the last hit did,
// which tells no damage event and no hurt sound; died when Health reached 0.
// vanilla: LivingEntity.hurtServer, actuallyHurt
func (m *Mannequin) Hurt(amount float64, bypassesCooldown bool) (landed, softened, died bool) {
	return m.HurtBy(amount, registry.DamageTypeGeneric, bypassesCooldown)
}

// HurtBy lands typed damage after the hurt window, armor, resistance and absorption.
func (m *Mannequin) HurtBy(amount float64, typ registry.DamageType, bypassesCooldown bool) (landed, softened, died bool) {
	if m.Dead || m.Dying() || m.RefusesEffectDamage(typ) {
		return false, false, false
	}
	amount = max(amount, 0)
	if m.InvulnerableTime > hurtCooldown && !bypassesCooldown {
		if amount <= m.LastHurt {
			return false, false, false
		}
		amount, m.LastHurt = amount-m.LastHurt, amount
		softened = true
	} else {
		m.LastHurt = amount
		m.InvulnerableTime = invulnerableTime
		m.HurtTime = 10 // vanilla: LivingEntity.hurtServer
	}
	// Armor absorbs the excess after the window has compared the raw hits.
	// vanilla: LivingEntity.getDamageAfterArmorAbsorb
	if !rules.ArmourBypassed(typ) {
		points, toughness := rules.ArmourValue(m.Attributes)
		amount = rules.DamageAfterArmour(amount, points, toughness)
	}
	amount = m.hurtAfterEffects(amount, typ)
	// LivingEntity's health and damage are floats, including successive hurts.
	m.Health = float64(max(float32(m.Health)-float32(amount), 0))
	return true, softened, m.Dying()
}

// MannequinDeathTicks is how long a dead mannequin lies before it is
// removed. vanilla: LivingEntity.tickDeath
const MannequinDeathTicks = 20

// MannequinStep is the removal or hurt a mannequin's tick asks the server to apply.
type MannequinStep struct {
	Remove     bool
	Damage     float64
	DamageType registry.DamageType
}

// Step advances travel, the hurt window and the death animation.
// vanilla: LivingEntity.baseTick, aiStep, tickDeath; Mannequin.isEffectiveAi
func (m *Mannequin) Step(w World) (out MannequinStep) {
	if m.Dead {
		return out
	}
	m.TickTimers()
	rules.StuckStep(&m.ArrowCount, &m.removeArrowTime)
	if m.InvulnerableTime > 0 {
		m.InvulnerableTime--
	}
	if m.Dying() {
		m.DeathTime++
		out.Remove = m.DeathTime >= MannequinDeathTicks
	}
	if m.Y < float64(w.MinY())-64 {
		out.Damage, out.DamageType = rules.VoidDamage, rules.DamageTypeOutOfWorld
	}
	if m.Immovable {
		return out
	}
	width, height := MannequinDimensions(m.Pose)
	m.updateFluids(w, NewAABB(m.BlastBox()), height*0.85)
	if m.water.height > 0 {
		m.FallDistance = 0
	}
	gravity := ArmorStandGravity
	if m.NoGravity {
		gravity = 0
	}
	from := player.Position{X: m.X, Y: m.Y, Z: m.Z}
	landed := m.travel(w, NewAABB(m.BlastBox()), width/2, gravity)
	if rules.FallResetSweep(w, max(m.FallDistance, landed), from, player.Position{X: m.X, Y: m.Y, Z: m.Z}, false, false) {
		m.FallDistance, landed = 0, 0
	}
	if landed > 0 && out.Damage == 0 {
		block := rules.FallOn(rules.OnPos(w, m.X, m.Y, m.Z, width/2))
		out.Damage = rules.FallDamage(landed*block.Scale+block.Offset, m.Attributes.Value(registry.AttributeSafeFallDistance), block.Modifier)
		out.DamageType = block.DamageType
	}
	return out
}

// SyncEquipmentModifiers applies changed equipment to the effective attribute map.
func (m *Mannequin) SyncEquipmentModifiers() {
	syncEquipmentModifiers(m.Attributes, livingEquipmentSlots(m.Worn, m.MainHand, m.OffHand), &m.attrWorn)
}

// EnchEquipment is the armor and hands whose enchantments act on the mannequin.
func (m *Mannequin) EnchEquipment() []rules.EnchPiece {
	return enchEquipment(m.Worn, m.MainHand, m.OffHand)
}

// WearEnchPiece wears an EnchEquipment piece and returns its break event, or zero.
func (m *Mannequin) WearEnchPiece(i int, n int32, rng *rand.Rand) uint8 {
	return wearEnchPiece(&m.Worn, &m.MainHand, &m.OffHand, i, n, rng)
}
