package protocol

import (
	"maps"
	"slices"

	"github.com/golem-mc/golem/nbt"
)

// A filled map's components. map_post_processing is held raw but written
// here for the cartography table's results; map_id is raw too, since what
// it names lives in the world's files, so only its VarInt is read and set.

// The minecraft:map_post_processing values (MapPostProcessing ordinals):
// zoom out or lock on the map data's next update.
// vanilla: MapPostProcessing
const (
	MapPostProcessingLock  = 0
	MapPostProcessingScale = 1
)

// SetMapPostProcessing lays the minecraft:map_post_processing component on
// the stack, storing lock or scale as the value its codec carries: one
// VarInt.
func (s *ItemStack) SetMapPostProcessing(op int32) {
	var value Buffer
	value.PutVarInt(op)
	// A copied stack — the cartography result's copy of its input map —
	// shares the Raw backing array; the write below must not reach the
	// stack it came from.
	s.Raw = slices.Clone(s.Raw)
	s.putRaw(RawComponent{Type: DataComponentMapPostProcessing, Data: value.Bytes()})
}

// RemoveMapPostProcessing takes the minecraft:map_post_processing component
// back off the stack: the mark vanilla strips from a map through
// MapItem.onCraftedBy once it leaves the cartography table's result slot.
func (s *ItemStack) RemoveMapPostProcessing() {
	s.dropRaw(DataComponentMapPostProcessing)
}

// MapPostProcessingOf returns the stack's minecraft:map_post_processing
// value and whether it carries one.
func (s ItemStack) MapPostProcessingOf() (int32, bool) {
	for _, raw := range s.Raw {
		if raw.Type != DataComponentMapPostProcessing {
			continue
		}
		r := NewReader(raw.Data)
		op := r.ReadVarInt()
		return op, r.Err() == nil && r.Remaining() == 0
	}
	return 0, false
}

// readMapPostProcessing checks a map_post_processing value: any VarInt, out
// of bounds read as lock.
// vanilla: MapPostProcessing.STREAM_CODEC, ByIdMap.OutOfBoundsStrategy.ZERO
func readMapPostProcessing(r *Reader) bool {
	r.ReadVarInt()
	return r.Err() == nil
}

// readMapID checks a map_id value: any VarInt.
// vanilla: MapId.STREAM_CODEC
func readMapID(r *Reader) bool {
	r.ReadVarInt()
	return r.Err() == nil
}

// SetMapID sets minecraft:map_id, the id the map's saved data is read by.
// vanilla: MapId.STREAM_CODEC
func (s *ItemStack) SetMapID(id int32) {
	var value Buffer
	value.PutVarInt(id)
	// A copied stack — a take's copy of its input map — shares the Raw
	// backing array; the write below must not reach the stack it came from.
	s.Raw = slices.Clone(s.Raw)
	s.putRaw(RawComponent{Type: DataComponentMapID, Data: value.Bytes()})
}

// MapIDOf returns the id of the saved map the stack's minecraft:map_id names
// and whether it carries one.
func (s ItemStack) MapIDOf() (int32, bool) {
	for _, raw := range s.Raw {
		if raw.Type != DataComponentMapID {
			continue
		}
		r := NewReader(raw.Data)
		id := r.ReadVarInt()
		if r.Err() != nil || r.Remaining() != 0 {
			return 0, false
		}
		return id, true
	}
	return 0, false
}

// MapDecoration is a target marker in minecraft:map_decorations.
// vanilla: MapDecorations.Entry
type MapDecoration struct {
	Type     string
	X, Z     float64
	Rotation float32
}

// MapDecorationKnown validates marker registry names; registry installs it.
var MapDecorationKnown func(string) bool

func mapDecorationsTag(entries map[string]MapDecoration, keys []string) *nbt.OrderedCompound {
	out := nbt.NewOrderedCompound(len(entries))
	for _, key := range keys {
		d := entries[key]
		entry := nbt.NewOrderedCompound(4)
		entry.Set("type", nbt.String(d.Type))
		entry.Set("x", nbt.Double(d.X))
		entry.Set("z", nbt.Double(d.Z))
		entry.Set("rotation", nbt.Float(d.Rotation))
		out.Set(key, entry)
	}
	return out
}

// MapDecorationsOf reads MapDecorations.CODEC's map of named entries.
func MapDecorationsOf(value nbt.Tag) (map[string]MapDecoration, bool) {
	entries, ok := nbt.CompoundView(value)
	if !ok {
		return nil, false
	}
	decorations := make(map[string]MapDecoration, len(entries))
	for key, entry := range entries {
		d, ok := nbt.CompoundView(entry)
		if !ok {
			return nil, false
		}
		name, ok := nbt.Get[nbt.String](d, "type")
		if !ok || MapDecorationKnown == nil || !MapDecorationKnown(string(name)) {
			return nil, false
		}
		_, xok := nbt.FloatField(d, "x")
		_, zok := nbt.FloatField(d, "z")
		rot, rok := nbt.FloatField(d, "rotation")
		if !xok || !zok || !rok {
			return nil, false
		}
		decorations[key] = MapDecoration{Type: string(name), X: nbt.DoubleFieldOr(d, "x", 0), Z: nbt.DoubleFieldOr(d, "z", 0), Rotation: rot}
	}
	return decorations, true
}

// SetMapDecorations reads the native map codec retaining its entry traversal.
func (s *ItemStack) SetMapDecorations(value nbt.Tag) bool {
	entries, ok := MapDecorationsOf(value)
	if !ok {
		return false
	}
	tag, _ := value.(nbt.CompoundTag)
	s.MapDecorations = entries
	s.mapDecorationOrder = nbt.HashOrderedKeys(tag)
	s.clearRemoval(DataComponentMapDecorations)
	s.NotePatch(DataComponentMapDecorations)
	return true
}

// MapDecorationsTag writes the stack's decoration entries in their retained traversal.
func (s ItemStack) MapDecorationsTag() *nbt.OrderedCompound {
	return mapDecorationsTag(s.MapDecorations, s.mapDecorationKeys())
}

func (s ItemStack) mapDecorationKeys() []string {
	keys := make([]string, 0, len(s.MapDecorations))
	for _, key := range s.mapDecorationOrder {
		if _, present := s.MapDecorations[key]; present {
			keys = append(keys, key)
		}
	}
	if len(keys) == len(s.MapDecorations) {
		return keys
	}
	// Map literals have no native traversal to retain.
	for _, key := range slices.Sorted(maps.Keys(s.MapDecorations)) {
		if !slices.Contains(keys, key) {
			keys = append(keys, key)
		}
	}
	return keys
}

// SetMapDecoration replaces a marker without mutating a copied stack's map.
// vanilla: MapDecorations.withDecoration, Util.copyAndPut
func (s *ItemStack) SetMapDecoration(key string, value MapDecoration) {
	keys := s.mapDecorationKeys()
	if _, present := s.MapDecorations[key]; !present {
		keys = append(keys, key)
	}
	entries := make(map[string]MapDecoration, len(s.MapDecorations)+1)
	maps.Copy(entries, s.MapDecorations)
	entries[key] = value
	s.MapDecorations, s.mapDecorationOrder = entries, keys
	s.clearRemoval(DataComponentMapDecorations)
	s.NotePatch(DataComponentMapDecorations)
}
