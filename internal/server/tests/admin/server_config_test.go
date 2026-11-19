package admin

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"os"
	"path/filepath"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
)

// TestNumericGamemodeAndDifficultyInConfig loads a server.properties whose
// gamemode and difficulty are numbers, and checks the running server: the
// joining player is creative and the world is hard.
func TestNumericGamemodeAndDifficultyInConfig(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), config.FileName)
	if err := os.WriteFile(path, []byte("gamemode=1\ndifficulty=3\nwhite-list=false\n"), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	cfg.OnlineMode = false

	srv, _ := servertest.Join(t, cfg, "Testificate")

	if got := srv.PlayerGameMode("Testificate"); got != protocol.GameModeCreative {
		t.Errorf("game mode = %d, want %d (creative)", got, protocol.GameModeCreative)
	}
	if got := srv.Difficulty(); got != 3 {
		t.Errorf("difficulty = %d, want 3 (hard)", got)
	}
}
