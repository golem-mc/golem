package main

import (
	"context"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/golem-mc/golem/launcher/ui"
)

var updateGolden = flag.Bool("update", false, "rewrite testdata/*.golden from the output")

// stoppedClock never moves, so every step takes 0.0s.
type stoppedClock struct{}

func (stoppedClock) Now() time.Time                     { return time.Unix(0, 0) }
func (stoppedClock) Every(time.Duration, func()) func() { return func() {} }

// plainGolden runs each command line on f in plain mode and compares what
// it printed with testdata/name.golden; the fixture's directory reads $DIR.
func plainGolden(t *testing.T, f *fixture, name string, commands ...string) {
	t.Helper()
	t.Setenv("NO_COLOR", "")
	t.Setenv("FORCE_COLOR", "")
	uiClock = stoppedClock{}
	t.Cleanup(func() { uiClock = ui.RealClock{} })
	var b strings.Builder
	for _, line := range commands {
		stdout, stderr, err := f.run(strings.Fields(line)...)
		b.WriteString(strings.TrimRight("$ golem-launcher "+line, " ") + "\n")
		if stdout != "" {
			b.WriteString("-- stdout --\n" + stdout)
		}
		if stderr != "" {
			b.WriteString("-- stderr --\n" + stderr)
		}
		if err != nil {
			b.WriteString("-- exit: " + exitOf(err) + " --\n")
		}
		b.WriteString("\n")
	}
	got := strings.ReplaceAll(b.String(), f.dir, "$DIR")
	path := filepath.Join("testdata", name+".golden")
	if *updateGolden {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got != string(want) {
		t.Errorf("output differs from %s (go test -run %s -update rewrites it):\n%s", path, t.Name(), got)
	}
}

func exitOf(err error) string {
	if _, ok := err.(usageError); ok {
		return "2"
	}
	return "1"
}

func TestGoldenPluginList(t *testing.T) {
	f := newFixture(t, `{"server": "v1.5.0", "plugins": ["example.com/shops@v1.0.0", "example.com/shops/v2@v2.0.0", "./my-plugin", "example.com/gone@v1.0.0"]}`)
	f.publish("example.com/shops/v2", "v2.0.0", "", map[string]string{"shops.go": "package shops\n"})
	writeMod(t, filepath.Join(f.dir, "my-plugin"), "example.com/mine")
	plainGolden(t, f, "plugin-list", "plugin list")
}

func TestGoldenOutdatedAndUpdate(t *testing.T) {
	f := newFixture(t, `{"server": "v1.0.0", "plugins": ["example.com/warps@v1.0.0", "example.com/shops@v1.0.0"]}`)
	latestServerOverride = func(context.Context) (string, error) { return "v1.5.0", nil }
	t.Cleanup(func() { latestServerOverride = nil })
	plainGolden(t, f, "outdated-update", "outdated", "update warps", "update", "outdated")
}

func TestGoldenPluginAddRemove(t *testing.T) {
	f := newFixture(t, `{"server": "v1.0.0", "plugins": []}`)
	plainGolden(t, f, "plugin-add-remove", "plugin add example.com/warps", "plugin add example.com/warps@v1.0.0",
		"plugin add example.com/shops@v1.2.0", "plugin remove warps", "plugin remove nothing", "plugin add")
}

func TestGoldenCheck(t *testing.T) {
	f := newFixture(t, `{"server": "v1.0.0", "plugins": ["example.com/warps@v1.1.0"]}`)
	plainGolden(t, f, "check", "check", "server v1.5.0", "check")
}

func TestGoldenCacheClean(t *testing.T) {
	f := newFixture(t, `{"server": "v1.0.0", "plugins": []}`)
	for _, p := range []string{"releases/v0.9.0", "builds/old"} {
		if err := os.MkdirAll(filepath.Join(f.cache, p), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	plainGolden(t, f, "cache-clean", "cache clean", "cache clean -color=sometimes")
}

// TestGoldenRun builds a local checkout and hands over to the server after
// one summary line.
func TestGoldenRun(t *testing.T) {
	f := newFixture(t, `{"server": "./srv", "plugins": ["example.com/shops@v1.2.0"]}`)
	srv := filepath.Join(f.dir, "srv")
	writeMod(t, srv, serverModule)
	if err := os.WriteFile(filepath.Join(srv, "go.mod"), []byte("module "+serverModule+"\n\ngo "+f.goLine+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(srv, "app"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(srv, "app", "app.go"), []byte("package app\n\nfunc Main() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var ran []string
	startServer = func(bin string, args []string) error {
		ran = append([]string{filepath.Base(bin)}, args...)
		return nil
	}
	t.Cleanup(func() { startServer = runServer })
	plainGolden(t, f, "run", "-- --port 25566")
	if strings.Join(ran, " ") != exe("golem")+" --port 25566" {
		t.Errorf("ran %q", ran)
	}
}

// TestColorFlag checks -color=always paints a pipe, and that colour stays
// off without it.
func TestColorFlag(t *testing.T) {
	f := newFixture(t, `{"server": "v1.0.0", "plugins": ["example.com/shops@v1.0.0"]}`)
	latestServerOverride = func(context.Context) (string, error) { return "v1.5.0", nil }
	t.Cleanup(func() { latestServerOverride = nil })
	t.Setenv("NO_COLOR", "")
	t.Setenv("FORCE_COLOR", "")
	out, errw, err := f.run("outdated", "--color=always")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"\x1b[1mNAME\x1b[0m", "\x1b[33mv1.0.0\x1b[0m", "\x1b[32mv1.2.0\x1b[0m"} {
		if !strings.Contains(out, want) {
			t.Errorf("stdout lacks %q:\n%q", want, out)
		}
	}
	if !strings.Contains(errw, "\x1b[32m✓\x1b[0m finding the latest versions") {
		t.Errorf("stderr: %q", errw)
	}
	out, _, _ = f.run("-color=always", "help")
	for _, want := range []string{"\x1b[1mCommands:\x1b[0m", "  \x1b[36mplugin add\x1b[0m     add plugins", "  \x1b[36m-config\x1b[0m file"} {
		if !strings.Contains(out, want) {
			t.Errorf("help lacks %q", want)
		}
	}
	t.Setenv("FORCE_COLOR", "1")
	t.Setenv("NO_COLOR", "1")
	if out, errw, _ := f.run("outdated", "-color=never"); strings.Contains(out+errw, "\x1b[") {
		t.Errorf("-color=never painted: %q %q", out, errw)
	}
	if _, errw, _ := f.run("outdated"); !strings.Contains(errw, "\x1b[") {
		t.Errorf("FORCE_COLOR did not paint: %q", errw)
	}
	for _, args := range [][]string{{"-color=never", "--help"}, {"plugin", "list", "-color=never", "-h"}, {"plugin", "list", "-color=never", "-bogus"}} {
		if out, errw, _ := f.run(args...); strings.Contains(out+errw, "\x1b[") {
			t.Errorf("%q painted: %q %q", args, out, errw)
		}
	}
}

// TestGoldenRunOutdated starts with a newer server and plugin out: the
// notices print after the steps, before the summary.
func TestGoldenRunOutdated(t *testing.T) {
	f := newStartFixture(t, `{"server": "v1.0.0", "plugins": ["example.com/shops@v1.0.0"]}`, "v1.5.0", "v1.5.0")
	plainGolden(t, f.fixture, "run-outdated", "")
	if len(f.started) != 1 {
		t.Error("run did not hand over to the server")
	}
}
