package mobs

import (
	"math"
	"slices"
	"sync"
	"testing"
)

// TestAShulkerBoxGrowsInTheTickAsTheShellOpens: the box a shell's opening
// grows is the one the server ticks and tells arrows and blows about.
// vanilla: Shulker.tick, updatePeekAmount
func TestAShulkerBoxGrowsInTheTickAsTheShellOpens(t *testing.T) {
	t.Parallel()
	srv, _, y := shulkerTests(t)
	at := srv.PlayerPosition("Watcher")
	id := srv.SpawnMob("minecraft:shulker", math.Floor(at.X)+40.5, y, math.Floor(at.Z)+0.5)
	if _, _, _, _, top, _, _ := srv.MobBox(id); top != y+1 {
		t.Fatalf("a shut shulker's box tops out at %v, want %v", top, y+1)
	}
	srv.SetShulkerPeek(id, 100)
	srv.TickMobs(5)
	_, _, _, _, top, _, _ := srv.MobBox(id)
	if top <= y+1.1 {
		t.Errorf("five ticks into an opening shell the box tops out at %v, want it grown past %v", top, y+1.1)
	}
}

// gameEvents collects the game events posted at a place.
type gameEvents struct {
	mu   sync.Mutex
	seen map[string][][3]float64
}

func (g *gameEvents) add(event string, x, y, z float64) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.seen[event] = append(g.seen[event], [3]float64{x, y, z})
}

func (g *gameEvents) at(event string, x, y, z float64) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return slices.Contains(g.seen[event], [3]float64{x, y, z})
}
