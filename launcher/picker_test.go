package main

import (
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/golem-mc/golem/launcher/ui"
)

func TestPluginAddSeveral(t *testing.T) {
	f := newFixture(t, `{"server": "v1.5.0", "plugins": []}`)
	f.must("plugin", "add", "example.com/warps", "example.com/shops@v1.0.0")
	want := []string{"example.com/warps@v1.1.0", "example.com/shops@v1.0.0"}
	if got := f.loadCfg().Plugins; !slices.Equal(got, want) {
		t.Fatalf("plugins = %q, want %q", got, want)
	}
}

func TestPluginAddSeveralFailsWhole(t *testing.T) {
	f := newFixture(t, `{"server": "v1.5.0", "plugins": []}`)
	_, _, err := f.run("plugin", "add", "example.com/warps", "example.com/nothing")
	if err == nil || !strings.Contains(err.Error(), "example.com/nothing") {
		t.Fatalf("err = %v, want one naming example.com/nothing", err)
	}
	if got := f.loadCfg().Plugins; len(got) != 0 {
		t.Errorf("launcher.json changed: %q", got)
	}
}

func TestEmbeddedCatalog(t *testing.T) {
	c, err := parseCatalog(embeddedCatalog)
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Plugins) != 8 {
		t.Errorf("%d plugins, want 8", len(c.Plugins))
	}
	if _, err := parseCatalog([]byte(`{"plugins":[{"name":"A","path":"a","requires":["b"]}]}`)); err == nil {
		t.Error("a requirement outside the catalog was accepted")
	}
}

func TestCatalogFallsBackToEmbedded(t *testing.T) {
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "no", http.StatusInternalServerError)
	}))
	defer bad.Close()
	old := catalogURL
	defer func() { catalogURL = old }()
	catalogURL = bad.URL
	if c := loadCatalog(t.Context()); len(c.Plugins) != 8 {
		t.Errorf("fallback catalog has %d plugins", len(c.Plugins))
	}
	good := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"plugins":[{"name":"Only","description":"d","path":"example.com/only"}]}`))
	}))
	defer good.Close()
	catalogURL = good.URL
	if c := loadCatalog(t.Context()); len(c.Plugins) != 1 || c.Plugins[0].Name != "Only" {
		t.Errorf("fetched catalog = %+v", c)
	}
}

// press feeds raw terminal input to p.
func press(p *picker, input string) {
	keys, _ := parseKeys([]byte(input))
	for _, k := range keys {
		if p.done {
			return
		}
		p.key(k)
	}
}

func testPicker(t *testing.T, installed ...string) *picker {
	t.Helper()
	c, err := parseCatalog(embeddedCatalog)
	if err != nil {
		t.Fatal(err)
	}
	return newPicker(c, installed)
}

const (
	pEss   = "github.com/golem-mc/essentials"
	pWG    = "github.com/golem-mc/worldguard/worldguardplugin"
	pWE    = "github.com/golem-mc/worldedit"
	pSpark = "github.com/golem-mc/spark"
)

func TestPickerSelect(t *testing.T) {
	p := testPicker(t)
	press(p, " jj \r") // tick Essentials, down to antibuild and chat, tick chat
	want := []string{pEss, "github.com/golem-mc/essentials/chat"}
	if !p.done || p.cancelled || !slices.Equal(p.selection(), want) {
		t.Errorf("done=%v cancelled=%v selection=%q, want %q", p.done, p.cancelled, p.selection(), want)
	}
}

func TestPickerRequires(t *testing.T) {
	p := testPicker(t)
	p.cursor = slices.IndexFunc(p.rows, func(r pickRow) bool { return r.path == pWG })
	press(p, " ")
	if got := p.selection(); !slices.Equal(got, []string{pWE, pWG}) || p.neededBy[pWE] != "WorldGuard" {
		t.Fatalf("selection = %q neededBy = %v", got, p.neededBy)
	}
	// WorldEdit cannot be unticked while WorldGuard needs it.
	p.cursor = slices.IndexFunc(p.rows, func(r pickRow) bool { return r.path == pWE })
	press(p, " ")
	if !p.ticked[pWE] || p.msg == "" {
		t.Errorf("WorldEdit untick: ticked=%v msg=%q", p.ticked[pWE], p.msg)
	}
	// Unticking WorldGuard lets its needed plugin go too.
	p.cursor = slices.IndexFunc(p.rows, func(r pickRow) bool { return r.path == pWG })
	press(p, " ")
	if got := p.selection(); len(got) != 0 {
		t.Errorf("selection after untick = %q", got)
	}
}

func TestPickerInstalled(t *testing.T) {
	p := testPicker(t, pSpark, pWE)
	p.cursor = slices.IndexFunc(p.rows, func(r pickRow) bool { return r.path == pSpark })
	press(p, " ")
	if len(p.selection()) != 0 {
		t.Errorf("an installed plugin was ticked: %q", p.selection())
	}
	// An installed requirement is not ticked again.
	p.cursor = slices.IndexFunc(p.rows, func(r pickRow) bool { return r.path == pWG })
	press(p, " ")
	if got := p.selection(); !slices.Equal(got, []string{pWG}) {
		t.Errorf("selection = %q", got)
	}
	view := strings.Join(p.view(ui.Style{}), "\n")
	if !strings.Contains(view, "[✓] Spark") || !strings.Contains(view, "installed") {
		t.Errorf("view lacks the installed entry:\n%s", view)
	}
}

func TestPickerCancel(t *testing.T) {
	for _, in := range []string{" q", " \x1b", " \x03"} {
		p := testPicker(t)
		press(p, in)
		if !p.cancelled {
			t.Errorf("%q did not cancel", in)
		}
	}
	p := testPicker(t)
	press(p, "\x1b[B\x1b[B\x1b[A \r") // down twice, up once: antibuild
	want := []string{"github.com/golem-mc/essentials/antibuild"}
	if got := p.selection(); !slices.Equal(got, want) || !p.done || p.cancelled {
		t.Errorf("arrow keys: selection = %q, want %q", got, want)
	}
}
