package storage

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
)

// TestAWholeSectionBiomeFillSurvivesASave fills one full chunk section with a
// biome other than its own, which leaves a 64-cell grid of one value, then
// saves and reopens. Packing that grid at the width its one-entry palette
// needs divides by zero.
// vanilla: PalettedContainer.unpack
func TestAWholeSectionBiomeFillSurvivesASave(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.LevelName = t.TempDir()

	srv, c := servertest.Join(t, cfg, "Painter", servertest.OpsListing(t, "Painter"))
	srv.StopTick()

	c.ChatCommand("fillbiome 0 64 0 15 79 15 minecraft:desert")
	servertest.AwaitCommandFeedbackText(t, c, "commands.fillbiome.success.count",
		fillBiomeText(64, 0, 64, 0, 12, 76, 12)...)
	srv.FlushSaves()
	srv.Shutdown()

	w, err := world.NewWorld(cfg.LevelName, world.NewFlatGenerator())
	if err != nil {
		t.Fatalf("reopen world: %v", err)
	}
	defer w.Close()
	pos := world.ChunkPos{}
	w.Acquire(pos)
	defer w.Release(pos)

	for _, p := range [][3]int32{{0, 64, 0}, {15, 79, 15}, {7, 70, 9}} {
		if got := w.NoiseBiome(p[0], p[1], p[2]).Name(); got != "minecraft:desert" {
			t.Errorf("biome at %v after reopen = %q, want desert", p, got)
		}
	}
	if got := w.NoiseBiome(0, 80, 0).Name(); got == "minecraft:desert" {
		t.Errorf("biome above the filled section = %q, want it untouched", got)
	}
}

func fillBiomeText(vals ...int64) []protocol.Text {
	out := make([]protocol.Text, len(vals))
	for i := range vals {
		out[i] = protocol.Text{Number: &vals[i]}
	}
	return out
}
