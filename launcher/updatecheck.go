package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"
)

const (
	// updateCheckTimeout bounds the check on start; a start never waits
	// longer for it.
	updateCheckTimeout = 2 * time.Second
	// versionCacheTTL is how long a start trusts a cached latest version.
	versionCacheTTL = 24 * time.Hour
	// launcherKey is the launcher's own entry in the version cache; the
	// others are module paths.
	launcherKey = "golem-launcher"
)

// startServer hands over to the server; tests replace it.
var startServer = runServer

// updateCheckDone runs once a start's update check has its answer; tests
// replace it.
var updateCheckDone = func() {}

// clock is the launcher's time; tests drive it.
type clock interface {
	Now() time.Time
	After(d time.Duration) <-chan time.Time
}

type realClock struct{}

func (realClock) Now() time.Time                         { return time.Now() }
func (realClock) After(d time.Duration) <-chan time.Time { return time.After(d) }

// debugf prints a note only when GOLEM_LAUNCHER_DEBUG is set.
func (l *launcher) debugf(format string, args ...any) {
	if os.Getenv("GOLEM_LAUNCHER_DEBUG") != "" {
		l.ui.Info("debug: "+format, args...)
	}
}

// cachedVersion is one entry of the version cache.
type cachedVersion struct {
	Version string    `json:"version"`
	Checked time.Time `json:"checked"`
}

func (l *launcher) versionCachePath() string { return filepath.Join(l.cache, "latest-versions.json") }

// readVersionCache answers the version cache by key, empty when there is
// none or it is unreadable. versionsMu must be held.
func (l *launcher) readVersionCache() map[string]cachedVersion {
	m := map[string]cachedVersion{}
	if data, err := os.ReadFile(l.versionCachePath()); err == nil {
		if err := json.Unmarshal(data, &m); err != nil {
			l.debugf("ignoring %s: %v", l.versionCachePath(), err)
			return map[string]cachedVersion{}
		}
	}
	return m
}

// latest answers key's latest version: from the version cache when cached is
// set and its entry is younger than versionCacheTTL, else from look, whose
// answer is then cached. A failed lookup is never cached.
func (l *launcher) latest(ctx context.Context, key string, cached bool, look func(context.Context) (string, error)) (string, error) {
	now := l.clock.Now()
	if cached {
		l.versionsMu.Lock()
		e, ok := l.readVersionCache()[key]
		l.versionsMu.Unlock()
		if age := now.Sub(e.Checked); ok && age >= 0 && age < versionCacheTTL {
			return e.Version, nil
		}
	}
	v, err := look(ctx)
	if err != nil {
		return "", err
	}
	l.versionsMu.Lock()
	defer l.versionsMu.Unlock()
	m := l.readVersionCache()
	m[key] = cachedVersion{Version: v, Checked: now}
	data, _ := json.MarshalIndent(m, "", "  ")
	if err := writeFileAtomic(l.versionCachePath(), data, 0o644); err != nil {
		l.debugf("write %s: %v", l.versionCachePath(), err)
	}
	return v, nil
}

// latestOf answers the latest version of the server (serverModule) or of the
// remote plugin path, through the version cache.
func (l *launcher) latestOf(ctx context.Context, path string, cached bool) (string, error) {
	if path == serverModule {
		return l.latest(ctx, path, cached, l.latestServer)
	}
	return l.latest(ctx, path, cached, func(ctx context.Context) (string, error) {
		_, v, err := l.latestPlugin(ctx, path)
		return v, err
	})
}

// outdatedItem is the server (path serverModule) or a plugin with a newer
// version than launcher.json pins.
type outdatedItem struct{ path, current, latest string }

// outdatedItems looks up the remote server and plugins of st in parallel and
// answers those with a newer version, in launcher.json's order, and the
// lookups that failed. Local entries are never looked up.
func (c *cli) outdatedItems(ctx context.Context, st state, cached bool) ([]outdatedItem, error) {
	var pinned []outdatedItem
	if st.srvDir == "" {
		pinned = append(pinned, outdatedItem{path: serverModule, current: st.cfg.Server})
	}
	for _, p := range st.plugins {
		if p.Dir == "" {
			pinned = append(pinned, outdatedItem{path: p.Path, current: p.Version})
		}
	}
	errs := make([]error, len(pinned))
	var wg sync.WaitGroup
	for i := range pinned {
		wg.Go(func() { pinned[i].latest, errs[i] = c.l.latestOf(ctx, pinned[i].path, cached) })
	}
	wg.Wait()
	var items []outdatedItem
	for i, it := range pinned {
		if errs[i] == nil && newer(it.current, it.latest) {
			items = append(items, it)
		}
	}
	return items, errors.Join(errs...)
}

// updateCheckOn is whether a start looks for newer versions: not with
// "updateCheck": false or GOLEM_NO_UPDATE_CHECK set true.
func updateCheckOn(cfg config) bool {
	if off, err := strconv.ParseBool(os.Getenv("GOLEM_NO_UPDATE_CHECK")); err == nil && off {
		return false
	}
	return cfg.UpdateCheck == nil || *cfg.UpdateCheck
}

// startUpdateCheck starts looking for newer versions of st's server and
// plugins and of the launcher, and answers a function that waits for the
// notices to print. They are none when the check is off or fails, and when it
// has not finished updateCheckTimeout after it started.
func (c *cli) startUpdateCheck(ctx context.Context, st state) func() []string {
	if !updateCheckOn(st.cfg) {
		return func() []string { return nil }
	}
	expiry := c.l.clock.After(updateCheckTimeout)
	ctx, cancel := context.WithCancel(ctx)
	expired := make(chan struct{})
	go func() {
		select {
		case <-expiry:
			close(expired)
			cancel()
		case <-ctx.Done():
		}
	}()
	// Read before the goroutine: a check cut short outlives the start.
	own, finished := ownVersion(), updateCheckDone
	done := make(chan []string, 1)
	go func() {
		done <- c.updateNotices(ctx, st, own)
		finished()
	}()
	return func() []string {
		defer cancel()
		// An answer that is in wins over an expiry that is also due.
		select {
		case n := <-done:
			return n
		default:
		}
		select {
		case n := <-done:
			return n
		case <-expired:
			c.l.debugf("update check: no answer within %v", updateCheckTimeout)
			return nil
		}
	}
}

// updateNotices answers one line per outdated item, using the version cache,
// with own the launcher's version; none once ctx is done, so a check cut short
// reports nothing.
func (c *cli) updateNotices(ctx context.Context, st state, own string) []string {
	var launcherLatest string
	var wg sync.WaitGroup
	if own != "" { // a build from a checkout has no version to compare
		wg.Go(func() {
			v, err := c.l.latest(ctx, launcherKey, true, c.l.latestRelease)
			if err != nil {
				c.l.debugf("update check: %s: %v", launcherKey, err)
			}
			launcherLatest = v
		})
	}
	items, err := c.outdatedItems(ctx, st, true)
	wg.Wait()
	if err != nil {
		c.l.debugf("update check: %v", err)
	}
	if ctx.Err() != nil {
		return nil
	}
	var lines []string
	for _, it := range items {
		name := it.path
		if name == serverModule {
			name = "golem"
		}
		lines = append(lines, fmt.Sprintf("↑ %s %s is out (you run %s)", name, it.latest, it.current))
	}
	if launcherLatest != "" && newer(own, launcherLatest) {
		lines = append(lines, fmt.Sprintf("↑ %s %s is out (you run %s): https://%s/releases/latest", launcherKey, launcherLatest, own, serverModule))
	}
	if len(items) > 0 {
		lines = append(lines, "  run `golem-launcher update` to update the server and plugins")
	}
	return lines
}
