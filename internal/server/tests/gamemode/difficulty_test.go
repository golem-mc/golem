package gamemode

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"os"
	"path/filepath"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/nbt"
)

// expectHealthWithin runs up to n held ticks until a Set Health arrives and
// reads it.
func expectHealthWithin(t *testing.T, srv *server.Server, c *servertest.Client, n int) servertest.HealthReport {
	t.Helper()
	r := protocol.NewReader(c.ExpectWithinTicks(srv, n, protocol.IDPlaySetHealth))
	h := servertest.HealthReport{Health: r.ReadF32(), Food: r.ReadVarInt(), Saturation: r.ReadF32()}
	if err := r.Err(); err != nil {
		t.Fatalf("set health: %v", err)
	}
	return h
}

// hungryPlayerFile writes a player file that arrives starving: no food, no
// saturation, and whatever health the test asks for. It is the only way a
// player can arrive hungry, since a living join starts full.
func hungryPlayerFile(t *testing.T, level, name string, health float64) {
	t.Helper()
	root := nbt.Compound{
		"playerGameType":      nbt.Int(0),
		"Health":              nbt.Float(health),
		"foodLevel":           nbt.Int(0),
		"foodSaturationLevel": nbt.Float(0),
		"foodExhaustionLevel": nbt.Float(0),
	}
	data, err := nbt.Encode("", root)
	if err != nil {
		t.Fatalf("encode player file: %v", err)
	}
	path := servertest.PlayerFile(level, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create player directory: %v", err)
	}
	if err := servertest.WriteGzip(path, data); err != nil {
		t.Fatalf("write player file: %v", err)
	}
}

// TestDifficultyCommandSetsTheWorld checks /difficulty hard: the runner is
// told, every player in the world is sent the Change Difficulty that makes
// their options menu agree, and the world's level.dat carries the new value
// once it is saved.
func TestDifficultyCommandSetsTheWorld(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.LevelName = t.TempDir()

	srv, connect := servertest.Start(t, cfg, servertest.OpsListing(t, "Testificate"))
	if got := srv.Difficulty(); got != 1 {
		t.Fatalf("starting difficulty = %d, want 1 (easy)", got)
	}

	c := connect().Join("Testificate")

	c.ChatCommand("difficulty hard")
	// The feedback names the difficulty by its display name, which is the
	// translation the client renders in the language it plays in.
	c.ExpectComponentFeedback("commands.difficulty.success",
		protocol.Text{Translate: "options.difficulty.hard"})

	// The change reaches the whole world, not only the runner.
	event := protocol.NewReader(c.ExpectPlay(protocol.IDPlayDifficulty))
	if got := event.ReadU8(); got != 3 {
		t.Errorf("change difficulty = %d, want 3 (hard)", got)
	}
	if got := event.ReadBool(); got {
		t.Error("difficulty is locked, want it free")
	}

	// And the world keeps it: a save writes the level.dat the game reads.
	srv.Shutdown()
	root := servertest.ReadSavedFile(t, filepath.Join(cfg.LevelName, "level.dat"), "Data")
	if got, ok := nbt.Get[nbt.Byte](root, "Difficulty"); !ok || got != 3 {
		t.Errorf("level.dat Difficulty = %v, %v; want 3", got, ok)
	}
}

// TestStarvationKillsOnHard checks the hard floor, which is the ground: a
// starving player on hard is bitten down to nothing and dies of it.
func TestStarvationKillsOnHard(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.LevelName = t.TempDir()
	cfg.Difficulty = "hard"

	hungryPlayerFile(t, cfg.LevelName, "Hungry", 1.0)

	srv, c := servertest.Dial(t, cfg)
	if got := srv.Difficulty(); got != 3 {
		t.Fatalf("starting difficulty = %d, want 3 (hard)", got)
	}
	entityID := c.JoinAs("Hungry")
	servertest.HoldTicks(t, srv)

	// One bite is the end of them: the health was one half-heart above the
	// floor, which on hard is nothing at all.
	got := expectHealthWithin(t, srv, c, 180)
	if got.Health != 0 {
		t.Errorf("health after the bite = %v, want 0, hard starving to the end", got.Health)
	}
	starved := protocol.Text{
		Translate: "death.attack.starve",
		With:      []protocol.Text{servertest.PlayerDisplayComponent("Hungry")},
	}
	c.ExpectDamageEvent(entityID, "minecraft:starve", 0)
	c.ExpectCombatDeath(entityID, starved)
	c.ExpectDeathMessage(starved)
	c.Disconnect()
}
