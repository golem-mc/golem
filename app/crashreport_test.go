package app

import (
	"os"
	"os/exec"
	"path/filepath"
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
