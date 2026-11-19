package main

import (
	"bytes"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

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
