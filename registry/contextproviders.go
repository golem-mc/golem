package registry

import (
	"sync"
	"sync/atomic"
)

// packContextProviders is the data packs' layer over the built-in context
// providers, nil before the first pack load.
var packContextProviders atomic.Pointer[contextProviderLayer]
var builtinContextProviders contextProviderLayer

type contextProviderLayer struct {
	ints, floats map[string][]byte
	mu           sync.Mutex
	decoded      map[LootProviderRef]decodedContextProvider
}

type decodedContextProvider struct {
	number  LootNumber
	message string
}

// ApplyPackContextProviders makes the data packs' context int and float
// providers (JSON by qualified name) override the built-in ones. Empty maps
// clear the layer. Each new layer starts with an empty decoded cache.
func ApplyPackContextProviders(ints, floats map[string][]byte) {
	packContextProviders.Store(&contextProviderLayer{ints: ints, floats: floats})
}

func contextProvidersNow() *contextProviderLayer {
	if l := packContextProviders.Load(); l != nil {
		return l
	}
	return &builtinContextProviders
}

func builtinContextProviderEntries(float bool) map[string]string {
	if float {
		return contextFloatProviderJSON
	}
	return contextIntProviderJSON
}

// HasBuiltinContextProvider reports whether the vanilla pack defines a provider.
func HasBuiltinContextProvider(name string, float bool) bool {
	_, ok := builtinContextProviderEntries(float)[name]
	return ok
}

func (l *contextProviderLayer) json(name string, float bool) ([]byte, bool) {
	entries := l.ints
	if float {
		entries = l.floats
	}
	if b, ok := entries[name]; ok {
		return b, true
	}
	s, ok := builtinContextProviderEntries(float)[name]
	return []byte(s), ok
}

// ContextIntProviderJSON returns the JSON of the context int provider entry
// with the qualified name, a data pack's before the built-in one.
// vanilla: ContextIntProviders
func ContextIntProviderJSON(name string) ([]byte, bool) {
	return contextProvidersNow().json(name, false)
}

// ContextFloatProviderJSON is ContextIntProviderJSON for float providers.
// vanilla: ContextFloatProviders
func ContextFloatProviderJSON(name string) ([]byte, bool) {
	return contextProvidersNow().json(name, true)
}

// ResolveContextProvider returns a registry entry decoded once per pack layer,
// or its codec error. References remain holders, resolved during evaluation.
func ResolveContextProvider(ref LootProviderRef) (LootNumber, string) {
	l := contextProvidersNow()
	l.mu.Lock()
	defer l.mu.Unlock()
	if n, ok := l.decoded[ref]; ok {
		return n.number, n.message
	}
	raw, ok := l.json(ref.Name, ref.Float)
	if !ok || DecodeContextProvider == nil {
		return nil, "Unknown element: " + ref.Name
	}
	n, msg := DecodeContextProvider(raw, ref.Float)
	if msg != "" {
		msg = ref.Name + ": " + msg
	}
	if l.decoded == nil {
		l.decoded = make(map[LootProviderRef]decodedContextProvider)
	}
	l.decoded[ref] = decodedContextProvider{n, msg}
	return n, msg
}
