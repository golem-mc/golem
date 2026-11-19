package entity

import (
	"iter"
	"math/rand/v2"
	"slices"
	"strings"
	"sync"

	"github.com/golem-mc/golem/nbt"
)

// MemoryMapOrder fixes module identity hashes and the immutable-map iteration salt.
// Contexts are immutable and may be shared by mobs in independent simulations.
type MemoryMapOrder struct {
	hashes map[string]uint32
	salt   uint32
}

var (
	memoryModuleHashes    sync.Map
	defaultMemoryMapOrder = &MemoryMapOrder{salt: rand.Uint32()}
)

// NewMemoryMapOrder copies explicit module hashes; unspecified modules use process identities.
func NewMemoryMapOrder(hashes map[string]uint32, salt uint32) *MemoryMapOrder {
	order := &MemoryMapOrder{hashes: make(map[string]uint32, len(hashes)), salt: salt}
	for name, hash := range hashes {
		order.hashes[memoryModuleName(name)] = hash & 0x7fffffff
	}
	return order
}

func memoryModuleName(name string) string {
	if !strings.ContainsRune(name, ':') {
		return "minecraft:" + name
	}
	return name
}

func (o *MemoryMapOrder) hash(name string) uint32 {
	if hash, ok := o.hashes[name]; ok {
		return hash
	}
	if hash, ok := memoryModuleHashes.Load(name); ok {
		return hash.(uint32)
	}
	hash, _ := memoryModuleHashes.LoadOrStore(name, rand.Uint32()&0x7fffffff)
	return hash.(uint32)
}

// memoryMap is native construction history, before resource-ID/NBT encoding.
type memoryMap struct {
	order   *MemoryMapOrder
	entries map[string]nbt.Tag
	keys    []string
}

func (m *Mob) newMemoryMap() *memoryMap {
	order := m.memoryMapOrder
	if order == nil {
		order = defaultMemoryMapOrder
	}
	return &memoryMap{order: order, entries: make(map[string]nbt.Tag)}
}

func (b *memoryMap) Set(name string, value nbt.Tag) {
	name = memoryModuleName(name)
	if _, exists := b.entries[name]; !exists {
		b.order.hash(name)
		b.keys = append(b.keys, name)
	}
	b.entries[name] = value
}

func (b *memoryMap) Delete(name string) {
	name = memoryModuleName(name)
	delete(b.entries, name)
	if i := slices.Index(b.keys, name); i >= 0 {
		b.keys = slices.Delete(b.keys, i, i+1)
	}
}

func (m *Mob) cloneMemoryMap(source *memoryMap) *memoryMap {
	b := m.newMemoryMap()
	if source != nil {
		for _, name := range source.keys {
			b.Set(name, nbt.Snapshot(source.entries[name]))
		}
	}
	return b
}

func (m *Mob) loadMemoryMap(tag nbt.Tag) *memoryMap {
	b := m.newMemoryMap()
	if source, ok := tag.(nbt.CompoundTag); ok {
		for _, name := range nbt.HashOrderedKeys(source) {
			b.Set(name, nbt.Snapshot(source.Entries()[name]))
		}
	}
	return b
}

// packed walks Map.copyOf's MapN table, stepping before examining the first slot.
// Module names provide equality only; their string hashes never order this table.
// vanilla: ImmutableCollections.MapN, MapNIterator
func (b *memoryMap) packed() iter.Seq2[string, nbt.Tag] {
	return func(yield func(string, nbt.Tag) bool) {
		if b == nil {
			return
		}
		n := len(b.keys)
		if n < 2 {
			for _, name := range b.keys {
				yield(name, b.entries[name])
			}
			return
		}
		slots := make([]string, 2*n)
		for _, name := range b.keys {
			i := int(b.order.hash(name) % uint32(len(slots)))
			for slots[i] != "" {
				i = (i + 1) % len(slots)
			}
			slots[i] = name
		}
		i := int(uint64(b.order.salt) * uint64(len(slots)) >> 32)
		step := 1
		if b.order.salt&1 != 0 {
			step = -1
		}
		for remaining := n; remaining > 0; {
			i = (i + step + len(slots)) % len(slots)
			if name := slots[i]; name != "" {
				if !yield(name, b.entries[name]) {
					return
				}
				remaining--
			}
		}
	}
}

func (b *memoryMap) encode() *nbt.OrderedCompound {
	n := 0
	if b != nil {
		n = len(b.keys)
	}
	tag := nbt.NewOrderedCompound(n)
	for name, value := range b.packed() {
		tag.Set(name, value)
	}
	return tag
}
