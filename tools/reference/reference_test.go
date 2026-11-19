package main

import (
	"bytes"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/golem-mc/golem/tools/internal/vanilla"
)

func sha(b []byte) string {
	h := sha1.Sum(b)
	return hex.EncodeToString(h[:])
}

// mojang stands in for the manifest, the metadata and the files they link to.
type mojang struct {
	*httptest.Server
	mu       sync.Mutex
	requests map[string]int
	files    map[string][]byte
}

func newMojang(t *testing.T, version string) *mojang {
	t.Helper()
	m := &mojang{requests: map[string]int{}, files: map[string][]byte{
		"/server.jar": []byte("server"), "/client.jar": []byte("client"), "/cfr.jar": []byte("cfr"),
		"/lib/a.jar": []byte("liba"), "/lib/mac.jar": []byte("libmac"),
	}}
	var assetIndex []byte
	m.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		m.mu.Lock()
		m.requests[r.URL.Path]++
		m.mu.Unlock()
		switch {
		case r.URL.Path == "/manifest.json":
			meta := m.meta(version)
			_ = json.NewEncoder(w).Encode(map[string]any{"versions": []any{
				map[string]any{"id": "1.0", "url": m.URL + "/other.json", "sha1": "x"},
				map[string]any{"id": version, "url": m.URL + "/meta.json", "sha1": sha(meta)},
			}})
		case r.URL.Path == "/meta.json":
			_, _ = w.Write(m.meta(version))
		case r.URL.Path == "/index.json":
			_, _ = w.Write(assetIndex)
		case strings.Count(r.URL.Path, "/") == 2 && len(strings.Split(r.URL.Path, "/")[1]) == 2: // an asset: /<2 hex>/<hash>
			_, _ = w.Write([]byte("asset"))
		default:
			if b, ok := m.files[r.URL.Path]; ok {
				_, _ = w.Write(b)
				return
			}
			http.NotFound(w, r)
		}
	}))
	assetIndex = []byte(`{"objects": {"minecraft/lang/en_us.json": {"hash": "` + sha([]byte("asset")) + `"}, "minecraft/sounds/x.ogg": {"hash": "00"}}}`)
	m.files["/index.json"] = assetIndex
	t.Cleanup(m.Close)
	return m
}

func (m *mojang) meta(version string) []byte {
	linux := []any{map[string]any{"action": "allow", "os": map[string]any{"name": "linux"}}}
	osx := []any{map[string]any{"action": "allow", "os": map[string]any{"name": "osx"}}}
	lib := func(path string, url string, body []byte, rules []any) map[string]any {
		return map[string]any{"downloads": map[string]any{"artifact": map[string]any{"path": path, "url": m.URL + url, "sha1": sha(body)}}, "rules": rules}
	}
	b, _ := json.Marshal(map[string]any{
		"id": version, "mainClass": "net.minecraft.client.main.Main",
		"javaVersion": map[string]any{"majorVersion": 25},
		"downloads": map[string]any{
			"server": map[string]any{"url": m.URL + "/server.jar", "sha1": sha(m.files["/server.jar"]), "size": 6},
			"client": map[string]any{"url": m.URL + "/client.jar", "sha1": sha(m.files["/client.jar"]), "size": 6},
		},
		"libraries":  []any{lib("com/a.jar", "/lib/a.jar", m.files["/lib/a.jar"], linux), lib("com/mac.jar", "/lib/mac.jar", m.files["/lib/mac.jar"], osx)},
		"assetIndex": map[string]any{"id": "99", "url": m.URL + "/index.json", "sha1": sha(m.files["/index.json"])},
	})
	return b
}

func (m *mojang) fetcher() *vanilla.Fetcher {
	return &vanilla.Fetcher{HTTP: m.Client(), ManifestURL: m.URL + "/manifest.json", UserAgent: "test"}
}

func TestSetupDownloadsOnceAndWritesTheServerFiles(t *testing.T) {
	m := newMojang(t, "26.3")
	cfg := config{dir: filepath.Join(t.TempDir(), "ref"), version: "26.3", port: 25601}
	var out bytes.Buffer
	if err := setup(m.fetcher(), cfg, m.URL+"/cfr.jar", sha([]byte("cfr")), &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "needs Java 25") {
		t.Errorf("output = %q", out.String())
	}
	for path, want := range map[string]string{
		cfg.serverJar(): "server", cfg.clientJar(): "client", vanilla.Ref{Dir: cfg.dir}.CFRJar(): "cfr",
		filepath.Join(cfg.dir, "eula.txt"): "eula=true\n",
	} {
		if got, err := os.ReadFile(path); err != nil || string(got) != want {
			t.Errorf("%s = %q, %v; want %q", path, got, err, want)
		}
	}
	props, _ := os.ReadFile(filepath.Join(cfg.dir, "server.properties"))
	for _, want := range []string{"server-port=25601", "online-mode=false", "white-list=false", `level-type=minecraft\:flat`} {
		if !strings.Contains(string(props), want) {
			t.Errorf("server.properties lacks %q:\n%s", want, props)
		}
	}
	// A second setup finds every file right and downloads none of them again.
	before := m.requests["/server.jar"] + m.requests["/client.jar"] + m.requests["/cfr.jar"]
	if err := setup(m.fetcher(), cfg, m.URL+"/cfr.jar", sha([]byte("cfr")), &out); err != nil {
		t.Fatal(err)
	}
	if after := m.requests["/server.jar"] + m.requests["/client.jar"] + m.requests["/cfr.jar"]; after != before {
		t.Errorf("%d files fetched again", after-before)
	}
}

func TestADownloadThatDoesNotMatchItsHashIsRefused(t *testing.T) {
	m := newMojang(t, "26.3")
	cfg := config{dir: t.TempDir(), version: "26.3", port: 25599}
	err := setup(m.fetcher(), cfg, m.URL+"/cfr.jar", "0000000000000000000000000000000000000000", &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "sha1 mismatch") {
		t.Errorf("err = %v", err)
	}
	if _, statErr := os.Stat(vanilla.Ref{Dir: cfg.dir}.CFRJar()); statErr == nil {
		t.Error("the mismatching file was kept")
	}
	if _, err := m.fetcher().VersionMeta("9.9"); err == nil || !strings.Contains(err.Error(), "no such Minecraft version") {
		t.Errorf("an unknown version: %v", err)
	}
}

func TestClientSetupSkipsSoundsAndOtherPlatformsLibraries(t *testing.T) {
	m := newMojang(t, "26.3")
	defer func(old string) { assetHost = old }(assetHost)
	assetHost = m.URL
	cfg := config{dir: t.TempDir(), version: "26.3"}
	var out bytes.Buffer
	if err := clientSetup(m.fetcher(), cfg, &out); err != nil {
		t.Fatal(err)
	}
	dir := cfg.clientDir()
	for _, want := range []string{"client.jar", "libraries/com/a.jar", "26.3.json", "assets/indexes/99.json"} {
		if _, err := os.Stat(filepath.Join(dir, want)); err != nil {
			t.Errorf("%s: %v", want, err)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "libraries", "com", "mac.jar")); err == nil {
		t.Error("a library for another platform was downloaded")
	}
	if !strings.Contains(out.String(), "1 of 2 assets (sounds skipped)") || !strings.Contains(out.String(), "1 libraries") {
		t.Errorf("output = %q", out.String())
	}
}

func TestClientCommandJoinsOfflineAndCarriesTheAgentWhenAsked(t *testing.T) {
	m := newMojang(t, "26.3")
	cfg := config{dir: t.TempDir(), version: "26.3"}
	meta, err := m.fetcher().VersionMeta("26.3")
	if err != nil {
		t.Fatal(err)
	}
	none := func(string) string { return "" }
	cmd, err := clientCommand(cfg, meta, "127.0.0.1:25565", "Tester", none)
	if err != nil {
		t.Fatal(err)
	}
	line := strings.Join(cmd, " ")
	for _, want := range []string{"java -Xmx2G -cp ", "com/a.jar", "client.jar net.minecraft.client.main.Main", "--username Tester",
		"--quickPlayMultiplayer 127.0.0.1:25565", "--assetIndex 99", "--accessToken 0"} {
		if !strings.Contains(line, want) {
			t.Errorf("command lacks %q:\n%s", want, line)
		}
	}
	if strings.Contains(line, "mac.jar") || strings.Contains(line, "javaagent") {
		t.Errorf("command = %s", line)
	}
	// The offline-mode UUID is the name-based (MD5) UUID of "OfflinePlayer:"
	// and the name, as a vanilla server derives it.
	if !strings.Contains(line, "--uuid ") {
		t.Error("no --uuid")
	}

	env := func(k string) string { return map[string]string{"CLIENTCTL": "1", "CLIENTCTL_PORT": "25777"}[k] }
	if _, err := clientCommand(cfg, meta, "x:1", "T", env); err == nil || !strings.Contains(err.Error(), "client:ctl-build") {
		t.Errorf("a missing agent jar: %v", err)
	}
	if err := os.WriteFile(filepath.Join(cfg.dir, "clientctl-agent.jar"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	cmd, err = clientCommand(cfg, meta, "x:1", "T", env)
	if err != nil {
		t.Fatal(err)
	}
	if line := strings.Join(cmd, " "); !strings.Contains(line, "-javaagent:") || !strings.Contains(line, "-Dclientctl.port=25777") {
		t.Errorf("command = %s", line)
	}
}

func TestOfflineUUIDIsTheNameBasedOne(t *testing.T) {
	cfg := config{dir: t.TempDir(), version: "26.3"}
	meta := &vanilla.VersionMeta{MainClass: "M"}
	cmd, err := clientCommand(cfg, meta, "x:1", "Tester", func(string) string { return "" })
	if err != nil {
		t.Fatal(err)
	}
	// uuid.NameSpaceDNS, "OfflinePlayer:Tester", version 3.
	const want = "8272b588-b0a4-382b-ab0c-4a835a0248ad"
	for i, a := range cmd {
		if a == "--uuid" && cmd[i+1] == want {
			return
		}
	}
	t.Errorf("uuid in %v, want %s", cmd, want)
}

func TestWaitForPort(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	exited, exitErr := make(chan struct{}), fmt.Errorf("exit status 1")
	if err := waitForPort(l.Addr().String(), time.Second, exited, &exitErr); err != nil {
		t.Errorf("a listening port: %v", err)
	}
	if err := waitForPort("127.0.0.1:1", 600*time.Millisecond, exited, &exitErr); err == nil || !strings.Contains(err.Error(), "did not come up") {
		t.Errorf("a port nobody listens on: %v", err)
	}
	close(exited)
	if err := waitForPort("127.0.0.1:1", 5*time.Second, exited, &exitErr); err == nil || !strings.Contains(err.Error(), "exited before it was ready: exit status 1") {
		t.Errorf("a server that exited: %v", err)
	}
}

// TestDumpReturnsWhenTheServerExitsEarly runs dump against a java that dies
// at once (a bound port, a missing EULA): dump has to report that, not hang
// waiting on the process it already saw exit.
func TestDumpReturnsWhenTheServerExitsEarly(t *testing.T) {
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "java"), []byte("#!/bin/sh\nexit 3\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	cfg := config{dir: t.TempDir(), version: "1.0", port: 1}
	if err := os.MkdirAll(filepath.Dir(cfg.serverJar()), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfg.serverJar(), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- dump(cfg, io.Discard, io.Discard) }()
	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "exited before it was ready") {
			t.Errorf("dump = %v, want the server's early exit", err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("dump did not return")
	}
}

// TestDumpRefusesAPortSomethingHolds has a listener stand where the server
// would: waitForPort would take it for the server dump starts.
func TestDumpRefusesAPortSomethingHolds(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	cfg := config{dir: t.TempDir(), version: "1.0", port: l.Addr().(*net.TCPAddr).Port}
	if err := os.MkdirAll(filepath.Dir(cfg.serverJar()), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfg.serverJar(), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := dump(cfg, io.Discard, io.Discard); err == nil || !strings.Contains(err.Error(), "already listens") {
		t.Errorf("dump = %v, want a refusal naming the port", err)
	}
}

// TestForwardSignalsPassesTermOnToTheChild signals this process while a child
// sleeps: the child has to get the signal, or it outlives its parent.
func TestForwardSignalsPassesTermOnToTheChild(t *testing.T) {
	cmd := exec.Command("sleep", "30")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	defer close(done)
	forwardSignals(cmd.Process, done) // the handler is in place when this returns
	if err := syscall.Kill(os.Getpid(), syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	waited := make(chan error, 1)
	go func() { waited <- cmd.Wait() }()
	select {
	case err := <-waited:
		if err == nil || !strings.Contains(err.Error(), "terminated") {
			t.Errorf("child exit = %v, want a termination by SIGTERM", err)
		}
	case <-time.After(10 * time.Second):
		_ = cmd.Process.Kill()
		t.Fatal("the child did not get the signal")
	}
}
