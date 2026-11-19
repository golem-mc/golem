package server

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// BenchmarkSelectorSort runs 1000 closest-entity or random-entity picks beside
// 2000 markers, as a data pack's tick function might.
func BenchmarkSelectorSort(b *testing.B) {
	for _, order := range []string{"random", "nearest"} {
		b.Run(order, func(b *testing.B) {
			s, _ := newTrackerBenchServer(b)
			s.mu.Lock()
			clear(s.clients)
			s.mu.Unlock()
			logger := slog.Default()
			slog.SetDefault(slog.New(slog.DiscardHandler))
			b.Cleanup(func() { slog.SetDefault(logger) })
			dir := filepath.Join(s.levelDir, "datapacks", "p")
			fnDir := filepath.Join(dir, "data", "zz", "function")
			if err := os.MkdirAll(fnDir, 0o755); err != nil {
				b.Fatal(err)
			}
			meta := `{"pack":{"pack_format":101,"description":"bench"}}`
			if err := os.WriteFile(filepath.Join(dir, "pack.mcmeta"), []byte(meta), 0o644); err != nil {
				b.Fatal(err)
			}
			line := "execute as @e[type=marker,limit=1,sort=" + order + "] run scoreboard players set @s x 1\n"
			if err := os.WriteFile(filepath.Join(fnDir, "t.mcfunction"), []byte(strings.Repeat(line, 1000)), 0o644); err != nil {
				b.Fatal(err)
			}
			for _, c := range []string{"scoreboard objectives add x dummy", "reload"} {
				s.RunRconLine(c)
			}
			for i := range 2000 {
				// Permute a grid within the held chunks so distance order differs
				// from entity ID order, without depending on a random seed.
				j := i * 977 % 2000
				s.RunRconLine(fmt.Sprintf("summon marker %d -60 %d", j%50-25, j/50-20))
			}
			b.ReportAllocs()
			b.ResetTimer()
			start := time.Now()
			for range b.N {
				s.RunRconLine("function zz:t")
			}
			b.ReportMetric(float64(time.Since(start).Nanoseconds())/float64(b.N)/1000, "ns/line")
		})
	}
}

// BenchmarkSelectorSweep resolves @e selectors beside 1000 cows and 1000 item
// entities, with no players: what a data pack's per-tick checks cost.
func BenchmarkSelectorSweep(b *testing.B) {
	s, w := newTrackerBenchServer(b)
	// The bench players have no connection for a selector to read.
	s.mu.Lock()
	clear(s.clients)
	s.mu.Unlock()
	for i := range 1000 {
		x, z := float64(i%40)*2-40+0.5, float64(i/40)*2-40+0.5
		s.spawnMobInDim(s.allDims()[0], entity.TypeCow, x, float64(world.GroundLevel), z, int64(i), nil)
		it := s.spawnItem(w, protocol.ItemStack{ItemID: int32(registry.ItemStone), Count: 1}, x, float64(world.GroundLevel), z, 0, 0, 0, -1)
		it.item.Age = -1 << 30 // Never merges or despawns.
	}
	for _, bc := range []struct{ name, line string }{
		{"type_distance", "execute if entity @e[type=minecraft:pig,distance=..5]"},
		{"type_all", "execute if entity @e[type=minecraft:cow]"},
		{"tag", "execute if entity @e[tag=foo]"},
		{"nbt", `execute if entity @e[type=minecraft:item,nbt={Item:{id:"minecraft:diamond"}}]`},
	} {
		b.Run(bc.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				s.RunRconLine(bc.line)
			}
		})
	}
}

// BenchmarkSelectorLimit runs 1000 arbitrary-order picks beside 1 or 2000 markers.
func BenchmarkSelectorLimit(b *testing.B) {
	for _, filter := range []string{"type=marker", "tag=t"} {
		for _, n := range []int{1, 2000} {
			b.Run(fmt.Sprintf("%s/N=%d", filter, n), func(b *testing.B) {
				s, _ := newTrackerBenchServer(b)
				s.mu.Lock()
				clear(s.clients)
				s.mu.Unlock()
				logger := slog.Default()
				slog.SetDefault(slog.New(slog.DiscardHandler))
				b.Cleanup(func() { slog.SetDefault(logger) })
				dir := filepath.Join(s.levelDir, "datapacks", "p")
				fnDir := filepath.Join(dir, "data", "zz", "function")
				if err := os.MkdirAll(fnDir, 0o755); err != nil {
					b.Fatal(err)
				}
				meta := `{"pack":{"pack_format":101,"description":"bench"}}`
				if err := os.WriteFile(filepath.Join(dir, "pack.mcmeta"), []byte(meta), 0o644); err != nil {
					b.Fatal(err)
				}
				line := "execute as @e[" + filter + ",limit=1] run scoreboard players set @s x 1\n"
				if err := os.WriteFile(filepath.Join(fnDir, "t.mcfunction"), []byte(strings.Repeat(line, 1000)), 0o644); err != nil {
					b.Fatal(err)
				}
				for _, c := range []string{"scoreboard objectives add x dummy", "reload"} {
					s.RunRconLine(c)
				}
				for range n {
					s.RunRconLine(`summon marker 0 -60 0 {Tags:["t"]}`)
				}
				if got := s.RunRconLine("execute if entity @e[type=marker,tag=t]"); len(got) != 1 || len(got[0].With) != 1 || got[0].With[0].Plain() != fmt.Sprint(n) {
					b.Fatalf("markers missing from selector: %+v", got)
				}

				b.ReportAllocs()
				b.ResetTimer()
				start := time.Now()
				for range b.N {
					s.RunRconLine("function zz:t")
				}
				b.ReportMetric(float64(time.Since(start).Nanoseconds())/float64(b.N)/1000, "ns/line")
			})
		}
	}
}
