package server

import (
	"fmt"
	"log/slog"
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/server/world"
)

// benchExecuteTp is the issue's command-block workload: every tagged armor
// stand teleported onto itself.
const benchExecuteTp = "execute as @e[type=armor_stand,tag=mcb] at @s run tp @s ~ ~ ~"

// newCommandBenchServer starts a server on the flat test world holding the
// spawn chunk, with stands tagged armor stands standing in it. The tick loop
// is never started.
func newCommandBenchServer(tb testing.TB, stands int) (*Server, *world.World) {
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.LevelName = tb.TempDir()
	cfg.LevelType = TestFlatLevelType
	s, err := newServer(cfg, 1)
	if err != nil {
		tb.Fatalf("create server: %v", err)
	}
	s.SetSpawning(false)
	// The command block's admin lines go to the log; keep them out of the
	// measurement.
	logger := slog.Default()
	slog.SetDefault(slog.New(slog.DiscardHandler))
	w := s.world
	pos := world.ChunkPos{X: 0, Z: 0}
	w.Acquire(pos)
	tb.Cleanup(func() {
		w.Release(pos)
		s.Shutdown()
		slog.SetDefault(logger)
	})
	w.FlushLighting()
	for range stands {
		s.RunRconLine(fmt.Sprintf("summon armor_stand 4 %d 4 {Tags:[\"mcb\"]}", world.GroundLevel))
	}
	return s, w
}

// BenchmarkCommandBlockExecute runs one command block's command, the step a
// repeating command block takes every tick.
func BenchmarkCommandBlockExecute(b *testing.B) {
	benchCommandBlockExecute(b, 10, true)
}

// BenchmarkCommandBlockExecute100Silent is the same step over a hundred
// entities, with the command block output and the admin log turned off, so
// no receiver is left for the teleport's feedback.
func BenchmarkCommandBlockExecute100Silent(b *testing.B) {
	benchCommandBlockExecute(b, 100, false)
}

func benchCommandBlockExecute(b *testing.B, stands int, feedback bool) {
	s, w := newCommandBenchServer(b, stands)
	if !feedback {
		s.SetGameRule(world.RuleCommandBlockOutput.String(), false)
		s.SetGameRule(world.RuleLogAdminCommands.String(), false)
		s.SetGameRule(world.RuleSendFeedback.String(), false)
	}
	y := int32(world.GroundLevel + 2)
	s.RunRconLine(fmt.Sprintf("setblock 2 %d 2 minecraft:command_block{Command:%q}", y, benchExecuteTp))
	be := w.BlockEntity(2, y, 2)
	if be == nil || be.Kind != world.KindCommandBlock {
		b.Fatal("no command block entity")
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := range b.N {
		s.performCommandBlock(w, be, int64(i))
	}
}

// BenchmarkRconLongExecuteChain matches and runs an execute of about 32k
// characters, the most a chat command carries, chaining as @s over one
// armor stand.
func BenchmarkRconLongExecuteChain(b *testing.B) {
	s, _ := newCommandBenchServer(b, 1)
	line := "execute as @e[type=armor_stand,tag=mcb] " + strings.Repeat("as @s ", 32700/6) + "run tp @s ~ ~ ~"
	if out := s.RunRconLine(line); len(out) != 1 {
		b.Fatalf("the command answered %d lines, want one for the armor stand", len(out))
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		s.RunRconLine(line)
	}
}

// BenchmarkRconExecute runs the same command over RCON.
func BenchmarkRconExecute(b *testing.B) {
	s, _ := newCommandBenchServer(b, 10)
	if out := s.RunRconLine(benchExecuteTp); len(out) != 10 {
		b.Fatalf("the command answered %d lines, want one per armor stand", len(out))
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		s.RunRconLine(benchExecuteTp)
	}
}

// BenchmarkTellEffectPlain sends an effect with nothing to send.
func BenchmarkTellEffectPlain(b *testing.B) {
	s, w := newCommandBenchServer(b, 0)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		s.tellEffect(w, world.Effect{X: 1, Y: 64, Z: 1})
	}
}

// BenchmarkFillFences fills a box with fences and then air, the shape pass
// running for every cell.
func BenchmarkFillFences(b *testing.B) {
	s, _ := newCommandBenchServer(b, 0)
	s.backstopMu.Lock()
	s.tickRunning = true
	s.backstopMu.Unlock()
	g := world.GroundLevel
	fence := fmt.Sprintf("fill 0 %d 0 15 %d 15 oak_fence", g, g+15)
	air := fmt.Sprintf("fill 0 %d 0 15 %d 15 air", g, g+15)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		s.RunRconLine(fence)
		s.RunRconLine(air)
		s.relightWG.Wait()
		s.flushAllBlockChanges()
	}
}
