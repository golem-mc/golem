package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"sync"

	"golang.org/x/mod/module"
	"golang.org/x/mod/semver"
	"golang.org/x/mod/sumdb"
	"golang.org/x/mod/sumdb/dirhash"
	modzip "golang.org/x/mod/zip"
)

// sumGolangOrg is the checksum database's verifier key, as cmd/go holds it
// (cmd/go/internal/modfetch/key.go).
const sumGolangOrg = "sum.golang.org+033de0ae+Ac4zctda0e5eza+HJyk9SxEdh+s3Ux18htTTAD8OuAn8"

// goRelease is a Go release's toolchain name.
var goRelease = regexp.MustCompile(`^go1\.\d+\.\d+$`)

// goCommand answers a go command to start the build with. The build itself
// runs the exact toolchain the server's go.mod names (GOTOOLCHAIN), which
// this one fetches and checks when it is another version, so any go from
// 1.21 on serves; with none on the PATH the launcher fetches its own.
func (l *launcher) goCommand(ctx context.Context) (string, error) {
	if path, err := exec.LookPath("go"); err == nil {
		out, err := exec.CommandContext(ctx, path, "env", "GOVERSION").Output()
		if v := strings.TrimSpace(string(out)); err == nil && goRelease.MatchString(v) &&
			semver.Compare("v"+strings.TrimPrefix(v, "go"), "v1.21.0") >= 0 {
			return path, nil
		}
	}
	// The release the launcher was built with, which builds the server of
	// the same release without a second download.
	v := runtime.Version()
	if !goRelease.MatchString(v) {
		return "", fmt.Errorf("no go command on the PATH, and this launcher was built by %s, not a Go release, so it cannot fetch one", v)
	}
	return l.toolchain(ctx, v)
}

// toolchain answers the go command of Go release v, fetching it on first
// use. Go publishes each release as the module golang.org/toolchain; its zip
// comes from the module proxy and its hash from the checksum database, as
// GOTOOLCHAIN's own download does.
func (l *launcher) toolchain(ctx context.Context, v string) (string, error) {
	m := module.Version{Path: "golang.org/toolchain", Version: "v0.0.1-" + v + "." + runtime.GOOS + "-" + runtime.GOARCH}
	dir := filepath.Join(l.cache, "toolchains", toolchainName(v))
	gobin := filepath.Join(dir, "bin", exe("go"))
	if _, err := os.Stat(gobin); err == nil {
		return gobin, nil
	}
	logger.Printf("downloading the Go toolchain %s for %s/%s", v, runtime.GOOS, runtime.GOARCH)
	escPath, _ := module.EscapePath(m.Path)
	escVer, _ := module.EscapeVersion(m.Version)
	data, err := l.fetch(ctx, goProxy()+"/"+escPath+"/@v/"+escVer+".zip", false)
	if errors.Is(err, errNotFound) {
		return "", fmt.Errorf("no toolchain of Go %s for %s/%s", v, runtime.GOOS, runtime.GOARCH)
	}
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
		return "", err
	}
	zipFile := dir + ".zip"
	if err := os.WriteFile(zipFile, data, 0o644); err != nil {
		return "", err
	}
	defer os.Remove(zipFile)
	if err := l.checkSum(m, zipFile); err != nil {
		return "", err
	}
	tmp := fmt.Sprintf("%s.tmp-%d", dir, os.Getpid())
	defer os.RemoveAll(tmp)
	if err := modzip.Unzip(tmp, m, zipFile); err != nil {
		return "", err
	}
	if runtime.GOOS != "windows" {
		// Module zips keep no file modes. The same files cmd/go marks in
		// its toolchain download (toolchain.Exec): every tool, lib's
		// scripts, and bin.
		for _, d := range []struct{ dir, pattern string }{{"pkg/tool", ""}, {"lib", "*.sh"}, {"bin", ""}} {
			if err := allowExec(filepath.Join(tmp, d.dir), d.pattern); err != nil {
				return "", err
			}
		}
	}
	if err := os.Rename(tmp, dir); err != nil {
		return "", err
	}
	return gobin, nil
}

// checkSum checks zipFile against the hash sum.golang.org records for m.
func (l *launcher) checkSum(m module.Version, zipFile string) error {
	ops := &sumOps{l: l, dir: filepath.Join(l.cache, "sumdb")}
	lines, err := sumdb.NewClient(ops).Lookup(m.Path, m.Version)
	if err != nil {
		return fmt.Errorf("look up %s in the checksum database: %w", m, err)
	}
	got, err := dirhash.HashZip(zipFile, dirhash.Hash1)
	if err != nil {
		return err
	}
	if slices.Contains(lines, m.Path+" "+m.Version+" "+got) {
		return nil
	}
	return fmt.Errorf("%s does not match the checksum database: SECURITY ERROR", m)
}

// allowExec sets the execute bits on the files under dir that match
// pattern, or on all of them when it is empty.
func allowExec(dir, pattern string) error {
	return filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if errors.Is(err, fs.ErrNotExist) && path == dir {
			return filepath.SkipDir
		}
		if err != nil || d.IsDir() {
			return err
		}
		if ok, _ := filepath.Match(pattern, d.Name()); pattern != "" && !ok {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		return os.Chmod(path, info.Mode()&0o777|0o111)
	})
}

// goProxy answers the first proxy GOPROXY names, or the public one.
func goProxy() string {
	first, _, _ := strings.Cut(os.Getenv("GOPROXY"), ",")
	first, _, _ = strings.Cut(first, "|")
	if strings.HasPrefix(first, "https://") || strings.HasPrefix(first, "http://") {
		return strings.TrimSuffix(first, "/")
	}
	return "https://proxy.golang.org"
}

// toolchainName is the directory of Go release v's toolchain; with no
// argument, of the release this launcher was built with.
func toolchainName(v ...string) string {
	rel := runtime.Version()
	if len(v) > 0 {
		rel = v[0]
	}
	return rel + "." + runtime.GOOS + "-" + runtime.GOARCH
}

func exe(name string) string {
	if runtime.GOOS == "windows" {
		return name + ".exe"
	}
	return name
}

// sumOps is the checksum database client's storage: its latest signed tree
// under dir/config and the tiles it has read under dir/cache.
type sumOps struct {
	l   *launcher
	dir string
	mu  sync.Mutex
}

func (o *sumOps) ReadRemote(path string) ([]byte, error) {
	return o.l.fetch(context.Background(), "https://sum.golang.org"+path, false)
}

func (o *sumOps) ReadConfig(file string) ([]byte, error) {
	if file == "key" {
		return []byte(sumGolangOrg), nil
	}
	data, err := os.ReadFile(filepath.Join(o.dir, "config", file))
	if errors.Is(err, fs.ErrNotExist) {
		// An empty tree: the client starts from nothing.
		return nil, nil
	}
	return data, err
}

func (o *sumOps) WriteConfig(file string, old, new []byte) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	cur, err := o.ReadConfig(file)
	if err != nil {
		return err
	}
	if !bytes.Equal(cur, old) {
		return sumdb.ErrWriteConflict
	}
	path := filepath.Join(o.dir, "config", file)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, new, 0o644)
}

func (o *sumOps) ReadCache(file string) ([]byte, error) {
	return os.ReadFile(filepath.Join(o.dir, "cache", file))
}

func (o *sumOps) WriteCache(file string, data []byte) {
	path := filepath.Join(o.dir, "cache", file)
	if os.MkdirAll(filepath.Dir(path), 0o755) == nil {
		_ = os.WriteFile(path, data, 0o644)
	}
}

func (o *sumOps) Log(string) {}

func (o *sumOps) SecurityError(msg string) { logger.Print(msg) }
