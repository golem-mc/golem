package player

import (
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// Save is the player file's attributes list: the loaded list with this map's
// instances written over their entries, plus each other instance with a
// non-default base or a persistent modifier. Loaded entries the map lacks are
// kept; nil for none. Transient modifiers are rebuilt each tick, never saved.
// vanilla: AttributeMap.save, AttributeInstance.pack
func (a *Attributes) Save(loaded *nbt.List) *nbt.List { return a.SaveExcept(loaded, nil) }

// SaveExcept is Save leaving out the attributes in skip, whose saved entries
// another writer owns; a loaded entry for one is kept as it was.
func (a *Attributes) SaveExcept(loaded *nbt.List, skip map[string]bool) *nbt.List {
	a.init()
	var items []nbt.Tag
	var listed attributeSet
	if loaded != nil {
		for _, item := range loaded.Items {
			attr, ok := savedAttribute(item)
			if ok && a.instances[attr.ID] != nil && !skip[attr.Name] {
				listed |= 1 << attr.ID
				items = append(items, packInstance(a.instances[attr.ID]))
				continue
			}
			items = append(items, item)
		}
	}
	for _, in := range a.snapshot(func(in *AttributeInstance) bool { return !listed.has(registry.AttributeID(in.Attribute.ID)) }) {
		if skip[in.Attribute.Name] {
			continue
		}
		tag := packInstance(&in)
		_, hasMods := tag.Entries()["modifiers"]
		if in.Base == a.bases[in.Attribute.ID] && !hasMods {
			continue
		}
		items = append(items, tag)
	}
	if items == nil {
		return nil
	}
	return &nbt.List{Elem: nbt.TypeCompound, Items: items}
}

// SavePlayer saves every instantiated player attribute, including defaults.
func (a *Attributes) SavePlayer(loaded *nbt.List) *nbt.List {
	list := a.pack(loaded, true)
	return &list
}

// SaveInstances packs mob and armor-stand instances, including defaults.
// A synthetic instance is included if loaded from disk or withSynthetic is set.
func (a *Attributes) SaveInstances(loaded *nbt.List, withSynthetic bool) *nbt.List {
	list := a.pack(loaded, withSynthetic)
	if len(list.Items) == 0 {
		return nil
	}
	return &list
}

// vanilla: AttributeMap.pack
func (a *Attributes) pack(loaded *nbt.List, withSynthetic bool) nbt.List {
	a.init()
	order := a.insertionOrder[:a.instanceCount]
	if a.synthetic != 0 {
		var inserted [registry.NumAttributes]uint8
		order = inserted[:0]
		for _, id := range a.insertionOrder[:a.instanceCount] {
			if !a.synthetic.has(registry.AttributeID(id)) {
				order = append(order, id)
			}
		}
		if withSynthetic {
			// Native instances and loaded data precede first-tick defaults.
			for _, id := range a.insertionOrder[:a.instanceCount] {
				if a.synthetic.has(registry.AttributeID(id)) {
					order = append(order, id)
				}
			}
		}
	}
	items := make([]nbt.Tag, 0, len(order))
	var packed attributeSet
	for _, id := range packedAttributeOrder(order) {
		items = append(items, packInstance(a.instances[id]))
		packed |= 1 << id
	}
	if loaded != nil {
		for _, item := range loaded.Items {
			if attr, ok := savedAttribute(item); !ok || !packed.has(registry.AttributeID(attr.ID)) {
				items = append(items, item)
			}
		}
	}
	return nbt.List{Elem: nbt.TypeCompound, Items: items}
}

func savedAttribute(item nbt.Tag) (registry.Attribute, bool) {
	tag, _ := nbt.CompoundView(item)
	id, _ := nbt.Get[nbt.String](tag, "id")
	return registry.AttributeByName(registry.Qualify(string(id)))
}

func packInstance(in *AttributeInstance) *nbt.OrderedCompound {
	var mods []nbt.Tag
	for _, m := range in.Modifiers {
		if m.Persistent {
			mods = append(mods, AttributeModifierTag(m.ID, m.Amount, m.Operation.Name()))
		}
	}
	return AttributeTag(in.Attribute.Name, in.Base, mods)
}

// AttributeTag writes AttributeInstance.Packed.CODEC's record in insertion order.
func AttributeTag(id string, base float64, mods []nbt.Tag) *nbt.OrderedCompound {
	tag := nbt.NewOrderedCompound(3)
	tag.Set("id", nbt.String(id))
	tag.Set("base", nbt.Double(base))
	if len(mods) > 0 {
		tag.Set("modifiers", nbt.List{Elem: nbt.TypeCompound, Items: mods})
	}
	return tag
}

// AttributeModifierTag writes AttributeModifier.MAP_CODEC's record in insertion order.
func AttributeModifierTag(id string, amount float64, operation string) *nbt.OrderedCompound {
	tag := nbt.NewOrderedCompound(3)
	tag.Set("id", nbt.String(id))
	tag.Set("amount", nbt.Double(amount))
	tag.Set("operation", nbt.String(operation))
	return tag
}

// Load applies a saved attributes list, skipping malformed entries and ones a
// player does not carry.
// vanilla: AttributeMap.apply
func (a *Attributes) Load(list nbt.List) {
	if !a.initialized && a.supplier == nil {
		// vanilla: LivingEntity.readAdditionalSaveData applies attributes before
		// Player.readAdditionalSaveData sets speed and ServerPlayer ticks ranges.
		a.initBases()
		defer a.initStartingAttributes()
	}
	for _, item := range list.Items {
		tag, ok := nbt.CompoundView(item)
		if !ok {
			continue
		}
		name, _ := nbt.Get[nbt.String](tag, "id")
		attr, ok := registry.AttributeByName(registry.Qualify(string(name)))
		id := registry.AttributeID(attr.ID)
		if !ok || !a.Has(id) {
			continue
		}
		a.Instance(id)
		if base, ok := nbt.NumberValue(tag["base"]); ok {
			a.SetBase(id, base)
		}
		mods, _ := nbt.Get[nbt.List](tag, "modifiers")
		for _, mi := range mods.Items {
			mt, ok := nbt.CompoundView(mi)
			if !ok {
				continue
			}
			mid, _ := nbt.Get[nbt.String](mt, "id")
			amount, okA := nbt.NumberValue(mt["amount"])
			opName, _ := nbt.Get[nbt.String](mt, "operation")
			op, okO := registry.AttributeOperationByName(string(opName))
			if mid == "" || !okA || !okO {
				continue
			}
			a.AddModifier(id, Modifier{ID: registry.Qualify(string(mid)), Amount: amount, Operation: op, Persistent: true})
		}
	}
}
