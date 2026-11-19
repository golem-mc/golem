package server

import (
	"net/netip"
	"testing"
	"time"
)

// TestQueryChallengeBookDropsTheOldestPastTheBound pins the bound on the
// live tokens: a mint past it drops the oldest one alone, so the newest
// handshakes keep theirs, and the order a re-mint leaves stale is compacted
// rather than growing with the handshakes.
func TestQueryChallengeBookDropsTheOldestPastTheBound(t *testing.T) {
	now := time.Unix(0, 0)
	b := newQueryChallengeBook()
	b.now = func() time.Time { return now }
	addr := func(i int) netip.AddrPort {
		return netip.AddrPortFrom(netip.AddrFrom4([4]byte{10, byte(i >> 16), byte(i >> 8), byte(i)}), 25565)
	}
	for i := range maxQueryChallenges + 1 {
		b.mint(addr(i), [4]byte{}, int32(i))
	}
	if _, ok := b.lookup(addr(0)); ok {
		t.Error("the oldest token outlived a mint past the bound")
	}
	for _, i := range []int{1, maxQueryChallenges} {
		if c, ok := b.lookup(addr(i)); !ok || c.token != int32(i) {
			t.Errorf("token %d = %v, %v; want it kept", i, c, ok)
		}
	}

	for i := range 3 * maxQueryChallenges {
		b.mint(addr(1), [4]byte{}, int32(i))
	}
	if len(b.order) >= 2*maxQueryChallenges || len(b.byAddr) > maxQueryChallenges {
		t.Errorf("re-mints left %d in the order and %d live, want both bounded", len(b.order), len(b.byAddr))
	}
}
