package app

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestMain(m *testing.M) {
	if os.Getenv("GOLEM_APP_HELPER") == "1" {
		os.Args = append([]string{"golem"}, strings.Fields(os.Getenv("GOLEM_APP_ARGS"))...)
		Main()
		return
	}
	os.Exit(m.Run())
}

func runHelper(t *testing.T, dir, args string, env ...string) ([]byte, error) {
	t.Helper()
	cmd := exec.Command(os.Args[0])
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), append([]string{"GOLEM_APP_HELPER=1", "GOLEM_APP_ARGS=" + args}, env...)...)
	return cmd.CombinedOutput()
}

func TestCrashReportFromBackgroundPanic(t *testing.T) {
	dir := t.TempDir()
	if _, err := runHelper(t, dir, "--help", "GOLEM_TEST_CRASH=goroutine"); err == nil {
		t.Fatal("the crashing run exited cleanly")
	}
	files, _ := filepath.Glob(filepath.Join(dir, "crash-reports", ".pending-*.txt"))
	if len(files) != 1 {
		t.Fatalf("pending files = %v", files)
	}
	b, _ := os.ReadFile(files[0])
	if !strings.Contains(string(b), "panic: test crash") || !strings.Contains(string(b), "goroutine ") {
		t.Fatalf("crash report lacks the trace:\n%s", b)
	}

	out, err := runHelper(t, dir, "--help")
	if err != nil {
		t.Fatalf("second run: %v\n%s", err, out)
	}
	reports, _ := filepath.Glob(filepath.Join(dir, "crash-reports", "crash-*-server.txt"))
	if len(reports) != 1 {
		t.Fatalf("reports = %v", reports)
	}
	if !strings.Contains(string(out), "the previous run crashed; see crash-reports/"+filepath.Base(reports[0])) {
		t.Fatalf("no WARN naming the report:\n%s", out)
	}
	left, _ := os.ReadDir(filepath.Join(dir, "crash-reports"))
	if len(left) != 1 {
		t.Fatalf("crash-reports after the clean stop = %v", left)
	}
}

func TestCleanStopLeavesNoCrashReport(t *testing.T) {
	dir := t.TempDir()
	if out, err := runHelper(t, dir, "--help"); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	left, _ := os.ReadDir(filepath.Join(dir, "crash-reports"))
	if len(left) != 0 {
		t.Fatalf("crash-reports = %v", left)
	}
}

func TestLivePendingFileSurvivesSecondStart(t *testing.T) {
	dir := t.TempDir()
	cdir := filepath.Join(dir, "crash-reports")
	if err := os.MkdirAll(cdir, 0o755); err != nil {
		t.Fatal(err)
	}
	// The test process is the live owner of the first pending file.
	live := filepath.Join(cdir, ".pending-"+strconv.Itoa(os.Getpid())+".txt")
	if err := os.WriteFile(live, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	// Run a second start whose own pid is another, so seed a non-empty file
	// under the parent's pid too.
	ppid := filepath.Join(cdir, ".pending-"+strconv.Itoa(os.Getppid())+".txt")
	if err := os.WriteFile(ppid, []byte("trace"), 0o644); err != nil {
		t.Fatal(err)
	}
	if out, err := runHelper(t, dir, "--help"); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if _, err := os.Stat(live); err != nil {
		t.Fatalf("empty live pending file removed: %v", err)
	}
	if b, err := os.ReadFile(ppid); err != nil || string(b) != "trace" {
		t.Fatalf("non-empty live pending file touched: %v %q", err, b)
	}
	if r, _ := filepath.Glob(filepath.Join(cdir, "crash-*-server.txt")); len(r) != 0 {
		t.Fatalf("reports = %v", r)
	}
}
