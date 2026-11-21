package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// writeConfig writes content to a fresh config file in a temp directory and
// returns its path.
func writeConfig(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), FileName)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	return path
}

func TestLoadCreatesDefaultFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	// The secret is drawn afresh by each loadDefaults.
	if !ValidManagementSecret(cfg.ManagementServerSecret) {
		t.Errorf("management-server-secret = %q, want 40 alphanumeric characters", cfg.ManagementServerSecret)
	}
	defaults := loadDefaults()
	defaults.ManagementServerSecret = cfg.ManagementServerSecret
	if *cfg != *defaults {
		t.Errorf("config = %+v, want defaults %+v", cfg, defaults)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("config file was not created: %v", err)
	}
	for _, want := range []string{"motd=A Go Minecraft Server", "server-port=25565", "max-players=20", "white-list=true"} {
		if !strings.Contains(string(data), want) {
			t.Errorf("config file missing %q, got:\n%s", want, data)
		}
	}

	// Loading again must not change anything.
	reloaded, err := Load(path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if *reloaded != *cfg {
		t.Errorf("reloaded = %+v, want %+v", reloaded, cfg)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(after) != string(data) {
		t.Errorf("reload rewrote the file:\n%s\nwant:\n%s", after, data)
	}
}

func TestLoadExistingFile(t *testing.T) {
	content := "# a comment\n" +
		"!another comment\n" +
		"motd=Hello \\u00dcbung\n" +
		"server-port: 25566\n" +
		"server-ip 127.0.0.1\n" +
		"max-players =  42 \n" +
		"online-mode=false\n" +
		"enable-status=true\n" +
		"hide-online-players=true\n" +
		"view-distance=16\n" +
		"simulation-distance=6\n" +
		"level-name=survival\n" +
		"unknown-key=ignored\n"
	path := writeConfig(t, content)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	want := Config{
		ServerIP:                    "127.0.0.1",
		ServerPort:                  25566,
		MOTD:                        "Hello Übung",
		MaxPlayers:                  42,
		OnlineMode:                  false,
		EnableStatus:                true,
		HideOnlinePlayers:           true,
		ViewDistance:                16,
		SimulationDistance:          6,
		LevelName:                   "survival",
		LevelType:                   "minecraft:normal",
		GeneratorSettings:           "{}",
		InitialEnabledPacks:         "vanilla",
		AutoSaveTicks:               6000,
		PauseWhenEmptySeconds:       60,
		ChatSpamThresholdSeconds:    10,
		CommandSpamThresholdSeconds: 10,
		GameMode:                    "survival",
		Difficulty:                  "easy",
		OpPermissionLevel:           4,
		// The file names no rcon key, so the defaults stand.
		RconPort:                       25575,
		BroadcastRconToOps:             true,
		BroadcastConsoleToOps:          true,
		EntityBroadcastRangePercentage: 100,
		MaxWorldSize:                   29999984,
		MaxChainedNeighborUpdates:      1000000,
		LogIPs:                         true,
		FunctionPermissionLevel:        2,
		QueryPort:                      25565,
		// The file names no compression key either.
		NetworkCompressionThreshold: 256,
		RegionFileCompression:       "deflate",
		SyncChunkWrites:             true,
		EnforceSecureProfile:        true,
		GenerateStructures:          true,
		SpawnProtection:             16,
		WhiteList:                   true,
		MaxTickTime:                 60000,
		MaxTNTPerTick:               100,
		ManagementServerHost:        "localhost",
		ManagementServerTLSEnabled:  true,
		// A file without the key gets a fresh one, checked by
		// TestLoadWritesBackAFreshManagementSecret.
		ManagementServerSecret: cfg.ManagementServerSecret,
		// Not a file key: the --universe command-line default.
		Universe: ".",
	}
	if *cfg != want {
		t.Errorf("config = %+v, want %+v", cfg, want)
	}
}

func TestLoadInvalidValueFallsBackToDefault(t *testing.T) {
	path := writeConfig(t, "server-port=not-a-number\nmotd=Test\n")

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.ServerPort != Default().ServerPort {
		t.Errorf("ServerPort = %d, want default %d", cfg.ServerPort, Default().ServerPort)
	}
	if cfg.MOTD != "Test" {
		t.Errorf("MOTD = %q, want %q", cfg.MOTD, "Test")
	}
}

// TestLoadRejectsLevelNamesOutsideTheDirectory checks a world name cannot
// send the files written beneath it somewhere else: everything the world
// holds, player data included, lives under this directory.
func TestLoadRejectsLevelNamesOutsideTheDirectory(t *testing.T) {
	// Checked on the value itself rather than through a file, because a
	// backslash in a .properties file is an escape and would never reach here.
	for _, value := range []string{"", "   ", ".", "..", "../elsewhere", "/etc", `worlds\survival`} {
		name := "unchanged"
		if err := parseLevelName(value, &name); err == nil {
			t.Errorf("level-name=%q was accepted as %q", value, name)
		}
		if name != "unchanged" {
			t.Errorf("level-name=%q was rejected but still set %q", value, name)
		}
	}

	// A rejected value in a file leaves the default in place.
	path := writeConfig(t, "level-name=../elsewhere\n")
	if cfg, err := Load(path); err != nil {
		t.Fatalf("load: %v", err)
	} else if cfg.LevelName != Default().LevelName {
		t.Errorf("level name = %q, want the default %q", cfg.LevelName, Default().LevelName)
	}

	// An ordinary name is kept, with trailing whitespace ignored.
	path = writeConfig(t, "level-name=survival \n")
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.LevelName != "survival" {
		t.Errorf("level name = %q, want %q", cfg.LevelName, "survival")
	}
}

func TestLoadRejectsDistancesOutOfRange(t *testing.T) {
	for _, value := range []string{"0", "1", "33", "-4"} {
		content := "view-distance=" + value + "\nsimulation-distance=" + value + "\n"
		path := writeConfig(t, content)

		cfg, err := Load(path)
		if err != nil {
			t.Fatalf("load: %v", err)
		}
		if cfg.ViewDistance != Default().ViewDistance {
			t.Errorf("view-distance=%s gave %d, want the default %d", value, cfg.ViewDistance, Default().ViewDistance)
		}
		if cfg.SimulationDistance != Default().SimulationDistance {
			t.Errorf("simulation-distance=%s gave %d, want the default %d",
				value, cfg.SimulationDistance, Default().SimulationDistance)
		}
	}

	// The ends of the range are settings, not mistakes.
	content := fmt.Sprintf("view-distance=%d\nsimulation-distance=%d\n", MinDistance, MaxDistance)
	path := writeConfig(t, content)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.ViewDistance != MinDistance || cfg.SimulationDistance != MaxDistance {
		t.Errorf("distances = %d/%d, want %d/%d",
			cfg.ViewDistance, cfg.SimulationDistance, MinDistance, MaxDistance)
	}
}

func TestLoadAddsMissingKeys(t *testing.T) {
	path := writeConfig(t, "motd=Only the MOTD\n")

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.MOTD != "Only the MOTD" {
		t.Errorf("MOTD = %q, want %q", cfg.MOTD, "Only the MOTD")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.Contains(string(data), "max-players=20") {
		t.Errorf("missing key was not written back, got:\n%s", data)
	}
}

// Rewriting the file must not discard what the operator put in it.
func TestSavePreservesCommentsAndUnknownKeys(t *testing.T) {
	content := "# my own note about the MOTD\n" +
		"motd=Custom\n" +
		"# a setting this build does not know yet\n" +
		"level-seed=12345\n"
	path := writeConfig(t, content)

	// Load appends the missing keys, which rewrites the file.
	if _, err := Load(path); err != nil {
		t.Fatalf("load: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	got := string(data)
	for _, want := range []string{
		"# my own note about the MOTD",
		"motd=Custom",
		"# a setting this build does not know yet",
		"level-seed=12345",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("rewritten file lost %q, got:\n%s", want, got)
		}
	}

	// The unknown key must survive a reload as well.
	reloaded, err := Load(path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if reloaded.MOTD != "Custom" {
		t.Errorf("MOTD = %q, want %q", reloaded.MOTD, "Custom")
	}
}

func TestSaveRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)
	cfg := Default()
	cfg.MOTD = "§6Grüße, 世界! 😀 = : # \\ end"
	cfg.ServerIP = "0.0.0.0"
	cfg.MaxPlayers = 100

	if err := cfg.Save(path); err != nil {
		t.Fatalf("save: %v", err)
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if *loaded != *cfg {
		t.Errorf("round trip = %+v, want %+v", loaded, cfg)
	}
}

func TestAddr(t *testing.T) {
	cfg := Default()
	if got := cfg.Addr(); got != ":25565" {
		t.Errorf("Addr() = %q, want %q", got, ":25565")
	}
	cfg.ServerIP = "127.0.0.1"
	if got := cfg.Addr(); got != "127.0.0.1:25565" {
		t.Errorf("Addr() = %q, want %q", got, "127.0.0.1:25565")
	}
}

// TestLoadWorldSettings checks the keys that choose the world: the kind of
// generator and the seed it starts from, both kept as written so a value a
// newer build supports survives a rewrite.
func TestLoadWorldSettings(t *testing.T) {
	content := "level-type=minecraft\\:flat\n" +
		"level-seed=-12345 \n" +
		"level-name=flatland\n"
	path := writeConfig(t, content)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.LevelType != "minecraft:flat" {
		t.Errorf("LevelType = %q, want the flat world", cfg.LevelType)
	}
	if cfg.LevelSeed != "-12345" {
		t.Errorf("LevelSeed = %q, want %q (trailing space ignored, value kept verbatim)", cfg.LevelSeed, "-12345")
	}

	// A file with neither key gets the defaults: the overworld, and a
	// seed chosen when the world is created.
	path = writeConfig(t, "level-name=world\n")
	cfg, err = Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.LevelType != "minecraft:normal" {
		t.Errorf("default LevelType = %q, want the overworld", cfg.LevelType)
	}
	if cfg.LevelSeed != "" {
		t.Errorf("default LevelSeed = %q, want empty (chosen at creation)", cfg.LevelSeed)
	}

	// A write of the test config round-trips the values it was given.
	cfg = Default()
	cfg.LevelType = "minecraft:flat"
	cfg.LevelSeed = "hello world"
	if err := cfg.Save(path); err != nil {
		t.Fatalf("save: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	for _, want := range []string{
		"level-type=minecraft:flat",
		"level-seed=hello world",
	} {
		if !strings.Contains(string(data), want) {
			t.Errorf("saved file lost %q, got:\n%s", want, data)
		}
	}
}

// TestLoadGameMode checks the gamemode key: the four names as written, a
// case-insensitive read, and everything else falling back to the default.
func TestLoadGameMode(t *testing.T) {
	for _, tc := range []struct {
		value string
		want  string
	}{
		{"survival", "survival"},
		{"creative", "creative"},
		{"adventure", "adventure"},
		{"spectator", "spectator"},
		{"Survival", "survival"}, // case-insensitive, like the vanilla server
		{" CREATIVE ", "creative"},
	} {
		path := writeConfig(t, "gamemode="+tc.value+"\n")
		cfg, err := Load(path)
		if err != nil {
			t.Fatalf("load %q: %v", tc.value, err)
		}
		if cfg.GameMode != tc.want {
			t.Errorf("gamemode=%q gave %q, want %q", tc.value, cfg.GameMode, tc.want)
		}
	}

	// A mode the server does not know is rejected; the default takes over.
	path := writeConfig(t, "gamemode=hardcore-with-extra-cheese\n")
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.GameMode != Default().GameMode {
		t.Errorf("gamemode = %q, want the default %q", cfg.GameMode, Default().GameMode)
	}

	// The default file says survival, the way a vanilla server's does.
	path = filepath.Join(t.TempDir(), FileName)
	if _, err := Load(path); err != nil {
		t.Fatalf("load: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.Contains(string(data), "gamemode=survival") {
		t.Errorf("config file missing gamemode=survival, got:\n%s", data)
	}
}

// TestLoadDifficultyAndOpLevel checks the difficulty key (the four names as
// written, case-insensitively, anything else falling back to the default) and
// the op-permission-level key (a number clamped into 0 to 4, a non-number
// falling back to the default).
func TestLoadDifficultyAndOpLevel(t *testing.T) {
	for _, tc := range []struct {
		value string
		want  string
	}{
		{"peaceful", "peaceful"},
		{"easy", "easy"},
		{"normal", "normal"},
		{"hard", "hard"},
		{"HARD", "hard"}, // case-insensitive, like the vanilla server
		{" Easy ", "easy"},
	} {
		path := writeConfig(t, "difficulty="+tc.value+"\n")
		cfg, err := Load(path)
		if err != nil {
			t.Fatalf("load %q: %v", tc.value, err)
		}
		if cfg.Difficulty != tc.want {
			t.Errorf("difficulty=%q gave %q, want %q", tc.value, cfg.Difficulty, tc.want)
		}
	}

	// A difficulty the server does not know is rejected; the default takes
	// over.
	path := writeConfig(t, "difficulty=impossible\n")
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Difficulty != Default().Difficulty {
		t.Errorf("difficulty = %q, want the default %q", cfg.Difficulty, Default().Difficulty)
	}

	// The ends of the op level range are settings, not mistakes.
	for _, value := range []int{0, 1, 2, 3, 4} {
		path := writeConfig(t, "op-permission-level="+strconv.Itoa(value)+"\n")
		cfg, err := Load(path)
		if err != nil {
			t.Fatalf("load %d: %v", value, err)
		}
		if cfg.OpPermissionLevel != value {
			t.Errorf("op-permission-level=%d gave %d", value, cfg.OpPermissionLevel)
		}
	}

	// A level outside 0 to 4 clamps to the nearer end rather than falling
	// back to the default, so a negative level never silently becomes owner.
	for _, tc := range []struct {
		value string
		want  int
	}{
		{"-3", 0},
		{"9", 4},
	} {
		path = writeConfig(t, "op-permission-level="+tc.value+"\n")
		cfg, err = Load(path)
		if err != nil {
			t.Fatalf("load %s: %v", tc.value, err)
		}
		if cfg.OpPermissionLevel != tc.want {
			t.Errorf("op-permission-level=%s gave %d, want %d", tc.value, cfg.OpPermissionLevel, tc.want)
		}
	}

	// A value that is not a number is refused, and the default takes over.
	path = writeConfig(t, "op-permission-level=owner\n")
	cfg, err = Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.OpPermissionLevel != Default().OpPermissionLevel {
		t.Errorf("op level = %d, want the default %d", cfg.OpPermissionLevel, Default().OpPermissionLevel)
	}

	// The default file says easy and op level 4, as a vanilla server's does.
	path = filepath.Join(t.TempDir(), FileName)
	if _, err := Load(path); err != nil {
		t.Fatalf("load: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	for _, want := range []string{"difficulty=easy", "op-permission-level=4"} {
		if !strings.Contains(string(data), want) {
			t.Errorf("config file missing %s, got:\n%s", want, data)
		}
	}
}

// TestLoadNetworkCompressionThreshold checks the compression key: the vanilla
// server's 256 by default, any number read as written — the negative range
// being how an operator disables compression — and the default file carrying
// the vanilla default.
func TestLoadNetworkCompressionThreshold(t *testing.T) {
	if got := Default().NetworkCompressionThreshold; got != 256 {
		t.Errorf("default threshold = %d, want the vanilla server's 256", got)
	}

	for _, tc := range []struct{ value, want int }{
		{64, 64},
		{0, 0},
		{-1, -1},
		{-5, -5},
	} {
		path := writeConfig(t, "network-compression-threshold="+strconv.Itoa(tc.value)+"\n")
		cfg, err := Load(path)
		if err != nil {
			t.Fatalf("load %d: %v", tc.value, err)
		}
		if cfg.NetworkCompressionThreshold != tc.want {
			t.Errorf("threshold = %d, want %d", cfg.NetworkCompressionThreshold, tc.want)
		}
	}

	path := filepath.Join(t.TempDir(), FileName)
	if _, err := Load(path); err != nil {
		t.Fatalf("load: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.Contains(string(data), "network-compression-threshold=256") {
		t.Errorf("config file missing network-compression-threshold=256, got:\n%s", data)
	}
}

// TestLoadAcceptsTransfers checks the accepts-transfers key: off by default,
// and read and written like the other booleans.
func TestLoadAcceptsTransfers(t *testing.T) {
	if Default().AcceptsTransfers {
		t.Error("accepts-transfers defaults on, want off")
	}

	path := writeConfig(t, "accepts-transfers=true\n")
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if !cfg.AcceptsTransfers {
		t.Error("accepts-transfers=true gave false")
	}

	path = filepath.Join(t.TempDir(), FileName)
	if _, err := Load(path); err != nil {
		t.Fatalf("load: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.Contains(string(data), "accepts-transfers=false") {
		t.Errorf("config file missing accepts-transfers=false, got:\n%s", data)
	}
}

// TestLoadBugReportLink checks the bug-report-link key: empty by default,
// read from a file, and present in the default file.
func TestLoadBugReportLink(t *testing.T) {
	if Default().BugReportLink != "" {
		t.Errorf("bug-report-link defaults to %q, want empty", Default().BugReportLink)
	}

	path := writeConfig(t, "bug-report-link=https://example.org/x\n")
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.BugReportLink != "https://example.org/x" {
		t.Errorf("bug-report-link = %q, want https://example.org/x", cfg.BugReportLink)
	}

	// An empty authority ("http:///x") and an opaque URI ("http:x") are both
	// valid to java.net.URI, so they are kept.
	for _, value := range []string{"http:///x", "http:x"} {
		path := writeConfig(t, "bug-report-link="+value+"\n")
		if cfg, err := Load(path); err != nil {
			t.Fatalf("load %q: %v", value, err)
		} else if cfg.BugReportLink != value {
			t.Errorf("bug-report-link = %q, want %q", cfg.BugReportLink, value)
		}
	}

	// A stray space around a URL is a typo, not part of the address.
	path = writeConfig(t, "bug-report-link= https://example.org/x \n")
	if cfg, err := Load(path); err != nil {
		t.Fatalf("load: %v", err)
	} else if cfg.BugReportLink != "https://example.org/x" {
		t.Errorf("bug-report-link = %q, want the URL without the spaces", cfg.BugReportLink)
	}

	// The scheme counts case-insensitively, and the URL is kept as written.
	path = writeConfig(t, "bug-report-link=HTTP://example.org/x\n")
	if cfg, err := Load(path); err != nil {
		t.Fatalf("load: %v", err)
	} else if cfg.BugReportLink != "HTTP://example.org/x" {
		t.Errorf("bug-report-link = %q, want the URL as written", cfg.BugReportLink)
	}

	path = filepath.Join(t.TempDir(), FileName)
	if _, err := Load(path); err != nil {
		t.Fatalf("load: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.Contains(string(data), "bug-report-link=") {
		t.Errorf("config file missing bug-report-link=, got:\n%s", data)
	}
}

// TestLoadRconSettings checks the rcon keys: off by default with vanilla's
// port, no password and its commands echoed to the operators, all four read
// from a file, and the default file carrying all four.
func TestLoadRconSettings(t *testing.T) {
	d := Default()
	if d.EnableRcon {
		t.Error("enable-rcon defaults on, want off")
	}
	if d.RconPort != 25575 {
		t.Errorf("default rcon.port = %d, want 25575", d.RconPort)
	}
	if d.RconPassword != "" {
		t.Errorf("default rcon.password = %q, want empty", d.RconPassword)
	}
	if !d.BroadcastRconToOps {
		t.Error("broadcast-rcon-to-ops defaults off, want on")
	}

	path := writeConfig(t, "enable-rcon=true\nrcon.port=25575\nrcon.password=sesame\nbroadcast-rcon-to-ops=false\n")
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if !cfg.EnableRcon {
		t.Error("enable-rcon=true gave false")
	}
	if cfg.RconPort != 25575 {
		t.Errorf("rcon.port = %d, want 25575", cfg.RconPort)
	}
	if cfg.RconPassword != "sesame" {
		t.Errorf("rcon.password = %q, want sesame", cfg.RconPassword)
	}
	if cfg.BroadcastRconToOps {
		t.Error("broadcast-rcon-to-ops=false gave true")
	}

	path = filepath.Join(t.TempDir(), FileName)
	if _, err := Load(path); err != nil {
		t.Fatalf("load: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	for _, want := range []string{"enable-rcon=false", "rcon.port=25575", "rcon.password=", "broadcast-rcon-to-ops=true"} {
		if !strings.Contains(string(data), want) {
			t.Errorf("config file missing %s, got:\n%s", want, data)
		}
	}
}

// TestLoadResourcePackKeys checks the resource-pack keys: off by default,
// read from a file, and present in the default file.
func TestLoadResourcePackKeys(t *testing.T) {
	d := Default()
	if d.ResourcePack != "" || d.RequireResourcePack {
		t.Errorf("default pack = %q require=%v, want none", d.ResourcePack, d.RequireResourcePack)
	}

	path := writeConfig(t, "resource-pack=https://example.org/p.zip\nresource-pack-id=11111111-2222-3333-4444-555555555555\n"+
		"resource-pack-sha1=0123456789abcdef0123456789abcdef01234567\nresource-pack-prompt={\"text\":\"hi\"}\nrequire-resource-pack=true\n")
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.ResourcePack != "https://example.org/p.zip" || cfg.ResourcePackID != "11111111-2222-3333-4444-555555555555" ||
		cfg.ResourcePackSHA1 != "0123456789abcdef0123456789abcdef01234567" || cfg.ResourcePackPrompt != `{"text":"hi"}` ||
		!cfg.RequireResourcePack {
		t.Errorf("config = %+v", cfg)
	}

	path = writeConfig(t, "")
	if _, err := Load(path); err != nil {
		t.Fatalf("load: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"resource-pack=", "resource-pack-id=", "resource-pack-sha1=", "resource-pack-prompt=", "require-resource-pack=false"} {
		if !strings.Contains(string(data), key) {
			t.Errorf("default file lacks %s:\n%s", key, data)
		}
	}
}

func TestExtraServerPropertiesDefaultsAndClamps(t *testing.T) {
	d := Default()
	if !d.BroadcastConsoleToOps || !d.LogIPs || d.EntityBroadcastRangePercentage != 100 ||
		d.MaxWorldSize != 29999984 || d.FunctionPermissionLevel != 2 {
		t.Errorf("defaults wrong: %+v", d)
	}
	path := writeConfig(t, "broadcast-console-to-ops=false\nlog-ips=false\nentity-broadcast-range-percentage=5000\nmax-world-size=0\nfunction-permission-level=9\n")
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.BroadcastConsoleToOps || cfg.LogIPs {
		t.Error("false booleans read as true")
	}
	bad, err := Load(writeConfig(t, "entity-broadcast-range-percentage=lots\nmax-world-size=\nfunction-permission-level=x\n"))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if bad.EntityBroadcastRangePercentage != 100 || bad.MaxWorldSize != 29999984 || bad.FunctionPermissionLevel != 2 {
		t.Errorf("unparseable values did not fall back: %+v", bad)
	}
	if cfg.EntityBroadcastRangePercentage != 1000 || cfg.MaxWorldSize != 1 || cfg.FunctionPermissionLevel != 4 {
		t.Errorf("clamps wrong: %+v", cfg)
	}
}

// TestLoadGenerateStructures checks generate-structures: on by default, read
// from a file, and present in the default file.
func TestLoadGenerateStructures(t *testing.T) {
	t.Parallel()
	cfg, err := Load(writeConfig(t, "generate-structures=false\n"))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.GenerateStructures {
		t.Error("generate-structures=false gave true")
	}
	if !Default().GenerateStructures {
		t.Error("generate-structures defaults off, want on")
	}
	path := filepath.Join(t.TempDir(), FileName)
	if _, err := Load(path); err != nil {
		t.Fatalf("load: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "generate-structures=true") {
		t.Errorf("default file lacks generate-structures=true:\n%s", data)
	}
}

// TestPlayerIdleTimeoutRoundTrip checks player-idle-timeout loads and saves.
func TestPlayerIdleTimeoutRoundTrip(t *testing.T) {
	path := writeConfig(t, "player-idle-timeout=7\n")
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.PlayerIdleTimeout != 7 {
		t.Fatalf("PlayerIdleTimeout = %d, want 7", cfg.PlayerIdleTimeout)
	}
	cfg.PlayerIdleTimeout = 3
	if err := cfg.Save(path); err != nil {
		t.Fatalf("save: %v", err)
	}
	again, err := Load(path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if again.PlayerIdleTimeout != 3 {
		t.Errorf("PlayerIdleTimeout = %d after save, want 3", again.PlayerIdleTimeout)
	}
}

// vanilla: DedicatedServerProperties.managementServerSecret, Settings.get
func TestLoadWritesBackAFreshManagementSecret(t *testing.T) {
	path := writeConfig(t, "motd=x\n")
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !ValidManagementSecret(cfg.ManagementServerSecret) {
		t.Fatalf("management-server-secret = %q, want 40 alphanumeric characters", cfg.ManagementServerSecret)
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), "management-server-secret="+cfg.ManagementServerSecret+"\n") {
		t.Errorf("the secret was not written back:\n%s", data)
	}
	reloaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.ManagementServerSecret != cfg.ManagementServerSecret {
		t.Errorf("reload secret = %q, want the saved %q", reloaded.ManagementServerSecret, cfg.ManagementServerSecret)
	}
	if other := GenerateManagementSecret(); other == cfg.ManagementServerSecret || !ValidManagementSecret(other) {
		t.Errorf("a second secret %q is invalid or repeats the first", other)
	}

	// A key present but empty stays empty.
	empty, err := Load(writeConfig(t, "management-server-secret=\n"))
	if err != nil {
		t.Fatal(err)
	}
	if empty.ManagementServerSecret != "" {
		t.Errorf("management-server-secret= loaded as %q, want empty", empty.ManagementServerSecret)
	}
	if Default().ManagementServerSecret != "" {
		t.Error("Default() has a management secret")
	}
}

// vanilla: DedicatedServerProperties
func TestLoadManagementServerKeys(t *testing.T) {
	cfg, err := Load(writeConfig(t, "management-server-enabled=true\n"+
		"management-server-host=0.0.0.0\n"+
		"management-server-port=25585\n"+
		"management-server-tls-enabled=false\n"+
		"management-server-tls-keystore=ks.p12\n"+
		"management-server-tls-keystore-password=pw\n"+
		"management-server-allowed-origins=http://a,http://b\n"+
		"status-heartbeat-interval=30\n"))
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.ManagementServerEnabled || cfg.ManagementServerHost != "0.0.0.0" || cfg.ManagementServerPort != 25585 ||
		cfg.ManagementServerTLSEnabled || cfg.ManagementServerTLSKeystore != "ks.p12" ||
		cfg.ManagementServerTLSKeystorePassword != "pw" || cfg.ManagementServerAllowedOrigins != "http://a,http://b" ||
		cfg.StatusHeartbeatInterval != 30 {
		t.Errorf("config = %+v", cfg)
	}
	d := Default()
	if d.ManagementServerEnabled || d.ManagementServerHost != "localhost" || d.ManagementServerPort != 0 ||
		!d.ManagementServerTLSEnabled || d.ManagementServerAllowedOrigins != "" || d.StatusHeartbeatInterval != 0 {
		t.Errorf("defaults = %+v", d)
	}
}

func TestValidManagementSecret(t *testing.T) {
	for s, want := range map[string]bool{
		strings.Repeat("a1", 20):          true,
		strings.Repeat("Z9", 20):          true,
		"short":                           false,
		"":                                false,
		strings.Repeat("a", 41):           false,
		strings.Repeat("a", 39) + "-":     false,
		strings.Repeat("a", 39) + "é"[:1]: false,
	} {
		if got := ValidManagementSecret(s); got != want {
			t.Errorf("ValidManagementSecret(%q) = %v, want %v", s, got, want)
		}
	}
}

// TestGeneratorSettingsKey checks that a fresh file holds `{}` and that a JSON
// value with ':' and ',' survives a save and a load unchanged.
func TestGeneratorSettingsKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "server.properties")
	if err := Default().Save(path); err != nil {
		t.Fatalf("save: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.Contains(string(data), "generator-settings={}") {
		t.Errorf("fresh file lacks generator-settings={}, got:\n%s", data)
	}

	const value = `{"layers":[{"block":"minecraft:stone","height":2}],"biome":"minecraft:desert"}`
	cfg := Default()
	cfg.GeneratorSettings = value
	if err := cfg.Save(path); err != nil {
		t.Fatalf("save: %v", err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if loaded.GeneratorSettings != value {
		t.Errorf("GeneratorSettings = %q, want %q", loaded.GeneratorSettings, value)
	}
}

// TestLoadLegacyKeys checks resource-pack-hash and announce-player-achievements
// are read but never written back.
func TestLoadLegacyKeys(t *testing.T) {
	path := writeConfig(t, "resource-pack=https://x/p.zip\nresource-pack-hash=0123456789abcdef0123456789abcdef01234567\n"+
		"announce-player-achievements=false\n")
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.LegacyResourcePackHash != "0123456789abcdef0123456789abcdef01234567" || cfg.LegacyAnnounceAchievements != "false" {
		t.Errorf("legacy = %q %q", cfg.LegacyResourcePackHash, cfg.LegacyAnnounceAchievements)
	}
	data, _ := os.ReadFile(path)
	if strings.Contains(string(data), "resource-pack-hash=") || strings.Contains(string(data), "announce-player-achievements") {
		t.Errorf("rewritten file keeps a legacy key:\n%s", data)
	}
	path = writeConfig(t, "")
	if _, err := Load(path); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(path)
	if strings.Contains(string(data), "resource-pack-hash") || strings.Contains(string(data), "announce-player-achievements") {
		t.Errorf("default file has legacy keys:\n%s", data)
	}
}
