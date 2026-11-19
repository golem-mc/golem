package entity

import (
	"math"

	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/registry"
)

// sulfurProfile is what the absorbed block's matching archetypes add up to.
// vanilla: SulfurCube.collectEquipmentChanges
type sulfurProfile struct {
	knockbackResist, explosionResist, bounciness float64
	friction, drag                               float64
	buoyant                                      bool
	contacts                                     []*registry.SulfurContact
	explosion                                    *registry.SulfurExplosion
	kbHorizontal, kbVertical                     float32
	hitSound, pushSound                          registry.Sound
	pushThreshold, pushCooldown                  float32
}

// sulfurDefaultProfile is SulfurCube's state with no block: the regular
// archetype's sounds and DEFAULT_KNOCKBACK_MODIFIERS.
var sulfurDefaultProfile = sulfurProfile{
	friction: 1, drag: 1, kbHorizontal: 0.33, kbVertical: 0.06,
	hitSound: registry.SoundEntitySulfurCubeRegularHit, pushSound: registry.SoundEntitySulfurCubeRegularPush,
	pushThreshold: 0.2, pushCooldown: 0.5,
}

// sulfurMatches lists the archetypes whose items hold the item, in registry
// order.
// vanilla: SulfurCube.matchingArchetypes
func sulfurMatches(item int32, fn func(a *registry.SulfurArchetype)) {
	list := registry.SulfurArchetypes()
	for i := range list {
		if list[i].Matches(registry.Item(item)) {
			fn(&list[i])
		}
	}
}

// sulfurProf is the profile of the cube's absorbed block, cached per item.
func (m *Mob) sulfurProf() *sulfurProfile {
	key := int32(0)
	if m.SulfurCubeFilled() {
		key = m.Body.ItemID + 1
	}
	if m.sulfur.prof != nil && m.sulfur.profKey == key {
		return m.sulfur.prof
	}
	p := sulfurDefaultProfile
	if key != 0 {
		sulfurMatches(m.Body.ItemID, func(a *registry.SulfurArchetype) {
			for _, mod := range a.Modifiers {
				p.addModifier(mod)
			}
			p.buoyant = p.buoyant || a.Buoyant
			if a.Explosion != nil {
				p.explosion = a.Explosion
			}
			if a.Contact != nil {
				p.contacts = append(p.contacts, a.Contact)
			}
			p.kbHorizontal, p.kbVertical = a.KnockbackHorizontal, a.KnockbackVertical
			p.hitSound = a.HitSound
			p.pushSound = a.PushSound
			p.pushThreshold, p.pushCooldown = a.PushThreshold, a.PushCooldown
		})
	}
	p.knockbackResist = math.Max(-2, math.Min(1, p.knockbackResist))
	p.explosionResist = math.Max(0, math.Min(1, p.explosionResist))
	p.bounciness = math.Max(0, p.bounciness)
	m.sulfur.prof, m.sulfur.profKey = &p, key
	return &p
}

// addModifier folds the archetype modifiers the cube's physics reads into the
// profile; the rest only reach the attribute map.
func (p *sulfurProfile) addModifier(m registry.SulfurModifier) {
	switch {
	case m.Operation == registry.AttributeAddValue && m.Attribute == registry.AttributeKnockbackResistance:
		p.knockbackResist += m.Amount
	case m.Operation == registry.AttributeAddValue && m.Attribute == registry.AttributeExplosionKnockbackResistance:
		p.explosionResist += m.Amount
	case m.Operation == registry.AttributeAddValue && m.Attribute == registry.AttributeBounciness:
		p.bounciness += m.Amount
	case m.Operation == registry.AttributeAddMultipliedTotal && m.Attribute == registry.AttributeFrictionModifier:
		p.friction *= 1 + m.Amount
	case m.Operation == registry.AttributeAddMultipliedTotal && m.Attribute == registry.AttributeAirDragModifier:
		p.drag *= 1 + m.Amount
	}
}

// sulfurFloat lifts a floating cube holding a buoyant block toward the
// surface of the fluid it is in.
// vanilla: SulfurCube.travelInFluid
func (m *Mob) sulfurFloat() {
	if !m.SulfurCubeFilled() || !m.sulfurProf().buoyant {
		return
	}
	height := m.lavaFluid.height
	if m.InWater() {
		height = m.waterFluid.height
	}
	vibe := 0.2 * float64(float32(math.Sin(float64(float32(m.Age)*0.4))))
	if immersion := height - m.fluidJumpThreshold() + vibe; immersion > 0 {
		m.VY += math.Min(1, immersion) * float64(float32(0.04))
	}
}

// syncSulfurModifiers puts the matching archetypes' modifiers on the map and
// takes the previous block's off, when the block or the map changed.
// vanilla: SulfurCube.collectEquipmentChanges
func (m *Mob) syncSulfurModifiers(a *player.Attributes) {
	key := int32(0)
	if m.SulfurCubeFilled() {
		key = m.Body.ItemID + 1
	}
	if m.sulfur.modsMap == a && m.sulfur.modsKey == key {
		return
	}
	m.sulfur.modsMap, m.sulfur.modsKey = a, key
	for _, mod := range m.sulfur.mods {
		a.RemoveModifier(mod.Attribute, mod.ID)
	}
	m.sulfur.mods = m.sulfur.mods[:0]
	if key == 0 {
		return
	}
	sulfurMatches(m.Body.ItemID, func(arch *registry.SulfurArchetype) {
		for _, mod := range arch.Modifiers {
			a.AddModifier(mod.Attribute, player.Modifier{ID: mod.ID, Amount: mod.Amount, Operation: mod.Operation})
			m.sulfur.mods = append(m.sulfur.mods, mod)
		}
	})
}

// ExplosionKnockbackResistance is the share of a blast's push the body
// shrugs off: its attribute map's (gear's without one), and a cube's archetype.
// vanilla: Attributes.EXPLOSION_KNOCKBACK_RESISTANCE
func (m *Mob) ExplosionKnockbackResistance() float64 {
	if m.attrs != nil {
		// The map carries the worn pieces' modifiers and a cube's archetype.
		return math.Max(0, math.Min(1, m.attrs.Value(rules.ExplosionKnockbackResistanceAttribute)))
	}
	v := rules.EquipmentAttributeValue(m.EnchEquipment(), rules.ExplosionKnockbackResistanceAttribute)
	if m.Type == TypeSulfurCube {
		v = math.Max(0, math.Min(1, v+m.sulfurProf().explosionResist))
	}
	return v
}
