package network

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestQueryTokenExpires pins the token's life: a stat carrying it is answered
// until thirty seconds after the handshake minted it and refused from then
// on, until the next handshake mints a fresh one.
// vanilla: QueryThreadGs4.pruneChallenges
func TestQueryTokenExpires(t *testing.T) {
	t.Parallel()
	var elapsed atomic.Int64
	start := time.Unix(1_000_000, 0)
	srv, _ := servertest.Start(t, queryConfig(), func(s *server.Server) {
		s.SetQueryClock(func() time.Time { return start.Add(time.Duration(elapsed.Load())) })
	})
	q := dialQuery(t, startQueryConn(t, srv))
	session := [4]byte{1, 2, 3, 4}
	stat := func(token int32, wait time.Duration) []byte {
		t.Helper()
		if _, err := q.Write(statRequest(session, token, 0)); err != nil {
			t.Fatalf("send a stat: %v", err)
		}
		return queryReply(t, q, wait)
	}
	// An answer is waited for long, as under load; a refusal is waited for
	// briefly, since only silence is expected.
	answered, refused := 2*time.Second, 200*time.Millisecond

	token := queryHandshake(t, q, session)
	elapsed.Store(int64(29 * time.Second))
	if stat(token, answered) == nil {
		t.Error("a stat 29 seconds after the handshake went unanswered")
	}
	elapsed.Store(int64(30 * time.Second))
	if got := stat(token, refused); got != nil {
		t.Errorf("a stat 30 seconds after the handshake answered: %q", got)
	}
	token = queryHandshake(t, q, session)
	if stat(token, answered) == nil {
		t.Error("a stat after a fresh handshake went unanswered")
	}
}
