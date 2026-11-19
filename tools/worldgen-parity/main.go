// Command worldgen-parity compares this repository's world generators with
// vanilla's, block by block and biome cell by biome cell, for one seed,
// dimension and chunk range. See tools/README.md.
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/tools/internal/vanilla"
)

// chunkRange is an inclusive rectangle of chunk coordinates.
type chunkRange struct {
	X0 int32 `json:"x0"`
	Z0 int32 `json:"z0"`
	X1 int32 `json:"x1"`
	Z1 int32 `json:"z1"`
}

func (r chunkRange) covers(o chunkRange) bool {
	return r.X0 <= o.X0 && r.Z0 <= o.Z0 && r.X1 >= o.X1 && r.Z1 >= o.Z1
}

func (r chunkRange) count() int { return int(r.X1-r.X0+1) * int(r.Z1-r.Z0+1) }

func (r chunkRange) positions() [][2]int32 {
	var out [][2]int32
	for z := r.Z0; z <= r.Z1; z++ {
		for x := r.X0; x <= r.X1; x++ {
			out = append(out, [2]int32{x, z})
		}
	}
	return out
}

// tiles splits the range into rectangles at most size chunks on a side.
func (r chunkRange) tiles(size int32) []chunkRange {
	var out []chunkRange
	for z := r.Z0; z <= r.Z1; z += size {
		for x := r.X0; x <= r.X1; x += size {
			out = append(out, chunkRange{x, z, min(x+size-1, r.X1), min(z+size-1, r.Z1)})
		}
	}
	return out
}

// parsePair reads "x,z".
func parsePair(s string) (int32, int32, error) {
	a, b, ok := strings.Cut(s, ",")
	if !ok {
		return 0, 0, fmt.Errorf("%q is not x,z", s)
	}
	x, err := strconv.ParseInt(strings.TrimSpace(a), 10, 32)
	if err != nil {
		return 0, 0, err
	}
	z, err := strconv.ParseInt(strings.TrimSpace(b), 10, 32)
	if err != nil {
		return 0, 0, err
	}
	return int32(x), int32(z), nil
}

func main() {
	seedSpec := flag.String("seed", "0", "world seed; a non-number is hashed as level-seed would be")
	dim := flag.String("dimension", "overworld", "overworld, the_nether or the_end")
	from := flag.String("from", "-4,-4", "first chunk x,z (inclusive)")
	to := flag.String("to", "3,3", "last chunk x,z (inclusive)")
	refDir := flag.String("reference", "reference-server", "the reference server directory (mise run reference:setup)")
	jar := flag.String("jar", "", "server jar (default <reference>/minecraft_server.<REFERENCE_VERSION>.jar)")
	heap := flag.String("heap", "2G", "JVM heap for the reference server")
	scope := flag.String("memory-max", "3G", "systemd MemoryMax for the reference server; empty runs it without a scope")
	jsonOut := flag.String("json", "", "also write the full report as JSON to this file")
	digests := flag.String("digests", "", "also write one \"seed x z vanilla-sha ours-sha\" line per chunk to this file")
	diff := flag.String("diff", "", "write each differing block as x y z vanilla ours to this file")
	top := flag.Int("top", 15, "how many mismatches and worst chunks to list")
	verbose := flag.Bool("v", false, "copy the reference server's console to stderr")
	var datapacks stringList
	flag.Var(&datapacks, "datapack", "data pack directory to copy into the world (repeatable)")
	structures := flag.Bool("structures", true, "generate structures in the vanilla world")
	recordEntities := flag.Bool("record-entities", false, "treat a cached vanilla world without its entities as a miss and record a new one")
	levelType := flag.String("level-type", "minecraft:normal", "world preset of both servers: minecraft:normal, minecraft:large_biomes, minecraft:amplified, minecraft:single_biome_surface or minecraft:flat")
	generatorSettings := flag.String("generator-settings", "", "flat world layers JSON of both servers (server.properties' generator-settings); empty is the default flat world")
	flag.Parse()

	if err := run(*seedSpec, *dim, *from, *to, *refDir, *jar, *heap, *scope, *jsonOut, *digests, *diff, *top, *verbose, datapacks, *structures, *recordEntities, *levelType, *generatorSettings); err != nil {
		fmt.Fprintln(os.Stderr, "worldgen-parity:", err)
		os.Exit(1)
	}
}

func run(seedSpec, dimName, from, to, refDir, jar, heap, scope, jsonOut, digestsOut, diffOut string, top int, verbose bool, datapacks []string, structures, recordEntities bool, levelType, generatorSettings string) error {
	// Both servers hash a non-numeric level-seed the same way; passing the
	// resolved number to vanilla keeps the two from parsing it differently.
	seed := world.ParseSeed(seedSpec)
	d, ok := dimensions[dimName]
	if !ok {
		return fmt.Errorf("-dimension %q is not overworld, the_nether or the_end", dimName)
	}
	x0, z0, err := parsePair(from)
	if err != nil {
		return fmt.Errorf("-from: %w", err)
	}
	x1, z1, err := parsePair(to)
	if err != nil {
		return fmt.Errorf("-to: %w", err)
	}
	r := chunkRange{min(x0, x1), min(z0, z1), max(x0, x1), max(z0, z1)}

	refAbs, err := filepath.Abs(refDir)
	if err != nil {
		return err
	}
	if jar == "" {
		jar = filepath.Join(refAbs, "minecraft_server."+vanilla.VersionFromEnv()+".jar")
	}
	jar, err = filepath.Abs(jar)
	if err != nil {
		return err
	}
	cacheDir := filepath.Join(refAbs, "worldgen", "cache")

	packs, err := datapackNames(datapacks)
	if err != nil {
		return err
	}
	config := cacheConfig{Version: vanilla.VersionFromEnv(), Datapacks: packs, NoStructures: !structures,
		LevelType: levelTypeID(levelType), GeneratorSettings: strings.TrimSpace(generatorSettings)}
	switch config.levelType() {
	case "minecraft:normal", "minecraft:large_biomes", "minecraft:amplified", "minecraft:single_biome_surface":
		if config.GeneratorSettings != "" {
			return fmt.Errorf("-generator-settings needs -level-type minecraft:flat")
		}
	case "minecraft:flat":
	default:
		return fmt.Errorf("-level-type %q is not minecraft:normal, minecraft:large_biomes, minecraft:amplified, minecraft:single_biome_surface or minecraft:flat", levelType)
	}
	dir := findCached(cacheDir, d, seed, r, config, recordEntities)
	if dir == "" {
		dir, err = generateVanilla(vanillaRun{
			Jar:       jar,
			Config:    config,
			Datapacks: datapacks,
			RunDir:    filepath.Join(refAbs, "worldgen", "run"),
			Heap:      heap,
			Scope:     scope,
			Verbose:   verbose,
		}, cacheDir, d, seed, r)
		if err != nil {
			return err
		}
	} else {
		fmt.Fprintf(os.Stderr, "using the cached vanilla world in %s\n", dir)
	}
	vanilla, err := loadVanilla(dir, d, r)
	if err != nil {
		return err
	}
	recorded, err := loadFeaturesOrder(dir)
	if err != nil {
		return err
	}
	if recorded == nil && d.Name == "overworld" {
		fmt.Fprintf(os.Stderr, "no recorded decoration order in %s; delete it to regenerate with one\n", dir)
	}
	ours, err := generateOurs(d, seed, config, r, recorded)
	if err != nil {
		return err
	}
	if digestsOut != "" {
		if err := writeDigests(digestsOut, seed, vanilla, ours); err != nil {
			return err
		}
	}
	var diff io.Writer
	var file *os.File
	var buffered *bufio.Writer
	if diffOut != "" {
		file, err = os.Create(diffOut)
		if err != nil {
			return err
		}
		defer file.Close()
		buffered = bufio.NewWriter(file)
		diff = buffered
	}
	rep, err := compare(d, seed, r, vanilla, ours, diff)
	if err != nil {
		return err
	}
	if buffered != nil {
		if err := buffered.Flush(); err != nil {
			return err
		}
		if err := file.Close(); err != nil {
			return err
		}
	}
	rep.rankFirst(featuresOnly(recorded), top)
	rep.writeText(os.Stdout, top)
	if jsonOut != "" {
		data, err := json.MarshalIndent(rep, "", "  ")
		if err != nil {
			return err
		}
		if err := os.WriteFile(jsonOut, data, 0o644); err != nil {
			return err
		}
	}
	return nil
}

// stringList collects repeated flags in their load order.
type stringList []string

func (s *stringList) String() string         { return strings.Join(*s, ",") }
func (s *stringList) Set(value string) error { *s = append(*s, value); return nil }
