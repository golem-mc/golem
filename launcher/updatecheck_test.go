package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeClock is a clock the test moves by hand.
type fakeClock struct {
	mu     sync.Mutex
	now    time.Time
	timers []fakeTimer
	armed  chan struct{} // gets one value per After
}

type fakeTimer struct {
	at time.Time
	c  chan time.Time
}

func newFakeClock() *fakeClock {
	return &fakeClock{now: time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC), armed: make(chan struct{}, 16)}
}

func (f *fakeClock) Now() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.now
}

func (f *fakeClock) After(d time.Duration) <-chan time.Time {
	f.mu.Lock()
	c := make(chan time.Time, 1)
	f.timers = append(f.timers, fakeTimer{f.now.Add(d), c})
	f.mu.Unlock()
	f.armed <- struct{}{}
	return c
}

func (f *fakeClock) Advance(d time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.now = f.now.Add(d)
	f.timers = slices.DeleteFunc(f.timers, func(t fakeTimer) bool {
		if t.at.After(f.now) {
			return false
		}
		t.c <- f.now
		return true
	})
}

// fakeReleases answers GitHub's latest-release query with latest; offline
// fails every request and hang blocks it until its context ends.
type fakeReleases struct {
	mu            sync.Mutex
	latest        string
	offline, hang bool
	requests      int
}

func (r *fakeReleases) RoundTrip(req *http.Request) (*http.Response, error) {
	r.mu.Lock()
	r.requests++
	latest, offline, hang := r.latest, r.offline, r.hang
	r.mu.Unlock()
	switch {
	case hang:
		<-req.Context().Done()
		return nil, req.Context().Err()
	case offline:
		return nil, errors.New("network is unreachable")
	case req.URL.Host != "api.github.com" || !strings.HasSuffix(req.URL.Path, "/releases/latest"):
		return &http.Response{StatusCode: http.StatusNotFound, Status: "404 Not Found", Body: io.NopCloser(strings.NewReader("")), Request: req}, nil
	}
	body := `{"tag_name": "` + latest + `"}`
	return &http.Response{StatusCode: http.StatusOK, Status: "200 OK", Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
}

func (r *fakeReleases) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.requests
}

// startFixture is a fixture whose launchers use a fake clock and release
// source, with the given launcher version, and whose cached releases of the
// server let run start without a build; started records the binaries run
// handed over to.
type startFixture struct {
	*fixture
	clock    *fakeClock
	releases *fakeReleases
	mu       sync.Mutex
	started  []string
}

func newStartFixture(t *testing.T, launcherJSON, launcherVersion, latest string) *startFixture {
	f := &startFixture{fixture: newFixture(t, launcherJSON), clock: newFakeClock(), releases: &fakeReleases{latest: latest}}
	testLauncher = func(l *launcher) {
		l.clock = f.clock
		l.http = &http.Client{Transport: f.releases}
	}
	oldVersion := version
	version = launcherVersion
	startServer = func(bin string, _ []string) error {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.started = append(f.started, bin)
		return nil
	}
	t.Cleanup(func() {
		testLauncher, version, startServer = nil, oldVersion, runServer
	})
	for _, v := range []string{"v1.0.0", "v1.5.0"} {
		if err := writeFileAtomic(filepath.Join(f.cache, "releases", v, releaseAsset("golem", v, runtime.GOOS, runtime.GOARCH)), nil, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return f
}

// start runs the launcher's default command and answers what it printed.
func (f *startFixture) start() string {
	f.t.Helper()
	n := len(f.started)
	out := f.must()
	if len(f.started) != n+1 {
		f.t.Fatalf("run did not hand over to the server; printed %q", out)
	}
	return out
}

const (
	serverNotice   = "↑ golem v1.5.0 is out (you run v1.0.0)\n"
	launcherNotice = "↑ golem-launcher v1.5.0 is out (you run v0.1.0): https://github.com/golem-mc/golem/releases/latest\n"
	updateHint     = "  run `golem-launcher update` to update the server and plugins\n"
)

func TestStartNoticesOutdated(t *testing.T) {
	f := newStartFixture(t, `{"server": "v1.0.0", "plugins": []}`, "v0.1.0", "v1.5.0")
	if got, want := f.start(), serverNotice+launcherNotice+updateHint; got != want {
		t.Errorf("start printed:\n%s\nwant:\n%s", got, want)
	}
}

func TestStartNoticesNoneWhenCurrent(t *testing.T) {
	f := newStartFixture(t, `{"server": "v1.5.0", "plugins": []}`, "v1.5.0", "v1.5.0")
	if got := f.start(); got != "" {
		t.Errorf("start with everything current printed %q", got)
	}
	// A build from a checkout has no version of its own to compare.
	version = ""
	f.releases.latest = "v2.0.0"
	f.clock.Advance(versionCacheTTL)
	if got, want := f.start(), "↑ golem v2.0.0 is out (you run v1.5.0)\n"+updateHint; got != want {
		t.Errorf("dev build start printed:\n%s\nwant:\n%s", got, want)
	}
}

func TestStartNoticesSilentOffline(t *testing.T) {
	f := newStartFixture(t, `{"server": "v1.0.0", "plugins": []}`, "v0.1.0", "v1.5.0")
	f.releases.offline = true
	if got := f.start(); got != "" {
		t.Errorf("an offline start printed %q", got)
	}
	if f.releases.count() == 0 {
		t.Error("no lookup was tried")
	}
}

func TestStartNoticesTimeout(t *testing.T) {
	f := newStartFixture(t, `{"server": "v1.0.0", "plugins": []}`, "v0.1.0", "v1.5.0")
	f.releases.hang = true
	type result struct {
		out string
		err error
	}
	done := make(chan result, 1)
	go func() {
		out, _, err := f.run()
		done <- result{out, err}
	}()
	<-f.clock.armed
	f.clock.Advance(updateCheckTimeout)
	select {
	case r := <-done:
		if r.err != nil || r.out != "" {
			t.Errorf("start after the timeout: err %v, printed %q", r.err, r.out)
		}
	case <-time.After(30 * time.Second): // a guard against a hang, not a wait the test counts on
		t.Fatal("the start still waits for the update check after its timeout")
	}
	if len(f.started) != 1 {
		t.Error("run did not hand over to the server")
	}
}

// A check that finished before the timeout is reported even when the build
// holds the start past it.
func TestStartNoticesFinishedBeforeSlowBuild(t *testing.T) {
	f := newStartFixture(t, `{"server": "v1.0.0", "plugins": []}`, "v0.1.0", "v1.5.0")
	finished := make(chan struct{}, 1)
	updateCheckDone = func() { finished <- struct{}{} }
	t.Cleanup(func() { updateCheckDone = func() {} })
	c := &cli{l: newLauncher(f.cache), configPath: f.config, out: &bytes.Buffer{}}
	st, err := c.load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		strings.TrimSuffix(serverNotice, "\n"),
		strings.TrimSuffix(launcherNotice, "\n"),
		strings.TrimSuffix(updateHint, "\n"),
	}
	for range 20 { // select picks at random between ready cases
		notices := c.startUpdateCheck(context.Background(), st)
		<-f.clock.armed
		<-finished
		f.clock.Advance(updateCheckTimeout)
		// Lets the expiry goroutine mark the check expired, so that both
		// answers are ready; the test passes without it but misses a lost one.
		time.Sleep(5 * time.Millisecond)
		if got := notices(); !slices.Equal(got, want) {
			t.Fatalf("notices = %q, want %q", got, want)
		}
	}
}

func TestStartNoticesCache(t *testing.T) {
	f := newStartFixture(t, `{"server": "v1.0.0", "plugins": []}`, "v0.1.0", "v1.5.0")
	want := serverNotice + launcherNotice + updateHint
	if got := f.start(); got != want {
		t.Fatalf("first start printed %q", got)
	}
	n := f.releases.count()
	f.clock.Advance(versionCacheTTL - time.Minute)
	if got := f.start(); got != want {
		t.Errorf("start from the cache printed %q", got)
	}
	if f.releases.count() != n {
		t.Errorf("a start within %v of the last check made %d requests", versionCacheTTL, f.releases.count()-n)
	}
	// outdated always asks, and refreshes the cache for the next start.
	f.releases.latest = "v2.0.0"
	if out := f.must("outdated"); !strings.Contains(out, "v2.0.0") {
		t.Errorf("outdated answered from the cache:\n%s", out)
	}
	n = f.releases.count()
	if got := f.start(); !strings.Contains(got, "golem v2.0.0 is out") || f.releases.count() != n {
		t.Errorf("start after outdated printed %q with %d requests", got, f.releases.count()-n)
	}
	f.clock.Advance(versionCacheTTL)
	f.releases.latest, version = "v1.0.0", "v1.0.0"
	if got := f.start(); got != "" || f.releases.count() == n {
		t.Errorf("start after the cache expired printed %q with %d requests", got, f.releases.count()-n)
	}
}

func TestStartNoticesOptOut(t *testing.T) {
	f := newStartFixture(t, `{"server": "v1.0.0", "plugins": [], "updateCheck": false}`, "v0.1.0", "v1.5.0")
	if got := f.start(); got != "" || f.releases.count() != 0 {
		t.Errorf(`"updateCheck": false: printed %q with %d requests`, got, f.releases.count())
	}
	if err := os.WriteFile(f.config, []byte(`{"server": "v1.0.0", "plugins": []}`), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GOLEM_NO_UPDATE_CHECK", "1")
	if got := f.start(); got != "" || f.releases.count() != 0 {
		t.Errorf("GOLEM_NO_UPDATE_CHECK=1: printed %q with %d requests", got, f.releases.count())
	}
}

// Plugins are checked like the server, from the fake module proxy; local
// entries never are. updateNotices is called directly so no build runs.
func TestUpdateNoticesPluginsAndLocal(t *testing.T) {
	f := newStartFixture(t, `{"server": "./srv", "plugins": ["example.com/shops@v1.0.0", "example.com/warps@v1.0.0", "./mine"]}`, "", "v1.5.0")
	writeMod(t, filepath.Join(f.dir, "srv"), serverModule)
	writeMod(t, filepath.Join(f.dir, "mine"), "example.com/mine")
	c := &cli{l: newLauncher(f.cache), configPath: f.config, out: &bytes.Buffer{}}
	st, err := c.load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"↑ example.com/shops v1.2.0 is out (you run v1.0.0)",
		"↑ example.com/warps v1.1.0 is out (you run v1.0.0)",
		strings.TrimSuffix(updateHint, "\n"),
	}
	if got := c.updateNotices(context.Background(), st, ""); !slices.Equal(got, want) {
		t.Errorf("notices = %q, want %q", got, want)
	}
	if f.releases.count() != 0 {
		t.Errorf("a local server was looked up (%d requests)", f.releases.count())
	}
	// Within the cache's lifetime no lookup reaches the (now unreachable)
	// proxy; after it, a failed lookup is silent.
	t.Setenv("GOPROXY", "off")
	f.clock.Advance(versionCacheTTL - time.Minute)
	if got := c.updateNotices(context.Background(), st, ""); !slices.Equal(got, want) {
		t.Errorf("cached notices = %q, want %q", got, want)
	}
	f.clock.Advance(time.Minute)
	c.l = newLauncher(f.cache) // a new command: no remembered queries
	if got := c.updateNotices(context.Background(), st, ""); len(got) != 0 {
		t.Errorf("notices with the proxy off = %q", got)
	}
}
