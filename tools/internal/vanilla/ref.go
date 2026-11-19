// Package vanilla reads the reference data of the Minecraft version this
// repository implements: the reports the vanilla server's data generator writes,
// and what the server jar and data pack hold. The data lives in an uncommitted
// directory (reference-server/ by default) filled by `mise run reference:setup`
// and `reference:reports`. Nothing here may import the packages the generators
// write into: after a version bump they may not compile.
package vanilla

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
)

// DefaultVersion is the Minecraft version the repository implements; a test
// compares it with protocol.GameVersion.
const DefaultVersion = "26.3"

// VersionFromEnv is $REFERENCE_VERSION, or DefaultVersion when it is unset:
// the one place the tools take a version from, so `reference setup` and `gen`
// always agree.
func VersionFromEnv() string {
	if v := os.Getenv("REFERENCE_VERSION"); v != "" {
		return v
	}
	return DefaultVersion
}

// RepoRoot is the directory holding go.mod above the working directory.
func RepoRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", errors.New("no go.mod above the working directory")
		}
		dir = parent
	}
}

// Ref locates the reference data on disk.
type Ref struct {
	// Dir is the reference directory, reference-server/ in the repository.
	Dir string
	// Version is the Minecraft version, such as "26.3".
	Version string
}

// ReportPath is a file under the data generator's reports directory.
func (r Ref) ReportPath(name string) string {
	return filepath.Join(r.Dir, "generated", "reports", name)
}

// readJSON decodes a file into v and points at the task that writes it when
// it is missing.
func (r Ref) readJSON(path string, v any) error {
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("%s is missing; run `mise run reference:setup` and `mise run reference:reports`", path)
	}
	if err != nil {
		return err
	}
	if err := json.Unmarshal(b, v); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	return nil
}

// Entry is one entry of a registry in the registries report.
type Entry struct {
	ProtocolID int `json:"protocol_id"`
}

// Registry is one registry of the registries report.
type Registry struct {
	Entries Ordered[Entry] `json:"entries"`
}

// Registries is registries.json: every built-in registry by name, such as
// "minecraft:item".
type Registries map[string]Registry

// Registries reads registries.json.
func (r Ref) Registries() (Registries, error) {
	var rs Registries
	if err := r.readJSON(r.ReportPath("registries.json"), &rs); err != nil {
		return nil, err
	}
	return rs, nil
}

// NamesByID lists a registry's entries by protocol ID. The IDs of a registry
// run from zero without gaps, so the position of a name is its ID; anything
// else is an error rather than a table with a hole in it.
func (rs Registries) NamesByID(registry string) ([]string, error) {
	reg, ok := rs[registry]
	if !ok {
		return nil, fmt.Errorf("the registries report has no %s", registry)
	}
	names := slices.Clone(reg.Entries.Keys())
	slices.SortStableFunc(names, func(a, b string) int {
		ea, _ := reg.Entries.Get(a)
		eb, _ := reg.Entries.Get(b)
		return ea.ProtocolID - eb.ProtocolID
	})
	for want, name := range names {
		if e, _ := reg.Entries.Get(name); e.ProtocolID != want {
			return nil, fmt.Errorf("%s IDs are not contiguous: %s is %d, expected %d", registry, name, e.ProtocolID, want)
		}
	}
	return names, nil
}

// BlockState is one state of a block in the block report.
type BlockState struct {
	ID         int               `json:"id"`
	Default    bool              `json:"default"`
	Properties map[string]string `json:"properties"`
}

// BlockDefinition is the block report's account of a block's class: the type
// names the BlockBehaviour it uses, and a few types carry fields of their own.
type BlockDefinition struct {
	Type string `json:"type"`
	// Concrete is the block a concrete powder turns into in water.
	Concrete *string `json:"concrete"`
}

// Block is one block of the block report.
type Block struct {
	Definition BlockDefinition `json:"definition"`
	// Properties lists the values each property can take; it is empty for a
	// block with none.
	Properties map[string]json.RawMessage `json:"properties"`
	States     []BlockState               `json:"states"`
}

// Blocks is blocks.json: every block by name, in the report's order.
type Blocks = Ordered[Block]

// Blocks reads blocks.json.
func (r Ref) Blocks() (Blocks, error) {
	var bs Blocks
	if err := r.readJSON(r.ReportPath("blocks.json"), &bs); err != nil {
		return Blocks{}, err
	}
	return bs, nil
}
