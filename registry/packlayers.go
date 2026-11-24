package registry

// PackLayers is the pack tags, context providers and slot sources in force:
// the layers a pack load publishes before its loot decodes against them.
type PackLayers struct {
	tags        *tagState
	itemTags    bool
	providers   *contextProviderLayer
	slotSources *map[string]SlotSource
}

// SnapshotPackLayers captures the layers now in force.
func SnapshotPackLayers() PackLayers {
	return PackLayers{
		tags:        tagsNow.Load(),
		itemTags:    itemTagsPacked.Load(),
		providers:   packContextProviders.Load(),
		slotSources: packSlotSources.Load(),
	}
}

// Restore publishes the captured layers again. It bumps the tag generation:
// caches keyed on it may have been filled from tags published since.
func (l PackLayers) Restore() {
	tagsNow.Store(l.tags)
	itemTagsPacked.Store(l.itemTags)
	packContextProviders.Store(l.providers)
	packSlotSources.Store(l.slotSources)
	tagsGeneration.Add(1)
}
