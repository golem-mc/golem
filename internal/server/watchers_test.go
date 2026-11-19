package server

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/player"
	"github.com/google/uuid"
)

func TestTrackedWatchersSparseSnapshot(t *testing.T) {
	c := &client{slot: 1023, player: &player.Player{UUID: uuid.UUID{15: 1}}}
	var set watcherSet[int]
	value := 10
	set.set(c, &value)
	t.Run("sparse", func(t *testing.T) {
		ids := trackedWatchers(set)
		if len(ids) != 1 || ids[0] != c.player.UUID {
			t.Fatalf("observers = %v, want [%v]", ids, c.player.UUID)
		}
		if cap(ids) != 1 {
			t.Fatalf("snapshot capacity = %d, want 1 for one live observer", cap(ids))
		}
	})
	set.delete(c)
	t.Run("empty", func(t *testing.T) {
		ids := trackedWatchers(set)
		if len(ids) != 0 || cap(ids) != 0 {
			t.Fatalf("empty snapshot len = %d, cap = %d, want 0, 0", len(ids), cap(ids))
		}
	})
}

func TestWatcherSlotsReuseWithoutInheritingConnections(t *testing.T) {
	s := &Server{clients: make(map[uuid.UUID]*client)}
	join := func(id byte) *client {
		c := &client{player: &player.Player{UUID: uuid.UUID{15: id}}}
		s.assignPlayerSlot(c)
		s.clients[c.player.UUID] = c
		return c
	}
	old, other := join(1), join(2)
	var set watcherSet[int]
	a, b, c := 10, 20, 30
	set.set(old, &a)
	set.set(other, &b)
	s.removePlayer(old)
	next := join(3)
	if next.slot != old.slot || next.slotGeneration <= old.slotGeneration {
		t.Fatal("departed slot was not reused with a new generation")
	}
	if _, ok := set.get(next); ok {
		t.Fatal("new connection inherited the old observer state")
	}
	set.set(next, &c)
	set.delete(old)
	set.set(old, &a) // A snapshot taken before logout may finish after the new join.
	if got, ok := set.get(next); !ok || *got != c {
		t.Fatal("old snapshot changed the reused slot")
	}
	if got, ok := set.get(other); !ok || *got != b {
		t.Fatal("slot reuse changed another observer")
	}
	ids := trackedWatchers(set)
	if len(ids) != 2 || ids[0] != next.player.UUID || ids[1] != other.player.UUID {
		t.Fatalf("observers = %v", ids)
	}
	set.delete(next)
	if _, ok := set.get(next); ok {
		t.Fatal("deleted observer remains")
	}
}
