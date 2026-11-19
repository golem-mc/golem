package rules

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/player"
)

func TestMovedTooQuickly(t *testing.T) {
	for _, tc := range []struct {
		name       string
		movedSq    float64
		expectedSq float64
		packets    int
		fallFlying bool
		want       bool
	}{
		{"ten blocks on one packet", 100, 0, 1, false, false},
		{"past ten blocks on one packet", 100.01, 0, 1, false, true},
		{"second packet of the tick", 199, 0, 2, false, false},
		{"five packets", 499, 0, 5, false, false},
		{"a burst past five counts as one", 101, 0, 6, false, true},
		{"carried velocity", 150, 60, 1, false, false},
		{"gliding", 299, 0, 1, true, false},
		{"past the glide", 301, 0, 1, true, true},
	} {
		if got := MovedTooQuickly(tc.movedSq, tc.expectedSq, tc.packets, tc.fallFlying); got != tc.want {
			t.Errorf("%s: MovedTooQuickly(%v, %v, %d, %v) = %v, want %v",
				tc.name, tc.movedSq, tc.expectedSq, tc.packets, tc.fallFlying, got, tc.want)
		}
	}
}

func TestMoveCheckJudge(t *testing.T) {
	at := func(x float64) player.Position { return player.Position{X: x, Yaw: 1, Pitch: 2} }
	step := func(tick uint64, from, to float64) MoveStep {
		return MoveStep{Tick: tick, Was: at(from), To: at(to), TickRunning: true, MovementCheck: true, ElytraCheck: true}
	}
	t.Run("measures from the tick's first position", func(t *testing.T) {
		var m MoveCheck
		if m.Judge(step(1, 0, 6)).Refused {
			t.Fatal("6 blocks refused")
		}
		// 144 squared is within two packets' 200.
		if m.Judge(step(1, 6, 12)).Refused {
			t.Fatal("12 blocks over two packets refused")
		}
		if v := m.Judge(step(1, 12, 18)); !v.Refused || !v.TooQuick || v.Back.X != 12 {
			t.Fatalf("18 blocks over three packets: %+v, want refused back to the body", v)
		}
	})
	t.Run("a new tick moves the anchor", func(t *testing.T) {
		var m MoveCheck
		m.Judge(step(1, 0, 9))
		if m.Judge(step(2, 9, 18)).Refused {
			t.Fatal("9 blocks from the new tick's start refused")
		}
	})
	t.Run("a teleport moves the anchor within the tick", func(t *testing.T) {
		var m MoveCheck
		m.Judge(step(1, 0, 1))
		m.Reset(500, 0, 0)
		if m.Judge(step(1, 500, 505)).Refused {
			t.Fatal("5 blocks from the teleport refused")
		}
	})
	t.Run("a burst falls back to one packet's budget", func(t *testing.T) {
		var m MoveCheck
		for range 5 {
			m.Judge(step(1, 0, 0))
		}
		if !m.Judge(step(1, 0, 11)).Refused {
			t.Fatal("sixth packet's 121 squared taken")
		}
	})
	t.Run("gliding has three hundred", func(t *testing.T) {
		var m MoveCheck
		s := step(1, 0, 17)
		s.FallFlying = true
		if m.Judge(s).Refused {
			t.Fatal("289 squared refused while gliding")
		}
		m = MoveCheck{}
		s = step(1, 0, 18)
		s.FallFlying = true
		if !m.Judge(s).Refused {
			t.Fatal("324 squared taken while gliding")
		}
	})
	t.Run("the rules switch the check off", func(t *testing.T) {
		for _, tc := range []struct {
			name  string
			apply func(*MoveStep)
		}{
			{"player_movement_check", func(s *MoveStep) { s.MovementCheck = false }},
			{"elytra_movement_check", func(s *MoveStep) { s.FallFlying, s.ElytraCheck = true, false }},
			{"frozen tick rate", func(s *MoveStep) { s.TickRunning = false }},
		} {
			var m MoveCheck
			s := step(1, 0, 40)
			tc.apply(&s)
			if m.Judge(s).Refused {
				t.Errorf("%s: far step refused", tc.name)
			}
		}
	})
	t.Run("a step into a block goes back with the packet's look", func(t *testing.T) {
		var m MoveCheck
		s := step(1, 0, 1)
		s.To.Yaw = 45
		s.Collides = func() bool { return true }
		v := m.Judge(s)
		if !v.Refused || v.TooQuick || v.Back.X != 0 || v.Back.Yaw != 45 {
			t.Fatalf("got %+v, want back to x=0 facing 45", v)
		}
		s.Spectator = true
		if m.Judge(s).Refused {
			t.Fatal("spectator refused")
		}
	})
}
