package registry

// PackLayers is a pack load's context providers and slot sources, which its
// loot and advancements resolve names against before Publish puts them in
// force, and the tags in force when the load began. A nil *PackLayers reads
// the layers in force.
type PackLayers struct {
	tags        *tagState
	itemTags    bool
	providers   *contextProviderLayer
	slotSources map[string]SlotSource
}

// StagePackLayers starts a pack load's layers: no pack context providers
// or slot sources yet, and the tags now in force.
func StagePackLayers() *PackLayers {
	return &PackLayers{tags: tagsNow.Load(), itemTags: itemTagsPacked.Load(), providers: &contextProviderLayer{}}
}

// SetContextProviders stages the packs' context int and float providers.
func (l *PackLayers) SetContextProviders(ints, floats map[string][]byte) {
	l.providers = &contextProviderLayer{ints: ints, floats: floats}
}

// SetSlotSources stages the packs' slot sources; the caller may add to the
// map until Publish.
func (l *PackLayers) SetSlotSources(sources map[string]SlotSource) { l.slotSources = sources }

// Publish puts the staged context providers and slot sources in force.
func (l *PackLayers) Publish() {
	packContextProviders.Store(l.providers)
	ApplyPackSlotSources(l.slotSources)
}

// RestoreTags puts the tags in force at StagePackLayers back. It bumps the
// tag generation: caches keyed on it may have been filled from tags since.
func (l *PackLayers) RestoreTags() {
	tagsNow.Store(l.tags)
	itemTagsPacked.Store(l.itemTags)
	tagsGeneration.Add(1)
}

// HasContextProvider reports whether a context provider entry has the
// qualified name, in the staged layer or a built-in one.
func (l *PackLayers) HasContextProvider(name string, float bool) bool {
	layer := contextProvidersNow()
	if l != nil {
		layer = l.providers
	}
	_, ok := layer.json(name, float)
	return ok
}

// HasSlotSource reports whether the packs define a slot source of the name.
func (l *PackLayers) HasSlotSource(name string) bool {
	if l == nil {
		_, ok := FindSlotSource(name)
		return ok
	}
	_, ok := l.slotSources[name]
	return ok
}
