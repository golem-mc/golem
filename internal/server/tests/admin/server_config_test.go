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

// TestNegativeOpPermissionLevelClampsInConfig loads a server.properties whose
// op-permission-level is negative, and checks the running server: the console
// /op grants level 0 rather than the default owner level 4.
func TestNegativeOpPermissionLevelClampsInConfig(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), config.FileName)
	if err := os.WriteFile(path, []byte("op-permission-level=-3\n"), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	cfg.OnlineMode = false

	srv, _ := servertest.Start(t, cfg, servertest.WriteOpsFile(t))
	if reply := srv.RunLine("op Nobody"); len(reply) != 1 || reply[0].Plain() != "Made Nobody a server operator" {
		t.Fatalf("op Nobody answered %v, want Made Nobody a server operator", reply)
	}
	for _, record := range servertest.ReadOpsFile(t, srv) {
		if record.Name == "Nobody" {
			if record.Level != 0 {
				t.Errorf("op-permission-level=-3 granted level %d, want 0", record.Level)
			}
			return
		}
	}
	t.Errorf("ops.json has no entry for Nobody")
}
