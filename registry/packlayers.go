package registry

// PackLayers holds the registry lookups used to decode candidate pack resources
// and compile functions. A nil *PackLayers reads the layers in force.
type PackLayers struct {
	tags        *TagLookup
	publishTags bool
	providers   *contextProviderLayer
	slotSources map[string]SlotSource
	predicates  map[string]LootCondition
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

// SetPredicates stages the decoded pack predicates for function compilation.
func (l *PackLayers) SetPredicates(predicates map[string]LootCondition) { l.predicates = predicates }

// FindPredicate reads a staged predicate or falls back to the built-ins.
func (l *PackLayers) FindPredicate(name string) (LootCondition, bool) {
	if l == nil {
		return FindPredicate(name)
	}
	if cond, ok := l.predicates[name]; ok {
		return cond, true
	}
	return findBuiltinPredicate(name)
}

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
