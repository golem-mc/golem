package server

import (
	"sync"
	"testing"

	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
)

// TestMobNameByIDDoesNotRaceTheNameWrite loops mobNameByID's lookup against
// a name-tag write, both under mobsMu, for the race detector: nameMob writes
// a mob's name there, and a name read after an unlocked lookup is the race
// the lock exists to keep off the wire.
func TestMobNameByIDDoesNotRaceTheNameWrite(t *testing.T) {
	t.Parallel()
	w := &world.World{}
	s := withDims(&Server{}, []*dimension{{w: w, mobs: newMobHerd(w)}})
	h := s.herdOf(w)
	m := &entity.Mob{ID: 7}
	ent := &mobEntity{mob: m, w: w}
	h.herd = entity.Mobs{m}
	h.remember(ent)

	const turns = 2000
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for range turns {
			s.mobsMu.Lock()
			ent.name = nbt.String("Boss")
			s.mobsMu.Unlock()
		}
	}()
	go func() {
		defer wg.Done()
		for range turns {
			name, ok := s.mobNameByID(w, 7)
			if !ok {
				t.Error("the mob was not found by its entity ID")
				return
			}
			if name == "" {
				t.Error("the found mob's name was empty")
				return
			}
		}
	}()
	wg.Wait()
}
