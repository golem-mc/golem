package registry

// PackLayers holds the tags, context providers and slot sources used to
// decode candidate pack resources. A nil *PackLayers reads the layers in force.
type PackLayers struct {
	tags        *TagLookup
	publishTags bool
	providers   *contextProviderLayer
	slotSources map[string]SlotSource
}

// StagePackLayers starts a pack load's layers: no pack context providers
// or slot sources yet, and the tags now in force.
func StagePackLayers() *PackLayers {
	return &PackLayers{tags: CurrentTagLookup(), providers: &contextProviderLayer{}}
}

// SetTags stages a replacement tag lookup for decoding and publication.
func (l *PackLayers) SetTags(tags *TagLookup) {
	l.tags = tags
	l.publishTags = true
}

// TagLookup returns the tag snapshot used to decode pack resources.
func (l *PackLayers) TagLookup() *TagLookup {
	if l == nil {
		return CurrentTagLookup()
	}
	return l.tags
}

// SetContextProviders stages the packs' context int and float providers.
func (l *PackLayers) SetContextProviders(ints, floats map[string][]byte) {
	l.providers = &contextProviderLayer{ints: ints, floats: floats}
}

// SetSlotSources stages the packs' slot sources; the caller may add to the
// map until Publish.
func (l *PackLayers) SetSlotSources(sources map[string]SlotSource) { l.slotSources = sources }

// Publish puts the staged tags, context providers and slot sources in force.
func (l *PackLayers) Publish() {
	if l.publishTags {
		l.tags.Publish()
	}
	packContextProviders.Store(l.providers)
	ApplyPackSlotSources(l.slotSources)
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
