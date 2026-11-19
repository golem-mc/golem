package entities

import (
	"fmt"
	"github.com/golem-mc/golem/internal/server/servertest"
	"testing"
	"time"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
)

// TestPlayerTrackersWhileMovingTeleportingAndRespawning keeps a player
// reporting steps while an operator teleports them and while they die and
// respawn, all against the running tick. The step's news, the teleport's,
// the respawn's and the environment tick's are written from four different
// goroutines, so the race detector is what sees whether the per-client
// bookkeeping behind them — where the others were last told the player is,
// and what their metadata last said about fire and air — stays ordered.
func TestPlayerTrackersWhileMovingTeleportingAndRespawning(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	// The readers below swallow everything but the keep-alives, and the
	// flood of steps and respawns can outpace them under the race detector,
	// so the exchange is pushed out of the test's way.
	srv, connect := servertest.Start(t, cfg, func(s *server.Server) {
		s.SetKeepAlive(time.Hour, servertest.DefaultKeepAliveTimeout)
	}, servertest.OpsListing(t, "Operator"))

	mover := connect().Join("Mover")

	operator := connect().Join("Operator")

	// One reader per connection, drinking from the mailbox so the server's
	// sends never block, answering any keep-alive on the way. The mover's
	// reader signals each Respawn packet, which is what tells the test the
	// previous death has been answered before the next blow lands.
	respawns := make(chan struct{}, 10)
	drain := func(c *servertest.Client, respawns chan<- struct{}) {
		for {
			id, data, err := c.ReadBy(time.Now().Add(time.Minute))
			if err != nil {
				return
			}
			if id == protocol.IDPlayRespawn && respawns != nil {
				respawns <- struct{}{}
			}
			if id == protocol.IDPlayKeepAlive {
				c.AnswerKeepAlive(data)
			}
		}
	}
	go drain(mover, respawns)
	go drain(operator, nil)

	// Steps and teleports interleaved: the read loop records where the
	// others were last told the player is, the operator's command runner
	// resets it from another goroutine.
	const steps = 200
	for i := range steps {
		operator.ChatCommand(fmt.Sprintf("tp Mover %d 64 %d", i%8+2, i%8+2))
		mover.MoveTo(float64(i)+0.5, 64, 0.5, 0, 0)
	}

	// Death and respawn while the tick keeps running: the respawn resets
	// the metadata baselines on the read loop's goroutine at the same
	// moment the tick's environment pass is measuring against them. The
	// respawn restarts the client-load grace, so each round opens its
	// loading screen the way a client does before the blow lands.
	for i := range 10 {
		mover.MoveTo(float64(i)+0.5, 64, 0.5, 0, 0)
		mover.Send(protocol.IDPlayLoaded, func(*protocol.Buffer) {})
		servertest.WaitFor(t, srv, 200, func() bool { return srv.Hurt("Mover", 1000) })
		mover.Send(protocol.IDPlayClientCommandIn, func(b *protocol.Buffer) {
			b.PutVarInt(protocol.ClientCommandPerformRespawn)
		})
		<-respawns
	}

	if srv.PlayerGameMode("Mover") < 0 {
		t.Error("the mover was gone from the world when the test ended")
	}
}
