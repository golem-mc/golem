package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/golem-mc/golem/tools/internal/vanilla"
	"github.com/google/uuid"
)

// assetHost serves the asset objects by hash. A variable so a test can point
// it elsewhere.
var assetHost = "https://resources.download.minecraft.net"

func (c config) clientDir() string { return filepath.Join(c.dir, "client") }

// clientMeta is the version metadata, kept beside the client's files.
func clientMeta(f *vanilla.Fetcher, cfg config) (*vanilla.VersionMeta, error) {
	path := filepath.Join(cfg.clientDir(), cfg.version+".json")
	if raw, err := os.ReadFile(path); err == nil {
		meta := &vanilla.VersionMeta{}
		if err := json.Unmarshal(raw, meta); err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		return meta, nil
	}
	meta, err := f.VersionMeta(cfg.version)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(cfg.clientDir(), 0o755); err != nil {
		return nil, err
	}
	return meta, os.WriteFile(path, meta.Raw, 0o644)
}

// clientSetup downloads the official client into <dir>/client: the jar, its
// libraries and its assets, sounds left out (most of the assets by size and
// nothing a smoke test needs; the client logs their absence and carries on).
func clientSetup(f *vanilla.Fetcher, cfg config, stdout io.Writer) error {
	meta, err := clientMeta(f, cfg)
	if err != nil {
		return err
	}
	dir := cfg.clientDir()
	fmt.Fprintf(stdout, "client jar (%d MB)\n", meta.Downloads.Client.Size/1_000_000)
	if err := f.Fetch(meta.Downloads.Client.URL, meta.Downloads.Client.SHA1, filepath.Join(dir, "client.jar")); err != nil {
		return err
	}
	var libs int
	for _, lib := range meta.Libraries {
		a := lib.Downloads.Artifact
		if a == nil || !lib.Allowed() {
			continue
		}
		libs++
		if err := f.Fetch(a.URL, a.SHA1, filepath.Join(dir, "libraries", a.Path)); err != nil {
			return err
		}
	}
	fmt.Fprintf(stdout, "%d libraries\n", libs)

	indexPath := filepath.Join(dir, "assets", "indexes", meta.AssetIndex.ID+".json")
	if err := f.Fetch(meta.AssetIndex.URL, meta.AssetIndex.SHA1, indexPath); err != nil {
		return err
	}
	raw, err := os.ReadFile(indexPath)
	if err != nil {
		return err
	}
	var index struct {
		Objects map[string]struct {
			Hash string `json:"hash"`
		} `json:"objects"`
	}
	if err := json.Unmarshal(raw, &index); err != nil {
		return fmt.Errorf("%s: %w", indexPath, err)
	}
	wanted := 0
	for name, obj := range index.Objects {
		if strings.HasPrefix(name, "minecraft/sounds/") {
			continue
		}
		wanted++
		h := obj.Hash
		if err := f.Fetch(assetHost+"/"+h[:2]+"/"+h, h, filepath.Join(dir, "assets", "objects", h[:2], h)); err != nil {
			return err
		}
	}
	fmt.Fprintf(stdout, "%d of %d assets (sounds skipped)\n", wanted, len(index.Objects))
	fmt.Fprintf(stdout, "client ready in %s\n", dir)
	return nil
}

// clientCommand is the command line that starts the client offline and has it
// join address straight away. With CLIENTCTL=1 the clientctl agent rides along:
// it answers HTTP on 127.0.0.1 with the client's own state and actions, so a
// test can drive the game instead of typing keystrokes at it.
func clientCommand(cfg config, meta *vanilla.VersionMeta, address, username string, env func(string) string) ([]string, error) {
	dir, err := filepath.Abs(cfg.clientDir())
	if err != nil {
		return nil, err
	}
	var classpath []string
	for _, lib := range meta.Libraries {
		if a := lib.Downloads.Artifact; a != nil && lib.Allowed() {
			classpath = append(classpath, filepath.Join(dir, "libraries", a.Path))
		}
	}
	classpath = append(classpath, filepath.Join(dir, "client.jar"))
	// The UUID an offline-mode server derives from the name, which the server
	// uses whatever the client says; any UUID would do for the client itself.
	id := uuid.NewMD5(uuid.NameSpaceDNS, []byte("OfflinePlayer:"+username))
	cmd := []string{"java", "-Xmx2G"}
	if env("CLIENTCTL") == "1" {
		agent, err := filepath.Abs(filepath.Join(cfg.dir, "clientctl-agent.jar"))
		if err != nil {
			return nil, err
		}
		if _, err := os.Stat(agent); err != nil {
			return nil, fmt.Errorf("no agent jar at %s - run 'mise run client:ctl-build' first", agent)
		}
		port := env("CLIENTCTL_PORT")
		if port == "" {
			port = "25700"
		}
		cmd = append(cmd, "-javaagent:"+agent, "-Dclientctl.port="+port)
	}
	cmd = append(cmd, "-cp", strings.Join(classpath, string(os.PathListSeparator)), meta.MainClass,
		"--username", username,
		"--version", cfg.version,
		"--gameDir", filepath.Join(dir, "game"),
		"--assetsDir", filepath.Join(dir, "assets"),
		"--assetIndex", meta.AssetIndex.ID,
		"--uuid", id.String(),
		"--accessToken", "0",
		"--userType", "legacy",
		"--versionType", "release",
		"--width", "854", "--height", "480",
		"--quickPlayMultiplayer", address)
	return cmd, nil
}

// clientRun starts the client, which needs a display: on a machine without one
// the client:run task starts Xvfb with Mesa's software OpenGL first.
func clientRun(cfg config, address, username string) error {
	meta, err := clientMeta(vanilla.NewFetcher(os.Stderr), cfg)
	if err != nil {
		return fmt.Errorf("%w; run `mise run client:setup` first", err)
	}
	cmd, err := clientCommand(cfg, meta, address, username, os.Getenv)
	if err != nil {
		return err
	}
	gameDir, err := filepath.Abs(filepath.Join(cfg.clientDir(), "game"))
	if err != nil {
		return err
	}
	if err := os.MkdirAll(gameDir, 0o755); err != nil {
		return err
	}
	// A first start shows an accessibility screen that holds up the quick join
	// until someone clicks past it, and a window without focus (which one under
	// Xvfb never has) pauses the game.
	options := filepath.Join(gameDir, "options.txt")
	if _, err := os.Stat(options); err != nil {
		if err := os.WriteFile(options, []byte("onboardAccessibility:false\npauseOnLostFocus:false\n"), 0o644); err != nil {
			return err
		}
	}
	// The client writes its logs/ and crash reports into the directory it is
	// started from, so it is started from its own game directory rather than
	// the repository's root.
	c := exec.Command(cmd[0], cmd[1:]...)
	c.Dir = gameDir
	c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
	return runForwarding(c)
}
