package command

import (
	"fmt"
	"testing"

	"github.com/golem-mc/golem/internal/server/advancement"
	"github.com/golem-mc/golem/internal/server/player"
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

func (*cachedSelectorBody) EntityType() string { return "minecraft:armor_stand" }

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
