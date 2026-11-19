package world

import (
	"log/slog"
	"slices"
	"sync"
)

// TemplateFiles is where a TemplateStore reads and writes template files.
type TemplateFiles interface {
	// ReadTemplate returns the file of the template id; ok is false when none
	// exists.
	ReadTemplate(id string) (data []byte, ok bool, err error)
	// ListTemplates returns the ids of every template file.
	ListTemplates() []string
	// WriteTemplate stores the file of the template id.
	WriteTemplate(id string, data []byte) error
}

// TemplateStore is the template cache /place and the structure blocks share:
// a template is read from its file once, and a save replaces the cached one.
// vanilla: StructureTemplateManager.structureRepository
type TemplateStore struct {
	files       TemplateFiles
	dataVersion int32
	mu          sync.Mutex
	m           map[string]*Template
}

// NewTemplateStore returns an empty store over files; saved templates carry
// dataVersion.
func NewTemplateStore(files TemplateFiles, dataVersion int32) *TemplateStore {
	return &TemplateStore{files: files, dataVersion: dataVersion, m: map[string]*Template{}}
}

// Get returns the cached template, trying world and pack files before embedded templates.
// vanilla: StructureTemplateManager.get
func (s *TemplateStore) Get(id string) (*Template, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if t, ok := s.m[id]; ok {
		return t, true
	}
	data, ok, err := s.files.ReadTemplate(id)
	if err == nil && ok {
		var t *Template
		t, err = ParseTemplate(data)
		if err == nil {
			s.m[id] = t
			return t, true
		}
		slog.Warn("Couldn't load structure " + id + ": " + err.Error())
	}
	t, ok := builtinTemplate(id)
	if !ok {
		return nil, false
	}
	s.m[id] = t
	return t, true
}

// List returns the ids of the template files and the cached templates, plus
// the templates the server jar carries, sorted.
// vanilla: StructureTemplateManager.listTemplates
func (s *TemplateStore) List(builtin map[string]bool) []string {
	seen := map[string]bool{}
	for _, id := range s.files.ListTemplates() {
		seen[id] = true
	}
	for id := range builtin {
		seen[id] = true
	}
	s.mu.Lock()
	for id := range s.m {
		seen[id] = true
	}
	s.mu.Unlock()
	ids := make([]string, 0, len(seen))
	for id := range seen {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	return ids
}

// Put caches t under id without writing a file.
func (s *TemplateStore) Put(id string, t *Template) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[id] = t
}

// Remove forgets the cached template of id.
// vanilla: StructureTemplateManager.remove
func (s *TemplateStore) Remove(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.m, id)
}

// Clear forgets every cached template, so the next Get reads the files again.
// vanilla: StructureTemplateManager.onResourceManagerReload
func (s *TemplateStore) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	clear(s.m)
}

// Save writes the cached template of id to its file. It is false when the id
// has no cached template, no file can carry the id or the write fails.
// vanilla: StructureTemplateManager.save
func (s *TemplateStore) Save(id string) bool {
	s.mu.Lock()
	t, ok := s.m[id]
	s.mu.Unlock()
	if !ok {
		return false
	}
	data, err := t.Encode(s.dataVersion)
	if err == nil {
		err = s.files.WriteTemplate(id, data)
	}
	if err != nil {
		slog.Warn("Couldn't save structure " + id + ": " + err.Error())
		return false
	}
	return true
}
