package world

import (
	"errors"
	"fmt"
	"log/slog"
	"sync"

	"github.com/golem-mc/golem/nbt"
)

// CommandStorages is /data's and /execute's storage (vanilla's CommandStorage):
// one file per namespace, data/<namespace>/command_storage.dat, a contents
// compound keyed by id path, loaded when a command first names the namespace
// and written by later saves.
// vanilla: CommandStorage
type CommandStorages struct {
	root string
	mu   sync.Mutex
	// files caches each namespace's storages read so far.
	files map[string]*commandStorageFile
}

// commandStorageFile is one namespace's file in memory: contents keyed by path,
// and whether a command changed it since reading.
type commandStorageFile struct {
	contents nbt.Compound
	dirty    bool
}

// NewCommandStorages holds the storages of the world under root.
func NewCommandStorages(root string) *CommandStorages {
	return &CommandStorages{root: root, files: map[string]*commandStorageFile{}}
}

// Read hands fn the live compound id names (an empty one for an unstored id)
// under the store's lock. fn must neither keep nor modify it.
// vanilla: CommandStorage.get
func (s *CommandStorages) Read(id string, fn func(storage nbt.CompoundTag)) {
	namespace, path, ok := splitID(id)
	if !ok {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	file, err := s.file(namespace)
	if err != nil {
		slog.Warn("could not read a command storage", "storage", id, "error", err)
		return
	}
	live, had := nbt.Get[nbt.CompoundTag](file.contents, path)
	if !had {
		live = &nbt.OrderedCompound{}
	}
	fn(live)
}

// Modify edits id's live compound under the store's lock. fn reports whether
// to store it and mark the namespace dirty; it must not retain the compound
// or call back into the store, and must leave it unchanged when returning false.
// vanilla: CommandStorage.get, CommandStorage.Container.put
func (s *CommandStorages) Modify(id string, fn func(live nbt.CompoundTag) (changed bool)) {
	namespace, path, ok := splitID(id)
	if !ok {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	file, err := s.file(namespace)
	if err != nil {
		slog.Warn("could not read a command storage", "storage", id, "error", err)
		return
	}
	live, had := nbt.Get[nbt.CompoundTag](file.contents, path)
	if !had {
		live = &nbt.OrderedCompound{}
	}
	if !fn(live) {
		return
	}
	if len(live.Entries()) == 0 {
		delete(file.contents, path)
	} else {
		file.contents[path] = live
	}
	file.dirty = true
}

// Set stores data under id, taking ownership of it; an empty data unstores
// the id, and a write equal to what is stored leaves the file unchanged.
// vanilla: CommandStorage.set
func (s *CommandStorages) Set(id string, data nbt.CompoundTag) {
	namespace, path, ok := splitID(id)
	if !ok {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	file, err := s.file(namespace)
	if err != nil {
		slog.Warn("could not read a command storage", "storage", id, "error", err)
		return
	}
	live, had := nbt.Get[nbt.CompoundTag](file.contents, path)
	if !had {
		live = &nbt.OrderedCompound{}
	}
	if nbt.Equal(live, data) {
		return
	}
	if len(data.Entries()) == 0 {
		delete(file.contents, path)
	} else {
		file.contents[path] = data
	}
	file.dirty = true
}

// Save writes every namespace a command changed.
func (s *CommandStorages) Save() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for namespace, file := range s.files {
		if !file.dirty {
			continue
		}
		data := nbt.Compound{"contents": file.contents}
		if err := writeSavedData(s.root, commandStoragePath(namespace), data); err != nil {
			slog.Error("could not save a command storage", "namespace", namespace, "error", err)
			continue
		}
		file.dirty = false
	}
}

// commandStoragePath is a namespace's file,
// data/<namespace>/command_storage.dat.
// vanilla: CommandStorage.Container.type
func commandStoragePath(namespace string) string {
	return fmt.Sprintf("data/%s/command_storage.dat", namespace)
}

// file reads a namespace's storages on first use and caches them.
func (s *CommandStorages) file(namespace string) (*commandStorageFile, error) {
	if file, ok := s.files[namespace]; ok {
		return file, nil
	}
	file := &commandStorageFile{contents: nbt.Compound{}}
	data, err := readSavedDataTags(s.root, commandStoragePath(namespace), MaxSavedData, true)
	if err != nil && !errors.Is(err, errNoSavedData) {
		return nil, err
	}
	// An empty saved compound has no map behind it; keep the made one.
	if contents, ok := nbt.Get[nbt.CompoundTag](data, "contents"); ok && contents.Entries() != nil {
		file.contents = contents.Entries()
	}
	s.files[namespace] = file
	return file, nil
}

// splitID splits a storage id at the colon, the namespace defaulting to
// minecraft; ok is false for a non-identifier.
func splitID(id string) (namespace, path string, ok bool) {
	namespace, path, has := cutID(id)
	if has {
		if !namespaceName(namespace) {
			return "", "", false
		}
		return namespace, path, true
	}
	// A bare word is the path, in the default namespace.
	if !resourceName(id) {
		return "", "", false
	}
	return "minecraft", id, true
}

// cutID splits at the first colon, reporting whether there was one.
func cutID(id string) (namespace, path string, has bool) {
	for i := 0; i < len(id); i++ {
		if id[i] == ':' {
			return id[:i], id[i+1:], true
		}
	}
	return "", id, false
}

// resourceName reports whether s uses only a resource location path's
// characters, slashes included.
func resourceName(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '_', r == '.', r == '-', r == '/':
		default:
			return false
		}
	}
	return true
}
