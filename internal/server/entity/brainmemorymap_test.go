package entity

import (
	"slices"
	"testing"

	"github.com/golem-mc/golem/nbt"
)

func TestMemoryMapTraversal(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name         string
		salt         uint32
		hashes       map[string]uint32
		source, want []string
	}{
		{"empty", 0, nil, nil, nil},
		{"singleton", 0xffffffff, map[string]uint32{"home": 7}, []string{"home"}, []string{"home"}},
		{"forward", 0, map[string]uint32{"home": 2, "meeting_point": 1}, []string{"home", "meeting_point"}, []string{"meeting_point", "home"}},
		{"backward", 0xffffffff, map[string]uint32{"home": 2, "meeting_point": 1}, []string{"home", "meeting_point"}, []string{"home", "meeting_point"}},
		{"collision", 0, map[string]uint32{"home": 1, "meeting_point": 1, "last_slept": 1}, []string{"home", "meeting_point", "last_slept"}, []string{"home", "meeting_point", "last_slept"}},
		{"wrap forward", 0, map[string]uint32{"home": 5, "meeting_point": 5, "last_slept": 5}, []string{"home", "meeting_point", "last_slept"}, []string{"last_slept", "home", "meeting_point"}},
		{"wrap backward", 0xffffffff, map[string]uint32{"home": 5, "meeting_point": 5, "last_slept": 5}, []string{"home", "meeting_point", "last_slept"}, []string{"last_slept", "meeting_point", "home"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := &Mob{memoryMapOrder: NewMemoryMapOrder(tc.hashes, tc.salt)}
			b := m.newMemoryMap()
			for i, name := range tc.source {
				b.Set(name, nbt.Int(i))
			}
			assertMemoryTraversal(t, b, tc.want)
			for i, name := range tc.source {
				if b.entries["minecraft:"+name] != nbt.Int(i) {
					t.Fatalf("module equality lost %s", name)
				}
			}
		})
	}
	m := &Mob{memoryMapOrder: NewMemoryMapOrder(map[string]uint32{"home": 1, "meeting_point": 7, "last_slept": 2}, 0xffffffff)}
	b := m.newMemoryMap()
	for _, name := range []string{"home", "meeting_point", "last_slept"} {
		b.Set(name, memoryTag(nbt.Long(1)))
	}
	assertMemoryTraversal(t, b, []string{"last_slept", "meeting_point", "home"})
	assertEncodedMemoryOrder(t, b, []string{"meeting_point", "home", "last_slept"})
	loaded := m.loadMemoryMap(b.encode())
	if !slices.Equal(loaded.keys, []string{"minecraft:meeting_point", "minecraft:home", "minecraft:last_slept"}) {
		t.Fatalf("ordered load history=%v", loaded.keys)
	}
	legacy := m.loadMemoryMap(b.encode().Entries())
	if !slices.Equal(legacy.keys, []string{"minecraft:home", "minecraft:meeting_point", "minecraft:last_slept"}) {
		t.Fatalf("legacy load history=%v", legacy.keys)
	}
	rebuilt := m.newMemoryMap()
	for name, value := range b.packed() {
		rebuilt.Set(name, value)
	}
	assertMemoryTraversal(t, rebuilt, []string{"home", "last_slept", "meeting_point"})
	assertEncodedMemoryOrder(t, rebuilt, []string{"home", "meeting_point", "last_slept"})
	b.Set("minecraft:home", memoryTag(nbt.Long(2)))
	if b.entries["minecraft:home"].(nbt.CompoundTag).Entries()["value"] != nbt.Long(2) {
		t.Fatal("overwrite lost the new value")
	}
	assertMemoryTraversal(t, b, []string{"last_slept", "meeting_point", "home"})
	b.Delete("meeting_point")
	assertMemoryTraversal(t, b, []string{"last_slept", "home"})
	b.Set("meeting_point", memoryTag(nbt.Long(3)))
	assertMemoryTraversal(t, b, []string{"meeting_point", "last_slept", "home"})
	if !slices.Equal(b.keys, []string{"minecraft:home", "minecraft:last_slept", "minecraft:meeting_point"}) {
		t.Fatalf("reinsertion did not append: %v", b.keys)
	}
	if got := nbt.HashOrderedKeys((*memoryMap)(nil).encode()); len(got) != 0 {
		t.Fatalf("nil map encoded as %v", got)
	}
}

func assertMemoryTraversal(t *testing.T, b *memoryMap, want []string) {
	t.Helper()
	var got []string
	for name := range b.packed() {
		got = append(got, name[len("minecraft:"):])
	}
	if !slices.Equal(got, want) {
		t.Fatalf("traversal=%v, want %v", got, want)
	}
}

func assertEncodedMemoryOrder(t *testing.T, b *memoryMap, want []string) {
	t.Helper()
	var got []string
	for _, name := range nbt.HashOrderedKeys(b.encode()) {
		got = append(got, name[len("minecraft:"):])
	}
	if !slices.Equal(got, want) {
		t.Fatalf("encoded=%v, want %v", got, want)
	}
}
