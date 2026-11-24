package datapack

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sync"
)

// Source says where a pack comes from.
// vanilla: PackSource
type Source int

const (
	// SourceBuiltIn is the vanilla pack.
	SourceBuiltIn Source = iota
	// SourceFeature is a pack bundled with the game that a feature flag
	// gates; it is never selected automatically.
	SourceFeature
	// SourceWorld is a folder under <world>/datapacks.
	SourceWorld
)

// Pack is one data pack the repository knows.
type Pack struct {
	// ID is "vanilla", a bundled pack's name, or "file/<folder>".
	ID     string
	Source Source
	// Title and Description are literal text, or translation keys when the
	// matching Key flag is set.
	Title          string
	TitleKey       bool
	Description    json.RawMessage
	DescriptionKey bool
	// Flags are the feature flags the pack needs enabled.
	Flags []string
	// Dir is the pack folder, empty for a bundled pack.
	Dir string
}

// bundled lists the packs the game carries, in id order.
// vanilla: ServerPacksSource, FeatureFlags
var bundled = []Pack{
	{ID: "minecart_improvements", Source: SourceFeature, Title: "dataPack.minecart_improvements.name", TitleKey: true,
		Description: json.RawMessage(`"dataPack.minecart_improvements.description"`), DescriptionKey: true,
		Flags: []string{"minecraft:minecart_improvements"}},
	{ID: "redstone_experiments", Source: SourceFeature, Title: "dataPack.redstone_experiments.name", TitleKey: true,
		Description: json.RawMessage(`"dataPack.redstone_experiments.description"`), DescriptionKey: true,
		Flags: []string{"minecraft:redstone_experiments"}},
	{ID: "trade_rebalance", Source: SourceFeature, Title: "dataPack.trade_rebalance.name", TitleKey: true,
		Description: json.RawMessage(`"dataPack.trade_rebalance.description"`), DescriptionKey: true,
		Flags: []string{"minecraft:trade_rebalance"}},
	{ID: "vanilla", Source: SourceBuiltIn, Title: "dataPack.vanilla.name", TitleKey: true,
		Description: json.RawMessage(`"dataPack.vanilla.description"`), DescriptionKey: true},
}

// Repository is a world's available packs and their ordered selection, a
// later pack overriding an earlier one. Safe for concurrent use.
// vanilla: PackRepository
type Repository struct {
	worldDir string
	skip     func(file string, err error)
	// mu guards available and selected: /datapack list reads them on a
	// command goroutine while a reload on another rewrites them.
	mu        sync.RWMutex
	available []Pack
	selected  []string
}

// Open scans the world's packs and selects level.dat's enabled ones, then
// every auto-added pack the disabled list does not name; none selects
// vanilla.
// vanilla: MinecraftServer.configurePackRepository
func Open(worldDir string, enabled, disabled []string, skip func(file string, err error)) (*Repository, error) {
	r, err := OpenOnly(worldDir, enabled, skip)
	if err != nil {
		return nil, err
	}
	selected := r.selected
	for _, p := range r.available {
		if p.Source == SourceFeature || slices.Contains(disabled, p.ID) || slices.Contains(selected, p.ID) {
			continue
		}
		selected = append(selected, p.ID)
	}
	if len(selected) == 0 {
		selected = []string{"vanilla"}
	}
	r.selected = selected
	return r, nil
}

// OpenOnly scans the world's packs and selects those of ids that exist,
// adding none automatically: safe mode's selection.
// vanilla: MinecraftServer.configureRepositoryWithSelection
func OpenOnly(worldDir string, ids []string, skip func(file string, err error)) (*Repository, error) {
	r := &Repository{worldDir: worldDir, skip: skip}
	if err := r.scan(); err != nil {
		return nil, err
	}
	for _, id := range ids {
		if _, ok := r.packLocked(id); ok && !slices.Contains(r.selected, id) {
			r.selected = append(r.selected, id)
		}
	}
	return r, nil
}

// scan lists the packs again.
func (r *Repository) scan() error {
	dirs, err := folderPacks(r.worldDir, r.skip)
	if err != nil {
		return err
	}
	available := slices.Clone(bundled)
	for _, dir := range dirs {
		name := filepath.Base(dir)
		available = append(available, Pack{
			ID: "file/" + name, Source: SourceWorld, Title: name, Dir: dir,
			Description: readDescription(dir),
		})
	}
	slices.SortFunc(available, func(a, b Pack) int {
		switch {
		case a.ID < b.ID:
			return -1
		case a.ID > b.ID:
			return 1
		}
		return 0
	})
	r.available = available
	return nil
}

// readDescription is the pack.description of a pack.mcmeta as JSON text, an
// empty string for one that has none.
func readDescription(dir string) json.RawMessage {
	fsys, err := packFS(dir)
	if err != nil {
		return json.RawMessage(`""`)
	}
	data, err := fs.ReadFile(fsys, "pack.mcmeta")
	if err != nil {
		return json.RawMessage(`""`)
	}
	var meta struct {
		Pack struct {
			Description json.RawMessage `json:"description"`
		} `json:"pack"`
	}
	if json.Unmarshal(data, &meta) != nil || len(meta.Pack.Description) == 0 {
		return json.RawMessage(`""`)
	}
	return meta.Pack.Description
}

// Reload scans the packs again and drops selected packs that are gone.
// vanilla: PackRepository.reload
func (r *Repository) Reload() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.scan(); err != nil {
		return err
	}
	r.selected = slices.DeleteFunc(r.selected, func(id string) bool {
		_, ok := r.packLocked(id)
		return !ok
	})
	return nil
}

// Pack finds a pack by id.
func (r *Repository) Pack(id string) (Pack, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.packLocked(id)
}

// packLocked is Pack with r.mu held.
func (r *Repository) packLocked(id string) (Pack, bool) {
	for _, p := range r.available {
		if p.ID == id {
			return p, true
		}
	}
	return Pack{}, false
}

// Available is every known pack in id order.
func (r *Repository) Available() []Pack {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return slices.Clone(r.available)
}

// SelectedIDs is the ids of the selected packs in layer order.
func (r *Repository) SelectedIDs() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return slices.Clone(r.selected)
}

// Selected is the selected packs in layer order.
func (r *Repository) Selected() []Pack {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.selectedLocked()
}

// selectedLocked is Selected with r.mu held.
func (r *Repository) selectedLocked() []Pack {
	out := make([]Pack, 0, len(r.selected))
	for _, id := range r.selected {
		p, _ := r.packLocked(id)
		out = append(out, p)
	}
	return out
}

// IsSelected reports whether the pack is selected.
func (r *Repository) IsSelected(id string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return slices.Contains(r.selected, id)
}

// SetSelected replaces the selection with the ids that name a known pack.
// vanilla: PackRepository.setSelected
func (r *Repository) SetSelected(ids []string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.selected = r.selected[:0:0]
	for _, id := range ids {
		if _, ok := r.packLocked(id); ok {
			r.selected = append(r.selected, id)
		}
	}
}

// Config is level.dat's DataPacks: the selected ids, every other known pack
// disabled.
// vanilla: MinecraftServer.configureRepositoryWithSelection
func (r *Repository) Config() (enabled, disabled []string) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, p := range r.available {
		if !slices.Contains(r.selected, p.ID) {
			disabled = append(disabled, p.ID)
		}
	}
	return slices.Clone(r.selected), disabled
}

// Dirs is the folders of the selected packs in layer order.
func (r *Repository) Dirs() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.dirsLocked(r.selected)
}

// DirsFor gives the pack folders a candidate selection would load.
func (r *Repository) DirsFor(ids []string) []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.dirsLocked(ids)
}

func (r *Repository) dirsLocked(ids []string) []string {
	var dirs []string
	for _, id := range ids {
		if p, ok := r.packLocked(id); ok && p.Dir != "" {
			dirs = append(dirs, p.Dir)
		}
	}
	return dirs
}

// Create makes <world>/datapacks/<name> with a pack.mcmeta holding the
// description (a JSON text component) and an empty data folder.
// vanilla: DataPackCommand.createPack
func (r *Repository) Create(name string, description json.RawMessage) error {
	root := filepath.Join(r.worldDir, Dir)
	pack := filepath.Join(root, name)
	var minFormat any = PackFormat
	if PackMinorFormat != 0 {
		minFormat = []int{PackFormat, PackMinorFormat}
	}
	meta, err := json.MarshalIndent(struct {
		Pack packMeta `json:"pack"`
	}{packMeta{description, minFormat, PackFormat}}, "", "  ")
	if err != nil {
		return fmt.Errorf("encode pack.mcmeta: %w", err)
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return err
	}
	if err := os.Mkdir(pack, 0o755); err != nil {
		return err
	}
	if err := os.Mkdir(filepath.Join(pack, "data"), 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(pack, "pack.mcmeta"), meta, 0o644)
}

// PackFormat is this version's data pack format (min_format, max_format).
// vanilla: SharedConstants.DATA_PACK_FORMAT_MAJOR
const PackFormat = 121

// PackMinorFormat is the format's minor version; a non-zero minor makes the
// compact codec write min_format as [major, minor].
// vanilla: SharedConstants.DATA_PACK_FORMAT_MINOR, PackFormat.codec
const PackMinorFormat = 0

// packMeta is the pack section of a pack.mcmeta.
// vanilla: PackMetadataSection.SERVER_TYPE
type packMeta struct {
	Description json.RawMessage `json:"description"`
	MinFormat   any             `json:"min_format"`
	MaxFormat   int             `json:"max_format"`
}

// Exists reports whether <world>/datapacks/<name> exists.
func (r *Repository) Exists(name string) bool {
	_, err := os.Lstat(filepath.Join(r.worldDir, Dir, name))
	return err == nil
}

var (
	strictSegment   = regexp.MustCompile(`^[-._a-z0-9]+$`)
	reservedWindows = regexp.MustCompile(`(?i)^(?:.*\.|(?:COM|CLOCK\$|CON|PRN|AUX|NUL|COM[1-9]|LPT[1-9])(?:\..*)?)$`)
)

// ValidName reports whether name is a legal pack folder name.
// vanilla: FileUtil.isValidPathSegment
func ValidName(name string) bool {
	return name != "." && name != ".." && strictSegment.MatchString(name)
}

// PortableName reports whether name is no file name Windows reserves.
// vanilla: FileUtil.isPathPartPortable
func PortableName(name string) bool { return !reservedWindows.MatchString(name) }
