package lifecycle

import (
	"errors"
	"io/fs"
	"os"
	"sync"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestShutdownTwice checks that a second Shutdown returns without touching
// the session lock the first already gave back, and that the test cleanup's
// CloseWorlds after it is just as quiet under the race detector.
func TestShutdownTwice(t *testing.T) {
	cfg := config.Default()
	cfg.OnlineMode = false
	srv := servertest.NewTestServer(t, cfg)

	var wg sync.WaitGroup
	for range 2 {
		wg.Go(srv.Shutdown)
	}
	wg.Wait()
	srv.Shutdown()
}

// TestALoginRacingAShutdownIsRefused logs a client in, stops the server while
// it is still in configuration, and then finishes the configuration: the
// join is refused with the shutdown's reason, the player never enters the
// world, and nothing is saved for them into the world the shutdown closed.
// vanilla: PlayerList.removeAll
func TestALoginRacingAShutdownIsRefused(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.LevelName = t.TempDir()
	srv, c := servertest.Dial(t, cfg)

	c.Login("Latecomer")
	srv.Shutdown()
	c.Configure()

	servertest.ExpectDisconnect(t, c, protocol.Text{Translate: "multiplayer.disconnect.server_shutdown"})
	servertest.AwaitServed(t, c.Served)
	if got := srv.PlayerCount(); got != 0 {
		t.Errorf("player count = %d, want 0 for a join refused by the shutdown", got)
	}
	if _, err := os.Stat(servertest.PlayerFile(cfg.LevelName, "Latecomer")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("the refused player's file: %v, want none written", err)
	}
}
