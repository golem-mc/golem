package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// maxDiffLines cuts one file's diff in the log; the rest is a command away.
const maxDiffLines = 60

// staleReport says how a committed file differs from what would be written:
// `diff -u` when the host has one, else the first line that differs.
func staleReport(rel, oldPath string, old, src []byte, exists bool) string {
	if !exists {
		return fmt.Sprintf("%s does not exist\n", rel)
	}
	if out := unifiedDiff(rel, oldPath, src); out != "" {
		return out
	}
	oldLines, newLines := strings.Split(string(old), "\n"), strings.Split(string(src), "\n")
	for i := 0; i < max(len(oldLines), len(newLines)); i++ {
		var a, b string
		if i < len(oldLines) {
			a = oldLines[i]
		}
		if i < len(newLines) {
			b = newLines[i]
		}
		if a != b {
			return fmt.Sprintf("%s: first difference at line %d\n  committed: %.200s\n  generated: %.200s\n", rel, i+1, a, b)
		}
	}
	return rel + "\n"
}

func unifiedDiff(rel, oldPath string, src []byte) string {
	dir, err := os.MkdirTemp("", "gen-diff-")
	if err != nil {
		return ""
	}
	defer os.RemoveAll(dir)
	newPath := filepath.Join(dir, "generated")
	if os.WriteFile(newPath, src, 0o644) != nil {
		return ""
	}
	// diff exits 1 when the files differ; any other failure is no diff.
	out, err := exec.Command("diff", "-u", "--label", "committed/"+rel, "--label", "generated/"+rel, oldPath, newPath).Output()
	if ee, ok := err.(*exec.ExitError); (err != nil && !ok) || (ok && ee.ExitCode() != 1) {
		return ""
	}
	lines := bytes.SplitAfter(bytes.TrimSuffix(out, []byte("\n")), []byte("\n"))
	if len(lines) > maxDiffLines {
		cut := append(bytes.Join(lines[:maxDiffLines], nil), fmt.Sprintf("... %d more diff lines\n", len(lines)-maxDiffLines)...)
		return string(cut)
	}
	return string(out)
}
