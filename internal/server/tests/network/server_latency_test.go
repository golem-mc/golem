package network

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
)

// readLatencyUpdate returns the latency the next Player Info Update that is
// only an UPDATE_LATENCY carries for id.
func readLatencyUpdate(c *servertest.Client, id uuid.UUID) int32 {
	c.T.Helper()
	for {
		r := protocol.NewReader(c.AwaitPlay(protocol.IDPlayPlayerInfoUpdate))
		if actions := r.ReadU8(); actions != 0x10 {
			continue
		}
		for range r.ReadVarInt() {
			u := r.ReadUUID()
			latency := r.ReadVarInt()
			if u == id {
				return latency
			}
		}
	}
}

// idleClock is the keep-alive clock the latency test moves by hand.
type idleClock struct{ nanos atomic.Int64 }

func (c *idleClock) now() time.Time          { return time.Unix(0, c.nanos.Load()) }
func (c *idleClock) advance(d time.Duration) { c.nanos.Add(int64(d)) }

// TestTabListLatencyFollowsKeepAliveRoundTrip answers a keep-alive 400 ms
// late and expects the periodic latency broadcast to carry (0*3+400)/4.
func TestTabListLatencyFollowsKeepAliveRoundTrip(t *testing.T) {
	t.Parallel()
	clock := servertest.NewStepClock()
	ka := &idleClock{}
	ka.nanos.Store(time.Unix(1_000_000, 0).UnixNano())
	srv, c := servertest.Dial(t, servertest.BlockTestConfig(), func(s *server.Server) {
		s.SetTickClock(clock, nil)
		s.SetKeepAliveClock(ka.now)
	})
	id := c.Login("Pinger")
	c.Configure()
	c.EnterWorld("Pinger")
	srv.SetKeepAlive(time.Second, time.Hour)

	ka.advance(2 * time.Second)
	servertest.StepTicks(t, srv, clock, 1)
	var ping []byte
	for ping == nil {
		// Not awaitPlay, which answers keep-alives on the spot.
		pid, data, err := c.ReadBy(time.Now().Add(servertest.ExpectTimeout))
		if err != nil {
			t.Fatalf("waiting for the keep-alive: %v", err)
		}
		if pid == protocol.IDPlayKeepAlive {
			ping = data
		}
	}
	ka.advance(400 * time.Millisecond)
	c.AnswerKeepAlive(ping)

	// The answer is read while the loop steps on; the broadcast is 601
	// ticks after the last one.
	servertest.StepTicks(t, srv, clock, 601)
	got := readLatencyUpdate(c, id)
	if got != 100 {
		t.Errorf("broadcast latency = %d, want 100", got)
	}
}
