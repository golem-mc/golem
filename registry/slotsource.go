package registry

import "sync/atomic"

// SlotSource names the slots a command or a loot entry reads, as one of the
// slot source types a data pack file or an inline compound spells.
// vanilla: SlotSources
type SlotSource interface{ isSlotSource() }

// SlotRangeSource is a slot range of a source: a context entity target
// (this, attacker, ...), block_entity, or container, the empty default.
// vanilla: RangeSlotSource
type SlotRangeSource struct {
	Source string
	Slots  string
}

// SlotFilteredSource keeps the slots whose item the filter matches.
// vanilla: FilteredSlotSource
type SlotFilteredSource struct {
	From   SlotSource
	Filter LootItemPredicate
}

// SlotLimitSource keeps the first Limit slots.
// vanilla: LimitSlotSource
type SlotLimitSource struct {
	From  SlotSource
	Limit int
}

// SlotContentsSource reads the slots inside each stack's container,
// bundle_contents or charged_projectiles component.
// vanilla: ContentsSlotSource
type SlotContentsSource struct {
	From      SlotSource
	Component string
}

// SlotGroupSource concatenates its terms.
// vanilla: GroupSlotSource
type SlotGroupSource struct{ Terms []SlotSource }

// SlotEmptySource has no slots.
// vanilla: EmptySlotSource
type SlotEmptySource struct{}

// SlotSourceReference names a data pack slot source.
type SlotSourceReference struct{ Name string }

func (SlotRangeSource) isSlotSource()     {}
func (SlotFilteredSource) isSlotSource()  {}
func (SlotLimitSource) isSlotSource()     {}
func (SlotContentsSource) isSlotSource()  {}
func (SlotGroupSource) isSlotSource()     {}
func (SlotEmptySource) isSlotSource()     {}
func (SlotSourceReference) isSlotSource() {}

var packSlotSources atomic.Pointer[map[string]SlotSource]

// ApplyPackSlotSources replaces the data packs' slot sources; nil clears.
// vanilla: RegistryDataLoader.RELOADABLE_REGISTRIES
func ApplyPackSlotSources(sources map[string]SlotSource) {
	if len(sources) == 0 {
		packSlotSources.Store(nil)
		return
	}
	packSlotSources.Store(&sources)
}

// FindSlotSource returns the slot source a data pack defines under the name.
func FindSlotSource(name string) (SlotSource, bool) {
	m := packSlotSources.Load()
	if m == nil {
		return nil, false
	}
	s, ok := (*m)[name]
	return s, ok
}
