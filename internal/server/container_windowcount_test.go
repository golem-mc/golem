package server

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/google/uuid"
)

func TestOpenContainerWindowCount(t *testing.T) {
	s := &Server{}
	c := &client{srv: s}
	other := &client{srv: s}
	for i, step := range []struct {
		c      *client
		window *openWindow
		want   int32
	}{
		{c, nil, 0},
		{c, &openWindow{}, 1},
		{c, &openWindow{}, 1},
		{other, &openWindow{}, 2},
		{c, nil, 1},
		{c, nil, 1},
		{other, nil, 0},
	} {
		step.c.setOpenWindow(step.window)
		if got := s.openContainerWindows.Load(); got != step.want {
			t.Fatalf("step %d: count = %d, want %d", i, got, step.want)
		}
	}
}

func TestContainerViewersWindowShortcut(t *testing.T) {
	s := &Server{clients: make(map[uuid.UUID]*client)}
	w := &world.World{}
	c := &client{srv: s, player: &player.Player{}}
	s.assignPlayerSlot(c)
	s.clients[uuid.Nil] = c
	viewers := func() int {
		n := 0
		s.containerViewers(w, 1, 2, 3, nil, func(*client) { n++ })
		return n
	}
	if got := viewers(); got != 0 {
		t.Fatalf("closed viewers = %d", got)
	}
	if got := s.containerOpenerCount(w, 1, 2, 3); got != 0 {
		t.Fatalf("closed openers = %d", got)
	}
	c.setOpenWindow(&openWindow{w: w, x: 1, y: 2, z: 3})
	if got := viewers(); got != 1 {
		t.Fatalf("open viewers = %d", got)
	}
	if got := s.containerOpenerCount(w, 1, 2, 3); got != 1 {
		t.Fatalf("open openers = %d", got)
	}
	c.player.SetVitals(player.Vitals{GameMode: protocol.GameModeSpectator})
	if got := s.containerOpenerCount(w, 1, 2, 3); got != 0 {
		t.Fatalf("spectator openers = %d", got)
	}
	if got := viewers(); got != 1 {
		t.Fatalf("spectator viewers = %d", got)
	}
	delete(s.clients, uuid.Nil)
	if got := viewers(); got != 0 {
		t.Fatalf("removed viewers = %d", got)
	}
	if got := s.containerOpenerCount(w, 1, 2, 3); got != 0 {
		t.Fatalf("removed openers = %d", got)
	}
	c.setOpenWindow(nil)
	if got := s.openContainerWindows.Load(); got != 0 {
		t.Fatalf("final count = %d", got)
	}
}
