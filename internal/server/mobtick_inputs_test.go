package server

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/google/uuid"
)

func TestMobTickPlayerSnapshots(t *testing.T) {
	s := dragonInteractionServer(t)
	for i := range 24 {
		p := &player.Player{UUID: uuid.New(), EntityID: int32(i + 1)}
		p.UpdateVitals(func(v *player.Vitals) {
			v.GameMode = int32(i % 4)
			v.Dead = i%5 == 0
			v.Health = float64(i + 1)
		})
		c := &client{srv: s, player: p}
		c.dim.Store(s.allDims()[i%len(s.allDims())])
		s.assignPlayerSlot(c)
		s.clients[p.UUID] = c
	}
	in := s.resetMobTickInputs()
	s.readMobTickInputs(in)
	seen := 0
	for w, snapshots := range in.players {
		for i := range snapshots {
			p := &snapshots[i]
			c := in.inWorld[w][i]
			if p.EntityID != c.player.EntityID || in.byEntityID[p.EntityID] != p || in.clientByEntityID[p.EntityID] != c {
				t.Fatalf("snapshot/client indexes disagree for player %d", p.EntityID)
			}
			v := c.player.Vitals()
			creative := v.GameMode == protocol.GameModeCreative
			spectator := v.GameMode == protocol.GameModeSpectator
			if p.Dead != v.Dead || p.Health != v.Health || p.Creative != creative || p.Spectator != spectator ||
				p.Huntable != (!v.Dead && !creative && !spectator) || p.Pushable != (!v.Dead && !spectator) {
				t.Fatalf("snapshot vitals disagree for player %d: %+v", p.EntityID, p)
			}
			c.player.UpdateVitals(func(v *player.Vitals) { v.Health = -1 })
			if p.Health != v.Health {
				t.Fatalf("live health update changed snapshot for player %d", p.EntityID)
			}
			seen++
		}
	}
	if seen != len(s.clients) {
		t.Fatalf("got %d snapshots, want %d", seen, len(s.clients))
	}
}
