package datapack

import (
	"encoding/json"
	"io/fs"
	"log/slog"
	"path/filepath"
	"regexp"
	"strings"
)

// filterSection is a pack.mcmeta filter: the blocked namespace and path
// patterns of its block list, a missing pattern matching everything.
// vanilla: ResourceFilterSection
type filterSection struct {
	namespaces []*regexp.Regexp
	paths      []*regexp.Regexp
}

func matchesAny(patterns []*regexp.Regexp, s string) bool {
	for _, p := range patterns {
		if p == nil || p.MatchString(s) {
			return true
		}
	}
	return false
}

// hides reports whether the filter hides the resource of the packs below
// it. The namespace and the path are tested against the whole list apart,
// not pattern by pattern, and a pattern matches anywhere
// in the text (Pattern.asPredicate).
// vanilla: ResourceFilterSection.isNamespaceFiltered, isPathFiltered
func (f filterSection) hides(namespace, path string) bool {
	return matchesAny(f.namespaces, namespace) && matchesAny(f.paths, path)
}

func (f filterSection) empty() bool { return len(f.namespaces) == 0 }

// readFilter reads a pack's filter section; one that does not parse is
// logged and filters nothing.
// vanilla: MultiPackResourceManager.getPackFilterSection
func readFilter(pack string) filterSection {
	fsys, err := packFS(pack)
	if err != nil {
		return filterSection{}
	}
	data, err := fs.ReadFile(fsys, "pack.mcmeta")
	if err != nil {
		return filterSection{}
	}
	var meta struct {
		Filter *struct {
			Block *[]struct {
				Namespace *string `json:"namespace"`
				Path      *string `json:"path"`
			} `json:"block"`
		} `json:"filter"`
	}
	if json.Unmarshal(data, &meta) != nil || meta.Filter == nil {
		return filterSection{}
	}
	fail := func() filterSection {
		slog.Error("Failed to get filter section from pack " + filepath.Base(pack))
		return filterSection{}
	}
	if meta.Filter.Block == nil {
		return fail()
	}
	var f filterSection
	for _, b := range *meta.Filter.Block {
		var ns, p *regexp.Regexp
		if b.Namespace != nil {
			if ns, err = regexp.Compile(*b.Namespace); err != nil {
				return fail()
			}
		}
		if b.Path != nil {
			if p, err = regexp.Compile(*b.Path); err != nil {
				return fail()
			}
		}
		f.namespaces = append(f.namespaces, ns)
		f.paths = append(f.paths, p)
	}
	return f
}

// Filters are the filter sections of the packs in layer order.
type Filters []filterSection

// LoadFilters reads the filter section of each pack.
func LoadFilters(packs []string) Filters {
	out := make(Filters, len(packs))
	for i, pack := range packs {
		out[i] = readFilter(pack)
	}
	return out
}

// Any reports whether some pack has a filter.
func (f Filters) Any() bool {
	for _, s := range f {
		if !s.empty() {
			return true
		}
	}
	return false
}

// Builtin reports whether a pack's filter hides the built-in resource
// (path relative to its namespace, with the extension, e.g. "recipe/stick.json").
func (f Filters) Builtin(namespace, path string) bool {
	for _, s := range f {
		if s.hides(namespace, path) {
			return true
		}
	}
	return false
}

// dropHidden removes from out, keyed "namespace:id", every resource of dir
// (path prefix, e.g. "recipe/") with extension ext that the pack's filter
// hides: called before the pack's own files are added.
func dropHidden[V any](out map[string]V, s filterSection, dir, ext string) {
	if s.empty() {
		return
	}
	for key := range out {
		ns, id, _ := strings.Cut(key, ":")
		if s.hides(ns, dir+id+ext) {
			delete(out, key)
		}
	}
}
