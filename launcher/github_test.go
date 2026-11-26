package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func init() {
	// Tests that stand in for the module proxy must not reach GitHub.
	githubHeads = false
}

// gitRepo is a repository the fake GitHub answers from.
type gitRepo struct {
	t    *testing.T
	name string
	dir  string
}

func (r *gitRepo) git(args ...string) string {
	r.t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = r.dir
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com")
	out, err := cmd.CombinedOutput()
	if err != nil {
		r.t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func newGitRepo(t *testing.T, name, modPath, goLine string, files map[string]string) *gitRepo {
	r := &gitRepo{t: t, name: name, dir: filepath.Join(t.TempDir(), name)}
	if err := os.MkdirAll(r.dir, 0o755); err != nil {
		t.Fatal(err)
	}
	r.git("init", "-b", "main")
	files["go.mod"] = "module " + modPath + "\n\ngo " + goLine + "\n"
	r.commitAt("2026-11-01T10:00:00Z", files)
	r.git("tag", "v0.1.0")
	r.commitAt("2026-11-02T11:30:15Z", map[string]string{"extra.txt": "x"})
	return r
}

// commitAt commits with both dates set to when.
func (r *gitRepo) commitAt(when string, files map[string]string) {
	r.t.Helper()
	t := r.t
	for name, content := range files {
		p := filepath.Join(r.dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	r.git("add", "-A")
	cmd := exec.Command("git", "-c", "commit.gpgsign=false", "commit", "-m", "c")
	cmd.Dir = r.dir
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com",
		"GIT_AUTHOR_DATE="+when, "GIT_COMMITTER_DATE="+when)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git commit: %v\n%s", err, out)
	}
}

// fakeGitHub answers the API and codeload requests from git repositories,
// and records them.
type fakeGitHub struct {
	t     *testing.T
	repos map[string]*gitRepo

	mu       sync.Mutex
	reqs     []string
	arrived  map[string]chan struct{} // per repo: closed by its zip request
	commits  int                      // commit requests waiting at the barrier
	barrier  chan struct{}
	together bool // all repos' commit requests were in flight at once
	zipFirst map[string]bool
}

func newFakeGitHub(t *testing.T, repos ...*gitRepo) *fakeGitHub {
	g := &fakeGitHub{t: t, repos: map[string]*gitRepo{}, arrived: map[string]chan struct{}{}, zipFirst: map[string]bool{}, barrier: make(chan struct{})}
	for _, r := range repos {
		g.repos[r.name] = r
		g.arrived[r.name] = make(chan struct{})
	}
	return g
}

func (g *fakeGitHub) requests() []string {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]string(nil), g.reqs...)
}

func (g *fakeGitHub) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	g.mu.Lock()
	g.reqs = append(g.reqs, req.URL.Path)
	g.mu.Unlock()
	parts := strings.Split(strings.Trim(req.URL.Path, "/"), "/")
	// /repos/golem-mc/<repo>/<what>... or /golem-mc/<repo>/zip/<sha>
	var repo *gitRepo
	var what []string
	if parts[0] == "repos" && len(parts) > 3 {
		repo, what = g.repos[parts[2]], parts[3:]
	} else if len(parts) == 4 && parts[2] == "zip" {
		repo, what = g.repos[parts[1]], parts[2:]
	}
	if repo == nil {
		http.NotFound(w, req)
		return
	}
	switch what[0] {
	case "commits":
		g.mu.Lock()
		g.commits++
		if g.commits == len(g.repos) {
			g.together = true
			close(g.barrier)
		}
		g.mu.Unlock()
		select {
		case <-g.barrier:
		case <-time.After(3 * time.Second):
		}
		out := repo.git("log", "-1", "--format=%H%n%cI", "main")
		sha, date, _ := strings.Cut(out, "\n")
		etag := `"` + sha + `"`
		if req.Header.Get("If-None-Match") == etag {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("ETag", etag)
		fmt.Fprintf(w, `{"sha":%q,"commit":{"committer":{"date":%q}}}`, sha, date)
	case "tags":
		names := strings.Fields(repo.git("tag"))
		etag := `"tags-` + strings.Join(names, ",") + `"`
		if req.Header.Get("If-None-Match") == etag {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		// A tag list is not answered before the repo's zip download has
		// started, if the commit's answer was in time.
		select {
		case <-g.arrived[repo.name]:
			g.mu.Lock()
			g.zipFirst[repo.name] = true
			g.mu.Unlock()
		case <-time.After(3 * time.Second):
		}
		w.Header().Set("ETag", etag)
		var list []string
		for _, n := range names {
			list = append(list, fmt.Sprintf(`{"name":%q}`, n))
		}
		fmt.Fprintf(w, "[%s]", strings.Join(list, ","))
	case "compare":
		base, sha, _ := strings.Cut(what[1], "...")
		status := "ahead"
		if repo.git("rev-parse", base+"^{commit}") == sha {
			status = "identical"
		} else if exec.Command("git", "-C", repo.dir, "merge-base", "--is-ancestor", base, sha).Run() != nil {
			status = "diverged"
		}
		fmt.Fprintf(w, `{"status":%q}`, status)
	case "zip":
		close2(g.arrived[repo.name])
		cmd := exec.Command("git", "archive", "--format=zip", "--prefix="+repo.name+"-"+what[1]+"/", what[1])
		cmd.Dir = repo.dir
		out, err := cmd.Output()
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		w.Write(out)
	}
}

func close2(c chan struct{}) {
	select {
	case <-c:
	default:
		close(c)
	}
}

func TestDevUpdateResolvesFromGitHub(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	f := newFixture(t, "")
	pkg := func(n string) map[string]string { return map[string]string{n + ".go": "package " + n + "\n"} }
	srv := newGitRepo(t, "golem", serverModule, f.goLine, map[string]string{"app/app.go": "package app\n\nfunc Main() {}\n"})
	perms := newGitRepo(t, "perms", "github.com/golem-mc/perms", f.goLine, pkg("perms"))
	vault := newGitRepo(t, "vault", "github.com/golem-mc/vault", f.goLine, pkg("vault"))
	spark := newGitRepo(t, "spark", "github.com/golem-mc/spark", f.goLine, pkg("spark"))
	gh := newFakeGitHub(t, srv, perms, vault, spark)
	ts := httptest.NewServer(gh)
	defer ts.Close()
	oldAPI, oldLoad, oldHeads := githubAPI, codeloadURL, githubHeads
	githubAPI, codeloadURL, githubHeads = ts.URL, ts.URL, true
	t.Cleanup(func() { githubAPI, codeloadURL, githubHeads = oldAPI, oldLoad, oldHeads })

	// The proxy answers nothing in time: a request to it fails the test by
	// its delay, and the checksum database is on.
	var proxyMu sync.Mutex
	var proxyReqs []string
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		proxyMu.Lock()
		proxyReqs = append(proxyReqs, r.URL.Path)
		proxyMu.Unlock()
		time.Sleep(5 * time.Second)
		http.Error(w, "slow", http.StatusGatewayTimeout)
	}))
	defer proxy.Close()
	t.Setenv("GOPROXY", proxy.URL)
	t.Setenv("GOSUMDB", "sum.golang.org")

	if err := os.WriteFile(f.config, []byte(`{"channel":"dev","server":"v0.1.0","plugins":["github.com/golem-mc/perms@v0.1.0","github.com/golem-mc/vault@v0.1.0","github.com/golem-mc/spark@v0.1.0"]}`), 0o644); err != nil {
		t.Fatal(err)
	}

	start := time.Now()
	f.must("update")
	if d := time.Since(start); d > 4*time.Second {
		t.Errorf("update took %v with a proxy that answers after 5 s", d)
	}
	proxyMu.Lock()
	if len(proxyReqs) != 0 {
		t.Errorf("update asked the module proxy: %v", proxyReqs)
	}
	proxyMu.Unlock()
	if !gh.together {
		t.Error("the modules' commit requests were not in flight at once")
	}
	for _, n := range []string{"golem", "perms", "vault", "spark"} {
		if !gh.zipFirst[n] {
			t.Errorf("%s: its zip download did not start before its tag list was answered", n)
		}
	}

	// The pinned versions are the ones go names for the same commits.
	cfg := f.loadCfg()
	pins := map[string]string{serverModule: cfg.Server}
	for _, p := range cfg.Plugins {
		path, v, _ := strings.Cut(p, "@")
		pins[path] = v
	}
	for _, r := range []*gitRepo{srv, perms, vault, spark} {
		mod := "github.com/golem-mc/" + r.name
		sha := r.git("rev-parse", "main")
		got := goListVersion(t, mod, sha, r.dir)
		if pins[mod] != got {
			t.Errorf("%s pinned %s, go list -m %s@%s says %s", mod, pins[mod], mod, sha[:12], got)
		}
		if !strings.HasPrefix(got, "v0.1.1-0.20261102113015-"+sha[:12]) {
			t.Errorf("%s: %s is not the pseudo-version of the commit after v0.1.0", mod, got)
		}
	}

	// Nothing moved: every module answers 304 and nothing more is asked.
	before := len(gh.requests())
	f.must("update")
	for _, p := range gh.requests()[before:] {
		if strings.Contains(p, "/compare/") || strings.Contains(p, "/zip/") {
			t.Errorf("a second update asked %s", p)
		}
	}
	if got := f.loadCfg(); got.Server != cfg.Server {
		t.Errorf("a second update moved the server to %s", got.Server)
	}
	proxyMu.Lock()
	defer proxyMu.Unlock()
	if len(proxyReqs) != 0 {
		t.Errorf("the module proxy was asked: %v", proxyReqs)
	}
}

// goListVersion asks go which version names sha of mod, from the git repo
// at dir.
func goListVersion(t *testing.T, mod, sha, dir string) string {
	t.Helper()
	cmd := exec.Command("go", "list", "-m", "-f", "{{.Version}}", mod+"@"+sha)
	cmd.Dir = t.TempDir()
	cmd.Env = append(os.Environ(),
		"GOPROXY=direct", "GOSUMDB=off", "GOFLAGS=-modcacherw", "GOPRIVATE=github.com/golem-mc",
		"GOMODCACHE="+t.TempDir(),
		"GIT_CONFIG_COUNT=1", "GIT_CONFIG_KEY_0=url."+dir+".insteadOf", "GIT_CONFIG_VALUE_0=https://"+mod)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go list -m %s@%s: %v\n%s", mod, sha, err, out)
	}
	return strings.TrimSpace(string(out))
}

func TestDevHeadFallsBackToGo(t *testing.T) {
	f := newFixture(t, `{"channel":"dev","server":"v1.0.0","plugins":[]}`)
	f.publishHead(serverModule, serverHead, "", map[string]string{"app/app.go": "package app\nfunc Main() {}\n"})
	down := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "rate limited", http.StatusForbidden)
	}))
	defer down.Close()
	oldAPI, oldHeads := githubAPI, githubHeads
	githubAPI, githubHeads = down.URL, true
	t.Cleanup(func() { githubAPI, githubHeads = oldAPI, oldHeads })

	out, errw, err := f.run("outdated")
	if err != nil {
		t.Fatalf("outdated: %v\n%s", err, errw)
	}
	if !strings.Contains(out, serverHead) {
		t.Errorf("outdated did not fall back to go's answer:\n%s", out)
	}
	if !strings.Contains(out+errw, "github could not answer for "+serverModule) {
		t.Errorf("the fallback was not said:\n%s%s", out, errw)
	}
}

func TestGithubModuleAndNewestTag(t *testing.T) {
	for in, want := range map[string]string{
		"github.com/golem-mc/vault":         "github.com/golem-mc/vault",
		"github.com/golem-mc/vault/economy": "github.com/golem-mc/vault",
		"github.com/golem-mc/vault/v2/x":    "github.com/golem-mc/vault/v2",
		"example.com/a/b":                   "example.com/a/b",
	} {
		if got := githubModule(in); got != want {
			t.Errorf("githubModule(%q) = %q, want %q", in, got, want)
		}
	}
	tags := []string{"v0.9.0", "v1.2.0", "v1.10.0", "v2.0.0", "nightly", "v1.3.0+incompatible", "v1.1"}
	if got := newestTag(tags, ""); got != "v1.10.0" {
		t.Errorf("newestTag(\"\") = %q", got)
	}
	if got := newestTag(tags, "v2"); got != "v2.0.0" {
		t.Errorf("newestTag(v2) = %q", got)
	}
	if got := newestTag(nil, ""); got != "" {
		t.Errorf("newestTag(nil) = %q", got)
	}
}
