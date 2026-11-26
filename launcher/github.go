package main

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"golang.org/x/mod/modfile"
	"golang.org/x/mod/module"
	"golang.org/x/mod/semver"
	"golang.org/x/mod/sumdb/dirhash"
	modzip "golang.org/x/mod/zip"
)

// The dev channel resolves a golem-mc module's HEAD from GitHub: the module
// proxy fetches every new commit upstream before it answers, which takes
// 15-30 s per module.
var (
	githubAPI   = "https://api.github.com"
	codeloadURL = "https://codeload.github.com"
	// githubHeads turns the GitHub lookup off; tests that stand in for the
	// module proxy do.
	githubHeads = true
)

const githubOrg = "github.com/golem-mc"

// majorSuffix is the /vN a module path ends in from v2 on.
var majorSuffix = regexp.MustCompile(`^v([2-9]|[1-9][0-9]+)$`)

// headEntry is what the launcher remembers of one module's main branch, so a
// request with its ETags can be answered 304.
type headEntry struct {
	ETag     string   `json:"etag"`
	Version  string   `json:"version"`
	TagsETag string   `json:"tagsETag,omitempty"`
	Tags     []string `json:"tags,omitempty"`
}

// headCall is one module's lookup in flight, shared by every query for it.
type headCall struct {
	done    chan struct{}
	version string
	err     error
}

func (l *launcher) headsPath() string { return filepath.Join(l.cache, "github-heads.json") }

// githubHead answers the query q (module@HEAD) from GitHub. handled is false
// when q is not for a golem-mc module, or the lookup failed (said in the
// step's output): go answers those. A path below a repository's root, and
// github.com/golem-mc itself, are answered as no module, so nested modules
// are not found on this path.
func (l *launcher) githubHead(ctx context.Context, q string) (version string, handled bool, err error) {
	mod, ok := strings.CutSuffix(q, "@HEAD")
	if !githubHeads || !ok || !strings.HasPrefix(mod+"/", "github.com/") {
		return "", false, nil
	}
	rest, ok := strings.CutPrefix(mod, githubOrg+"/")
	if !ok {
		if mod == githubOrg || mod == "github.com" {
			return "", true, fmt.Errorf("%s is not a module", mod)
		}
		return "", false, nil
	}
	repo, tail, _ := strings.Cut(rest, "/")
	if tail != "" && !majorSuffix.MatchString(tail) {
		return "", true, fmt.Errorf("%s is not a module", mod)
	}

	l.headsMu.Lock()
	c, running := l.headCalls[mod]
	if !running {
		if l.headCalls == nil {
			l.headCalls = map[string]*headCall{}
		}
		c = &headCall{done: make(chan struct{})}
		l.headCalls[mod] = c
	}
	l.headsMu.Unlock()
	if !running {
		c.version, c.err = l.lookupHead(ctx, mod, repo, tail)
		if c.err != nil && ctx.Err() == nil {
			l.ui.Info("github could not answer for %s (%v); asking the module proxy", mod, c.err)
		}
		close(c.done)
	}
	select {
	case <-c.done:
	case <-ctx.Done():
		return "", true, ctx.Err()
	}
	if c.err != nil {
		return "", false, nil
	}
	return c.version, true, nil
}

// githubModule answers the module of package p: the repository's root for a
// path under github.com/golem-mc (with its /vN suffix), p itself otherwise.
func githubModule(p string) string {
	rest, ok := strings.CutPrefix(p, githubOrg+"/")
	if !ok {
		return p
	}
	elems := strings.Split(rest, "/")
	if len(elems) > 1 && majorSuffix.MatchString(elems[1]) {
		return githubOrg + "/" + elems[0] + "/" + elems[1]
	}
	return githubOrg + "/" + elems[0]
}

// ghResult is one GitHub API answer.
type ghResult struct {
	body []byte
	etag string
	same bool // 304
	err  error
}

// ghGet GETs an API path, conditional on etag when it is not "".
func (l *launcher) ghGet(ctx context.Context, p, etag string) ghResult {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, githubAPI+p, nil)
	if err != nil {
		return ghResult{err: err}
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	if token := os.Getenv("GITHUB_TOKEN"); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if etag != "" {
		req.Header.Set("If-None-Match", etag)
	}
	resp, err := l.http.Do(req)
	if err != nil {
		return ghResult{err: err}
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusNotModified:
		return ghResult{same: true, etag: etag}
	case http.StatusOK:
		body, err := io.ReadAll(resp.Body)
		return ghResult{body: body, etag: resp.Header.Get("ETag"), err: err}
	}
	return ghResult{err: fmt.Errorf("GET %s: %s", p, resp.Status)}
}

// lookupHead answers the version go names for mod@HEAD, the module at the
// root of repo with major version suffix major ("" or "vN").
func (l *launcher) lookupHead(ctx context.Context, mod, repo, major string) (string, error) {
	api := "/repos/golem-mc/" + repo
	l.headsMu.Lock()
	old := l.readHeads()[mod]
	l.headsMu.Unlock()

	// The commit and the tags are asked at once; so is the zip, once the
	// commit's sha is known, long before the version is.
	commitC, tagsC := make(chan ghResult, 1), make(chan ghResult, 1)
	etag := ""
	if old.Version != "" {
		etag = old.ETag
	}
	go func() { commitC <- l.ghGet(ctx, api+"/commits/main", etag) }()
	go func() { tagsC <- l.ghGet(ctx, api+"/tags?per_page=100", old.TagsETag) }()

	cr := <-commitC
	if cr.err != nil {
		return "", cr.err
	}
	if cr.same {
		return old.Version, nil
	}
	var commit struct {
		SHA    string `json:"sha"`
		Commit struct {
			Committer struct {
				Date time.Time `json:"date"`
			} `json:"committer"`
		} `json:"commit"`
	}
	if err := json.Unmarshal(cr.body, &commit); err != nil || len(commit.SHA) != 40 {
		return "", fmt.Errorf("read %s/commits/main: %v", api, err)
	}
	sha := commit.SHA
	var zipC chan zipResult
	if pctx := l.prefetchCtx; pctx != nil {
		// Not ctx: pluginModule cancels it once it has its answer.
		zipC = make(chan zipResult, 1)
		go func() {
			data, err := l.codeload(pctx, repo, sha)
			zipC <- zipResult{data, err}
		}()
	}

	tr := <-tagsC
	if tr.err != nil {
		return "", tr.err
	}
	entry := headEntry{ETag: cr.etag, TagsETag: tr.etag, Tags: old.Tags}
	if !tr.same {
		var tags []struct{ Name string }
		if err := json.Unmarshal(tr.body, &tags); err != nil {
			return "", fmt.Errorf("read %s/tags: %w", api, err)
		}
		entry.Tags = nil
		for _, t := range tags {
			entry.Tags = append(entry.Tags, t.Name)
		}
	}
	base := newestTag(entry.Tags, major)

	version := ""
	if base == "" {
		version = module.PseudoVersion(major, "", commit.Commit.Committer.Date, sha[:12])
	} else {
		// go bases the version on the newest tag below the commit; any
		// other shape of history is left to it.
		cmp := l.ghGet(ctx, api+"/compare/"+base+"..."+sha+"?per_page=1", "")
		if cmp.err != nil {
			return "", cmp.err
		}
		var st struct{ Status string }
		if err := json.Unmarshal(cmp.body, &st); err != nil {
			return "", err
		}
		switch st.Status {
		case "identical":
			version = base
		case "ahead":
			version = module.PseudoVersion(major, base, commit.Commit.Committer.Date, sha[:12])
		default:
			return "", fmt.Errorf("%s is %s %s", sha[:12], st.Status, base)
		}
	}
	entry.Version = version

	if zipC != nil {
		l.zips.Go(func() {
			z := <-zipC
			err := z.err
			if err == nil {
				err = l.installZip(l.prefetchCtx, mod, version, commit.Commit.Committer.Date, z.data)
			}
			if err != nil {
				l.debugf("prefetch %s@%s: %v", mod, version, err)
			}
		})
	}
	l.headsMu.Lock()
	defer l.headsMu.Unlock()
	m := l.readHeads()
	m[mod] = entry
	data, _ := json.MarshalIndent(m, "", "  ")
	if err := writeFileAtomic(l.headsPath(), data, 0o644); err != nil {
		l.debugf("write %s: %v", l.headsPath(), err)
	}
	return version, nil
}

// readHeads answers the remembered main branches by module. headsMu must be
// held.
func (l *launcher) readHeads() map[string]headEntry {
	m := map[string]headEntry{}
	if data, err := os.ReadFile(l.headsPath()); err == nil && json.Unmarshal(data, &m) != nil {
		return map[string]headEntry{}
	}
	return m
}

// newestTag answers the highest release or pre-release tag of the module
// whose path has major version suffix major, or "".
func newestTag(tags []string, major string) string {
	best := ""
	for _, t := range tags {
		if !semver.IsValid(t) || semver.Canonical(t) != t {
			continue
		}
		if major == "" && semver.Major(t) != "v0" && semver.Major(t) != "v1" || major != "" && semver.Major(t) != major {
			continue
		}
		if best == "" || semver.Compare(t, best) > 0 {
			best = t
		}
	}
	return best
}

type zipResult struct {
	data []byte
	err  error
}

// codeload downloads repo's source at sha.
func (l *launcher) codeload(ctx context.Context, repo, sha string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, codeloadURL+"/golem-mc/"+repo+"/zip/"+sha, nil)
	if err != nil {
		return nil, err
	}
	resp, err := l.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("codeload %s: %s", repo, resp.Status)
	}
	return io.ReadAll(resp.Body)
}

// waitZips waits for the prefetched modules to be in the module cache.
func (l *launcher) waitZips() { l.zips.Wait() }

// modCache answers go's module cache directory.
func (l *launcher) modCache(ctx context.Context) (string, error) {
	l.modCacheOnce.Do(func() {
		var gobin string
		if gobin, l.modCacheErr = l.goCommand(ctx); l.modCacheErr != nil {
			return
		}
		g := &goRun{ctx: ctx, bin: gobin, toolchain: "local", dir: os.TempDir()}
		var out string
		if out, l.modCacheErr = g.run("env", "GOMODCACHE"); l.modCacheErr == nil {
			l.modCacheDir = strings.TrimSpace(out)
		}
	})
	return l.modCacheDir, l.modCacheErr
}

// installZip writes the source archive data of a GitHub repository into go's
// module cache as mod@version, in go's format, so go neither asks the proxy
// nor the checksum database for it.
func (l *launcher) installZip(ctx context.Context, mod, version string, t time.Time, data []byte) error {
	cache, err := l.modCache(ctx)
	if err != nil {
		return err
	}
	esc, err := module.EscapePath(mod)
	if err != nil {
		return err
	}
	dl := filepath.Join(cache, "cache", "download", filepath.FromSlash(esc), "@v")
	if fileExists(filepath.Join(dl, version+".info")) {
		return nil
	}
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return err
	}
	tmp, err := os.MkdirTemp(l.cache, "zip-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	src := filepath.Join(tmp, "src")
	for _, f := range zr.File {
		// The archive's files sit in one directory, <repo>-<sha>.
		_, name, ok := strings.Cut(f.Name, "/")
		if !ok || name == "" || !f.Mode().IsRegular() || !filepath.IsLocal(name) {
			continue
		}
		if err := extractZipFile(f, filepath.Join(src, filepath.FromSlash(name))); err != nil {
			return err
		}
	}
	gomod, err := os.ReadFile(filepath.Join(src, "go.mod"))
	if err != nil {
		return err
	}
	if got := modfile.ModulePath(gomod); got != mod {
		return fmt.Errorf("go.mod names %q, not %s", got, mod)
	}
	zipFile := filepath.Join(tmp, "m.zip")
	out, err := os.Create(zipFile)
	if err != nil {
		return err
	}
	err = modzip.CreateFromDir(out, module.Version{Path: mod, Version: version}, src)
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	sum, err := dirhash.HashZip(zipFile, dirhash.Hash1)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dl, 0o777); err != nil {
		return err
	}
	if err := os.Rename(zipFile, filepath.Join(dl, version+".zip")); err != nil {
		return err
	}
	info, _ := json.Marshal(struct {
		Version string
		Time    time.Time
	}{version, t.UTC()})
	// .info last: it is what says the version is cached.
	for _, w := range []struct {
		ext  string
		data []byte
	}{{".ziphash", []byte(sum)}, {".mod", gomod}, {".info", info}} {
		if err := writeFileAtomic(filepath.Join(dl, version+w.ext), w.data, 0o666); err != nil {
			return err
		}
	}
	return nil
}

func extractZipFile(f *zip.File, dst string) error {
	if err := os.MkdirAll(path.Dir(filepath.ToSlash(dst)), 0o755); err != nil {
		return err
	}
	r, err := f.Open()
	if err != nil {
		return err
	}
	defer r.Close()
	w, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(w, r); err != nil {
		w.Close()
		return errors.Join(err, os.Remove(dst))
	}
	return w.Close()
}
