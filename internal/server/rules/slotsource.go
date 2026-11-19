package rules

import (
	"slices"
	"strconv"
	"strings"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/registry"
)

// maxSlotSourceDepth bounds how far named sources may refer to one another,
// so a pack whose sources loop yields nothing past it.
const maxSlotSourceDepth = 16

// SlotSourceStacks is a copy of the stack in every slot the source finds in
// the context, empty slots included. An entity or block entity the context
// does not hold has no slots.
// vanilla: SlotSource.provide, SlotCollection.itemCopies
func SlotSourceStacks(source registry.SlotSource, ctx *LootContext) []protocol.ItemStack {
	return slotSourceStacks(source, ctx, 0)
}

func slotSourceStacks(source registry.SlotSource, ctx *LootContext, depth int) []protocol.ItemStack {
	if depth > maxSlotSourceDepth {
		return nil
	}
	switch k := source.(type) {
	case registry.SlotSourceReference:
		if named, ok := registry.FindSlotSource(k.Name); ok {
			return slotSourceStacks(named, ctx, depth+1)
		}
	case registry.SlotRangeSource:
		return slotRangeStacks(k, ctx)
	case registry.SlotFilteredSource:
		return slices.DeleteFunc(slotSourceStacks(k.From, ctx, depth+1), func(s protocol.ItemStack) bool {
			return !itemPredicate(k.Filter, ToolOf(s))
		})
	case registry.SlotLimitSource:
		stacks := slotSourceStacks(k.From, ctx, depth+1)
		return stacks[:min(len(stacks), k.Limit)]
	case registry.SlotContentsSource:
		var out []protocol.ItemStack
		for _, s := range slotSourceStacks(k.From, ctx, depth+1) {
			switch k.Component {
			case "minecraft:container":
				out = append(out, s.Container...)
			case "minecraft:bundle_contents":
				out = append(out, s.BundleContents()...)
			default:
				out = append(out, s.ChargedProjectiles()...)
			}
		}
		return out
	case registry.SlotGroupSource:
		var out []protocol.ItemStack
		for _, term := range k.Terms {
			out = append(out, slotSourceStacks(term, ctx, depth+1)...)
		}
		return out
	}
	return nil
}

// slotRangeStacks reads a range off the context argument the source names.
// vanilla: RangeSlotSource.provide, SlotProvider.getSlotsFromRange
func slotRangeStacks(r registry.SlotRangeSource, ctx *LootContext) []protocol.ItemStack {
	var entity LootEntity
	switch r.Source {
	case "container":
		if ctx.ContainerSlots != nil {
			return ctx.ContainerSlots(r.Slots)
		}
		return nil
	case "block_entity":
		return containerRange(ctx.Container, r.Slots)
	case "this":
		entity = ctx.This
	case "attacker":
		entity = ctx.Attacker
	case "direct_attacker":
		entity = ctx.DirectAttacker
	case "attacking_player":
		entity = ctx.AttackingPlayer
	case "target_entity":
		entity = ctx.Target
	case "interacting_entity":
		entity = ctx.Interacting
	}
	if entity.SlotStacks == nil {
		return nil
	}
	return entity.SlotStacks(r.Slots)
}

// containerRange reads a container.N or container.* range off a block
// entity's slots; any other range names slots a block entity has none of.
func containerRange(slots []protocol.ItemStack, name string) []protocol.ItemStack {
	index, ok := strings.CutPrefix(name, "container.")
	if !ok {
		return nil
	}
	if index == "*" {
		return slices.Clone(slots)
	}
	n, err := strconv.Atoi(index)
	if err != nil || n < 0 || n >= len(slots) {
		return nil
	}
	return []protocol.ItemStack{slots[n]}
}
