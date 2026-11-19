package main

import (
	"bytes"
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
	if a, ok := l.queries[q]; ok {
		return a.version, a.err
	}
	v, err := l.askModule(ctx, q)
	if ctx.Err() == nil {
		if l.queries == nil {
			l.queries = map[string]queryAnswer{}
		}
		l.queries[q] = queryAnswer{v, err}
	}
	return v, err
}

func (l *launcher) askModule(ctx context.Context, q string) (string, error) {
	gobin, err := l.goCommand(ctx)
	if err != nil {
		return "", err
	}
	var stderr bytes.Buffer
	g := &goRun{ctx: ctx, bin: gobin, toolchain: "local", dir: os.TempDir(), stderr: &stderr}
	out, err := g.run("list", "-m", "-json", q)
	if err != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return "", fmt.Errorf("%w: %s", err, msg)
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
	var first error
	for mod = path; ; {
		version, err := l.queryModule(ctx, mod+"@"+v)
		if err == nil {
			return mod, version, nil
		}
		if first == nil {
			first = err
		}
		i := strings.LastIndex(mod, "/")
		if i < 0 {
			return "", "", fmt.Errorf("%s@%s: no module provides it: %w", path, v, first)
		}
		mod = mod[:i]
	}
}

// latestServerOverride replaces latestServer's lookup; tests set it.
var latestServerOverride func(ctx context.Context) (string, error)

// latestServer answers the newest release tag of the server, or its newest
// commit's pseudo-version while there is no release.
func (l *launcher) latestServer(ctx context.Context) (string, error) {
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
// that module's newest version.
func (l *launcher) latestPlugin(ctx context.Context, path string) (mod, latest string, err error) {
	return l.pluginModule(ctx, path, "latest")
}

// pin resolves a version that may be a branch, "latest" or a short query to
// the exact version go names for it; path is a module or a package in one.
func (l *launcher) pin(ctx context.Context, path, v string) (string, error) {
	if isPinned(v) {
		return v, nil
	}
	_, got, err := l.pluginModule(ctx, path, v)
	if err != nil {
		return "", err
	}
	if !isPinned(got) {
		return "", errors.New("go resolved " + path + "@" + v + " to " + got + ", which is not a version")
	}
	return got, nil
}
