package command

import (
	"fmt"
	"sync"
	"testing"

	"github.com/golem-mc/golem/internal/server/advancement"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/registry"
)

func TestParsedSelectorCache(t *testing.T) {
	selectorCache.Lock()
	clear(selectorCache.entries)
	selectorCache.Unlock()

	first, err := parsedSelector("@s", true)
	if err != nil {
		t.Fatal(err)
	}
	again, err := parsedSelector("@s", true)
	if err != nil || again != first {
		t.Fatal("successful parse was not reused")
	}
	if _, err := parsedSelector("@s", false); err == nil || err.Key != keySelectorNotAllowed {
		t.Fatalf("cached privileged selector bypassed permission: %v", err)
	}
	for range 2 {
		if _, err := parsedSelector("@e[", true); err == nil {
			t.Fatal("invalid selector parsed")
		}
	}
	selectorCache.Lock()
	n := len(selectorCache.entries)
	selectorCache.Unlock()
	if n != 1 {
		t.Fatalf("failed parses entered cache: %d entries", n)
	}
	for i := range 1024 {
		if _, err := parsedSelector(fmt.Sprintf("player%d", i), false); err != nil {
			t.Fatal(err)
		}
	}
	selectorCache.Lock()
	n = len(selectorCache.entries)
	selectorCache.Unlock()
	if n != 0 {
		t.Fatalf("cache not emptied past 1024 entries: %d", n)
	}
}

type cachedSelectorBody struct {
	forkBody
	x float64
}

func (e *cachedSelectorBody) Position() player.Position { return player.Position{X: e.x} }
func (e *cachedSelectorBody) Box() (float64, float64, float64, float64, float64, float64) {
	return e.x - .3, 0, -.3, e.x + .3, 1, .3
}

type cachedSelectorSource struct{ forkSource }

func (*cachedSelectorSource) OnlineTargetsIn(string) []Target { return nil }

func TestParsedSelectorConcurrentResolution(t *testing.T) {
	const word = "@e[distance=..10,sort=nearest,limit=2]"
	first, err := parsedSelector(word, true)
	if err != nil {
		t.Fatal(err)
	}
	near, far := &cachedSelectorBody{x: 1}, &cachedSelectorBody{x: 5}
	src := &cachedSelectorSource{forkSource{bodies: []Entity{far, &cachedSelectorBody{x: 20}, near}}}
	var wg sync.WaitGroup
	for range 2 {
		wg.Go(func() {
			for range 100 {
				sel, err := parsedSelector(word, true)
				if err != nil || sel != first {
					t.Error("concurrent resolution did not reuse selector")
					return
				}
				optional := resolveOptionalEntities(src, word)
				required, key := resolveRequiredEntities(src, word)
				resolved, resolveErr := resolveAs(src, word, argumentEntityNotFoundKey, false)
				if key != "" || resolveErr != nil {
					t.Errorf("resolution failed: %s, %v", key, resolveErr)
					return
				}
				for _, found := range [][]Entity{optional, required, resolved} {
					if len(found) != 2 || found[0] != near || found[1] != far {
						t.Errorf("cached selector changed selection or order: %v", found)
						return
					}
				}
			}
		})
	}
	wg.Wait()
}

func (*cachedSelectorBody) EntityType() string { return "minecraft:armor_stand" }

func TestParsedSelectorSeesTagReload(t *testing.T) {
	t.Cleanup(func() { registry.ApplyPackTags(nil) })
	const word = "@e[type=#test:cached_selector]"
	body := &cachedSelectorBody{}
	src := &cachedSelectorSource{forkSource{bodies: []Entity{body}}}
	sel, err := parsedSelector(word, true)
	if err != nil {
		t.Fatal(err)
	}
	if got := resolveOptionalEntities(src, word); len(got) != 0 {
		t.Fatalf("unknown tag selected %v", got)
	}
	registry.ApplyPackTags(registry.PackTags{"minecraft:entity_type": {
		"test:cached_selector": {Entries: []registry.PackTagEntry{{ID: "minecraft:armor_stand", Required: true}}},
	}})
	again, err := parsedSelector(word, true)
	if err != nil || again != sel {
		t.Fatal("reload test did not reuse selector")
	}
	if got := resolveOptionalEntities(src, word); len(got) != 1 || got[0] != body {
		t.Fatalf("reloaded tag selected %v", got)
	}
	registry.ApplyPackTags(nil)
	if got := resolveOptionalEntities(src, word); len(got) != 0 {
		t.Fatalf("removed tag selected %v", got)
	}
}

type cachedAdvancementTarget struct {
	Target
	progress *advancement.Progress
}

func (e *cachedAdvancementTarget) Advancements() *advancement.Progress { return e.progress }

type cachedAdvancementSource struct {
	Source
	target Target
}

func (*cachedAdvancementSource) PermLevel() int            { return PermLevelOwners }
func (*cachedAdvancementSource) Position() player.Position { return player.Position{} }
func (s *cachedAdvancementSource) OnlineTargets() []Target { return []Target{s.target} }

func TestParsedSelectorSeesAdvancementReload(t *testing.T) {
	t.Cleanup(func() { advancement.ApplyPack(nil) })
	const word = "@a[advancements={test:cached_selector=false}]"
	target := &cachedAdvancementTarget{progress: advancement.NewProgress(advancement.Current())}
	src := &cachedAdvancementSource{target: target}
	sel, err := parsedSelector(word, true)
	if err != nil {
		t.Fatal(err)
	}
	if got := resolveOptionalEntities(src, word); len(got) != 0 {
		t.Fatalf("unknown advancement selected %v", got)
	}
	tree := advancement.ApplyPack([]registry.Advancement{{ID: "test:cached_selector", Criteria: []registry.AdvancementCriterion{{Name: "test", Trigger: "minecraft:impossible"}}, Requirements: [][]string{{"test"}}}})
	target.progress.Reload(tree)
	again, err := parsedSelector(word, true)
	if err != nil || again != sel {
		t.Fatal("reload test did not reuse selector")
	}
	if got := resolveOptionalEntities(src, word); len(got) != 1 || got[0] != target {
		t.Fatalf("reloaded advancement selected %v", got)
	}
	target.progress.Reload(advancement.ApplyPack(nil))
	if got := resolveOptionalEntities(src, word); len(got) != 0 {
		t.Fatalf("removed advancement selected %v", got)
	}
}
