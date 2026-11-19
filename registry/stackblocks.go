package registry

import (
	"strings"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/nbt"
)

// StackBlocksAttacks returns the stack's blocks_attacks: its own component
// when it carries one, else the item's default. False for a stack that
// blocks nothing, including one whose patch removes the component.
// vanilla: ItemStack.get(DataComponents.BLOCKS_ATTACKS)
func StackBlocksAttacks(stack protocol.ItemStack) (BlocksAttacks, bool) {
	if stack.Removes(protocol.DataComponentBlocksAttacks) {
		return BlocksAttacks{}, false
	}
	if len(stack.Raw) > 0 {
		if own, ok := stack.BlocksAttacksOf(); ok {
			if b, ok := resolveBlocksAttacks(own); ok {
				return b, true
			}
		}
	}
	if def := ItemBlocksAttacks(stack.ItemID); def != nil {
		return *def, true
	}
	return BlocksAttacks{}, false
}

// StackWeapon returns the stack's weapon component: its own, else the
// item's. False for a stack that is no weapon, including one whose patch
// removes the component.
// vanilla: ItemStack.get(DataComponents.WEAPON)
func StackWeapon(stack protocol.ItemStack) (Weapon, bool) {
	if stack.Removes(protocol.DataComponentWeapon) {
		return Weapon{}, false
	}
	if len(stack.Raw) > 0 {
		if own, ok := stack.WeaponOf(); ok {
			return Weapon{DamagePerAttack: own.DamagePerAttack, DisableBlockingSeconds: own.DisableBlockingSeconds}, true
		}
	}
	if def := ItemWeapon(stack.ItemID); def != nil {
		return *def, true
	}
	return Weapon{}, false
}

// StackDamagePerAttack is the wear one landed hit costs the stack, zero for
// one that is no weapon.
func StackDamagePerAttack(stack protocol.ItemStack) int32 {
	w, _ := StackWeapon(stack)
	return w.DamagePerAttack
}

func resolveBlocksAttacks(own protocol.BlocksAttacksComponent) (BlocksAttacks, bool) {
	b := BlocksAttacks{
		BlockDelaySeconds:    own.BlockDelaySeconds,
		DisableCooldownScale: own.DisableCooldownScale,
		ItemDamage:           ItemDamageFunction{own.ItemDamage[0], own.ItemDamage[1], own.ItemDamage[2]},
	}
	for _, d := range own.Reductions {
		set, ok := resolveDamageSet(d.Type)
		if !ok {
			return BlocksAttacks{}, false
		}
		b.Reductions = append(b.Reductions, DamageReduction{Angle: d.Angle, Type: set, Base: d.Base, Factor: d.Factor})
	}
	var ok bool
	if b.BypassedBy, ok = resolveDamageSet(own.BypassedBy); !ok {
		return BlocksAttacks{}, false
	}
	b.BlockSound, b.DisableSound = own.BlockSound, own.DisableSound
	return b, true
}

func resolveDamageSet(own protocol.DamageSetComponent) (*DamageTypeSet, bool) {
	if !own.Present {
		return nil, true
	}
	if own.Tag != "" {
		return &DamageTypeSet{Tag: DamageTypeTagID(own.TagID), IsTag: true}, true
	}
	count := len(damageTypesNow.Load().names) - 1
	set := &DamageTypeSet{Types: make([]DamageType, 0, len(own.IDs))}
	for _, id := range own.IDs {
		if id < 0 || int(id) >= count {
			return nil, false
		}
		set.Types = append(set.Types, DamageType(id+1))
	}
	return set, true
}

// blocksAttacksOf reads a minecraft:blocks_attacks value in the codec's
// shape: every field optional but a reduction's and the item damage's own.
// vanilla: BlocksAttacks.CODEC
func blocksAttacksOf(value nbt.Tag) (protocol.BlocksAttacksComponent, bool) {
	entry, ok := nbt.CompoundView(value)
	if !ok {
		return protocol.BlocksAttacksComponent{}, false
	}
	b := protocol.BlocksAttacksComponent{DisableCooldownScale: 1, ItemDamage: [3]float32{1, 0, 1}}
	for _, f := range []struct {
		key string
		dst *float32
	}{{"block_delay_seconds", &b.BlockDelaySeconds}, {"disable_cooldown_scale", &b.DisableCooldownScale}} {
		if field, has := entry[f.key]; has {
			v, ok := floatOf(field)
			if !ok || v < 0 {
				return protocol.BlocksAttacksComponent{}, false
			}
			*f.dst = v
		}
	}
	if field, has := entry["damage_reductions"]; has {
		list, ok := field.(nbt.List)
		if !ok {
			return protocol.BlocksAttacksComponent{}, false
		}
		for _, item := range list.Items {
			d, ok := damageReductionOf(item)
			if !ok {
				return protocol.BlocksAttacksComponent{}, false
			}
			b.Reductions = append(b.Reductions, d)
		}
	} else {
		b.Reductions = []protocol.DamageReductionComponent{{Angle: 90, Factor: 1}}
	}
	if field, has := entry["item_damage"]; has {
		fn, ok := nbt.CompoundView(field)
		if !ok {
			return protocol.BlocksAttacksComponent{}, false
		}
		for i, key := range []string{"threshold", "base", "factor"} {
			v, ok := floatOf(fn[key])
			if !ok || (i == 0 && v < 0) {
				return protocol.BlocksAttacksComponent{}, false
			}
			b.ItemDamage[i] = v
		}
	}
	if field, has := entry["bypassed_by"]; has {
		if b.BypassedBy, ok = damageSetOf(field); !ok {
			return protocol.BlocksAttacksComponent{}, false
		}
	}
	for key, dst := range map[string]**protocol.SoundHolder{"block_sound": &b.BlockSound, "disabled_sound": &b.DisableSound} {
		if field, has := entry[key]; has {
			sound, ok := soundHolderOf(field)
			if !ok {
				return protocol.BlocksAttacksComponent{}, false
			}
			*dst = &sound
		}
	}
	return b, true
}

func damageReductionOf(value nbt.Tag) (protocol.DamageReductionComponent, bool) {
	entry, ok := nbt.CompoundView(value)
	if !ok {
		return protocol.DamageReductionComponent{}, false
	}
	d := protocol.DamageReductionComponent{Angle: 90}
	if field, has := entry["horizontal_blocking_angle"]; has {
		if d.Angle, ok = floatOf(field); !ok || d.Angle <= 0 {
			return protocol.DamageReductionComponent{}, false
		}
	}
	if field, has := entry["type"]; has {
		if d.Type, ok = damageSetOf(field); !ok {
			return protocol.DamageReductionComponent{}, false
		}
	}
	if d.Base, ok = floatOf(entry["base"]); !ok {
		return protocol.DamageReductionComponent{}, false
	}
	if d.Factor, ok = floatOf(entry["factor"]); !ok {
		return protocol.DamageReductionComponent{}, false
	}
	return d, true
}

// damageSetOf reads a damage type holder set: a "#tag", a name or a list of
// names. vanilla: RegistryCodecs.homogeneousList
func damageSetOf(value nbt.Tag) (protocol.DamageSetComponent, bool) {
	set := protocol.DamageSetComponent{Present: true}
	names := []nbt.Tag{value}
	if list, ok := value.(nbt.List); ok {
		names = list.Items
	}
	if s, ok := value.(nbt.String); ok && strings.HasPrefix(string(s), "#") {
		set.Tag = Qualify(string(s[1:]))
		return set, protocol.ValidIdentifier(set.Tag) && DamageTypeTagExists(set.Tag)
	}
	reg, _ := Get("minecraft:damage_type")
	set.IDs = make([]int32, 0, len(names))
	for _, n := range names {
		s, ok := n.(nbt.String)
		if !ok {
			return protocol.DamageSetComponent{}, false
		}
		id := reg.ID(Qualify(string(s)))
		if id < 0 {
			return protocol.DamageSetComponent{}, false
		}
		set.IDs = append(set.IDs, id)
	}
	return set, true
}

// blocksAttacksTag writes a minecraft:blocks_attacks in the shape
// blocksAttacksOf reads, every field spelled out.
func blocksAttacksTag(b protocol.BlocksAttacksComponent) *nbt.OrderedCompound {
	reductions := nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{}}
	for _, d := range b.Reductions {
		entry := nbt.NewOrderedCompound(4)
		entry.Set("horizontal_blocking_angle", nbt.Float(d.Angle))
		if d.Type.Present {
			entry.Set("type", damageSetTag(d.Type))
		}
		entry.Set("base", nbt.Float(d.Base))
		entry.Set("factor", nbt.Float(d.Factor))
		reductions.Items = append(reductions.Items, entry)
	}
	out := nbt.NewOrderedCompound(7)
	out.Set("block_delay_seconds", nbt.Float(b.BlockDelaySeconds))
	out.Set("disable_cooldown_scale", nbt.Float(b.DisableCooldownScale))
	out.Set("damage_reductions", reductions)
	damage := nbt.NewOrderedCompound(3)
	damage.Set("threshold", nbt.Float(b.ItemDamage[0]))
	damage.Set("base", nbt.Float(b.ItemDamage[1]))
	damage.Set("factor", nbt.Float(b.ItemDamage[2]))
	out.Set("item_damage", damage)
	if b.BypassedBy.Present {
		out.Set("bypassed_by", damageSetTag(b.BypassedBy))
	}
	for _, field := range []struct {
		key   string
		sound *protocol.SoundHolder
	}{{"block_sound", b.BlockSound}, {"disabled_sound", b.DisableSound}} {
		if field.sound != nil {
			if tag, ok := soundHolderTag(*field.sound); ok {
				out.Set(field.key, tag)
			}
		}
	}
	return out
}

func damageSetTag(set protocol.DamageSetComponent) nbt.Tag {
	if set.Tag != "" {
		return nbt.String("#" + set.Tag)
	}
	reg, _ := Get("minecraft:damage_type")
	names := nbt.List{Elem: nbt.TypeString, Items: []nbt.Tag{}}
	for _, id := range set.IDs {
		if int(id) < len(reg.Entries) {
			names.Items = append(names.Items, nbt.String(reg.Entries[id]))
		}
	}
	return names
}

// weaponOf reads a minecraft:weapon value. vanilla: Weapon.CODEC
func weaponOf(value nbt.Tag) (protocol.WeaponComponent, bool) {
	entry, ok := nbt.CompoundView(value)
	if !ok {
		return protocol.WeaponComponent{}, false
	}
	w := protocol.WeaponComponent{DamagePerAttack: 1}
	if field, has := entry["item_damage_per_attack"]; has {
		if w.DamagePerAttack, ok = nbt.IntValue(field); !ok || w.DamagePerAttack < 0 {
			return protocol.WeaponComponent{}, false
		}
	}
	if field, has := entry["disable_blocking_for_seconds"]; has {
		if w.DisableBlockingSeconds, ok = floatOf(field); !ok || w.DisableBlockingSeconds < 0 {
			return protocol.WeaponComponent{}, false
		}
	}
	return w, true
}

func weaponTag(w protocol.WeaponComponent) *nbt.OrderedCompound {
	out := nbt.NewOrderedCompound(2)
	out.Set("item_damage_per_attack", nbt.Int(w.DamagePerAttack))
	out.Set("disable_blocking_for_seconds", nbt.Float(w.DisableBlockingSeconds))
	return out
}

// withBlocksAndWeaponTags adds the stack's own blocks_attacks and weapon to
// components as stored components.
func withBlocksAndWeaponTags(components nbt.Compound, stack protocol.ItemStack) nbt.Compound {
	b, hasBlocks := stack.BlocksAttacksOf()
	w, hasWeapon := stack.WeaponOf()
	if (hasBlocks || hasWeapon) && components == nil {
		components = nbt.Compound{}
	}
	if hasBlocks {
		components["minecraft:blocks_attacks"] = blocksAttacksTag(b)
	}
	if hasWeapon {
		components["minecraft:weapon"] = weaponTag(w)
	}
	return components
}

// readCombatComponent reads one stored tool, weapon, blocks_attacks or attack_range into
// the stack, reporting whether the value is whole.
func readCombatComponent(stack *protocol.ItemStack, name string, value nbt.Tag) bool {
	switch name {
	case "minecraft:attack_range":
		a, ok := attackRangeOf(value)
		if ok {
			stack.SetAttackRange(a)
		}
		return ok
	case "minecraft:tool":
		tool, ok := toolOf(value)
		if ok {
			stack.SetTool(tool)
		}
		return ok
	case "minecraft:weapon":
		w, ok := weaponOf(value)
		if ok {
			stack.SetWeapon(w)
		}
		return ok
	}
	b, ok := blocksAttacksOf(value)
	if ok {
		stack.SetBlocksAttacks(b)
	}
	return ok
}
