package nbt

import (
	"maps"
	"slices"
)

// CompoundTag is a compound with mutable named entries. Compound supports
// map literals; OrderedCompound also retains collision order for chat.
type CompoundTag interface {
	Tag
	Entries() Compound
	Set(string, Tag)
	Delete(string)
}

func (c Compound) Entries() Compound       { return c }
func (c Compound) Set(key string, tag Tag) { c[key] = tag }
func (c Compound) Delete(key string)       { delete(c, key) }

// OrderedCompound retains insertion order within Java HashMap buckets and
// its capacity after removals. Mutate it through Set and Delete, not Entries.
type OrderedCompound struct {
	values   Compound
	keys     []string
	capacity int
}

// NewOrderedCompound reserves Go storage without changing the emulated Java table capacity.
func NewOrderedCompound(sizeHint int) *OrderedCompound {
	return &OrderedCompound{
		values:   make(Compound, sizeHint),
		keys:     make([]string, 0, sizeHint),
		capacity: 16,
	}
}

func (*OrderedCompound) Type() Type          { return TypeCompound }
func (c *OrderedCompound) Entries() Compound { return c.values }

func (c *OrderedCompound) Set(key string, tag Tag) {
	if c.values == nil {
		c.values = Compound{}
		c.capacity = max(16, c.capacity)
	}
	if _, exists := c.values[key]; !exists {
		c.keys = append(c.keys, key)
	}
	c.values[key] = tag
	for len(c.values) > c.capacity-c.capacity/4 {
		c.capacity *= 2
	}
}

// Clone copies the entries, their order and the retained capacity, sharing
// the children.
func (c *OrderedCompound) Clone() *OrderedCompound {
	return &OrderedCompound{values: maps.Clone(c.values), keys: slices.Clone(c.keys), capacity: c.capacity}
}

func (c *OrderedCompound) Delete(key string) {
	delete(c.values, key)
	if i := slices.Index(c.keys, key); i >= 0 {
		c.keys = slices.Delete(c.keys, i, i+1)
	}
}

// PlainCopy strips ordering recursively at boundaries whose consumers still
// require map compounds, without sharing mutable state with the source.
func PlainCopy(tag Tag) Tag {
	switch v := tag.(type) {
	case CompoundTag:
		out := make(Compound, len(v.Entries()))
		for key, child := range v.Entries() {
			out[key] = PlainCopy(child)
		}
		return out
	case List:
		out := List{Elem: v.Elem, Items: make([]Tag, len(v.Items))}
		for i, item := range v.Items {
			out.Items[i] = PlainCopy(item)
		}
		return out
	default:
		return Copy(tag)
	}
}

// CompoundView borrows a compound's entries without stripping its children. Use
// the original CompoundTag for mutations that add or remove keys.
func CompoundView(tag Tag) (Compound, bool) {
	compound, ok := tag.(CompoundTag)
	if !ok {
		return nil, false
	}
	return compound.Entries(), true
}

// ShallowCopyCompound copies key history while borrowing children; nil becomes empty.
func ShallowCopyCompound(tag CompoundTag) *OrderedCompound {
	if tag == nil {
		return NewOrderedCompound(0)
	}
	out := NewOrderedCompound(len(tag.Entries()))
	for _, key := range HashOrderedKeys(tag) {
		out.Set(key, tag.Entries()[key])
	}
	return out
}
