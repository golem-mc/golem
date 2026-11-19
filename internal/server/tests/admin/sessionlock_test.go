package admin

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/server"
)

// TestASecondServerCannotOpenAHeldWorldFolder checks that a server takes the
// session lock on its world folder: a second server on the same folder fails
// with the vanilla already-locked text rather than running beside the first,
// and stopping the first lets the next one in.
func TestASecondServerCannotOpenAHeldWorldFolder(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.LevelName = t.TempDir()
	first, _ := servertest.Start(t, cfg)

	restarted := *cfg
	if second, err := server.New(&restarted); err == nil {
		second.Shutdown()
		t.Fatal("a second server opened a world folder a server already held")
	} else if !strings.Contains(err.Error(), "already locked") {
		t.Errorf("second server error = %v, want the vanilla already-locked text", err)
	}

	first.Shutdown()
	after := *cfg
	third, err := server.New(&after)
	if err != nil {
		t.Fatalf("after the first server stopped, a server could not open the world: %v", err)
	}
	third.Shutdown()
}
