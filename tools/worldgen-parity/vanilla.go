package main

import (
	"bufio"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// cacheConfig separates worlds generated with different server settings.
type cacheConfig struct {
	Version      string   `json:"version"`
	Datapacks    []string `json:"datapacks,omitempty"`
	NoStructures bool     `json:"no_structures,omitempty"`
	// LevelType is the world preset, empty for minecraft:normal so the
	// caches recorded before it keep their names.
	LevelType string `json:"level_type,omitempty"`
	// GeneratorSettings is a flat world's generator-settings JSON, empty for
	// the default flat world.
	GeneratorSettings string `json:"generator_settings,omitempty"`
}

// levelTypeID is a -level-type value as vanilla looks it up: lowercased, in
// the minecraft namespace when it names none, and empty for minecraft:normal.
// vanilla: DedicatedServerProperties.WorldDimensionData.create
func levelTypeID(value string) string {
	id := strings.ToLower(strings.TrimSpace(value))
	if !strings.Contains(id, ":") {
		id = "minecraft:" + id
	}
	if id == "minecraft:normal" {
		return ""
	}
	return id
}

// levelType is the world preset both servers generate.
func (c cacheConfig) levelType() string {
	if c.LevelType == "" {
		return "minecraft:normal"
	}
	return c.LevelType
}

// propertyValue escapes a server.properties value for java.util.Properties.
var propertyValue = strings.NewReplacer(`\`, `\\`, ":", `\:`, "=", `\=`, "\n", `\n`).Replace

// matches reports whether a cache recorded under c serves a run of other;
// metadata with no version never does.
func (c cacheConfig) matches(other cacheConfig) bool {
	return c.Version == other.Version && c.NoStructures == other.NoStructures && slices.Equal(c.Datapacks, other.Datapacks) &&
		c.LevelType == other.LevelType && c.GeneratorSettings == other.GeneratorSettings
}

// suffix is the cache path suffix that tells configurations apart.
func (c cacheConfig) suffix() string {
	data, _ := json.Marshal(c)
	return fmt.Sprintf("_%x", sha256.Sum256(data))[:17]
}

// cacheMeta describes the cached world's generation and recorded range.
type cacheMeta struct {
	cacheConfig
	Dimension string     `json:"dimension"`
	Seed      int64      `json:"seed"`
	Range     chunkRange `json:"range"`
	// Order is "server-start" when the FEATURES order was recorded from server
	// start on one worker; a cache without it recorded from after spawn.
	Order    string `json:"order"`
	Reloads  bool   `json:"reloads,omitempty"`
	Lighting int    `json:"lighting,omitempty"`
}

// cacheOrder is cacheMeta.Order of the caches this tool writes.
const cacheOrder = "server-start"

// cacheLighting includes source priming and both light-stage completions.
const cacheLighting = 2

func cacheOrderFor(d dimension) string {
	if d.Name == "overworld" {
		return cacheOrder + "-light2"
	}
	return cacheOrder
}

// findCached returns a cached vanilla dimension for the seed whose range
// covers r, or "" when there is none. With recordEntities an entry without
// its entities/ folder does not count.
func findCached(cacheDir string, d dimension, seed int64, r chunkRange, config cacheConfig, recordEntities bool) string {
	entries, err := os.ReadDir(cacheDir)
	if err != nil {
		return ""
	}
	for _, e := range entries {
		dir := filepath.Join(cacheDir, e.Name())
		data, err := os.ReadFile(filepath.Join(dir, "meta.json"))
		if err != nil {
			continue
		}
		var m cacheMeta
		if json.Unmarshal(data, &m) != nil {
			continue
		}
		if d.Name == "overworld" && m.Lighting < cacheLighting {
			continue
		}
		if recordEntities {
			if _, err := os.Stat(filepath.Join(dir, "entities")); err != nil {
				continue
			}
		}
		if m.matches(config) && m.Dimension == d.Name && m.Order == cacheOrderFor(d) && m.Seed == seed && m.Range.covers(r) {
			return dir
		}
	}
	return ""
}

// vanillaRun is how a reference server is started for one world.
type vanillaRun struct {
	Config    cacheConfig
	Datapacks []string
	Jar       string // the server jar, absolute
	RunDir    string // where the jar unpacks its libraries and writes its world
	Heap      string // the JVM's -Xmx
	Scope     string // the systemd MemoryMax the JVM runs under; "" runs it bare
	Verbose   bool   // copy the server's console to stderr
}

// forceloadTile is the chunks per side of one /forceload command: the command
// takes at most 256 chunks.
const forceloadTile = 16

// generateVanilla generates the range on the reference server and moves the
// dimension's region and entities files into a new cache entry, returning its
// directory.
//
// /tick freeze runs before any chunk in the range is forceloaded, so no tick
// moves fluids on a generated chunk; the spawn chunks do tick for the few
// seconds before the freeze.
func generateVanilla(run vanillaRun, cacheDir string, d dimension, seed int64, r chunkRange) (string, error) {
	if _, err := os.Stat(run.Jar); err != nil {
		return "", fmt.Errorf("reference server jar: %w (run 'mise run reference:setup')", err)
	}
	if err := os.MkdirAll(run.RunDir, 0o755); err != nil {
		return "", err
	}
	worldDir := filepath.Join(run.RunDir, "world")
	if err := os.RemoveAll(worldDir); err != nil {
		return "", err
	}
	if err := copyDatapacks(worldDir, run.Datapacks); err != nil {
		return "", err
	}
	gamePort, err := freePort()
	if err != nil {
		return "", err
	}
	rconPort, err := freePort()
	if err != nil {
		return "", err
	}
	const password = "worldgen-parity"
	// pause-when-empty-seconds=0 keeps an empty server ticking its chunk
	// loading; max-tick-time=-1 keeps the watchdog from killing a server
	// that a loaded machine slows down.
	props := strings.Join([]string{
		"level-name=world",
		fmt.Sprintf("level-seed=%d", seed),
		"level-type=" + propertyValue(run.Config.levelType()),
		"generator-settings=" + propertyValue(run.Config.GeneratorSettings),
		fmt.Sprintf("generate-structures=%t", !run.Config.NoStructures),
		"online-mode=false",
		"server-ip=127.0.0.1",
		fmt.Sprintf("server-port=%d", gamePort),
		"enable-rcon=true",
		fmt.Sprintf("rcon.port=%d", rconPort),
		"rcon.password=" + password,
		"pause-when-empty-seconds=0",
		"max-tick-time=-1",
		"sync-chunk-writes=false",
		"spawn-protection=0",
		"",
	}, "\n")
	if err := os.WriteFile(filepath.Join(run.RunDir, "server.properties"), []byte(props), 0o644); err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(run.RunDir, "eula.txt"), []byte("eula=true\n"), 0o644); err != nil {
		return "", err
	}

	// --jfrProfile (net.minecraft.server.Main) records from server start, so spawn
	// is covered; one background thread (Util.MAX_THREADS_SYSTEM_PROPERTY) keeps
	// FEATURES tasks from overlapping.
	args := []string{"-Xmx" + run.Heap, "-Dmax.bg.threads=1", "-jar", run.Jar, "--jfrProfile", "--port", fmt.Sprint(gamePort), "nogui"}
	name := "java"
	if run.Scope != "" && !userBus() {
		// A systemd unit has no user session; its own MemoryMax
		// bounds the JVM instead.
		fmt.Fprintln(os.Stderr, "no systemd user bus; running the reference server without -memory-max")
	} else if run.Scope != "" {
		args = append([]string{"--user", "--scope", "-q", "-p", "MemoryMax=" + run.Scope, "-p", "MemorySwapMax=0", "java"}, args...)
		name = "systemd-run"
	}
	cmd := exec.Command(name, args...)
	cmd.Dir = run.RunDir
	logFile, err := os.Create(filepath.Join(run.RunDir, "console.log"))
	if err != nil {
		return "", err
	}
	defer logFile.Close()
	var out io.Writer = logFile
	if run.Verbose {
		out = io.MultiWriter(logFile, os.Stderr)
	}
	cmd.Stdout, cmd.Stderr = out, out
	// stdin stays open so the server's console reader does not see EOF.
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return "", err
	}
	defer stdin.Close()
	fmt.Fprintf(os.Stderr, "starting the reference server in %s (console in console.log)\n", run.RunDir)
	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("start %s: %w", name, err)
	}
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()
	stopped := false
	defer func() {
		if !stopped {
			_ = cmd.Process.Kill()
			if err := <-exited; err != nil {
				fmt.Fprintln(os.Stderr, "reference server:", err)
			}
		}
	}()

	rc, err := waitRcon(fmt.Sprintf("127.0.0.1:%d", rconPort), password, exited)
	if err != nil {
		return "", err
	}
	defer rc.Close()

	if _, err := rc.Command("tick freeze"); err != nil {
		return "", err
	}
	// forceload takes at most 256 chunks per call.
	for _, tile := range r.tiles(forceloadTile) {
		reply, err := rc.Command(fmt.Sprintf("execute in %s run forceload add %d %d %d %d", d.ID(), tile.X0*16, tile.Z0*16, tile.X1*16+15, tile.Z1*16+15))
		if err != nil {
			return "", err
		}
		if !strings.HasPrefix(reply, "Marked") { // commands.forceload.added.*
			return "", fmt.Errorf("forceload %v: %q", tile, reply)
		}
	}
	fmt.Fprintf(os.Stderr, "generating %d chunks\n", r.count())
	if err := waitLoaded(rc, d, r, exited); err != nil {
		return "", err
	}
	reply, err := rc.Command("jfr stop")
	if err != nil {
		return "", err
	}
	const stoppedPrefix = "JFR profiling stopped and dumped to " // commands.jfr.stopped
	if !strings.HasPrefix(reply, stoppedPrefix) || len(reply) == len(stoppedPrefix) {
		return "", fmt.Errorf("jfr stop: %q", reply)
	}
	recording := filepath.Join(run.RunDir, strings.TrimPrefix(reply, stoppedPrefix))
	defer os.Remove(recording)
	data, err := exec.Command("jfr", "print", "--json", "--events", "minecraft.ChunkGeneration,minecraft.ChunkRegionRead", recording).Output()
	if err != nil {
		return "", fmt.Errorf("read JFR recording: %w", err)
	}
	order, err := featuresOrder(data, d.ID())
	if err != nil {
		return "", fmt.Errorf("read FEATURES order: %w", err)
	}
	if _, err := rc.Command("save-all flush"); err != nil {
		return "", err
	}
	if _, err := rc.Command("stop"); err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	select {
	case err := <-exited:
		if err != nil {
			return "", fmt.Errorf("the reference server stopped with %w; see console.log", err)
		}
	case <-time.After(5 * time.Minute):
		return "", fmt.Errorf("the reference server did not stop")
	}
	stopped = true

	cacheName := fmt.Sprintf("%s_seed%d_%d_%d_%d_%d_start", d.Name, seed, r.X0, r.Z0, r.X1, r.Z1)
	if d.Name == "overworld" {
		cacheName += "_light2"
	}
	dest := filepath.Join(cacheDir, cacheName+run.Config.suffix())
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return "", err
	}
	if err := os.RemoveAll(dest); err != nil {
		return "", err
	}
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return "", err
	}
	// A world keeps every dimension's files, the overworld's too, under
	// dimensions/<namespace>/<path>, not in region/, DIM-1/ and DIM1/.
	regionDir := filepath.Join(worldDir, "dimensions", "minecraft", d.Name, "region")
	if err := os.Rename(regionDir, filepath.Join(dest, "region")); err != nil {
		return "", fmt.Errorf("keep the region files: %w", err)
	}
	// A dimension whose range saved no entity has no entities folder; an empty
	// one still marks the entry as recorded.
	entitiesDir := filepath.Join(worldDir, "dimensions", "minecraft", d.Name, "entities")
	if err := os.Rename(entitiesDir, filepath.Join(dest, "entities")); os.IsNotExist(err) {
		err = os.Mkdir(filepath.Join(dest, "entities"), 0o755)
		if err != nil {
			return "", err
		}
	} else if err != nil {
		return "", fmt.Errorf("keep the entities files: %w", err)
	}
	var features strings.Builder
	for _, p := range order {
		if p.Load {
			features.WriteString("load ")
		} else if p.Light != "" {
			features.WriteString(p.Light + " ")
		}
		fmt.Fprintf(&features, "%d,%d\n", p.Pos[0], p.Pos[1])
	}
	if err := os.WriteFile(filepath.Join(dest, "features.txt"), []byte(features.String()), 0o644); err != nil {
		return "", err
	}
	lighting := 0
	if d.Name == "overworld" {
		lighting = cacheLighting
	}
	meta, _ := json.Marshal(cacheMeta{cacheConfig: run.Config, Dimension: d.Name, Seed: seed, Range: r, Order: cacheOrderFor(d), Reloads: true, Lighting: lighting})
	if err := os.WriteFile(filepath.Join(dest, "meta.json"), meta, 0o644); err != nil {
		return "", err
	}
	_ = os.RemoveAll(worldDir)
	return dest, nil
}

// waitRcon connects once the server has opened its rcon port, which it does
// after preparing the spawn area.
func waitRcon(addr, password string, exited <-chan error) (*rcon, error) {
	deadline := time.Now().Add(10 * time.Minute)
	for {
		select {
		case err := <-exited:
			return nil, fmt.Errorf("the reference server exited during startup (%v); see console.log", err)
		default:
		}
		rc, err := dialRcon(addr, password)
		if err == nil {
			return rc, nil
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("rcon at %s: %w", addr, err)
		}
		time.Sleep(time.Second)
	}
}

// waitLoaded polls until every chunk of the range is loaded at entity
// ticking, which a forceloaded chunk reaches only once fully generated.
func waitLoaded(rc *rcon, d dimension, r chunkRange, exited <-chan error) error {
	pending := r.positions()
	deadline := time.Now().Add(30 * time.Minute)
	for len(pending) > 0 {
		select {
		case err := <-exited:
			return fmt.Errorf("the reference server exited while generating (%v); see console.log", err)
		default:
		}
		var still [][2]int32
		for _, p := range pending {
			reply, err := rc.Command(fmt.Sprintf("execute in %s if loaded %d 0 %d", d.ID(), p[0]*16, p[1]*16))
			if err != nil {
				return err
			}
			if !strings.Contains(reply, "passed") {
				still = append(still, p)
			}
		}
		pending = still
		if len(pending) == 0 {
			break
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("%d chunks still not generated after 30 minutes", len(pending))
		}
		time.Sleep(2 * time.Second)
	}
	return nil
}

func freePort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}

// loadVanilla reads every chunk of the range out of a cached world, and its
// entities where the entry recorded them.
func loadVanilla(dir string, d dimension, r chunkRange) (map[[2]int32]*chunkData, error) {
	_, err := os.Stat(filepath.Join(dir, "entities"))
	recorded := err == nil
	out := map[[2]int32]*chunkData{}
	for _, p := range r.positions() {
		root, err := readRegionChunk(dir, "region", p[0], p[1])
		if err != nil {
			return nil, err
		}
		if root == nil {
			return nil, fmt.Errorf("vanilla world in %s has no chunk %d,%d", dir, p[0], p[1])
		}
		cd, err := decodeVanillaChunk(d, root)
		if err != nil {
			return nil, err
		}
		if recorded {
			cd.EntitiesRecorded = true
			if cd.Entities, err = readEntitiesChunk(dir, p[0], p[1]); err != nil {
				return nil, err
			}
		}
		out[p] = cd
	}
	return out, nil
}

// loadFeaturesOrder returns nil for a cache created without a recording.
func loadFeaturesOrder(dir string) ([]recordedStep, error) {
	f, err := os.Open(filepath.Join(dir, "features.txt"))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	out := make([]recordedStep, 0)
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		load := strings.HasPrefix(line, "load ")
		light := ""
		if load {
			line = strings.TrimPrefix(line, "load ")
		} else {
			for _, phase := range []string{"light_sources", "initialize_light", "light"} {
				if strings.HasPrefix(line, phase+" ") {
					light = phase
					line = strings.TrimPrefix(line, phase+" ")
					break
				}
			}
		}
		x, z, err := parsePair(line)
		if err != nil {
			return nil, fmt.Errorf("features.txt: %w", err)
		}
		out = append(out, recordedStep{Pos: [2]int32{x, z}, Load: load, Light: light})
	}
	return out, scanner.Err()
}

// datapackNames validates packs before starting a server or selecting a cache.
func datapackNames(paths []string) ([]string, error) {
	var names []string
	for _, path := range paths {
		info, err := os.Stat(filepath.Join(path, "pack.mcmeta"))
		if err != nil {
			return nil, fmt.Errorf("data pack %s: %w", path, err)
		}
		if info.IsDir() {
			return nil, fmt.Errorf("data pack %s: pack.mcmeta is a directory", path)
		}
		name := filepath.Base(filepath.Clean(path))
		if name == "." || name == string(filepath.Separator) || slices.Contains(names, name) {
			return nil, fmt.Errorf("duplicate or invalid data pack name %q", name)
		}
		names = append(names, name)
	}
	return names, nil
}

func copyDatapacks(worldDir string, paths []string) error {
	names, err := datapackNames(paths)
	if err != nil {
		return err
	}
	for i, path := range paths {
		if err := os.CopyFS(filepath.Join(worldDir, "datapacks", names[i]), os.DirFS(path)); err != nil {
			return fmt.Errorf("copy data pack %s: %w", path, err)
		}
	}
	return nil
}

// userBus reports whether systemd-run --user can reach a user manager.
func userBus() bool {
	dir := os.Getenv("XDG_RUNTIME_DIR")
	if dir == "" {
		return false
	}
	_, err := os.Stat(filepath.Join(dir, "bus"))
	return err == nil
}
