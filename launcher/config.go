package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"golang.org/x/mod/modfile"
	"golang.org/x/mod/module"
	"golang.org/x/mod/semver"
)

// config is launcher.json.
type config struct {
	// Server is the server's version: a release tag, a pseudo-version or a
	// commit; or a local golem checkout, relative to launcher.json,
	// which is rebuilt on every start.
	Server string `json:"server"`
	// Plugins are import paths with a version (path@v1.2.3 or path@commit),
	// or local module directories, which are rebuilt on every start.
	Plugins []string `json:"plugins"`
	// AllowDependencyUpgrades lets a plugin raise the version of a module
	// the server itself uses, which is otherwise refused: the server was
	// tested with its own versions.
	AllowDependencyUpgrades bool `json:"allow-dependency-upgrades,omitempty"`
	// UpdateCheck, false, turns off the check on start for newer versions
	// of the server, the plugins and the launcher; unset is on.
	UpdateCheck *bool `json:"updateCheck,omitempty"`
}

// pluginSpec is one entry of Plugins: Dir for a local module, else Path at
// Version.
type pluginSpec struct {
	Path, Version, Dir string
}

func (p pluginSpec) String() string {
	if p.Dir != "" {
		return p.Dir
	}
	return p.Path + "@" + p.Version
}

// commit is an abbreviated or full commit hash.
var commit = regexp.MustCompile(`^[0-9a-f]{7,40}$`)

// isPinned reports whether v names one version for good: an exact semantic
// version (a pseudo-version is one) or a commit, never a branch or "latest".
func isPinned(v string) bool {
	return semver.IsValid(v) && semver.Canonical(v) == strings.TrimSuffix(v, "+incompatible") || commit.MatchString(v)
}

// isLocalPath is whether a "server" or plugin entry names a directory, not a
// version or import path.
func isLocalPath(s string) bool { return strings.HasPrefix(s, ".") || filepath.IsAbs(s) }

// serverDir answers the absolute directory of a local server checkout, or ""
// when Server is a version. base is the directory holding launcher.json.
func (c config) serverDir(base string) (string, error) {
	if !isLocalPath(c.Server) {
		return "", nil
	}
	dir := c.Server
	if !filepath.IsAbs(dir) {
		dir = filepath.Join(base, dir)
	}
	dir, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(filepath.Join(dir, "go.mod"))
	if err != nil || modfile.ModulePath(data) != serverModule {
		return "", fmt.Errorf("server %s: not a %s checkout (no go.mod for that module)", c.Server, serverModule)
	}
	return dir, nil
}

// checkoutRoot answers the nearest directory at or above dir whose go.mod is
// serverModule's, or "".
func checkoutRoot(dir string) string {
	for {
		if data, err := os.ReadFile(filepath.Join(dir, "go.mod")); err == nil && modfile.ModulePath(data) == serverModule {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

// defaultServer answers the "server" for a new launcher.json in dir: the
// checkout it is inside, else this launcher's version, else the latest
// release (latest asks GitHub; a source build has no version of its own).
func defaultServer(dir string, latest func() (string, error)) (string, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	if root := checkoutRoot(abs); root != "" {
		rel, err := filepath.Rel(abs, root)
		if err != nil {
			return "", err
		}
		return filepath.ToSlash(rel), nil
	}
	if v := ownVersion(); v != "" {
		return v, nil
	}
	v, err := latest()
	if err != nil {
		return "", err
	}
	if !isPinned(v) {
		return "", fmt.Errorf("the latest release %q is not a version; put a commit or a local checkout in \"server\"", v)
	}
	return v, nil
}

// loadConfig reads path, writing one that runs the server defaultServer
// picks, with no plugins, when there is none.
func loadConfig(path string, latest func() (string, error)) (config, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		server, err := defaultServer(filepath.Dir(path), latest)
		if err != nil {
			return config{}, fmt.Errorf("%s is missing and no server version could be chosen for it: %w", path, err)
		}
		cfg := config{Server: server, Plugins: []string{}}
		data, _ := json.MarshalIndent(cfg, "", "  ")
		if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
			return config{}, err
		}
		logger.Printf("wrote %s for server %s with no plugins", path, cfg.Server)
		return cfg, nil
	}
	if err != nil {
		return config{}, err
	}
	var cfg config
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&cfg); err != nil {
		return config{}, fmt.Errorf("read %s: %w", path, err)
	}
	if !isLocalPath(cfg.Server) && !isPinned(cfg.Server) {
		return config{}, fmt.Errorf("%s: server %q is not a version: give a release tag (v1.2.3), a pseudo-version, a commit or a local checkout (./path)", path, cfg.Server)
	}
	return cfg, nil
}

// pluginSpecs parses Plugins; a local directory is relative to base, the
// directory holding launcher.json.
func (c config) pluginSpecs(base string) ([]pluginSpec, error) {
	var specs []pluginSpec
	for _, s := range c.Plugins {
		var p pluginSpec
		if isLocalPath(s) {
			dir := s
			if !filepath.IsAbs(dir) {
				dir = filepath.Join(base, dir)
			}
			dir, err := filepath.Abs(dir)
			if err != nil {
				return nil, err
			}
			data, err := os.ReadFile(filepath.Join(dir, "go.mod"))
			if err != nil {
				return nil, fmt.Errorf("plugin %s: not a module directory: %w", s, err)
			}
			p = pluginSpec{Path: modfile.ModulePath(data), Dir: dir}
			if p.Path == "" {
				return nil, fmt.Errorf("plugin %s: go.mod has no module line", s)
			}
		} else {
			path, v, ok := strings.Cut(s, "@")
			if !ok || !isPinned(v) {
				return nil, fmt.Errorf("plugin %q: give an exact version, e.g. %s@v1.2.3 or a commit", s, path)
			}
			if err := module.CheckImportPath(path); err != nil {
				return nil, fmt.Errorf("plugin %q: %w", s, err)
			}
			p = pluginSpec{Path: path, Version: v}
		}
		if p.Path == serverModule {
			return nil, fmt.Errorf("plugin %s: the server itself is not a plugin", s)
		}
		if slices.ContainsFunc(specs, func(q pluginSpec) bool { return q.Path == p.Path }) {
			return nil, fmt.Errorf("plugin %s is listed twice", p.Path)
		}
		specs = append(specs, p)
	}
	return specs, nil
}
