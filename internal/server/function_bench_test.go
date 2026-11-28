package server

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/golem-mc/golem/plugin"
)

// BenchmarkFunctionLines runs a function of 1000 identical lines, which a
// datapack's tick function does every tick.
func BenchmarkFunctionLines(b *testing.B) {
	lines := map[string]string{
		"score":                       "scoreboard players add #c x 1",
		"execute_if_score":            "execute if score #c x matches 1.. run scoreboard players add #c x 1",
		"execute_as_tp":               "execute as @e[type=marker,tag=t] at @s run tp @s ~ ~ ~",
		"execute_as_score":            "execute as @e[type=marker,tag=t] at @s run scoreboard players add @s x 1",
		"execute_if_block_tag":        "execute as @e[type=marker,tag=t] at @s if block ~ ~-1 ~ #minecraft:dirt run scoreboard players add @s x 1",
		"execute_as_positioned_local": "execute as @e[type=marker,tag=t] at @s positioned ^ ^ ^0.25 if block ~ ~ ~ air run scoreboard players add @s x 1",
		"execute_as_particle":         "execute as @e[type=marker,tag=t] at @s run particle minecraft:dust{color:[1.0,0.0,0.0],scale:1.0} ~ ~1 ~ 0 0 0 0 1",
		"execute_as_store_data":       "execute as @e[type=marker,tag=t] store result score @s x run data get entity @s Pos[0]",
		"execute_as_merge_display":    "execute as @e[type=item_display,tag=t] run data merge entity @s {transformation:{translation:[0f,1f,0f],left_rotation:[0f,0f,0f,1f],scale:[1f,1f,1f],right_rotation:[0f,0f,0f,1f]},start_interpolation:0,interpolation_duration:2}",
		"macro_call":                  `execute as @e[type=marker,tag=t] run function zz:m {x:1,o:"x"}`,
		"execute_as_setblock_toggle":  "execute as @e[type=marker,tag=t] at @s run setblock ~ ~3 ~ stone\nexecute as @e[type=marker,tag=t] at @s run setblock ~ ~3 ~ air",
		"summon_kill":                 "summon marker ~ ~ ~ {Tags:[\"z\"]}\nkill @e[type=marker,tag=z]",
	}
	for _, name := range []string{"score", "execute_if_score", "execute_as_tp", "execute_as_score", "execute_as_positioned_local", "execute_as_particle", "execute_as_store_data", "execute_as_merge_display", "execute_if_block_tag", "execute_as_setblock_toggle", "summon_kill", "macro_call"} {
		b.Run(name, func(b *testing.B) {
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
			body := strings.Repeat(lines[name]+"\n", 1000)
			if name == "execute_as_setblock_toggle" {
				// Two lines per copy: 500 copies keep the function at 1000 lines.
				body = strings.Repeat(lines[name]+"\n", 500)
				s.backstopMu.Lock()
				s.tickRunning = true
				s.backstopMu.Unlock()
				b.Cleanup(func() {
					s.backstopMu.Lock()
					s.tickRunning = false
					s.backstopMu.Unlock()
				})
			}
			if err := os.WriteFile(filepath.Join(fnDir, "t.mcfunction"), []byte(body), 0o644); err != nil {
				b.Fatal(err)
			}
			if name == "macro_call" {
				macro := "$scoreboard players add @s $(o) $(x)\n"
				if err := os.WriteFile(filepath.Join(fnDir, "m.mcfunction"), []byte(macro), 0o644); err != nil {
					b.Fatal(err)
				}
			}
			for _, c := range []string{"scoreboard objectives add x dummy", "scoreboard players set #c x 1", "reload"} {
				s.RunRconLine(c)
			}
			if strings.HasPrefix(name, "execute_as") || name == "execute_if_block_tag" || name == "macro_call" {
				for i := range 20 {
					s.RunRconLine(fmt.Sprintf("summon marker %d -60 0 {Tags:[\"t\"]}", i))
				}
			}
			if name == "execute_as_merge_display" {
				for range 20 {
					s.RunRconLine(`summon item_display ~ ~ ~ {Tags:["t"],item:{id:"minecraft:diamond",count:1}}`)
				}
			}
			b.ReportAllocs()
			b.ResetTimer()
			start := time.Now()
			for range b.N {
				s.RunRconLine("function zz:t")
				s.relightWG.Wait()
			}
			b.ReportMetric(float64(time.Since(start).Nanoseconds())/float64(b.N)/1000, "ns/line")
		})
	}
}

// BenchmarkFunctionStorageLines runs a function of 1000 identical lines that
// read or write a command storage beside a list of N compounds.
func BenchmarkFunctionStorageLines(b *testing.B) {
	lines := map[string]string{
		"if_data":          "execute if data storage zz:s v run scoreboard players add #c x 1",
		"store":            "execute store result storage zz:s v int 1 run scoreboard players get #c x",
		"modify_set":       "data modify storage zz:s w set value 1",
		"get":              "data get storage zz:s v",
		"function_with":    "function zz:m with storage zz:s args",
		"store_storage_as": "execute as @e[type=marker,tag=t] store result storage zz:s v int 1 run scoreboard players get @s x",
	}
	for _, name := range []string{"if_data", "store", "modify_set", "get", "function_with", "store_storage_as"} {
		for _, n := range []int{0, 100, 1000} {
			b.Run(fmt.Sprintf("%s/N=%d", name, n), func(b *testing.B) {
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
				body := strings.Repeat(lines[name]+"\n", 1000)
				if err := os.WriteFile(filepath.Join(fnDir, "t.mcfunction"), []byte(body), 0o644); err != nil {
					b.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(fnDir, "m.mcfunction"), []byte("$return $(v)\n"), 0o644); err != nil {
					b.Fatal(err)
				}
				for _, c := range []string{"scoreboard objectives add x dummy", "scoreboard players set #c x 1", "reload", `data merge storage zz:s {v:1,args:{v:1}}`} {
					s.RunRconLine(c)
				}
				if name == "store_storage_as" {
					for i := range 20 {
						s.RunRconLine(fmt.Sprintf("summon marker %d -60 0 {Tags:[\"t\"]}", i))
					}
					s.RunRconLine("scoreboard players set @e[type=marker,tag=t] x 1")
				}
				for i := range n {
					s.RunRconLine(fmt.Sprintf(`data modify storage zz:s big append value {id:"entry_%d",score:%d,pos:[1.0d,2.0d,3.0d],tags:["a","b","c"]}`, i, i))
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

// BenchmarkEntityQueryLines runs 1000 selector commands beside unrelated markers.
func BenchmarkEntityQueryLines(b *testing.B) {
	lines := []struct{ name, line string }{
		{"missing_type", "execute if entity @e[type=zombie]"},
		{"tagged_tp", "tp @e[type=armor_stand,tag=one,limit=1] 0 -60 0"},
		{"tagged_data", "data merge entity @e[type=armor_stand,tag=one,limit=1] {CustomNameVisible:0b}"},
		{"uuid", "execute as 00000001-0000-0002-0000-000300000004 run scoreboard players add #c x 1"},
		{"summon", "execute summon marker run kill @s"},
		{"other_kind", "execute if entity @e[type=marker,tag=b]"},
		{"nearest", "execute as @e[type=marker,limit=1,sort=nearest] run scoreboard players add #c x 1"},
	}
	for _, line := range lines {
		for _, n := range []int{1, 2000} {
			b.Run(fmt.Sprintf("%s/N=%d", line.name, n), func(b *testing.B) {
				s, _ := newTrackerBenchServer(b)
				s.mu.Lock()
				clear(s.clients)
				s.mu.Unlock()
				logger := slog.Default()
				slog.SetDefault(slog.New(slog.DiscardHandler))
				b.Cleanup(func() { slog.SetDefault(logger) })
				fnDir := filepath.Join(s.levelDir, "datapacks", "p", "data", "zz", "function")
				if err := os.MkdirAll(fnDir, 0o755); err != nil {
					b.Fatal(err)
				}
				meta := `{"pack":{"pack_format":101,"description":"bench"}}`
				if err := os.WriteFile(filepath.Join(fnDir, "..", "..", "..", "pack.mcmeta"), []byte(meta), 0o644); err != nil {
					b.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(fnDir, "t.mcfunction"), []byte(strings.Repeat(line.line+"\n", 1000)), 0o644); err != nil {
					b.Fatal(err)
				}
				for _, cmd := range []string{"scoreboard objectives add x dummy", "reload"} {
					s.RunRconLine(cmd)
				}
				for range n {
					if line.name == "other_kind" {
						s.RunRconLine(`summon item 0 -60 0 {Item:{id:"stone",count:1},PickupDelay:32767s,Age:-32768s}`)
						continue
					}
					s.RunRconLine("summon marker 0 -60 0")
				}
				if line.name == "other_kind" {
					s.RunRconLine(`summon marker 0 -60 0 {Tags:["b"]}`)
				}
				if line.name == "tagged_tp" || line.name == "tagged_data" {
					s.RunRconLine(`summon armor_stand 0 -60 0 {Tags:["one"],CustomNameVisible:1b}`)
				}
				if line.name == "uuid" {
					s.RunRconLine("summon husk 0 -60 0 {UUID:[I;1,2,3,4],NoAI:1b}")
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

// BenchmarkFunctionLinesWithPlugin is BenchmarkFunctionLines/score with a
// plugin command registered.
func BenchmarkFunctionLinesWithPlugin(b *testing.B) {
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
	body := strings.Repeat("scoreboard players add #c x 1\n", 1000)
	if err := os.WriteFile(filepath.Join(fnDir, "t.mcfunction"), []byte(body), 0o644); err != nil {
		b.Fatal(err)
	}
	for _, c := range []string{"scoreboard objectives add x dummy", "scoreboard players set #c x 1", "reload"} {
		s.RunRconLine(c)
	}
	fixture := &registeredCommandBenchPlugin{}
	s.loadPlugins([]plugin.Registration{{Name: "bench", Plugin: fixture}})
	s.enablePlugins()
	if !fixture.registered {
		b.Fatal("benchmark command was not registered")
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		s.runLine(s.consoleSource(), "function zz:t")
	}
}

// BenchmarkExecutePredicate runs an execute-if-predicate line over 20
// executors, which a datapack's tick function does every tick.
func BenchmarkExecutePredicate(b *testing.B) {
	s, _ := newTrackerBenchServer(b)
	s.mu.Lock()
	clear(s.clients)
	s.mu.Unlock()
	logger := slog.Default()
	slog.SetDefault(slog.New(slog.DiscardHandler))
	b.Cleanup(func() { slog.SetDefault(logger) })
	for i := range 20 {
		s.RunRconLine(fmt.Sprintf("summon marker %d -60 0 {Tags:[\"t\"]}", i))
	}
	const line = `execute as @e[type=marker,tag=t] if predicate {type:"minecraft:random_chance",chance:0.5}`
	for _, t := range s.RunRconLine(line) {
		if t.Color == "red" {
			b.Fatalf("command failed: %+v", t)
		}
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		s.RunRconLine(line)
	}
}
