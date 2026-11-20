package datapack

import (
	"errors"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"sync"
)

// GeneratedDir is the folder of a world that holds the structure templates a
// structure block saved. vanilla: LevelResource.GENERATED_DIR
const GeneratedDir = "generated"

// structureExt is the suffix of a structure template file.
// vanilla: StructureTemplateManager.WORLD_STRUCTURE_LISTER
const structureExt = ".nbt"

// ErrInvalidID is returned for a structure id that is no valid identifier.
var ErrInvalidID = errors.New("invalid structure id")

// StructureFile returns the template file id names; ok is false when no
// source has it. The world's generated folder comes first, then the packs in
// layer order, the later winning. An invalid id returns ErrInvalidID.
// vanilla: StructureTemplateManager.tryLoad
func StructureFile(worldDir string, packs []string, id string) (data []byte, ok bool, err error) {
	views := openViews(packs)
	return structureFile(worldDir, views, id)
}

// packView is an opened pack with its filter section.
type packView struct {
	fs     fs.FS
	filter filterSection
}

// openViews opens each pack's view; a pack that cannot be opened is left out.
func openViews(packs []string) []packView {
	views := make([]packView, 0, len(packs))
	for _, pack := range packs {
		if v, err := packFS(pack); err == nil {
			views = append(views, packView{v, readFilter(pack)})
		}
	}
	return views
}

func structureFile(worldDir string, views []packView, id string) ([]byte, bool, error) {
	ns, name, found := strings.Cut(id, ":")
	if !found || !validNamespace(ns) || !validPath(name) || slices.Contains(strings.Split(name, "/"), "..") {
		return nil, false, ErrInvalidID
	}
	if data, err := os.ReadFile(filepath.Join(worldDir, GeneratedDir, ns, "structure", filepath.FromSlash(name)+structureExt)); err == nil {
		return data, true, nil
	}
	rel := path.Join("data", ns, "structure", name+structureExt)
	for _, v := range slices.Backward(views) {
		if data, err := fs.ReadFile(v.fs, rel); err == nil {
			return data, true, nil
		}
		// A filter hides the packs below it.
		if v.filter.hides(ns, "structure/"+name+structureExt) {
			break
		}
	}
	return nil, false, nil
}

// ListStructures returns the ids of the template files in the world's
// generated folder and the packs, sorted and without duplicates.
// vanilla: StructureTemplateManager.listGenerated, listTemplates
func ListStructures(worldDir string, packs []string) []string {
	return listStructures(worldDir, openViews(packs))
}

func listStructures(worldDir string, views []packView) []string {
	seen := map[string]bool{}
	scan := func(root fs.FS) {
		nss, _ := fs.ReadDir(root, ".")
		for _, ns := range nss {
			base := path.Join(ns.Name(), "structure")
			_ = fs.WalkDir(root, base, func(file string, d fs.DirEntry, err error) error {
				if err != nil || d.IsDir() || !strings.HasSuffix(file, structureExt) {
					return nil
				}
				rel := strings.TrimPrefix(file, base+"/")
				seen[ns.Name()+":"+strings.TrimSuffix(rel, structureExt)] = true
				return nil
			})
		}
	}
	for _, v := range views {
		for id := range seen {
			ns, name, _ := strings.Cut(id, ":")
			if v.filter.hides(ns, "structure/"+name+structureExt) {
				delete(seen, id)
			}
		}
		if data, err := fs.Sub(v.fs, "data"); err == nil {
			scan(data)
		}
	}
	// The generated folder is no pack, so no filter hides it.
	scan(os.DirFS(filepath.Join(worldDir, GeneratedDir)))
	ids := make([]string, 0, len(seen))
	for id := range seen {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	return ids
}

// SaveStructure writes a template under the world's generated folder,
// creating folders; an invalid id returns ErrInvalidID.
// vanilla: StructureTemplateManager.save
func SaveStructure(worldDir, id string, data []byte) error {
	ns, name, found := strings.Cut(id, ":")
	if !found || !validNamespace(ns) || !validPath(name) || slices.Contains(strings.Split(name, "/"), "..") {
		return ErrInvalidID
	}
	path := filepath.Join(worldDir, GeneratedDir, ns, "structure", filepath.FromSlash(name)+structureExt)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	// Synced before the rename, so a power loss right after it cannot leave
	// an empty template in place of the old one.
	tmp := path + ".tmp"
	file, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	if _, err := file.Write(data); err != nil {
		file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// TemplateFiles reads and writes template files of one world folder and the
// enabled packs. It is safe for concurrent use.
type TemplateFiles struct {
	WorldDir string
	mu       sync.Mutex
	views    []packView
}

// SetPacks sets the enabled pack folders in layer order.
func (f *TemplateFiles) SetPacks(dirs []string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.views = openViews(dirs)
}

// ReadTemplate returns the file of the template id from the world or the
// enabled packs.
func (f *TemplateFiles) ReadTemplate(id string) ([]byte, bool, error) {
	f.mu.Lock()
	views := f.views
	f.mu.Unlock()
	return structureFile(f.WorldDir, views, id)
}

// ListTemplates returns the ids of every template file of the world and the
// enabled packs.
func (f *TemplateFiles) ListTemplates() []string {
	f.mu.Lock()
	views := f.views
	f.mu.Unlock()
	return listStructures(f.WorldDir, views)
}

// WriteTemplate saves the file of the template id under the world.
func (f *TemplateFiles) WriteTemplate(id string, data []byte) error {
	return SaveStructure(f.WorldDir, id, data)
}
