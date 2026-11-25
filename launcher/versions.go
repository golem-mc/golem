package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
)

// queryAnswer is one remembered answer of queryModule.
type queryAnswer struct {
	version string
	err     error
}

// queryModule answers the version go's module query q (a path@version, where
// the version may be latest, a branch or a commit) resolves to. The module
// proxy answers for public modules and git for the private server. go's
// complaint is part of the error, not printed: pluginModule expects some
// queries to fail.
func (l *launcher) queryModule(ctx context.Context, q string) (string, error) {
	l.queriesMu.Lock()
	a, ok := l.queries[q]
	l.queriesMu.Unlock()
	if ok {
		return a.version, a.err
	}
	v, err := l.askModule(ctx, q)
	if ctx.Err() == nil {
		l.queriesMu.Lock()
		if l.queries == nil {
			l.queries = map[string]queryAnswer{}
		}
		l.queries[q] = queryAnswer{v, err}
		l.queriesMu.Unlock()
	}
	return v, err
}

func (l *launcher) askModule(ctx context.Context, q string) (string, error) {
	gobin, err := l.goCommand(ctx)
	if err != nil {
		return "", err
	}
	g := &goRun{ctx: ctx, bin: gobin, toolchain: "local", dir: os.TempDir()}
	out, err := g.run("list", "-m", "-json", q)
	if err != nil {
		// Not a goError, so a step does not print the complaint a
		// second time.
		if ge, ok := errors.AsType[*goError](err); ok {
			if msg := strings.TrimSpace(ge.stderr); msg != "" {
				return "", fmt.Errorf("go %s: %w: %s", strings.Join(ge.args, " "), ge.err, msg)
			}
		}
		return "", err
	}
	var m struct{ Version string }
	if err := json.Unmarshal([]byte(out), &m); err != nil || m.Version == "" {
		return "", fmt.Errorf("resolve %s: go answered no version", q)
	}
	return m.Version, nil
}

// pluginModule answers the module that provides package path at the query
// version v, and the version v resolves to: the longest prefix of path that
// is a module at v (go's own choice when modules nest).
func (l *launcher) pluginModule(ctx context.Context, path, v string) (mod, version string, err error) {
	// Every candidate is asked at once: a cold miss costs the proxy a round
	// trip upstream. The longest answer wins, so the rest are cancelled as
	// soon as one longer than them has answered.
	var mods []string
	for m := path; ; m = m[:strings.LastIndex(m, "/")] {
		mods = append(mods, m)
		if !strings.Contains(m, "/") {
			break
		}
	}
	type answer struct {
		version string
		err     error
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	done := make([]chan answer, len(mods))
	for i, m := range mods {
		done[i] = make(chan answer, 1)
		go func() {
			ver, err := l.queryModule(ctx, m+"@"+v)
			done[i] <- answer{ver, err}
		}()
	}
	var first error
	for i, m := range mods {
		a := <-done[i]
		if a.err == nil {
			return m, a.version, nil
		}
		if first == nil {
			first = a.err
		}
	}
	return "", "", fmt.Errorf("%s@%s: no module provides it: %w", path, v, first)
}

func (ch updateChannel) query() string {
	if ch == devChannel {
		return "HEAD"
	}
	return "latest"
}

// latestServerOverride replaces the stable server lookup in tests.
var latestServerOverride func(ctx context.Context) (string, error)

// latestServer answers the newest version of the server on ch.
func (l *launcher) latestServer(ctx context.Context, ch updateChannel) (string, error) {
	if ch == devChannel {
		return l.queryModule(ctx, serverModule+"@"+ch.query())
	}
	if latestServerOverride != nil {
		return latestServerOverride(ctx)
	}
	v, err := l.latestRelease(ctx)
	if err == nil && isPinned(v) {
		return v, nil
	}
	if err != nil && !strings.Contains(err.Error(), "no release yet") {
		return "", err
	}
	return l.queryModule(ctx, serverModule+"@latest")
}

// latestPlugin answers the module of the remote plugin package path and
// that module's newest version on ch.
func (l *launcher) latestPlugin(ctx context.Context, path string, ch updateChannel) (mod, latest string, err error) {
	return l.pluginModule(ctx, path, ch.query())
}

// pin resolves a version that may be a branch, "latest" or a short query to
// the exact version go names for it; path is a module or a package in one.
func (l *launcher) pin(ctx context.Context, path, v string) (string, error) {
	if isPinned(v) {
		return v, nil
	}
	var got string
	err := l.step("resolving "+path+"@"+v, func() (err error) {
		_, got, err = l.pluginModule(ctx, path, v)
		return err
	})
	if err != nil {
		return "", err
	}
	if !isPinned(got) {
		return "", errors.New("go resolved " + path + "@" + v + " to " + got + ", which is not a version")
	}
	return got, nil
}
