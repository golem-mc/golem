package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
)

// queryModule answers the version go's module query q (a path@version, where
// the version may be latest, a branch or a commit) resolves to. The module
// proxy answers for public modules and git for the private server.
func (l *launcher) queryModule(ctx context.Context, q string) (string, error) {
	gobin, err := l.goCommand(ctx)
	if err != nil {
		return "", err
	}
	g := &goRun{ctx: ctx, bin: gobin, toolchain: "local", dir: os.TempDir()}
	out, err := g.run("list", "-m", "-json", q)
	if err != nil {
		return "", err
	}
	var m struct{ Version string }
	if err := json.Unmarshal([]byte(out), &m); err != nil || m.Version == "" {
		return "", fmt.Errorf("resolve %s: go answered no version", q)
	}
	return m.Version, nil
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

// latestPlugin answers the newest version of the remote plugin path.
func (l *launcher) latestPlugin(ctx context.Context, path string) (string, error) {
	return l.queryModule(ctx, path+"@latest")
}

// pin resolves a version that may be a branch, "latest" or a short query to
// the exact version go names for it.
func (l *launcher) pin(ctx context.Context, path, v string) (string, error) {
	if isPinned(v) {
		return v, nil
	}
	got, err := l.queryModule(ctx, path+"@"+v)
	if err != nil {
		return "", fmt.Errorf("%s@%s: %w", path, v, err)
	}
	if !isPinned(got) {
		return "", errors.New("go resolved " + path + "@" + v + " to " + got + ", which is not a version")
	}
	return got, nil
}
