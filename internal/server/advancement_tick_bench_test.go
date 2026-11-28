package server

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/golem-mc/golem/internal/server/advancement"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/world"
)

// BenchmarkTickLocationAdvancements runs a player's per-tick advancement
// checks on a tick that is not a location tick, for a player on foot.
func BenchmarkTickLocationAdvancements(b *testing.B) {
	benchTickLocationAdvancements(b, 1)
}

// BenchmarkTickLocationAdvancementsLocationTick includes the once-a-second
// location predicates with the structure advancements still pending.
func BenchmarkTickLocationAdvancementsLocationTick(b *testing.B) {
	benchTickLocationAdvancements(b, 20)
}

func benchTickLocationAdvancements(b *testing.B, age int) {
	s, _ := newCommandBenchServer(b, 0)
	p := &player.Player{EntityID: s.nextEntity()}
	p.SetPosition(player.Position{X: 2.5, Y: float64(world.GroundLevel), Z: 4.5, OnGround: true})
	p.UpdateVitals(func(v *player.Vitals) { v.Health, v.AgeTicks = 20, age })
	c := &client{srv: s, player: p}
	c.dim.Store(s.allDims()[0])
	c.advancements.Store(advancement.NewProgress(advancement.Current()))
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		c.tickLocationAdvancements()
	}
}

// BenchmarkAdvancementRewardFunction completes a pack advancement whose
// reward function revokes it, once per iteration.
func BenchmarkAdvancementRewardFunction(b *testing.B) {
	s, _ := newCommandBenchServer(b, 0)
	b.Cleanup(func() { advancement.ApplyPack(nil) })
	root := filepath.Join(b.TempDir(), "pack")
	for f, body := range map[string]string{
		"pack.mcmeta":                      `{"pack":{"description":"b"}}`,
		"data/bench/advancement/a.json":    `{"rewards":{"function":"bench:r"},"criteria":{"c":{"trigger":"minecraft:impossible"}}}`,
		"data/bench/function/r.mcfunction": "advancement revoke @s only bench:a\nscoreboard players add @s n 1\n",
	} {
		path := filepath.Join(root, filepath.FromSlash(f))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			b.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			b.Fatal(err)
		}
	}
	if err := s.loadFunctions([]string{root}); err != nil {
		b.Fatal(err)
	}
	s.RunRconLine("scoreboard objectives add n dummy")
	p := &player.Player{EntityID: s.nextEntity(), UUID: uuid.New(), Name: "Bench"}
	p.UpdateVitals(func(v *player.Vitals) { v.Health = 20 })
	p.SetPosition(player.Position{X: 2.5, Y: float64(world.GroundLevel), Z: 4.5, OnGround: true})
	c := &client{srv: s, player: p}
	c.dim.Store(s.allDims()[0])
	c.worldScope = worldScope{srv: s, in: c.dimension, by: c.Profile}
	progress := advancement.NewProgress(advancement.Current())
	c.advancements.Store(progress)
	s.clients[p.UUID] = c
	b.Cleanup(func() { delete(s.clients, p.UUID) })
	node := advancement.Current().Get("bench:a")
	if node == nil {
		b.Fatal("pack advancement not loaded")
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if !progress.Award(node, "c", time.Now()) {
			b.Fatal("advancement already done")
		}
		c.AnnounceAdvancements()
	}
}
