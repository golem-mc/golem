package server

import (
	"sync"
	"testing"

	"github.com/golem-mc/golem/internal/server/world"
)

// TestRandomNumberDrawsDoesNotRaceSave draws one named sequence from two
// goroutines while a third saves it, for the race detector: a command runs
// on its own client's read goroutine, so two players drawing one name and
// the autosave must not touch one stream at once.
func TestRandomNumberDrawsDoesNotRaceSave(t *testing.T) {
	t.Parallel()
	sequences := world.NewRandomSequences(t.TempDir())
	draws := randomNumberDraws{sequences: sequences, id: "minecraft:dice", seed: 7}

	const turns = 200
	var wg sync.WaitGroup
	wg.Add(3)
	for range 2 {
		go func() {
			defer wg.Done()
			for range turns {
				draws.IntBetween(1, 6)
			}
		}()
	}
	go func() {
		defer wg.Done()
		for range turns {
			sequences.Save()
		}
	}()
	wg.Wait()
}
