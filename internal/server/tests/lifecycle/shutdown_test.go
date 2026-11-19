package lifecycle

import (
	"sync"
	"testing"

	"github.com/golem-mc/golem/internal/config"
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
