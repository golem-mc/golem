package advancement

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"time"
)

// timeLayout is a criterion's obtained time in the file.
// vanilla: AdvancementProgress.OBTAINED_TIME_FORMAT, "yyyy-MM-dd HH:mm:ss Z"
const timeLayout = "2006-01-02 15:04:05 -0700"

// dataVersionKey is the world version stamp DataFixTypes.wrapCodec adds.
const dataVersionKey = "DataVersion"

// savedProgress is one advancement's entry in the file.
type savedProgress struct {
	Criteria map[string]string `json:"criteria,omitempty"`
	Done     bool              `json:"done"`
}

// Parse reads a progress file. An entry for an advancement the tree lacks is
// kept for the save, unlike vanilla, because the tree leaves out the
// recipe-unlock advancements vanilla files are full of. A criterion the
// advancement lacks, or with an unparsable time, is dropped.
// vanilla: PlayerAdvancements.load, applyFrom
func Parse(tree *Tree, data []byte) (*Progress, error) {
	var root map[string]json.RawMessage
	if err := json.Unmarshal(data, &root); err != nil {
		return NewProgress(tree), err
	}
	p := NewProgress(tree)
	for id, raw := range root {
		if id == dataVersionKey {
			continue
		}
		n := tree.Get(id)
		if n == nil {
			if p.unknown == nil {
				p.unknown = map[string][]byte{}
			}
			p.unknown[id] = raw
			continue
		}
		var saved savedProgress
		if json.Unmarshal(raw, &saved) != nil {
			continue
		}
		for name, when := range saved.Criteria {
			at, err := time.Parse(timeLayout, when)
			if err != nil || !hasCriterion(n, name) {
				continue
			}
			if p.obtained[n.index] == nil {
				p.obtained[n.index] = map[string]time.Time{}
			}
			p.obtained[n.index][name] = at
		}
		p.changed[n] = struct{}{}
		p.rootsToUpdate[n.Root()] = struct{}{}
	}
	return p, nil
}

// Load reads a progress file. A missing file reads as no progress with no
// error; a damaged one as no progress and the error.
func Load(tree *Tree, path string) (*Progress, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return NewProgress(tree), nil
	}
	if err != nil {
		return NewProgress(tree), err
	}
	p, err := Parse(tree, data)
	if err != nil {
		return p, fmt.Errorf("parse %s: %w", path, err)
	}
	return p, nil
}

// Reload rebuilds the progress over tree, keeping what is obtained of the
// advancements tree still has and, as Parse does, the saved entries of the
// others. The next Flush resets the client's tree and sends it afresh, and
// no tab is selected.
// vanilla: PlayerAdvancements.reload
func (p *Progress) Reload(tree *Tree) {
	data, err := p.Encode(0)
	if err != nil {
		return
	}
	fresh, err := Parse(tree, data)
	if err != nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.tree, p.obtained, p.visible = fresh.tree, fresh.obtained, fresh.visible
	p.changed, p.rootsToUpdate, p.unknown = fresh.changed, fresh.rootsToUpdate, fresh.unknown
	p.first, p.selectedTab = true, nil
}

// Tree is the tree the progress is over.
func (p *Progress) Tree() *Tree {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.tree
}

// Encode returns the progress file stamped with dataVersion: every
// advancement with a criterion obtained.
// vanilla: PlayerAdvancements.save, asData
func (p *Progress) Encode(dataVersion int32) ([]byte, error) {
	p.mu.Lock()
	entries := make(map[string]json.RawMessage, len(p.unknown))
	for id, raw := range p.unknown {
		entries[id] = raw
	}
	for _, n := range p.tree.nodes {
		got := p.obtained[n.index]
		if len(got) == 0 {
			continue
		}
		saved := savedProgress{Criteria: map[string]string{}, Done: p.isDone(n)}
		for name, at := range got {
			saved.Criteria[name] = at.Format(timeLayout)
		}
		raw, err := json.Marshal(saved)
		if err != nil {
			p.mu.Unlock()
			return nil, err
		}
		entries[n.ID] = raw
	}
	p.mu.Unlock()

	// Gson's pretty printing, two spaces deep, with the version stamp last.
	ids := make([]string, 0, len(entries))
	for id := range entries {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	var b bytes.Buffer
	b.WriteString("{\n")
	for _, id := range ids {
		key, _ := json.Marshal(id)
		var value bytes.Buffer
		if err := json.Indent(&value, entries[id], "  ", "  "); err != nil {
			return nil, err
		}
		fmt.Fprintf(&b, "  %s: %s,\n", key, value.Bytes())
	}
	fmt.Fprintf(&b, "  %q: %d\n}", dataVersionKey, dataVersion)
	return b.Bytes(), nil
}

// Save writes through a synced temp file renamed into place, so a crash or
// power loss leaves either the old file or the new one whole.
func (p *Progress) Save(path string, dataVersion int32) error {
	data, err := p.Encode(dataVersion)
	if err != nil {
		return err
	}
	return WriteFile(path, data)
}

// WriteFile writes encoded advancement progress the way Save does.
func WriteFile(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	temp := path + ".tmp"
	file, err := os.OpenFile(temp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	if _, err := file.Write(data); err != nil {
		file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return fmt.Errorf("sync %s: %w", temp, err)
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(temp, path)
}
