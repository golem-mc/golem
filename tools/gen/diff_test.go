package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestDiffCutsAtTheLimitAndCountsTheRest deletes a file of n lines, whose
// diff is n+3 lines long.
func TestDiffCutsAtTheLimitAndCountsTheRest(t *testing.T) {
	if _, err := exec.LookPath("diff"); err != nil {
		t.Skip("no diff on the PATH")
	}
	notice := func(more int) string { return fmt.Sprintf("... %d more diff lines\n", more) }
	for _, c := range []struct {
		lines int
		want  string // the notice at the end, if any
	}{
		{maxDiffLines - 3, ""},
		{maxDiffLines - 2, notice(1)},
		{maxDiffLines + 40, notice(43)}, // 103 diff lines, 60 shown
	} {
		var old strings.Builder
		for i := range c.lines {
			fmt.Fprintf(&old, "line %d\n", i)
		}
		path := filepath.Join(t.TempDir(), "old")
		if err := os.WriteFile(path, []byte(old.String()), 0o644); err != nil {
			t.Fatal(err)
		}
		got := unifiedDiff("x.go", path, nil)
		if c.want == "" {
			if strings.Contains(got, "more diff lines") || strings.Count(got, "\n") != maxDiffLines {
				t.Errorf("%d lines: got %d diff lines, want exactly %d and no notice", c.lines, strings.Count(got, "\n"), maxDiffLines)
			}
			continue
		}
		if !strings.HasSuffix(got, c.want) || strings.Count(got, "\n") != maxDiffLines+1 {
			t.Errorf("%d lines: got %q ... (%d lines), want the notice %q", c.lines, got[max(0, len(got)-40):], strings.Count(got, "\n"), c.want)
		}
	}
}
