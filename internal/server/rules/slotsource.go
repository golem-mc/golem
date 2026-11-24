package rules

import (
	"math"
	"math/big"
	"slices"
	"strconv"
	"strings"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/registry"
)

// maxSlotSourceDepth bounds how far named sources may refer to one another,
// so a pack whose sources loop yields nothing past it.
const maxSlotSourceDepth = 16

const (
	itemContainerMaxSize     = 256  // vanilla: ItemContainerContents.MAX_SIZE
	chargedProjectilesMaxLen = 1024 // vanilla: ChargedProjectiles.MAX_SIZE
)

// SlotAccess reads and writes one slot of a container, entity or item component.
// vanilla: SlotAccess
type SlotAccess struct {
	Get func() protocol.ItemStack
	Set func(protocol.ItemStack) bool
}

// SlotSelector filters, limits and counts slots selected during replace/modify.
// vanilla: SlotSelector
type SlotSelector struct {
	filter func(protocol.ItemStack) bool
	limit  int
	count  *int
}

// AnySlotSelector selects every slot without a limit.
func AnySlotSelector() SlotSelector { return SlotSelector{} }

// TrackingSlotSelector selects every slot and increments selected on each hit.
// vanilla: SlotSelector.tracking
func TrackingSlotSelector(selected *int) SlotSelector {
	return SlotSelector{limit: math.MaxInt, count: selected}
}

// NonEmptyTrackingSlotSelector selects non-empty slots and increments selected.
// vanilla: SlotSelector.tracking(SlotSelector.NON_EMPTY_SLOTS, selectedCount)
func NonEmptyTrackingSlotSelector(selected *int) SlotSelector {
	return SlotSelector{
		filter: func(s protocol.ItemStack) bool { return !s.Empty() },
		limit:  math.MaxInt,
		count:  selected,
	}
}

// Filter narrows the selector with an additional stack predicate.
func (s SlotSelector) Filter(pred func(protocol.ItemStack) bool) SlotSelector {
	prev := s.filter
	if prev == nil {
		s.filter = pred
		return s
	}
	s.filter = func(stack protocol.ItemStack) bool {
		return prev(stack) && pred(stack)
	}
	return s
}

// Limit caps how many slots the selector may accept.
func (s SlotSelector) Limit(n int) SlotSelector {
	if s.count == nil {
		return SlotSelector{filter: s.filter, limit: n, count: new(int)}
	}
	if n < s.limit {
		s.limit = n
	}
	return s
}

// TrySelect tests stack and advances the selection counter when accepted.
func (s SlotSelector) TrySelect(stack protocol.ItemStack) bool {
	if s.count != nil {
		if *s.count >= s.limit {
			return false
		}
		if s.filter != nil && !s.filter(stack) {
			return false
		}
		*s.count++
		return true
	}
	return s.filter == nil || s.filter(stack)
}

// ItemProviderMode controls how an ItemProvider hands stacks to target slots.
// vanilla: ItemCommands.replaceItemProvider, fillItemProvider, overrideItemProvider
type ItemProviderMode int

const (
	ItemProviderReplace  ItemProviderMode = iota // items in order; slots past the last kept
	ItemProviderFill                             // items cycled over every slot
	ItemProviderOverride                         // items in order; slots past the last emptied
)

// ItemProvider hands item stacks out to slots one at a time.
// vanilla: ItemProvider, ArrayItemProvider
type ItemProvider struct {
	Items []protocol.ItemStack
	Mode  ItemProviderMode
	pos   int
}

// Restart resets the provider to the first item.
func (p *ItemProvider) Restart() { p.pos = 0 }

// HasNext reports whether another item is available.
func (p *ItemProvider) HasNext() bool {
	switch p.Mode {
	case ItemProviderFill:
		return len(p.Items) > 0
	case ItemProviderOverride:
		return true
	}
	return p.pos < len(p.Items)
}

// Peek returns the next item without advancing.
func (p *ItemProvider) Peek() protocol.ItemStack {
	if p.Mode == ItemProviderFill && len(p.Items) > 0 {
		return p.Items[p.pos%len(p.Items)]
	}
	if p.pos < len(p.Items) {
		return p.Items[p.pos]
	}
	return protocol.ItemStack{}
}

// Next returns the next item and advances.
func (p *ItemProvider) Next() protocol.ItemStack {
	stack := p.Peek()
	if p.Mode == ItemProviderFill && len(p.Items) > 0 {
		p.pos++
		return stack
	}
	if p.pos < len(p.Items) {
		p.pos++
	}
	return stack
}

// FindNextNonEmpty advances to the next non-empty item, returning false if none.
// vanilla: ItemProvider.findNextNonEmpty
func (p *ItemProvider) FindNextNonEmpty() bool {
	if p.Mode == ItemProviderFill {
		for off := range len(p.Items) {
			if !p.Items[(p.pos+off)%len(p.Items)].Empty() {
				p.pos += off
				return true
			}
		}
		return false
	}
	for i := p.pos; i < len(p.Items); i++ {
		if !p.Items[i].Empty() {
			p.pos = i
			return true
		}
	}
	return false
}

// SlotCollection is the resolved set of slots a SlotSource provides.
// vanilla: SlotCollection
type SlotCollection interface {
	ItemCopies() []protocol.ItemStack
	ReplaceSlotItems(items *ItemProvider, sel SlotSelector) int
	ModifySlots(modify func(SlotAccess), sel SlotSelector)
}

type emptySlotCollection struct{}

func (emptySlotCollection) ItemCopies() []protocol.ItemStack                 { return nil }
func (emptySlotCollection) ReplaceSlotItems(*ItemProvider, SlotSelector) int { return 0 }
func (emptySlotCollection) ModifySlots(func(SlotAccess), SlotSelector)       {}

type sliceSlotCollection []SlotAccess

func (c sliceSlotCollection) ItemCopies() []protocol.ItemStack {
	out := make([]protocol.ItemStack, len(c))
	for i, slot := range c {
		out[i] = slot.Get()
	}
	return out
}

func (c sliceSlotCollection) ReplaceSlotItems(items *ItemProvider, sel SlotSelector) int {
	success := 0
	for _, slot := range c {
		if !items.HasNext() {
			break
		}
		if sel.TrySelect(slot.Get()) && slot.Set(items.Next()) {
			success++
		}
	}
	return success
}

func (c sliceSlotCollection) ModifySlots(modify func(SlotAccess), sel SlotSelector) {
	for _, slot := range c {
		if sel.TrySelect(slot.Get()) {
			modify(slot)
		}
	}
}

type filteredSlotCollection struct {
	inner  SlotCollection
	filter func(protocol.ItemStack) bool
}

func (c filteredSlotCollection) ItemCopies() []protocol.ItemStack {
	return slices.DeleteFunc(c.inner.ItemCopies(), func(s protocol.ItemStack) bool {
		return !c.filter(s)
	})
}

func (c filteredSlotCollection) ReplaceSlotItems(items *ItemProvider, sel SlotSelector) int {
	return c.inner.ReplaceSlotItems(items, sel.Filter(c.filter))
}

func (c filteredSlotCollection) ModifySlots(modify func(SlotAccess), sel SlotSelector) {
	c.inner.ModifySlots(modify, sel.Filter(c.filter))
}

type limitedSlotCollection struct {
	inner SlotCollection
	limit int
}

func (c limitedSlotCollection) ItemCopies() []protocol.ItemStack {
	stacks := c.inner.ItemCopies()
	return stacks[:min(len(stacks), c.limit)]
}

func (c limitedSlotCollection) ReplaceSlotItems(items *ItemProvider, sel SlotSelector) int {
	return c.inner.ReplaceSlotItems(items, sel.Limit(c.limit))
}

func (c limitedSlotCollection) ModifySlots(modify func(SlotAccess), sel SlotSelector) {
	c.inner.ModifySlots(modify, sel.Limit(c.limit))
}

type concatSlotCollection []SlotCollection

func (c concatSlotCollection) ItemCopies() []protocol.ItemStack {
	var out []protocol.ItemStack
	for _, term := range c {
		out = append(out, term.ItemCopies()...)
	}
	return out
}

func (c concatSlotCollection) ReplaceSlotItems(items *ItemProvider, sel SlotSelector) int {
	total := 0
	for _, term := range c {
		total += term.ReplaceSlotItems(items, sel)
	}
	return total
}

func (c concatSlotCollection) ModifySlots(modify func(SlotAccess), sel SlotSelector) {
	for _, term := range c {
		term.ModifySlots(modify, sel)
	}
}

type contentsSlotCollection struct {
	inner     SlotCollection
	component string
}

func (c contentsSlotCollection) ItemCopies() []protocol.ItemStack {
	var out []protocol.ItemStack
	for _, s := range c.inner.ItemCopies() {
		switch c.component {
		case "minecraft:container":
			out = append(out, s.Container...)
		case "minecraft:bundle_contents":
			out = append(out, s.BundleContents()...)
		default:
			out = append(out, s.ChargedProjectiles()...)
		}
	}
	return out
}

func (c contentsSlotCollection) ReplaceSlotItems(items *ItemProvider, sel SlotSelector) int {
	total := 0
	c.inner.ModifySlots(func(parent SlotAccess) {
		total += replaceComponentSlots(parent, c.component, items, sel)
	}, AnySlotSelector())
	return total
}

func (c contentsSlotCollection) ModifySlots(modify func(SlotAccess), sel SlotSelector) {
	c.inner.ModifySlots(func(parent SlotAccess) {
		modifyComponentSlots(parent, c.component, modify, sel)
	}, AnySlotSelector())
}

// replaceComponentSlots runs replaceSlotItems on a stack's container component
// and writes the updated component back through parent.
// vanilla: ContainerComponentManipulator.ComponentSlotCollection.replaceSlotItems
func replaceComponentSlots(parent SlotAccess, component string, items *ItemProvider, sel SlotSelector) int {
	stack := parent.Get()
	switch component {
	case "minecraft:container":
		list := slices.Clone(stack.Container)
		replaced := 0
		for i := 0; i < len(list) && items.HasNext(); i++ {
			if sel.TrySelect(list[i]) {
				list[i] = items.Next()
				replaced++
			}
		}
		for items.HasNext() && len(list) < itemContainerMaxSize && sel.TrySelect(protocol.ItemStack{}) {
			list = append(list, items.Next())
			replaced++
		}
		if !stack.Empty() && replaced > 0 {
			setComponentContents(&stack, component, list)
			parent.Set(stack)
		}
		return replaced
	case "minecraft:bundle_contents":
		list := slices.Clone(stack.BundleContents())
		weight := BundleItemsWeight(list)
		replaced := 0
		for i := 0; i < len(list) && items.HasNext(); i++ {
			if !sel.TrySelect(list[i]) {
				continue
			}
			next := items.Next()
			if nw, ok := bundleWeightAfterReplace(weight, list[i], next); ok {
				list[i] = next
				weight = nw
				replaced++
			}
		}
		one := big.NewRat(1, 1)
		for items.HasNext() && weight.Cmp(one) < 0 && sel.TrySelect(protocol.ItemStack{}) {
			if !items.FindNextNonEmpty() {
				break
			}
			nw, ok := bundleWeightWithAdded(weight, items.Peek())
			if !ok {
				break
			}
			list = append(list, items.Next())
			weight = nw
			replaced++
		}
		if !stack.Empty() && replaced > 0 {
			setComponentContents(&stack, component, list)
			parent.Set(stack)
		}
		return replaced
	default: // minecraft:charged_projectiles
		list := slices.Clone(stack.ChargedProjectiles())
		replaced := 0
		for i := 0; i < len(list) && items.HasNext(); i++ {
			if sel.TrySelect(list[i]) {
				list[i] = items.Next()
				replaced++
			}
		}
		for items.HasNext() && len(list) < chargedProjectilesMaxLen && sel.TrySelect(protocol.ItemStack{}) {
			if !items.FindNextNonEmpty() {
				break
			}
			list = append(list, items.Next())
			replaced++
		}
		if !stack.Empty() && replaced > 0 {
			setComponentContents(&stack, component, list)
			parent.Set(stack)
		}
		return replaced
	}
}

// modifyComponentSlots runs modifySlots on a stack's container component and
// writes the updated component back through parent.
// vanilla: ContainerComponentManipulator.ComponentSlotCollection.modifySlots
func modifyComponentSlots(parent SlotAccess, component string, modify func(SlotAccess), sel SlotSelector) {
	stack := parent.Get()
	modified := false
	switch component {
	case "minecraft:container":
		list := slices.Clone(stack.Container)
		for i := range list {
			if !sel.TrySelect(list[i]) {
				continue
			}
			idx := i
			modify(SlotAccess{
				Get: func() protocol.ItemStack { return list[idx] },
				Set: func(s protocol.ItemStack) bool { list[idx] = s; modified = true; return true },
			})
		}
		if !stack.Empty() && modified {
			setComponentContents(&stack, component, list)
			parent.Set(stack)
		}
	case "minecraft:bundle_contents":
		list := slices.Clone(stack.BundleContents())
		weight := BundleItemsWeight(list)
		for i := range list {
			if !sel.TrySelect(list[i]) {
				continue
			}
			idx := i
			modify(SlotAccess{
				Get: func() protocol.ItemStack { return list[idx] },
				Set: func(s protocol.ItemStack) bool {
					nw, ok := bundleWeightAfterReplace(weight, list[idx], s)
					if !ok {
						return false
					}
					list[idx] = s
					weight = nw
					modified = true
					return true
				},
			})
		}
		if !stack.Empty() && modified {
			setComponentContents(&stack, component, list)
			parent.Set(stack)
		}
	default: // minecraft:charged_projectiles
		list := slices.Clone(stack.ChargedProjectiles())
		for i := range list {
			if !sel.TrySelect(list[i]) {
				continue
			}
			idx := i
			modify(SlotAccess{
				Get: func() protocol.ItemStack { return list[idx] },
				Set: func(s protocol.ItemStack) bool { list[idx] = s; modified = true; return true },
			})
		}
		if !stack.Empty() && modified {
			setComponentContents(&stack, component, list)
			parent.Set(stack)
		}
	}
}

// setComponentContents writes items back to stack's container component,
// keeping an explicit empty patch when the item lacks a default for it and
// clearing any removal when restoring the default.
// vanilla: ContainerComponentManipulator.setContents
func setComponentContents(stack *protocol.ItemStack, component string, items []protocol.ItemStack) {
	keepEmpty := !registry.ItemHasComponentDefault(stack.ItemID, component)
	switch component {
	case "minecraft:container":
		stack.SetContainerPatch(trimTrailingEmpty(items), keepEmpty)
	case "minecraft:bundle_contents":
		stack.SetBundleContentsPatch(mergeBundleStacks(items), keepEmpty)
	default: // minecraft:charged_projectiles
		stack.SetChargedProjectilesPatch(slices.DeleteFunc(items, protocol.ItemStack.Empty), keepEmpty)
	}
}

func trimTrailingEmpty(items []protocol.ItemStack) []protocol.ItemStack {
	for len(items) > 0 && items[len(items)-1].Empty() {
		items = items[:len(items)-1]
	}
	return items
}

func bundleWeightWithAdded(weight *big.Rat, add protocol.ItemStack) (*big.Rat, bool) {
	itemWeight, ok := BundleItemWeight(add)
	if !ok {
		return nil, false
	}
	next := new(big.Rat).Add(weight, itemWeight.Mul(itemWeight, big.NewRat(int64(add.Count), 1)))
	if next.Cmp(big.NewRat(1, 1)) > 0 {
		return nil, false
	}
	return next, true
}

func bundleWeightAfterReplace(weight *big.Rat, cur, next protocol.ItemStack) (*big.Rat, bool) {
	adjusted := new(big.Rat).Set(weight)
	if !cur.Empty() {
		if w, ok := BundleItemWeight(cur); ok {
			adjusted.Sub(adjusted, w.Mul(w, big.NewRat(int64(cur.Count), 1)))
		}
	}
	if next.Empty() {
		return adjusted, true
	}
	return bundleWeightWithAdded(adjusted, next)
}

// mergeBundleStacks drops empty stacks and merges identical stackable entries.
// vanilla: BundleContents.Mutable.mergeIdenticalStacks
func mergeBundleStacks(items []protocol.ItemStack) []protocol.ItemStack {
	for i := 0; i < len(items); i++ {
		if items[i].Empty() {
			items = slices.Delete(items, i, i+1)
			i--
			continue
		}
		if registry.StackMaxStackSize(items[i]) <= 1 {
			continue
		}
		for j := i + 1; j < len(items); j++ {
			if items[j].Same(items[i]) {
				items[j].Count += items[i].Count
				items = slices.Delete(items, i, i+1)
				i--
				break
			}
		}
	}
	return items
}

// SlotSourceCollection resolves source in ctx to a readable and writable
// SlotCollection. An entity or block entity the context does not hold yields
// an empty collection.
// vanilla: SlotSource.provide
func SlotSourceCollection(source registry.SlotSource, ctx *LootContext) SlotCollection {
	return slotSourceCollection(source, ctx, 0)
}

// SlotSourceStacks is a copy of the stack in every slot the source finds in
// the context, empty slots included.
// vanilla: SlotSource.provide, SlotCollection.itemCopies
func SlotSourceStacks(source registry.SlotSource, ctx *LootContext) []protocol.ItemStack {
	return SlotSourceCollection(source, ctx).ItemCopies()
}

func slotSourceCollection(source registry.SlotSource, ctx *LootContext, depth int) SlotCollection {
	if depth > maxSlotSourceDepth {
		return emptySlotCollection{}
	}
	switch k := source.(type) {
	case registry.SlotSourceReference:
		if named, ok := registry.FindSlotSource(k.Name); ok {
			return slotSourceCollection(named, ctx, depth+1)
		}
	case registry.SlotRangeSource:
		slots := slotRangeAccess(k, ctx)
		if len(slots) == 0 {
			return emptySlotCollection{}
		}
		return sliceSlotCollection(slots)
	case registry.SlotFilteredSource:
		return filteredSlotCollection{
			inner: slotSourceCollection(k.From, ctx, depth+1),
			filter: func(s protocol.ItemStack) bool {
				return itemPredicate(k.Filter, ToolOf(s))
			},
		}
	case registry.SlotLimitSource:
		return limitedSlotCollection{
			inner: slotSourceCollection(k.From, ctx, depth+1),
			limit: k.Limit,
		}
	case registry.SlotContentsSource:
		return contentsSlotCollection{
			inner:     slotSourceCollection(k.From, ctx, depth+1),
			component: k.Component,
		}
	case registry.SlotGroupSource:
		if len(k.Terms) == 0 {
			return emptySlotCollection{}
		}
		terms := make(concatSlotCollection, len(k.Terms))
		for i, term := range k.Terms {
			terms[i] = slotSourceCollection(term, ctx, depth+1)
		}
		return terms
	}
	return emptySlotCollection{}
}

// slotRangeAccess reads a range off the context argument the source names.
// vanilla: RangeSlotSource.provide, SlotProvider.getSlotsFromRange
func slotRangeAccess(r registry.SlotRangeSource, ctx *LootContext) []SlotAccess {
	var entity LootEntity
	switch r.Source {
	case "container":
		if ctx.ContainerSlots != nil {
			return ctx.ContainerSlots(r.Slots)
		}
		return nil
	case "block_entity":
		return containerRangeAccess(ctx.Container, r.Slots)
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
	if entity.SlotAccess == nil {
		return nil
	}
	return entity.SlotAccess(r.Slots)
}

// containerRangeAccess reads a container.N or container.* range off a block
// entity's snapshot slots; any other range names slots a block entity has none of.
func containerRangeAccess(slots []protocol.ItemStack, name string) []SlotAccess {
	index, ok := strings.CutPrefix(name, "container.")
	if !ok {
		return nil
	}
	readOnly := func(stack protocol.ItemStack) SlotAccess {
		return SlotAccess{
			Get: func() protocol.ItemStack { return stack },
			Set: func(protocol.ItemStack) bool { return false },
		}
	}
	if index == "*" {
		out := make([]SlotAccess, len(slots))
		for i, s := range slots {
			out[i] = readOnly(s)
		}
		return out
	}
	n, err := strconv.Atoi(index)
	if err != nil || n < 0 || n >= len(slots) {
		return nil
	}
	return []SlotAccess{readOnly(slots[n])}
}
