package management

import (
	"encoding/json"
	"testing"
	"time"
)

// vanilla: ServerActivityMonitor
func TestLoginActivityIsRateLimited(t *testing.T) {
	t.Parallel()
	for gap, want := range map[time.Duration]int{29 * time.Second: 1, 30 * time.Second: 2} {
		now := time.Unix(1000, 0)
		s := &Server{conns: map[*conn]struct{}{}, now: func() time.Time { return now }}
		c := &conn{out: make(chan json.RawMessage, 8)}
		s.conns[c] = struct{}{}
		s.LoginActivity()
		now = now.Add(gap)
		s.LoginActivity()
		if got := len(c.out); got != want {
			t.Errorf("logins %v apart sent %d server/activity, want %d", gap, got, want)
		}
	}
}
