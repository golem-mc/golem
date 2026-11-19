package datapack

import (
	"encoding/json"
	"io/fs"
	"math"
	"regexp"
	"slices"
	"strings"
)

// overlayDir is what an overlay entry's directory must match.
// vanilla: OverlayMetadataSection.PATTERN
var overlayDir = regexp.MustCompile(`^[-_a-zA-Z0-9.]+$`)

// formatVersion is a pack format: major, then minor.
type formatVersion struct{ major, minor int }

func (a formatVersion) less(b formatVersion) bool {
	return a.major < b.major || (a.major == b.major && a.minor < b.minor)
}

// formatBound reads a min_format or max_format: an int or [major, minor].
// bareMinor is the minor of a bare int. vanilla: PackFormat.bound
func formatBound(raw json.RawMessage, bareMinor int) (formatVersion, bool) {
	var n int
	if json.Unmarshal(raw, &n) == nil {
		return formatVersion{n, bareMinor}, true
	}
	var pair []int
	if json.Unmarshal(raw, &pair) == nil && len(pair) == 2 {
		return formatVersion{pair[0], pair[1]}, true
	}
	return formatVersion{}, false
}

// overlayDirs returns the directories of the pack.mcmeta overlay entries
// whose format range holds the current format, in list order.
// vanilla: OverlayMetadataSection.overlaysForVersion
func overlayDirs(base fs.FS) []string {
	data, err := fs.ReadFile(base, "pack.mcmeta")
	if err != nil {
		return nil
	}
	var meta struct {
		Overlays struct {
			Entries []struct {
				Directory string          `json:"directory"`
				Min       json.RawMessage `json:"min_format"`
				Max       json.RawMessage `json:"max_format"`
			} `json:"entries"`
		} `json:"overlays"`
	}
	if json.Unmarshal(data, &meta) != nil {
		return nil
	}
	cur := formatVersion{PackFormat, PackMinorFormat}
	var dirs []string
	for _, e := range meta.Overlays.Entries {
		if !overlayDir.MatchString(e.Directory) {
			continue
		}
		if e.Min == nil || e.Max == nil {
			continue
		}
		lo, okLo := formatBound(e.Min, 0)
		hi, okHi := formatBound(e.Max, math.MaxInt32)
		if !okLo || !okHi {
			continue
		}
		if !cur.less(lo) && !hi.less(cur) {
			dirs = append(dirs, e.Directory)
		}
	}
	return dirs
}

// layeredFS is a pack's overlays over the pack itself, highest priority first.
// vanilla: OverlayedPackResources
type layeredFS []fs.FS

// withOverlays returns base under its applicable overlays, or base itself.
func withOverlays(base fs.FS) (fs.FS, error) {
	dirs := overlayDirs(base)
	if len(dirs) == 0 {
		return base, nil
	}
	layers := make(layeredFS, 0, len(dirs)+1)
	for _, dir := range slices.Backward(dirs) {
		sub, err := fs.Sub(base, dir)
		if err != nil {
			return nil, err
		}
		layers = append(layers, sub)
	}
	return append(layers, base), nil
}

// Open opens name from the first layer that has it. pack.mcmeta is always the
// base's, and a directory lists the union of the layers (ReadDir).
func (l layeredFS) Open(name string) (fs.File, error) {
	if name == "pack.mcmeta" {
		return l[len(l)-1].Open(name)
	}
	var firstErr error
	for _, layer := range l {
		f, err := layer.Open(name)
		if err == nil {
			return f, nil
		}
		if firstErr == nil {
			firstErr = err
		}
	}
	return nil, firstErr
}

// ReadDir merges the layers' entries of a directory by name, the first
// layer's entry winning, sorted by name.
func (l layeredFS) ReadDir(name string) ([]fs.DirEntry, error) {
	seen := map[string]bool{}
	var out []fs.DirEntry
	var firstErr error
	found := false
	for _, layer := range l {
		entries, err := fs.ReadDir(layer, name)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		found = true
		for _, e := range entries {
			if !seen[e.Name()] {
				seen[e.Name()] = true
				out = append(out, e)
			}
		}
	}
	if !found {
		return nil, firstErr
	}
	slices.SortFunc(out, func(a, b fs.DirEntry) int {
		return strings.Compare(a.Name(), b.Name())
	})
	return out, nil
}
