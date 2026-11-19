package registry

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/nbt"
)

// adventurePredicatesOf reads a can_place_on or can_break value: one
// predicate, or a non-empty list of them. A predicate with component
// matchers is refused, as the wire reader refuses one.
// vanilla: AdventureModePredicate.CODEC
func adventurePredicatesOf(value nbt.Tag) ([]protocol.BlockPredicate, bool) {
	if entry, ok := nbt.CompoundView(value); ok {
		p, ok := blockPredicateOf(entry)
		return []protocol.BlockPredicate{p}, ok
	}
	list, ok := value.(nbt.List)
	if !ok || len(list.Items) == 0 {
		return nil, false
	}
	predicates := make([]protocol.BlockPredicate, 0, len(list.Items))
	for _, item := range list.Items {
		entry, ok := nbt.CompoundView(item)
		if !ok {
			return nil, false
		}
		p, ok := blockPredicateOf(entry)
		if !ok {
			return nil, false
		}
		predicates = append(predicates, p)
	}
	return predicates, true
}

// blockPredicateOf reads one predicate; unknown keys are skipped.
// vanilla: BlockPredicate.CODEC
func blockPredicateOf(entry nbt.Compound) (protocol.BlockPredicate, bool) {
	var p protocol.BlockPredicate
	for _, key := range [...]string{"components", "predicates"} {
		if field, has := entry[key]; has {
			if matchers, ok := nbt.CompoundView(field); !ok || len(matchers) != 0 {
				return p, false
			}
		}
	}
	if field, has := entry["blocks"]; has {
		set, ok := blockSetOf(field)
		if !ok {
			return p, false
		}
		p.Blocks = set
	}
	if field, has := entry["state"]; has {
		props, ok := statePropertiesOf(field)
		if !ok {
			return p, false
		}
		p.Props = props
	}
	if field, has := entry["nbt"]; has {
		// vanilla: TagParser.LENIENT_CODEC, an SNBT string or a compound
		tag, ok := compoundOrSNBT(field)
		if !ok {
			return p, false
		}
		p.NBT = tag
	}
	return p, true
}

// statePropertiesOf reads a property name -> exact string or {min, max}
// map, retaining the codec's entry traversal.
// vanilla: StatePropertiesPredicate.CODEC
func statePropertiesOf(value nbt.Tag) ([]protocol.BlockPredicateProp, bool) {
	state, ok := value.(nbt.CompoundTag)
	if !ok {
		return nil, false
	}
	props := make([]protocol.BlockPredicateProp, 0, len(state.Entries()))
	for _, name := range nbt.HashOrderedKeys(state) {
		m := protocol.BlockPredicateProp{Name: name}
		switch v := state.Entries()[name].(type) {
		case nbt.String:
			m.Value = string(v)
		case nbt.CompoundTag:
			m.Ranged = true
			for key, end := range map[string]*string{"min": &m.Min, "max": &m.Max} {
				if field, has := v.Entries()[key]; has {
					s, ok := field.(nbt.String)
					if !ok {
						return nil, false
					}
					*end = string(s)
				}
			}
		default:
			return nil, false
		}
		props = append(props, m)
	}
	return props, true
}

// adventurePredicatesTag writes predicates in the shape adventurePredicatesOf
// reads: a lone predicate bare, more as a list, as the compact codec writes.
func adventurePredicatesTag(predicates []protocol.BlockPredicate) nbt.Tag {
	entries := make([]nbt.Tag, 0, len(predicates))
	for _, p := range predicates {
		entry := nbt.NewOrderedCompound(3)
		if p.Blocks.Present {
			entry.Set("blocks", blockSetTag(p.Blocks))
		}
		if p.Props != nil {
			state := nbt.NewOrderedCompound(len(p.Props))
			for _, m := range p.Props {
				if !m.Ranged {
					state.Set(m.Name, nbt.String(m.Value))
					continue
				}
				ranged := nbt.NewOrderedCompound(2)
				if m.Min != "" {
					ranged.Set("min", nbt.String(m.Min))
				}
				if m.Max != "" {
					ranged.Set("max", nbt.String(m.Max))
				}
				state.Set(m.Name, ranged)
			}
			entry.Set("state", state)
		}
		if p.NBT != nil {
			// The lenient codec's primary form is the SNBT string.
			entry.Set("nbt", nbt.String(nbt.Stringify(p.NBT)))
		}
		entries = append(entries, entry)
	}
	if len(entries) == 1 {
		return entries[0]
	}
	return nbt.List{Elem: nbt.TypeCompound, Items: entries}
}

// readAdventureOrEntityData is readComponentKnown for can_place_on,
// can_break, entity_data and block_entity_data.
func readAdventureOrEntityData(stack *protocol.ItemStack, name string, value nbt.Tag) (ok, modeled bool) {
	switch name {
	case "minecraft:can_place_on", "minecraft:can_break":
		predicates, ok := adventurePredicatesOf(value)
		component := int32(protocol.DataComponentCanPlaceOn)
		if name == "minecraft:can_break" {
			component = protocol.DataComponentCanBreak
		}
		if !ok || !stack.SetBlockPredicates(component, predicates) {
			return false, true
		}
	case "minecraft:entity_data":
		id, tag, ok := typedEntityDataOf(value, EntityTypeID)
		if !ok || !stack.SetEntityData(id, tag) {
			return false, true
		}
	case "minecraft:block_entity_data":
		id, tag, ok := typedEntityDataOf(value, protocol.BlockEntityTypeID)
		if !ok || !stack.SetBlockEntityData(id, tag) {
			return false, true
		}
	default:
		if ok, modeled := readConsumableComponent(stack, name, value); modeled {
			return ok, true
		}
		if ok, modeled := readRecordComponent(stack, name, value); modeled {
			return ok, true
		}
		return readProtectionComponent(stack, name, value)
	}
	return true, true
}

// putAdventureAndEntityData is StackComponents for the components
// readAdventureOrEntityData reads; it returns components, made when nil.
func putAdventureAndEntityData(stack protocol.ItemStack, components nbt.Compound) nbt.Compound {
	for _, adventure := range [...]struct {
		name       string
		predicates func() ([]protocol.BlockPredicate, bool)
	}{{"minecraft:can_place_on", stack.CanPlaceOn}, {"minecraft:can_break", stack.CanBreak}} {
		// An empty list, which a client may send, has no NBT shape.
		if predicates, ok := adventure.predicates(); ok && len(predicates) > 0 {
			if components == nil {
				components = nbt.Compound{}
			}
			components[adventure.name] = adventurePredicatesTag(predicates)
		}
	}
	if id, tag, ok := stack.EntityDataTag(); ok {
		if name := EntityType(id).Name(); name != "" {
			if components == nil {
				components = nbt.Compound{}
			}
			components["minecraft:entity_data"] = typedEntityDataTag(name, tag)
		}
	}
	if id, tag, ok := stack.BlockEntityDataTag(); ok {
		if name, ok := protocol.BlockEntityTypeName(id); ok {
			if components == nil {
				components = nbt.Compound{}
			}
			components["minecraft:block_entity_data"] = typedEntityDataTag(name, tag)
		}
	}
	return components
}
