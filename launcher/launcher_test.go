package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"golang.org/x/mod/module"
	modzip "golang.org/x/mod/zip"
)

func TestIsPinned(t *testing.T) {
	for v, want := range map[string]bool{
		"v1.2.3": true, "v0.0.0-20261115120000-abcdefabcdef": true, "v1.2.3-rc.1": true,
		"5f70a91e": true, "v2.0.0+incompatible": true,
		"v1": false, "v1.2": false, "latest": false, "main": false, "": false, "upgrade": false,
	} {
		if got := isPinned(v); got != want {
			t.Errorf("isPinned(%q) = %v, want %v", v, got, want)
		}
	}
}

func TestPluginSpecs(t *testing.T) {
	base := t.TempDir()
	local := filepath.Join(base, "warps")
	if err := os.MkdirAll(local, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(local, "go.mod"), []byte("module example.com/warps\n\ngo 1.27\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	specs, err := config{Plugins: []string{"github.com/a/b@v1.2.0", "./warps"}}.pluginSpecs(base)
	if err != nil {
		t.Fatal(err)
	}
	want := []pluginSpec{{Path: "github.com/a/b", Version: "v1.2.0"}, {Path: "example.com/warps", Dir: local}}
	if !slices.Equal(specs, want) {
		t.Fatalf("specs = %v, want %v", specs, want)
	}
	for _, bad := range [][]string{
		{"github.com/a/b"}, {"github.com/a/b@latest"}, {"github.com/a/b@main"},
		{"./missing"}, {serverModule + "@v1.0.0"}, {"github.com/a/b@v1.0.0", "github.com/a/b@v1.1.0"},
	} {
		if _, err := (config{Plugins: bad}).pluginSpecs(base); err == nil {
			t.Errorf("pluginSpecs(%q) took it", bad)
		}
	}
}

func TestVerifyRelease(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	key := base64.StdEncoding.EncodeToString(pub)
	bin := []byte("server binary")
	h := sha256.Sum256(bin)
	sums := []byte(hex.EncodeToString(h[:]) + "  golem_v1.0.0_linux_amd64\n")
	sig := []byte(base64.StdEncoding.EncodeToString(ed25519.Sign(priv, sums)) + "\n")
	if err := verifyRelease(key, sums, sig, "golem_v1.0.0_linux_amd64", bin); err != nil {
		t.Fatalf("a good release: %v", err)
	}
	if err := verifyRelease(key, sums, sig, "golem_v1.0.0_linux_amd64", []byte("tampered")); err == nil {
		t.Error("a tampered binary passed")
	}
	if err := verifyRelease(key, append([]byte("x"), sums...), sig, "golem_v1.0.0_linux_amd64", bin); err == nil {
		t.Error("tampered sums passed")
	}
	if err := verifyRelease(key, sums, sig, "golem_v1.0.0_darwin_arm64", bin); err == nil {
		t.Error("an unlisted file passed")
	}
}

// TestPluginCannotImportInternal checks that Go itself, not the launcher,
// keeps a plugin out of the server's internal packages.
func TestPluginCannotImportInternal(t *testing.T) {
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	gomod, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	sum, err := os.ReadFile(filepath.Join(root, "go.sum"))
	if err != nil {
		t.Fatal(err)
	}
	// The server's own go.mod, renamed, so every dependency of the
	// internal package resolves offline from the module cache.
	mod := strings.Replace(string(gomod), "module "+serverModule, "module example.com/cheat", 1) +
		"\nrequire " + serverModule + " v0.0.0\nreplace " + serverModule + " => " + root + "\n"
	dir := t.TempDir()
	for name, content := range map[string]string{
		"go.mod":  mod,
		"go.sum":  string(sum),
		"main.go": "package main\n\nimport _ \"" + serverModule + "/internal/server\"\n\nfunc main() {}\n",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command("go", "build", "-o", os.DevNull, ".")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOFLAGS=-mod=mod", "GOPROXY=off")
	out, err := cmd.CombinedOutput()
	if err == nil || !strings.Contains(string(out), "use of internal package "+serverModule+"/internal/server not allowed") {
		t.Fatalf("go build = %v, want the internal package error:\n%s", err, out)
	}
}

func TestUpgrades(t *testing.T) {
	base := "github.com/google/uuid v1.6.0 github.com/google/uuid\ngolang.org/x/sys v0.50.0 golang.org/x/sys/unix\n" +
		serverModule + " v1.0.0 " + serverModule + "/app\n"
	plugins := "github.com/google/uuid v1.7.0 github.com/google/uuid\ngolang.org/x/sys v0.50.0 golang.org/x/sys/unix\n" +
		"example.com/warps v1.0.0 example.com/warps\n"
	want := []string{"github.com/google/uuid v1.6.0 => v1.7.0"}
	if got := upgrades(base, plugins); !slices.Equal(got, want) {
		t.Fatalf("upgrades = %q, want %q", got, want)
	}
}

func TestToolchainOf(t *testing.T) {
	dir := t.TempDir()
	for mod, want := range map[string]string{
		"module m\n\ngo 1.27.4\n":                       "go1.27.4",
		"module m\n\ngo 1.27\n":                         "go1.27.0",
		"module m\n\ngo 1.27.0\n\ntoolchain go1.27.6\n": "go1.27.6",
	} {
		path := filepath.Join(dir, "go.mod")
		if err := os.WriteFile(path, []byte(mod), 0o644); err != nil {
			t.Fatal(err)
		}
		if got, err := toolchainOf(path); err != nil || got != want {
			t.Errorf("toolchainOf(%q) = %q, %v, want %q", mod, got, err, want)
		}
	}
}

func TestMainSourceImportsPluginsSorted(t *testing.T) {
	got := string(mainSource([]pluginSpec{{Path: "example.com/z"}, {Path: "example.com/a"}}))
	if a, z := strings.Index(got, `_ "example.com/a"`), strings.Index(got, `_ "example.com/z"`); a < 0 || z < a {
		t.Fatalf("plugins not imported in order:\n%s", got)
	}
	if !strings.Contains(got, "app.Main()") {
		t.Fatalf("main does not run the server:\n%s", got)
	}
}

func writeMod(t *testing.T, dir, module string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module "+module+"\n\ngo 1.27\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func noLatest() (string, error) { return "", errors.New("there is no release yet") }

func TestServerDir(t *testing.T) {
	base := t.TempDir()
	checkout := filepath.Join(base, "checkout")
	writeMod(t, checkout, serverModule)
	writeMod(t, filepath.Join(base, "other"), "example.com/other")
	for server, want := range map[string]string{"./checkout": checkout, checkout: checkout, "v1.2.3": ""} {
		if got, err := (config{Server: server}).serverDir(base); err != nil || got != want {
			t.Errorf("serverDir(%q) = %q, %v, want %q", server, got, err, want)
		}
	}
	for _, bad := range []string{"./missing", "./other"} {
		if _, err := (config{Server: bad}).serverDir(base); err == nil {
			t.Errorf("serverDir(%q) took it", bad)
		}
	}
}

func TestLoadConfigAcceptsLocalServer(t *testing.T) {
	path := filepath.Join(t.TempDir(), "launcher.json")
	if err := os.WriteFile(path, []byte(`{"server": "../mc", "plugins": []}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if cfg, err := loadConfig(path, noLatest); err != nil || cfg.Server != "../mc" {
		t.Fatalf("loadConfig = %v, %v", cfg, err)
	}
	if err := os.WriteFile(path, []byte(`{"server": "main"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := loadConfig(path, noLatest); err == nil {
		t.Fatal("a branch name was taken")
	}
}

func TestDefaultServerInsideCheckout(t *testing.T) {
	root := t.TempDir()
	writeMod(t, root, serverModule)
	deep := filepath.Join(root, "run", "dev")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}
	for dir, want := range map[string]string{deep: "../..", root: "."} {
		path := filepath.Join(dir, "launcher.json")
		cfg, err := loadConfig(path, noLatest)
		if err != nil || cfg.Server != want {
			t.Fatalf("in %s: server = %q, %v, want %q", dir, cfg.Server, err, want)
		}
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("launcher.json not written: %v", err)
		}
		if _, err := cfg.serverDir(dir); err != nil {
			t.Errorf("the written path %q does not resolve: %v", cfg.Server, err)
		}
	}
	// A module that is not the server's does not make a checkout.
	other := t.TempDir()
	writeMod(t, other, "example.com/other")
	if got := checkoutRoot(other); got != "" {
		t.Errorf("checkoutRoot = %q for another module", got)
	}
}

// fixture is a launcher.json in a temp directory, a fake module proxy and a
// local server checkout; no test here touches the network.
type fixture struct {
	t      *testing.T
	dir    string
	config string
	cache  string
	proxy  string
	goLine string
}

// newFixture serves, from a file:// proxy: the server at v1.0.0 and v1.5.0,
// example.com/warps v1.0.0 (needs the server v1.0.0) and v1.1.0 (needs
// v1.5.0), and example.com/shops v1.0.0 and v1.2.0.
func newFixture(t *testing.T, launcherJSON string) *fixture {
	t.Helper()
	out, err := exec.Command("go", "env", "GOVERSION").Output()
	if err != nil {
		t.Skip("no go command")
	}
	f := &fixture{t: t, dir: t.TempDir(), goLine: strings.TrimPrefix(strings.TrimSpace(string(out)), "go")}
	f.config = filepath.Join(f.dir, "launcher.json")
	f.cache = filepath.Join(f.dir, "cache")
	f.proxy = filepath.Join(f.dir, "proxy")
	t.Setenv("GOPROXY", "file://"+f.proxy)
	t.Setenv("GOSUMDB", "off")
	t.Setenv("GOMODCACHE", filepath.Join(f.dir, "modcache"))
	t.Setenv("GOPRIVATE", "")
	// go makes the module cache read-only; the temp directory must be
	// removable.
	t.Cleanup(func() {
		filepath.WalkDir(f.dir, func(p string, d os.DirEntry, err error) error {
			if err == nil && d.IsDir() {
				os.Chmod(p, 0o755)
			}
			return nil
		})
	})
	old := serverPrivate
	serverPrivate = false
	t.Cleanup(func() { serverPrivate = old })

	for _, v := range []string{"v1.0.0", "v1.5.0"} {
		f.publish(serverModule, v, "", map[string]string{"app/app.go": "package app\n\nfunc Main() {}\n"})
	}
	f.publish("example.com/warps", "v1.0.0", serverModule+" v1.0.0", map[string]string{"warps.go": "package warps\n"})
	f.publish("example.com/warps", "v1.1.0", serverModule+" v1.5.0", map[string]string{"warps.go": "package warps\n"})
	f.publish("example.com/shops", "v1.0.0", "", map[string]string{"shops.go": "package shops\n"})
	f.publish("example.com/shops", "v1.2.0", "", map[string]string{"shops.go": "package shops\n"})
	if launcherJSON != "" {
		if err := os.WriteFile(f.config, []byte(launcherJSON), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return f
}

// publish adds one module version to the fake proxy; require is a require
// directive's body or "".
func (f *fixture) publish(path, version, require string, files map[string]string) {
	f.t.Helper()
	src := filepath.Join(f.dir, "src", path+"@"+version)
	gomod := "module " + path + "\n\ngo " + f.goLine + "\n"
	if require != "" {
		gomod += "\nrequire " + require + "\n"
	}
	files["go.mod"] = gomod
	for name, content := range files {
		p := filepath.Join(src, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			f.t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			f.t.Fatal(err)
		}
	}
	v := filepath.Join(f.proxy, path, "@v")
	if err := os.MkdirAll(v, 0o755); err != nil {
		f.t.Fatal(err)
	}
	var zipped bytes.Buffer
	if err := modzip.CreateFromDir(&zipped, module.Version{Path: path, Version: version}, src); err != nil {
		f.t.Fatal(err)
	}
	list, _ := os.ReadFile(filepath.Join(v, "list"))
	for name, content := range map[string][]byte{
		version + ".info": []byte(fmt.Sprintf(`{"Version":%q,"Time":"2026-01-01T00:00:00Z"}`, version)),
		version + ".mod":  []byte(gomod),
		version + ".zip":  zipped.Bytes(),
		"list":            append(list, version+"\n"...),
	} {
		if err := os.WriteFile(filepath.Join(v, name), content, 0o644); err != nil {
			f.t.Fatal(err)
		}
	}
}

// run runs the launcher with args and the fixture's -config and -cache.
func (f *fixture) run(args ...string) (stdout, stderr string, err error) {
	var out, errw bytes.Buffer
	args = append([]string{"-config", f.config, "-cache", f.cache}, args...)
	err = execute(context.Background(), args, &out, &errw, func() {})
	return out.String(), errw.String(), err
}

func (f *fixture) must(args ...string) string {
	f.t.Helper()
	out, errw, err := f.run(args...)
	if err != nil {
		f.t.Fatalf("%v: %v\n%s", args, err, errw)
	}
	return out
}

func (f *fixture) loadCfg() config {
	f.t.Helper()
	data, err := os.ReadFile(f.config)
	if err != nil {
		f.t.Fatal(err)
	}
	var cfg config
	if err := json.Unmarshal(data, &cfg); err != nil {
		f.t.Fatal(err)
	}
	return cfg
}

func TestHelpTopLevel(t *testing.T) {
	var out, errw bytes.Buffer
	for _, args := range [][]string{{"--help"}, {"help"}} {
		out.Reset()
		if err := execute(context.Background(), args, &out, &errw, func() {}); err != nil {
			t.Fatalf("%v: %v", args, err)
		}
		if got := out.String(); got != wantTopHelp {
			t.Errorf("%v printed:\n%s\nwant:\n%s", args, got, wantTopHelp)
		}
	}
	if errw.Len() != 0 {
		t.Errorf("help wrote to stderr: %s", errw.String())
	}
}

func TestUnknownCommandAndFlagExitTwo(t *testing.T) {
	for _, args := range [][]string{{"bogus"}, {"plugin", "bogus"}, {"-nope"}, {"plugin", "list", "-nope"}, {"plugin", "add"}, {"check", "extra"}} {
		var out, errw bytes.Buffer
		err := execute(context.Background(), args, &out, &errw, func() {})
		if _, ok := errors.AsType[usageError](err); !ok {
			t.Errorf("%v: err = %v, want a usage error", args, err)
		}
		if out.Len() != 0 || !strings.Contains(errw.String(), "golem-launcher") {
			t.Errorf("%v: stdout %q, stderr %q", args, out.String(), errw.String())
		}
	}
}

func TestPluginAddPinsLatestAndRemove(t *testing.T) {
	f := newFixture(t, `{"server": "v1.5.0", "plugins": ["example.com/shops@v1.0.0"]}`)
	out := f.must("plugin", "add", "example.com/warps")
	if !strings.Contains(out, "added example.com/warps@v1.1.0") {
		t.Errorf("diff line = %q", out)
	}
	want := []string{"example.com/shops@v1.0.0", "example.com/warps@v1.1.0"}
	if got := f.loadCfg().Plugins; !slices.Equal(got, want) {
		t.Fatalf("plugins = %q, want %q", got, want)
	}
	f.must("plugin", "remove", "shops")
	if got := f.loadCfg().Plugins; !slices.Equal(got, want[1:]) {
		t.Fatalf("plugins after remove = %q", got)
	}
	if _, _, err := f.run("plugin", "remove", "nothing"); err == nil {
		t.Error("removing an unlisted plugin succeeded")
	}
}

func TestPluginAddRefusedNeedingNewerServer(t *testing.T) {
	f := newFixture(t, `{"server": "v1.0.0", "plugins": []}`)
	_, errw, err := f.run("plugin", "add", "example.com/warps")
	if err == nil {
		t.Fatal("a plugin needing server v1.5.0 was added to a server v1.0.0")
	}
	for _, want := range []string{"v1.0.0", "v1.5.0", "golem-launcher server v1.5.0"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not name %q (stderr %q)", err, want, errw)
		}
	}
	if got := f.loadCfg(); got.Server != "v1.0.0" || len(got.Plugins) != 0 {
		t.Errorf("launcher.json changed: %+v", got)
	}
	// An older release of it fits.
	f.must("plugin", "add", "example.com/warps@v1.0.0")
	if got := f.loadCfg().Plugins; !slices.Equal(got, []string{"example.com/warps@v1.0.0"}) {
		t.Errorf("plugins = %q", got)
	}
}

func TestPluginList(t *testing.T) {
	f := newFixture(t, `{"server": "v1.5.0", "plugins": ["example.com/shops@v1.0.0"]}`)
	out := f.must("plugin", "list")
	for _, want := range []string{"example.com/shops", "v1.0.0", "remote", "v1.2.0"} {
		if !strings.Contains(out, want) {
			t.Errorf("plugin list lacks %q:\n%s", want, out)
		}
	}
}

func TestServerShowAndSet(t *testing.T) {
	f := newFixture(t, `{"server": "v1.0.0", "plugins": ["example.com/warps@v1.0.0"]}`)
	if out := f.must("server"); strings.TrimSpace(out) != "v1.0.0" {
		t.Errorf("server = %q", out)
	}
	f.must("server", "v1.5.0")
	if got := f.loadCfg().Server; got != "v1.5.0" {
		t.Errorf("server = %q after set", got)
	}
	if _, _, err := f.run("server", "v9.9.9"); err == nil {
		t.Error("an unpublished server version was accepted")
	}
	if got := f.loadCfg().Server; got != "v1.5.0" {
		t.Errorf("a refused server version was written: %q", got)
	}
}

func TestOutdatedAndUpdate(t *testing.T) {
	f := newFixture(t, `{"server": "v1.0.0", "plugins": ["example.com/warps@v1.0.0", "example.com/shops@v1.0.0"]}`)
	latestServerOverride = func(context.Context) (string, error) { return "v1.5.0", nil }
	t.Cleanup(func() { latestServerOverride = nil })
	out := f.must("outdated")
	for _, want := range []string{`server\s+v1.0.0\s+v1.5.0`, `example.com/warps\s+v1.0.0\s+v1.1.0`, `example.com/shops\s+v1.0.0\s+v1.2.0`} {
		if !regexp.MustCompile(want).MatchString(out) {
			t.Errorf("outdated lacks %q:\n%s", want, out)
		}
	}
	// Updating one plugin without the server it needs is refused.
	if _, _, err := f.run("update", "warps"); err == nil || !strings.Contains(err.Error(), "v1.5.0") {
		t.Errorf("update warps = %v, want a refusal naming v1.5.0", err)
	}
	if got := f.loadCfg().Plugins[0]; got != "example.com/warps@v1.0.0" {
		t.Errorf("a refused update wrote %q", got)
	}
	f.must("update")
	got := f.loadCfg()
	if got.Server != "v1.5.0" || !slices.Equal(got.Plugins, []string{"example.com/warps@v1.1.0", "example.com/shops@v1.2.0"}) {
		t.Errorf("after update: %+v", got)
	}
	if out := f.must("outdated"); !strings.Contains(out, "up to date") {
		t.Errorf("outdated after update = %q", out)
	}
}

func TestCheckReportsWhatFails(t *testing.T) {
	f := newFixture(t, `{"server": "v1.0.0", "plugins": ["example.com/warps@v1.1.0"]}`)
	_, _, err := f.run("check")
	if err == nil || !strings.Contains(err.Error(), "v1.5.0") {
		t.Fatalf("check = %v, want the server version it needs", err)
	}
	if err := os.WriteFile(f.config, []byte(`{"server": "v1.5.0", "plugins": ["example.com/warps@v1.1.0"]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if out := f.must("check"); !strings.HasPrefix(out, "ok") {
		t.Errorf("check = %q", out)
	}
}

func TestBuildPrintsPath(t *testing.T) {
	f := newFixture(t, "")
	srv := filepath.Join(f.dir, "srv")
	writeMod(t, srv, serverModule)
	if err := os.MkdirAll(filepath.Join(srv, "app"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(srv, "go.mod"), []byte("module "+serverModule+"\n\ngo "+f.goLine+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(srv, "app", "app.go"), []byte("package app\n\nfunc Main() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(f.config, []byte(`{"server": "./srv", "plugins": []}`), 0o644); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(f.dir, "out", "server-bin")
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		t.Fatal(err)
	}
	out := f.must("build", "-o", dest)
	if strings.TrimSpace(out) != dest {
		t.Errorf("build printed %q, want %q", out, dest)
	}
	if _, err := os.Stat(dest); err != nil {
		t.Error(err)
	}
}

func TestCacheClean(t *testing.T) {
	f := newFixture(t, `{"server": "v1.0.0", "plugins": []}`)
	for _, p := range []string{"releases/v0.9.0", "releases/v1.0.0", "builds/old", "toolchains/go1.0.0.x", "baselines/old"} {
		if err := os.MkdirAll(filepath.Join(f.cache, p), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	keep := filepath.Join(f.cache, "builds", buildKey(config{Server: "v1.0.0"}, "", nil))
	if err := os.MkdirAll(keep, 0o755); err != nil {
		t.Fatal(err)
	}
	f.must("cache", "clean")
	for _, gone := range []string{"releases/v0.9.0", "builds/old", "toolchains/go1.0.0.x", "baselines/old"} {
		if fileExists(filepath.Join(f.cache, gone)) {
			t.Errorf("%s survived", gone)
		}
	}
	for _, kept := range []string{filepath.Join(f.cache, "releases/v1.0.0"), keep} {
		if !fileExists(kept) {
			t.Errorf("%s was deleted", kept)
		}
	}
}

const wantTopHelp = `golem-launcher builds and runs a golem server with the plugins
that launcher.json lists.

Usage:
  golem-launcher [flags] [run] [-- server options]
  golem-launcher [flags] <command> [arguments]
  golem-launcher help <command>

Commands:
  run            build if needed, then run the server (the default)
  build          build only and print the binary's path (for CI or a Docker image)
  plugin add     add a plugin, pinned (no version: the latest release tag)
  plugin remove  remove a plugin
  plugin list    list plugins: pinned version, local or remote, latest available
  server         show the server version, or set it
  outdated       show newer versions of the server and each plugin
  update         move the server and plugins to their latest versions, pinned
  check          resolve and run every build check without building
  cache clean    delete builds, toolchains and releases launcher.json does not need

Flags (before or after the command):
  -config file   the launcher's configuration (default launcher.json)
  -cache dir     where toolchains, releases and builds are kept
                 (default: the user cache directory + /golem)
  -rebuild       build the server even when a cached build or a release matches

launcher.json, pinned to exact versions (the commands above edit it):
  {
    "server": "v0.1.0",
    "plugins": [
      "github.com/golem-mc/essentials@v1.4.0",
      "./my-plugin"
    ]
  }

Server options follow --; see them with: golem-launcher -- --help
Plugins and the launcher.json format: docs/plugins.md
`
