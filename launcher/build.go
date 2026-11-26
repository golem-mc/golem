package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"time"

	"golang.org/x/mod/modfile"
	"golang.org/x/mod/semver"
	"golang.org/x/sys/cpu"

	"github.com/golem-mc/golem/launcher/ui"
)

// serverPrivate (a variable so tests can serve the server from a fake proxy)
// is whether serverModule's repository is private, as in a private fork. A
// private one is fetched with git, credentials and all, and never asked of the
// public module proxy or checksum database (GOPRIVATE).
var serverPrivate = false

// buildSchema changes when a build of the same inputs would come out
// differently, so older cached builds are not reused.
const buildSchema = "1"

type launcher struct {
	cache string
	http  *http.Client
	ui    *ui.UI
	clock clock
	// versionsMu guards the version cache file (versionCachePath).
	versionsMu sync.Mutex
	// queries holds queryModule's answers, so a command asks go once per
	// module query however many plugins share the module; queriesMu guards
	// it, as the update check asks in parallel.
	queriesMu sync.Mutex
	queries   map[string]queryAnswer
	// toolchainMu serialises toolchain's download: concurrent queries share
	// its zip and unpack directory.
	toolchainMu sync.Mutex
	// headsMu guards github-heads.json and headCalls; zips holds the module
	// downloads a dev update started, and prefetchZips asks for them.
	headsMu     sync.Mutex
	headCalls   map[string]*headCall
	zips        sync.WaitGroup
	prefetchCtx context.Context
	// modCacheOnce finds go's module cache directory once.
	modCacheOnce sync.Once
	modCacheDir  string
	modCacheErr  error
}

// testLauncher, when set, adjusts every new launcher; tests give it a fake
// clock or HTTP transport.
var testLauncher func(*launcher)

func newLauncher(cache string, u *ui.UI) *launcher {
	l := &launcher{cache: cache, http: &http.Client{Timeout: 15 * time.Minute}, ui: u, clock: realClock{}}
	if testLauncher != nil {
		testLauncher(l)
	}
	return l
}

// step runs f as the step name: a spinner while it runs, then ✓, or ✗ and
// the go command's messages when it fails.
func (l *launcher) step(name string, f func() error) error {
	t := l.ui.Step(name)
	if err := f(); err != nil {
		if ge, ok := errors.AsType[*goError](err); ok {
			t.Fail(ge.stderr)
		} else {
			t.Fail("")
		}
		return err
	}
	t.Done()
	return nil
}

// plural answers n and noun, "no plugins", "1 plugin", "3 plugins".
func plural(n int, noun string) string {
	switch n {
	case 0:
		return "no " + noun + "s"
	case 1:
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

// server answers the binary to run: the release when there are no plugins
// and one is published, else a build.
func (l *launcher) server(ctx context.Context, cfg config, srvDir string, plugins []pluginSpec, rebuild bool) (string, error) {
	if len(plugins) == 0 && !rebuild && srvDir == "" {
		bin, err := l.release(ctx, cfg.Server)
		if !errors.Is(err, errNoRelease) {
			return bin, err
		}
		l.ui.Info("no signed release of %s for %s/%s; using a build from source", cfg.Server, runtime.GOOS, runtime.GOARCH)
	}
	return l.build(ctx, cfg, srvDir, plugins, rebuild)
}

// buildKey names the cache directory of a build of cfg's server and plugins.
func buildKey(cfg config, srvDir string, plugins []pluginSpec) string {
	specs := make([]string, len(plugins))
	for i, p := range plugins {
		specs[i] = p.String()
	}
	slices.Sort(specs)
	data, _ := json.Marshal([]any{buildSchema, cfg.Server, srvDir, specs, cfg.AllowDependencyUpgrades, runtime.GOOS, runtime.GOARCH, amd64Level()})
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:8])
}

// build answers a build of the server with plugins, reusing a cached one
// unless the server (srvDir, when local) or a plugin is a local directory,
// whose source can change under the same name (the go build cache keeps that
// rebuild short).
func (l *launcher) build(ctx context.Context, cfg config, srvDir string, plugins []pluginSpec, rebuild bool) (string, error) {
	dir := filepath.Join(l.cache, "builds", buildKey(cfg, srvDir, plugins))
	bin := filepath.Join(dir, exe("golem"))
	local := srvDir != "" || slices.ContainsFunc(plugins, func(p pluginSpec) bool { return p.Dir != "" })
	if _, err := os.Stat(bin); err == nil && !rebuild && !local {
		l.ui.OK("using cached build of golem %s with %s", cfg.Server, plural(len(plugins), "plugin"))
		return bin, nil
	}
	g, err := l.resolve(ctx, filepath.Join(dir, "src"), cfg, srvDir, plugins)
	if err != nil {
		return "", err
	}

	// The release's own flags (tools/release), no cgo, so no C toolchain is
	// needed, and the server's PGO profile; but GOAMD64 is this CPU's level
	// (env), since the binary only runs here.
	tmp := bin + ".tmp"
	err = l.step(fmt.Sprintf("building golem %s with %s", cfg.Server, plural(len(plugins), "plugin")), func() error {
		dirOut, err := g.run("list", "-m", "-f", "{{.Dir}}", serverModule)
		if err != nil {
			return err
		}
		pgo := "-pgo=off"
		if p := filepath.Join(strings.TrimSpace(dirOut), "default.pgo"); fileExists(p) {
			pgo = "-pgo=" + p
		}
		_, err = g.run("build", "-trimpath", pgo, "-o", tmp, ".")
		return err
	})
	if err != nil {
		return "", err
	}
	return bin, os.Rename(tmp, bin)
}

// resolve writes the main module for cfg's server and plugins into workDir,
// resolves its dependencies and runs the checks, answering the go runner
// to build with. Nothing is compiled.
func (l *launcher) resolve(ctx context.Context, workDir string, cfg config, srvDir string, plugins []pluginSpec) (*goRun, error) {
	gobin, err := l.goCommand(ctx)
	if err != nil {
		return nil, err
	}
	g := &goRun{ctx: ctx, bin: gobin, toolchain: "local", noSumDB: cfg.updateChannel() == devChannel && githubHeads}

	// The server's go.mod names the toolchain every later step runs.
	// A local checkout is built as it is, as v0.0.0 (a replace has no
	// version of its own), and needs no network or token.
	srv := modInfo{Version: "v0.0.0", GoMod: filepath.Join(srvDir, "go.mod")}
	if srvDir == "" {
		err := l.step("downloading golem "+cfg.Server, func() (err error) {
			l.waitZips()
			srv, err = g.download(serverModule + "@" + cfg.Server)
			return err
		})
		if err != nil {
			return nil, err
		}
	}
	if g.toolchain, err = toolchainOf(srv.GoMod); err != nil {
		return nil, err
	}
	goLine := strings.TrimPrefix(g.toolchain, "go")

	g.dir = workDir
	if err := writeModule(g.dir, goLine, plugins); err != nil {
		return nil, err
	}
	// The server is required, not fetched with go get: go get of a version
	// would downgrade or drop a plugin needing a newer one, where the check
	// below should name it.
	edit := []string{"mod", "edit", "-require=" + serverModule + "@" + srv.Version}
	if srvDir != "" {
		edit = append(edit, "-replace="+serverModule+"="+srvDir)
	}
	// A local module is replaced once however many of its packages are
	// listed; go get takes package paths and finds their modules itself.
	get := []string{"get"}
	replaced := map[string]bool{}
	for _, p := range plugins {
		if p.Dir != "" {
			if !replaced[p.Module] {
				replaced[p.Module] = true
				edit = append(edit, "-replace="+p.Module+"="+p.ModDir, "-require="+p.Module+"@v0.0.0")
			}
		} else {
			if g.noSumDB {
				// A require names the module, so go does not ask the proxy
				// which shorter path provides the package.
				edit = append(edit, "-require="+githubModule(p.Path)+"@"+p.Version)
				continue
			}
			get = append(get, p.Path+"@"+p.Version)
		}
	}
	resolving := "resolving the server's modules"
	if len(plugins) > 0 {
		resolving = "resolving " + plural(len(plugins), "plugin")
	}
	err = l.step(resolving, func() error {
		l.waitZips()
		if _, err := g.run(edit...); err != nil {
			return err
		}
		if len(get) > 1 {
			if _, err := g.run(get...); err != nil {
				return err
			}
		}
		_, err := g.run("mod", "tidy")
		return err
	})
	if err != nil {
		return nil, err
	}
	if err := l.check(g, cfg, srvDir, srv.Version, goLine); err != nil {
		return nil, err
	}
	return g, nil
}

// writeModule writes a fresh main module into dir: a main package importing
// the plugins, so their init functions register them, then running the
// server.
func writeModule(dir, goLine string, plugins []pluginSpec) error {
	if err := os.RemoveAll(dir); err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	gomod := "module golem-custom\n\ngo " + goLine + "\n"
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(gomod), 0o644); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "main.go"), mainSource(plugins), 0o644)
}

// mainSource is the generated main package.
func mainSource(plugins []pluginSpec) []byte {
	var b bytes.Buffer
	b.WriteString("// Code generated by golem-launcher. DO NOT EDIT.\n\npackage main\n\nimport (\n")
	fmt.Fprintf(&b, "\t%q\n", serverModule+"/app")
	if len(plugins) > 0 {
		b.WriteString("\n")
	}
	paths := make([]string, len(plugins))
	for i, p := range plugins {
		paths[i] = p.Path
	}
	slices.Sort(paths)
	for _, p := range paths {
		fmt.Fprintf(&b, "\t_ %q\n", p)
	}
	b.WriteString(")\n\nfunc main() { app.Main() }\n")
	return b.Bytes()
}

// toolchainOf answers the toolchain a go.mod file asks for: its toolchain
// line, else its go line as a release.
func toolchainOf(gomod string) (string, error) {
	data, err := os.ReadFile(gomod)
	if err != nil {
		return "", err
	}
	f, err := modfile.Parse(gomod, data, nil)
	if err != nil {
		return "", err
	}
	if f.Toolchain != nil && goRelease.MatchString(f.Toolchain.Name) {
		return f.Toolchain.Name, nil
	}
	if f.Go == nil {
		return "", fmt.Errorf("%s has no go line", gomod)
	}
	v := "go" + f.Go.Version
	// "go 1.27" means the first release of 1.27, go1.27.0.
	if strings.Count(v, ".") == 1 {
		v += ".0"
	}
	if !goRelease.MatchString(v) {
		return "", fmt.Errorf("%s asks for Go %s, which is not a release", gomod, f.Go.Version)
	}
	return v, nil
}

// check refuses a build whose plugins would change the server: a newer
// server than launcher.json names, a newer version of a module the server
// uses (unless allowed), or an import of the server's internals.
func (l *launcher) check(g *goRun, cfg config, srvDir, want, goLine string) error {
	err := l.step("checking the server version", func() error {
		got, err := g.run("list", "-m", "-f", "{{.Version}}", serverModule)
		if err != nil {
			return err
		}
		// A local checkout stands in for every version a plugin asks for.
		if got = strings.TrimSpace(got); got != want && srvDir == "" {
			graph, _ := g.run("mod", "graph")
			return fmt.Errorf("launcher.json names server %s, but a plugin needs %s (%s); run `golem-launcher server %s` to raise it, or use an older plugin",
				want, got, strings.Join(requirers(graph, serverModule+"@"+got), ", "), got)
		}
		return nil
	})
	if err != nil {
		return err
	}

	var pkgs string
	err = l.step("checking the plugins' imports", func() (err error) {
		pkgs, err = g.run("list", "-deps", "-f", listFormat, ".")
		return err
	})
	if err != nil {
		return err
	}
	var allowed []string
	err = l.step("checking dependency upgrades", func() error {
		base, err := l.baseline(g, srvDir, want, goLine)
		if err != nil {
			return err
		}
		if up := upgrades(base, pkgs); len(up) > 0 {
			msg := "plugins raise modules the server uses:\n\t" + strings.Join(up, "\n\t")
			if !cfg.AllowDependencyUpgrades {
				return errors.New(msg + "\nset \"allow-dependency-upgrades\": true in launcher.json to build anyway")
			}
			allowed = up
		}
		return nil
	})
	if len(allowed) > 0 {
		l.ui.Warn("plugins raise modules the server uses (allow-dependency-upgrades):\n\t%s", strings.Join(allowed, "\n\t"))
	}
	return err
}

// listFormat prints a package's module, version, path and imports, one
// package a line.
const listFormat = `{{with .Module}}{{.Path}} {{.Version}}{{else}}std -{{end}} {{.ImportPath}}{{range .Imports}} {{.}}{{end}}`

// moduleVersions answers the version of each module listFormat's lines
// name.
func moduleVersions(list string) map[string]string {
	v := map[string]string{}
	for line := range strings.Lines(list) {
		if f := strings.Fields(line); len(f) >= 3 && f[0] != "std" {
			v[f[0]] = f[1]
		}
	}
	return v
}

// upgrades answers the modules the server's own build links that the
// plugin build links at a newer version.
func upgrades(base, plugins string) []string {
	got := moduleVersions(plugins)
	var up []string
	for m, v := range moduleVersions(base) {
		if g, ok := got[m]; ok && m != serverModule && semver.Compare(g, v) > 0 {
			up = append(up, m+" "+v+" => "+g)
		}
	}
	slices.Sort(up)
	return up
}

// baseline answers listFormat's lines for the server built with no plugins,
// kept per server version.
func (l *launcher) baseline(g *goRun, srvDir, version, goLine string) (string, error) {
	cached := filepath.Join(l.cache, "baselines", baselineName(version))
	// A local checkout changes under one name, so its baseline is not kept.
	if data, err := os.ReadFile(cached); err == nil && srvDir == "" {
		return string(data), nil
	}
	b := *g
	b.dir = g.dir + "-baseline"
	defer os.RemoveAll(b.dir)
	if err := writeModule(b.dir, goLine, nil); err != nil {
		return "", err
	}
	edit := []string{"mod", "edit", "-require=" + serverModule + "@" + version}
	if srvDir != "" {
		edit = append(edit, "-replace="+serverModule+"="+srvDir)
	}
	if _, err := b.run(edit...); err != nil {
		return "", err
	}
	if _, err := b.run("mod", "tidy"); err != nil {
		return "", err
	}
	list, err := b.run("list", "-deps", "-f", listFormat, ".")
	if err != nil {
		return "", err
	}
	if srvDir != "" {
		return list, nil
	}
	if err := os.MkdirAll(filepath.Dir(cached), 0o755); err != nil {
		return "", err
	}
	return list, os.WriteFile(cached, []byte(list), 0o644)
}

// baselineName is the file name of a server version's baseline.
func baselineName(version string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(version))
}

// requirers answers the modules go mod graph says require target, other
// than the generated main module.
func requirers(graph, target string) []string {
	var r []string
	sc := bufio.NewScanner(strings.NewReader(graph))
	for sc.Scan() {
		if from, to, ok := strings.Cut(sc.Text(), " "); ok && to == target && from != "golem-custom" {
			r = append(r, from)
		}
	}
	return r
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// goRun runs go commands in dir with the build's fixed environment.
type goRun struct {
	ctx       context.Context
	bin, dir  string
	toolchain string
	// noSumDB exempts golem-mc's modules from the checksum database, which
	// stalls on every new commit; the dev channel sets it.
	noSumDB bool
}

// env is the caller's environment with the settings the build depends on
// replaced. GITHUB_TOKEN, when set, reaches git as an HTTP header for
// github.com only, the way actions/checkout passes its token.
func (g *goRun) env() []string {
	fixed := map[string]string{
		"GOTOOLCHAIN": g.toolchain,
		// The user's flags could change the build; go get and tidy keep
		// go.mod complete, so the default -mod=readonly holds.
		"GOFLAGS":     "",
		"GOWORK":      "off",
		"CGO_ENABLED": "0",
		"GOAMD64":     amd64Level(),
		"GOOS":        runtime.GOOS,
		"GOARCH":      runtime.GOARCH,
		"GOROOT":      "",
		"GOPRIVATE":   os.Getenv("GOPRIVATE"),
	}
	if g.noSumDB {
		fixed["GONOSUMDB"] = strings.Trim(os.Getenv("GONOSUMDB")+","+githubOrg+"/*", ",")
	}
	if serverPrivate {
		fixed["GOPRIVATE"] = strings.Trim(fixed["GOPRIVATE"]+","+serverModule, ",")
	}
	env := slices.DeleteFunc(os.Environ(), func(kv string) bool {
		k, _, _ := strings.Cut(kv, "=")
		_, ok := fixed[k]
		return ok || strings.HasPrefix(k, "GIT_CONFIG_")
	})
	for k, v := range fixed {
		if v != "" {
			env = append(env, k+"="+v)
		}
	}
	if token := os.Getenv("GITHUB_TOKEN"); token != "" {
		basic := base64.StdEncoding.EncodeToString([]byte("x-access-token:" + token))
		env = append(env, "GIT_CONFIG_COUNT=1",
			"GIT_CONFIG_KEY_0=http.https://github.com/.extraheader",
			"GIT_CONFIG_VALUE_0=Authorization: Basic "+basic)
	}
	return env
}

// run runs go with args, answering its standard output. Its standard error
// is kept for the error a failure answers, which the step prints.
func (g *goRun) run(args ...string) (string, error) {
	cmd := exec.CommandContext(g.ctx, g.bin, args...)
	cmd.Dir = g.dir
	cmd.Env = g.env()
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return "", &goError{args: args, err: err, stderr: stderr.String()}
	}
	return string(out), nil
}

// goError is a go command that failed, with what it wrote to standard error.
type goError struct {
	args   []string
	err    error
	stderr string
}

func (e *goError) Error() string { return "go " + strings.Join(e.args, " ") + ": " + e.err.Error() }

func (e *goError) Unwrap() error { return e.err }

// modInfo is go mod download -json's answer.
type modInfo struct {
	Version, GoMod, Dir, Error string
}

// download fetches one module version outside any main module.
func (g *goRun) download(spec string) (modInfo, error) {
	d := *g
	d.dir = os.TempDir()
	out, err := d.run("mod", "download", "-json", spec)
	var m modInfo
	if jerr := json.Unmarshal([]byte(out), &m); jerr == nil && m.Error != "" {
		return m, fmt.Errorf("download %s: %s", spec, m.Error)
	}
	if err != nil {
		return m, err
	}
	return m, json.Unmarshal([]byte(out), &m)
}

// amd64Level is the highest GOAMD64 level this CPU supports, up to v3 ("" off
// amd64). v4's AVX-512 is left out: the compiler gains little from it, and some
// CPUs lower their clock while running it.
func amd64Level() string {
	if runtime.GOARCH != "amd64" {
		return ""
	}
	x := cpu.X86
	v2 := x.HasCX16 && x.HasLAHF && x.HasPOPCNT && x.HasSSE3 && x.HasSSE41 && x.HasSSE42 && x.HasSSSE3
	v3 := v2 && x.HasAVX && x.HasAVX2 && x.HasBMI1 && x.HasBMI2 && x.HasF16C && x.HasFMA && x.HasLZCNT && x.HasMOVBE && x.HasOSXSAVE
	switch {
	case v3:
		return "v3"
	case v2:
		return "v2"
	}
	return "v1"
}
