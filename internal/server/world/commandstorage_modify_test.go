package world

import (
	"testing"

	"github.com/golem-mc/golem/nbt"
)

func TestCommandStorageModifyPersistsPathEdits(t *testing.T) {
	root := t.TempDir()
	storage := NewCommandStorages(root)
	storage.Modify("test:created", func(live nbt.CompoundTag) bool {
		if _, ok := live.(*nbt.OrderedCompound); !ok {
			t.Fatalf("new storage = %T, want ordered compound", live)
		}
		live.Set("v", nbt.Int(1))
		return true
	})
	storage.Save()
	storage = NewCommandStorages(root)
	storage.Modify("test:created", func(live nbt.CompoundTag) bool {
		if got := live.Entries()["v"]; got != nbt.Int(1) {
			t.Fatalf("loaded value = %v, want 1", got)
		}
		live.Set("v", nbt.Int(2))
		return true
	})
	storage.Save()
	storage = NewCommandStorages(root)
	storage.Read("test:created", func(live nbt.CompoundTag) {
		if got := live.Entries()["v"]; got != nbt.Int(2) {
			t.Errorf("saved edit = %v, want 2", got)
		}
	})
	storage.Modify("test:created", func(nbt.CompoundTag) bool { return false })
	storage.Modify("test:absent", func(nbt.CompoundTag) bool { return false })
	if file := storage.files["test"]; file.dirty || len(file.contents) != 1 {
		t.Fatalf("unchanged edit: dirty=%v, contents=%v", file.dirty, file.contents)
	}
	storage.Modify("test:created", func(live nbt.CompoundTag) bool {
		live.Delete("v")
		return true
	})
	storage.Save()
	storage = NewCommandStorages(root)
	storage.Read("test:created", func(live nbt.CompoundTag) {
		if len(live.Entries()) != 0 {
			t.Errorf("deleted storage = %v", live)
		}
	})
	if len(storage.files["test"].contents) != 0 {
		t.Error("empty storage path was retained")
	}
}

// A namespace saved after its last entry was removed reloads with empty
// contents, which the next write must still be able to fill.
func TestCommandStorageWritesAfterReloadingAnEmptyNamespace(t *testing.T) {
	root := t.TempDir()
	storage := NewCommandStorages(root)
	storage.Modify("test:gone", func(live nbt.CompoundTag) bool { live.Set("v", nbt.Int(1)); return true })
	storage.Save()
	storage.Modify("test:gone", func(live nbt.CompoundTag) bool { live.Delete("v"); return true })
	storage.Save()

	storage = NewCommandStorages(root)
	storage.Modify("test:new", func(live nbt.CompoundTag) bool { live.Set("v", nbt.Int(2)); return true })
	storage.Set("test:set", nbt.Compound{"v": nbt.Int(3)})
	storage.Save()
	storage = NewCommandStorages(root)
	for id, want := range map[string]nbt.Int{"test:new": 2, "test:set": 3} {
		storage.Read(id, func(live nbt.CompoundTag) {
			if got := live.Entries()["v"]; got != want {
				t.Errorf("%s = %v, want %d", id, got, want)
			}
		})
	}
}
