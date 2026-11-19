package rules

import (
	"math/rand"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/registry"
)

func TestComposterProviderDecodedOncePerLayer(t *testing.T) {
	decode := registry.DecodeContextProvider
	calls := 0
	registry.DecodeContextProvider = func(data []byte, float bool) (registry.LootNumber, string) {
		calls++
		return decode(data, float)
	}
	t.Cleanup(func() {
		registry.DecodeContextProvider = decode
		registry.ApplyPackContextProviders(nil, nil)
	})
	registry.ApplyPackContextProviders(nil, nil)
	stack := protocol.ItemStack{ItemID: int32(registry.ItemApple), Count: 1}
	state := registry.BlockComposter.DefaultState()
	rng := rand.New(rand.NewSource(1))
	for range 2 {
		if _, inserted, used := ComposterInsert(state, stack, &LootContext{}, rng); !inserted || !used {
			t.Fatal("default apple refused")
		}
	}
	if calls != 1 {
		t.Fatalf("two insertions decoded %d providers, want 1", calls)
	}
	for _, layers := range []string{"2", "3"} {
		registry.ApplyPackContextProviders(map[string][]byte{"minecraft:compostable/medium": []byte(layers)}, nil)
		for range 2 {
			after, _, _ := ComposterInsert(state, stack, &LootContext{}, rng)
			if registry.StateValue(after, registry.PropLevel) != layers {
				t.Fatalf("override %s produced level %s", layers, registry.StateValue(after, registry.PropLevel))
			}
		}
	}
	registry.ApplyPackContextProviders(nil, nil)
	after, _, _ := ComposterInsert(state, stack, &LootContext{}, rng)
	if registry.StateValue(after, registry.PropLevel) != "1" || calls != 4 {
		t.Fatalf("cleared layer: level %s, decodes %d; want 1, 4", registry.StateValue(after, registry.PropLevel), calls)
	}
}

func TestContextProviderCachedReferences(t *testing.T) {
	registry.ApplyPackContextProviders(map[string][]byte{
		"pk:a":     []byte(`{"type":"minecraft:negate","input":"pk:b"}`),
		"pk:b":     []byte(`{"type":"minecraft:from_float","input":"pk:b"}`),
		"pk:cycle": []byte(`{"type":"minecraft:negate","input":"pk:cycle"}`),
		"pk:bad":   []byte(`{`),
	}, map[string][]byte{"pk:b": []byte(`2.5`)})
	t.Cleanup(func() { registry.ApplyPackContextProviders(nil, nil) })
	for range 2 {
		if got, err := EvalIntProvider(registry.LootProviderRef{Name: "pk:a"}, &LootContext{}, nil); err != nil || got != -2 {
			t.Fatalf("int/float reference chain = %d, %v", got, err)
		}
		for name, want := range map[string]string{
			"pk:cycle":   "context provider pk:cycle refers to itself",
			"pk:missing": "Unknown element: pk:missing",
			"pk:bad":     "pk:bad: unexpected EOF",
		} {
			_, err := EvalIntProvider(registry.LootProviderRef{Name: name}, &LootContext{}, nil)
			if err == nil || err.Error() != want {
				t.Errorf("%s: error %v, want %q", name, err, want)
			}
		}
	}
}

func TestContextProviderCacheConcurrent(t *testing.T) {
	decode := registry.DecodeContextProvider
	var calls atomic.Int32
	registry.DecodeContextProvider = func(data []byte, float bool) (registry.LootNumber, string) {
		calls.Add(1)
		return decode(data, float)
	}
	registry.ApplyPackContextProviders(map[string][]byte{"pk:n": []byte(`2`)}, nil)
	t.Cleanup(func() {
		registry.DecodeContextProvider = decode
		registry.ApplyPackContextProviders(nil, nil)
	})
	var wg sync.WaitGroup
	for range 16 {
		wg.Go(func() {
			got, err := EvalIntProvider(registry.LootProviderRef{Name: "pk:n"}, &LootContext{}, nil)
			if err != nil || got != 2 {
				t.Errorf("provider = %d, %v", got, err)
			}
		})
	}
	wg.Wait()
	if got := calls.Load(); got != 1 {
		t.Fatalf("concurrent evaluations decoded %d times, want 1", got)
	}
}
