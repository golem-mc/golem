package entity

import (
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// Saved ids for native attributes and rolled spawn modifiers.
// vanilla: Attributes.SPAWN_REINFORCEMENTS_CHANCE, Zombie, Mob.RANDOM_SPAWN_BONUS_ID
const (
	attrArmor           = "minecraft:armor"
	attrMaxHealth       = "minecraft:max_health"
	attrReinforcements  = "minecraft:spawn_reinforcements"
	attrKnockbackResist = "minecraft:knockback_resistance"
	attrFollowRange     = "minecraft:follow_range"

	modLeaderBonus    = "minecraft:leader_zombie_bonus"
	modCallerCharge   = "minecraft:reinforcement_caller_charge"
	modCalleeCharge   = "minecraft:reinforcement_callee_charge"
	modRandomSpawn    = "minecraft:random_spawn_bonus"
	modZombieSpawn    = "minecraft:zombie_random_spawn_bonus"
	opAddValue        = "add_value"
	opAddMultiplyTot  = "add_multiplied_total"
	opAddMultiplyBase = "add_multiplied_base"
)

// zombieModelledAttributes are the attributes the zombie traits write into
// the saved list themselves.
var zombieModelledAttributes = map[string]bool{
	attrReinforcements: true, attrKnockbackResist: true, attrFollowRange: true, attrMaxHealth: true,
}

// traitAttributes are the attribute ids the kind's save writes from its own
// traits (a list holding only these wakes no map); nil for none. The map is
// shared and must not be changed.
func traitAttributes(t MobType) map[string]bool {
	switch {
	case Family(t) == TypeZombie:
		return zombieModelledAttributes
	}
	return nil
}

// wolfTamingBase reports whether a saved max_health entry is only the base
// the wolf's taming sets: no modifiers, and base 40 on a tame wolf. A wild
// wolf's base is reset to 8 whatever was saved, so any base counts; a tame
// wolf keeps a saved base, so another one is an edit.
// vanilla: TamableAnimal.readAdditionalSaveData (setTame(true, false) skips
// Wolf.applyTamingSideEffects; setTame(false, true) runs it)
func wolfTamingBase(m *Mob, c nbt.Compound) bool {
	if mods, ok := nbt.Get[nbt.List](c, "modifiers"); ok && len(mods.Items) > 0 {
		return false
	}
	return !m.Tamable.Tame || nbt.DoubleFieldOr(c, "base", -1) == m.MaxHealth
}

// wolfAttributesTag is a wolf's "attributes" list with the maximum health
// merged in: 8 wild, 40 tame.
// vanilla: Wolf.applyTamingSideEffects sets the base either way, so
// AttributeMap.pack writes it.
func wolfAttributesTag(m *Mob, kept nbt.Tag) nbt.List {
	base := m.MaxHealth
	return mergeTraits(kept, []*attrTraits{{id: attrMaxHealth, base: &base}}, nil)
}

// attrTraits is what this server models of an attribute: its owned
// modifiers (written only when nonzero), their ids, and a rolled base.
type attrTraits struct {
	id      string
	base    *float64
	owned   map[string]bool
	written []nbt.Tag
}

// entry is a new saved attribute, nil where there is nothing to write.
func (a *attrTraits) entry(defaultBase float64) *nbt.OrderedCompound {
	if len(a.written) == 0 && a.base == nil {
		return nil
	}
	base := defaultBase
	if a.base != nil {
		base = *a.base
	}
	return a.merge(nbt.Compound{"id": nbt.String(a.id), "base": nbt.Double(base)})
}

// merge puts this attribute's traits into a loaded entry, leaving the
// modifiers it does not own where they were.
func (a *attrTraits) merge(c nbt.Compound) *nbt.OrderedCompound {
	base, _ := nbt.NumberValue(c["base"])
	if a.base != nil {
		base = *a.base
	}
	mods := nbt.List{Elem: nbt.TypeCompound}
	if old, ok := nbt.Get[nbt.List](c, "modifiers"); ok {
		for _, it := range old.Items {
			if mc, ok := nbt.CompoundView(it); ok {
				if id, _ := nbt.Get[nbt.String](mc, "id"); a.owned[registry.Qualify(string(id))] {
					continue
				}
				id, _ := nbt.Get[nbt.String](mc, "id")
				op, _ := nbt.Get[nbt.String](mc, "operation")
				amount, _ := nbt.NumberValue(mc["amount"])
				it = player.AttributeModifierTag(registry.Qualify(string(id)), amount, string(op))
			}
			mods.Items = append(mods.Items, it)
		}
	}
	mods.Items = append(mods.Items, a.written...)
	return player.AttributeTag(a.id, base, mods.Items)
}

// zombieAttributesTag is a zombie-family body's "attributes" list with the
// modelled attributes merged into the loaded entries. Caller and callee
// charges save as one caller charge.
// vanilla: LivingEntity.addAdditionalSaveData, AttributeInstance.Packed,
// Zombie.hurtServer (the caller charge exists only after a call)
func zombieAttributesTag(m *Mob, kept nbt.Tag) nbt.List {
	t := m.Traits()
	nonzero := (*attrTraits).nonzero
	reinforce := &attrTraits{id: attrReinforcements, owned: map[string]bool{modLeaderBonus: true, modCallerCharge: true, modCalleeCharge: true}}
	if t.ReinforceBase != 0 {
		reinforce.base = &t.ReinforceBase
	}
	nonzero(reinforce, modLeaderBonus, t.LeaderBonus, opAddValue)
	nonzero(reinforce, modCallerCharge, t.Charge, opAddValue)
	knock := &attrTraits{id: attrKnockbackResist, owned: map[string]bool{modRandomSpawn: true}}
	nonzero(knock, modRandomSpawn, t.KnockbackResist, opAddValue)
	follow := m.followTraits()
	follow.owned[modZombieSpawn] = true
	nonzero(follow, modZombieSpawn, t.FollowBonus, opAddMultiplyTot)
	health := &attrTraits{id: attrMaxHealth, owned: map[string]bool{modLeaderBonus: true}}
	if t.LeaderHealth != 0 {
		base := m.MaxHealth / (1 + t.LeaderHealth)
		health.base = &base
		nonzero(health, modLeaderBonus, t.LeaderHealth, opAddMultiplyTot)
	}
	ours := []*attrTraits{reinforce, knock, follow, health}
	defaults := map[string]float64{attrReinforcements: 0, attrKnockbackResist: 0, attrFollowRange: m.BaseFollowRange(), attrMaxHealth: m.MaxHealth}
	return mergeTraits(kept, ours, defaults)
}

// spawnBonusAttributes is a body's "attributes" list (kept, as the kind's
// save left it) with Mob.finalizeSpawn's follow_range bonus merged in; false
// when there is no list to write.
// vanilla: LivingEntity.addAdditionalSaveData, Mob.finalizeSpawn
func spawnBonusAttributes(m *Mob, kept nbt.Tag) (nbt.List, bool) {
	list := mergeTraits(kept, []*attrTraits{m.followTraits()}, map[string]float64{attrFollowRange: m.BaseFollowRange()})
	return list, len(list.Items) > 0
}

// followTraits is the follow_range entry of Mob.finalizeSpawn's bonus.
func (m *Mob) followTraits() *attrTraits {
	follow := &attrTraits{id: attrFollowRange, owned: map[string]bool{modRandomSpawn: true}}
	follow.nonzero(modRandomSpawn, m.followSpawnBonus, opAddMultiplyBase)
	return follow
}

// nonzero writes an owned modifier unless its amount is 0.
func (a *attrTraits) nonzero(id string, amount float64, op string) {
	if amount != 0 {
		a.written = append(a.written, player.AttributeModifierTag(id, amount, op))
	}
}

// mergeTraits merges the modelled attributes into the kept "attributes"
// list, appending an entry for each one the list lacks; defaults holds the
// base of a new entry whose trait rolls none.
func mergeTraits(kept nbt.Tag, ours []*attrTraits, defaults map[string]float64) nbt.List {
	list := nbt.List{Elem: nbt.TypeCompound}
	done := map[string]bool{}
	if old, ok := kept.(nbt.List); ok {
		for _, it := range old.Items {
			c, isC := nbt.CompoundView(it)
			if !isC {
				list.Items = append(list.Items, it)
				continue
			}
			id, _ := nbt.Get[nbt.String](c, "id")
			merged := false
			for _, a := range ours {
				if a.id == registry.Qualify(string(id)) {
					list.Items = append(list.Items, a.merge(c))
					done[a.id] = true
					merged = true
				}
			}
			if !merged {
				list.Items = append(list.Items, it)
			}
		}
	}
	for _, a := range ours {
		if done[a.id] {
			continue
		}
		if e := a.entry(defaults[a.id]); e != nil {
			list.Items = append(list.Items, e)
		}
	}
	return list
}

// zombieTraitsOfTag reads the modelled attributes from the "attributes"
// list; false when the list is absent.
func zombieTraitsOfTag(compound nbt.Compound) (t ZombieTraits, ok bool) {
	list, ok := nbt.Get[nbt.List](compound, "attributes")
	if !ok {
		return t, false
	}
	for _, it := range list.Items {
		c, isC := nbt.CompoundView(it)
		if !isC {
			continue
		}
		id, _ := nbt.Get[nbt.String](c, "id")
		amount := func(mod string) float64 { return modifierAmount(c, mod) }
		switch registry.Qualify(string(id)) {
		case attrReinforcements:
			t.ReinforceBase = nbt.DoubleFieldOr(c, "base", 0)
			t.LeaderBonus = amount(modLeaderBonus)
			t.Charge = amount(modCallerCharge) + amount(modCalleeCharge)
		case attrKnockbackResist:
			t.KnockbackResist = amount(modRandomSpawn)
		case attrFollowRange:
			t.FollowBonus = amount(modZombieSpawn)
		case attrMaxHealth:
			t.LeaderHealth = amount(modLeaderBonus)
		}
	}
	return t, true
}

// modifierAmount is the amount of a saved attribute entry's modifier, 0
// when it has none by that id.
func modifierAmount(entry nbt.Compound, mod string) float64 {
	mods, _ := nbt.Get[nbt.List](entry, "modifiers")
	for _, mi := range mods.Items {
		if mc, ok := nbt.CompoundView(mi); ok {
			if mid, _ := nbt.Get[nbt.String](mc, "id"); registry.Qualify(string(mid)) == mod {
				return nbt.DoubleFieldOr(mc, "amount", 0)
			}
		}
	}
	return 0
}

// followRangeEntry is the "attributes" list's follow_range entry; false for
// none.
func followRangeEntry(compound nbt.Compound) (nbt.Compound, bool) {
	return savedAttributeEntry(compound, attrFollowRange)
}

// savedAttributeEntry finds an attribute in the saved list, accepting unqualified ids.
func savedAttributeEntry(compound nbt.Compound, name string) (nbt.Compound, bool) {
	list, _ := nbt.Get[nbt.List](compound, "attributes")
	for _, it := range list.Items {
		if c, ok := nbt.CompoundView(it); ok {
			if id, _ := nbt.Get[nbt.String](c, "id"); registry.Qualify(string(id)) == name {
				return c, true
			}
		}
	}
	return nil, false
}

// readRolledAttributes restores native attributes before an edited map is built.
func (m *Mob) readRolledAttributes(compound nbt.Compound) {
	if IsEquine(m.Type) {
		readHorseAttributes(m, compound)
	}
	if m.IsShulker() {
		m.readShulkerArmor(compound)
	}
	if follow, ok := followRangeEntry(compound); ok {
		m.followSpawnBonus = modifierAmount(follow, modRandomSpawn)
	}
}

// onlySpawnBonus reports whether a saved follow_range entry is what
// spawnBonusAttributes writes: the kind's base and no modifier but
// Mob.finalizeSpawn's.
func onlySpawnBonus(entry nbt.Compound, base float64) bool {
	if nbt.DoubleFieldOr(entry, "base", base) != base {
		return false
	}
	mods, _ := nbt.Get[nbt.List](entry, "modifiers")
	for _, mi := range mods.Items {
		mc, _ := nbt.CompoundView(mi)
		if id, _ := nbt.Get[nbt.String](mc, "id"); registry.Qualify(string(id)) != modRandomSpawn {
			return false
		}
	}
	return true
}

// horseAttributesTag saves only the horse attributes that have instances.
// vanilla: AttributeMap.pack
func horseAttributesTag(m *Mob) nbt.List {
	entry := func(id string, base float64) nbt.Tag {
		return player.AttributeTag(id, base, nil)
	}
	list := nbt.List{Elem: nbt.TypeCompound}
	if m.Horse.healthSet {
		list.Items = append(list.Items, entry("minecraft:max_health", m.MaxHealth))
	}
	list.Items = append(list.Items, entry("minecraft:movement_speed", m.Horse.Speed))
	if m.Horse.jumpSet {
		list.Items = append(list.Items, entry("minecraft:jump_strength", m.Horse.Jump))
	}
	return list
}

// readHorseAttributes reads a horse's rolled health, pace and jump back.
func readHorseAttributes(m *Mob, compound nbt.Compound) {
	list, ok := nbt.Get[nbt.List](compound, "attributes")
	if !ok {
		return
	}
	for _, it := range list.Items {
		c, isC := nbt.CompoundView(it)
		if !isC {
			continue
		}
		id, _ := nbt.Get[nbt.String](c, "id")
		base := nbt.DoubleFieldOr(c, "base", -1)
		if base <= 0 {
			continue
		}
		switch registry.Qualify(string(id)) {
		case "minecraft:max_health":
			m.Horse.healthSet = true
			m.MaxHealth = base
			m.syncMaxHealth()
		case "minecraft:movement_speed":
			m.Horse.Speed = base
		case "minecraft:jump_strength":
			m.Horse.jumpSet = true
			m.Horse.Jump = base
		}
	}
}
