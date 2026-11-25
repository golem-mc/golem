package world

import (
	"bytes"
	"compress/gzip"
	"maps"
	"math"
	"slices"
	"strings"

	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// KindStructureBlock is the block entity id of the structure block, which
// the world carries as a raw compound.
const KindStructureBlock = "minecraft:structure_block"

// StructureMode is a structure block's mode, vanilla's StructureMode ordinals.
// vanilla: StructureMode
type StructureMode int

// The modes, in wire order.
const (
	StructureSave StructureMode = iota
	StructureLoad
	StructureCorner
	StructureData
)

// MaxStructureAxis bounds each axis of a structure block's offset and size.
// vanilla: StructureBlockEntity.MAX_OFFSET_PER_AXIS, MAX_SIZE_PER_AXIS
const MaxStructureAxis = 48

// structureScanRadius is how far from a save block detectSize looks for
// corner blocks on x and z.
// vanilla: StructureBlockEntity.detectSize
const structureScanRadius = 80

var (
	structureModeNames = [...]string{"SAVE", "LOAD", "CORNER", "DATA"}
	rotationLegacy     = [...]string{"NONE", "CLOCKWISE_90", "CLOCKWISE_180", "COUNTERCLOCKWISE_90"}
	mirrorLegacy       = [...]string{"NONE", "LEFT_RIGHT", "FRONT_BACK"}
)

// StateName is the value of the block state's mode property.
func (m StructureMode) StateName() string {
	return strings.ToLower(structureModeNames[m])
}

func structureModeOfState(state uint16) StructureMode {
	mode := registry.StateValue(state, registry.PropMode)
	for m := StructureSave; m <= StructureData; m++ {
		if mode == m.StateName() {
			return m
		}
	}
	return StructureLoad
}

// StructureBlock is the data of a structure block entity.
// vanilla: StructureBlockEntity
type StructureBlock struct {
	Name                      string
	Author, Metadata          string
	Pos, Size                 [3]int32
	Rotation                  Rotation
	Mirror                    Mirror
	Mode                      StructureMode
	IgnoreEntities, Strict    bool
	Powered, ShowAir, ShowBox bool
	Integrity                 float32
	Seed                      int64
}

// NewStructureBlock is a fresh entity of a block in the given mode.
// vanilla: StructureBlockEntity.<init>
func NewStructureBlock(mode StructureMode) StructureBlock {
	return StructureBlock{Pos: [3]int32{0, 1, 0}, Mode: mode, IgnoreEntities: true, ShowBox: true, Integrity: 1}
}

func clampAxis(v, lo int32) int32 { return min(max(v, lo), MaxStructureAxis) }

func legacyIndex(names []string, tag nbt.Tag, def int) int {
	if s, ok := tag.(nbt.String); ok {
		if i := slices.Index(names, string(s)); i >= 0 {
			return i
		}
	}
	return def
}

// ParseStructureBlock reads a structure block entity compound, clamping the
// offset and size as the game does and defaulting a missing mode to DATA.
// vanilla: StructureBlockEntity.loadAdditional
func ParseStructureBlock(c nbt.Compound) StructureBlock {
	s := NewStructureBlock(StructureData)
	str := func(k string) string { v, _ := nbt.Get[nbt.String](c, k); return string(v) }
	flag := func(k string, def bool) bool {
		if v, ok := nbt.ByteField(c, k); ok {
			return v != 0
		}
		return def
	}
	// A name that is no identifier reads as none.
	// vanilla: StructureBlockEntity.setStructureName
	s.Name, _ = ParseStructureName(str("name"))
	s.Author, s.Metadata = str("author"), str("metadata")
	for i, k := range [3]string{"X", "Y", "Z"} {
		if v, ok := nbt.IntField(c, "pos"+k); ok {
			s.Pos[i] = min(max(v, -MaxStructureAxis), MaxStructureAxis)
		}
		if v, ok := nbt.IntField(c, "size"+k); ok {
			s.Size[i] = clampAxis(v, 0)
		}
	}
	s.Rotation = Rotation(legacyIndex(rotationLegacy[:], c["rotation"], 0))
	s.Mirror = Mirror(legacyIndex(mirrorLegacy[:], c["mirror"], 0))
	s.Mode = StructureMode(legacyIndex(structureModeNames[:], c["mode"], int(StructureData)))
	s.IgnoreEntities = flag("ignoreEntities", true)
	s.Strict = flag("strict", false)
	s.Powered = flag("powered", false)
	s.ShowAir = flag("showair", false)
	s.ShowBox = flag("showboundingbox", true)
	if v, ok := nbt.FloatField(c, "integrity"); ok {
		s.Integrity = v
	}
	if v, ok := nbt.LongValue(c["seed"]); ok {
		s.Seed = v
	}
	return s
}

// Compound is the entity's saved form, without the id and position.
func (s StructureBlock) Compound() nbt.Compound {
	c := nbt.Compound{}
	s.save(func(k string, v nbt.Tag) { c[k] = v })
	return c
}

// save sets the entity's fields in the order saveAdditional writes them.
// vanilla: StructureBlockEntity.saveAdditional
func (s StructureBlock) save(set func(string, nbt.Tag)) {
	b := func(v bool) nbt.Byte {
		if v {
			return 1
		}
		return 0
	}
	set("name", nbt.String(s.Name))
	set("author", nbt.String(s.Author))
	set("metadata", nbt.String(s.Metadata))
	set("posX", nbt.Int(s.Pos[0]))
	set("posY", nbt.Int(s.Pos[1]))
	set("posZ", nbt.Int(s.Pos[2]))
	set("sizeX", nbt.Int(s.Size[0]))
	set("sizeY", nbt.Int(s.Size[1]))
	set("sizeZ", nbt.Int(s.Size[2]))
	set("rotation", nbt.String(rotationLegacy[s.Rotation]))
	set("mirror", nbt.String(mirrorLegacy[s.Mirror]))
	set("mode", nbt.String(structureModeNames[s.Mode]))
	set("ignoreEntities", b(s.IgnoreEntities))
	set("strict", b(s.Strict))
	set("powered", b(s.Powered))
	set("showair", b(s.ShowAir))
	set("showboundingbox", b(s.ShowBox))
	set("integrity", nbt.Float(s.Integrity))
	set("seed", nbt.Long(s.Seed))
}

// ParseStructureName returns the qualified template id of a structure block
// name; ok is false for an empty name or one that is no identifier.
// vanilla: Identifier.tryParse
func ParseStructureName(name string) (string, bool) {
	if name == "" {
		return "", false
	}
	ns, path, found := strings.Cut(name, ":")
	if !found {
		ns, path = "minecraft", name
	}
	valid := func(s string, slash bool) bool {
		if s == "" {
			return false
		}
		for _, r := range s {
			ok := r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_' || r == '-' || r == '.' || slash && r == '/'
			if !ok {
				return false
			}
		}
		return true
	}
	if !valid(ns, false) || !valid(path, true) {
		return "", false
	}
	return ns + ":" + path, true
}

// CornerBox is the box the corner blocks of a save block enclose.
type CornerBox struct{ Min, Max [3]int32 }

// DetectSize finds the offset and size a save block at pos takes from the
// corner blocks of its name. corners are the positions of CORNER blocks
// sharing the name inside the scan box. ok is false when the box is not more
// than one block across on every axis.
// vanilla: StructureBlockEntity.detectSize, calculateEnclosingBoundingBox
func DetectSize(pos [3]int32, corners [][3]int32) (offset, size [3]int32, ok bool) {
	if len(corners) == 0 {
		return offset, size, false
	}
	box := CornerBox{corners[0], corners[0]}
	// A single corner encloses the save block too.
	enclose := corners[1:]
	if len(corners) == 1 {
		enclose = [][3]int32{pos}
	}
	for _, c := range enclose {
		for i := range c {
			box.Min[i], box.Max[i] = min(box.Min[i], c[i]), max(box.Max[i], c[i])
		}
	}
	for i := range pos {
		d := box.Max[i] - box.Min[i]
		if d <= 1 {
			return offset, size, false
		}
		offset[i], size[i] = box.Min[i]-pos[i]+1, d-1
	}
	return offset, size, true
}

// StructureScanBox is the cube detectSize looks for corner blocks in, given
// the world's block height range.
func StructureScanBox(pos [3]int32, minY, maxY int32) (lo, hi [3]int32) {
	return [3]int32{pos[0] - structureScanRadius, minY, pos[2] - structureScanRadius},
		[3]int32{pos[0] + structureScanRadius, maxY, pos[2] + structureScanRadius}
}

// CarriedAt is a carried block entity compound and where it stands.
type CarriedAt struct {
	Pos [3]int32
	Tag nbt.Compound
}

// TickingCarriedOfKind lists the carried block entities of one id in the
// ticking chunks.
func (w *World) TickingCarriedOfKind(kind string) []CarriedAt {
	w.mu.RLock()
	defer w.mu.RUnlock()
	var out []CarriedAt
	w.forTickingCarriedOfKind(kind, func(_ *loadedChunk, _ int, at CarriedAt) {
		out = append(out, at)
	})
	return out
}

// forTickingCarriedOfKind visits existing list slots while the caller holds w.mu.
func (w *World) forTickingCarriedOfKind(kind string, visit func(*loadedChunk, int, CarriedAt)) {
	for pos := range w.tickingChunks {
		loaded := w.chunks[pos]
		for i, item := range loaded.chunk.carriedBlockEntities.Items {
			c, ok := nbt.CompoundView(item)
			if !ok {
				continue
			}
			if id, _ := nbt.Get[nbt.String](c, "id"); string(id) != kind {
				continue
			}
			if x, y, z, ok := carriedPosInTag(c); ok {
				visit(loaded, i, CarriedAt{Pos: [3]int32{x, y, z}, Tag: c})
			}
		}
	}
}

// CarriedOfKind lists the carried block entities of one id inside the box,
// over the loaded chunks only.
func (w *World) CarriedOfKind(kind string, lo, hi [3]int32) []CarriedAt {
	w.mu.RLock()
	defer w.mu.RUnlock()
	var out []CarriedAt
	for cx := ChunkOf(float64(lo[0])); cx <= ChunkOf(float64(hi[0])); cx++ {
		for cz := ChunkOf(float64(lo[2])); cz <= ChunkOf(float64(hi[2])); cz++ {
			loaded, ok := w.chunks[ChunkPos{X: cx, Z: cz}]
			if !ok {
				continue
			}
			for _, item := range loaded.chunk.carriedBlockEntities.Items {
				c, ok := nbt.CompoundView(item)
				if !ok {
					continue
				}
				if id, _ := nbt.Get[nbt.String](c, "id"); string(id) != kind {
					continue
				}
				x, y, z, ok := carriedPosInTag(c)
				if !ok || x < lo[0] || x > hi[0] || y < lo[1] || y > hi[1] || z < lo[2] || z > hi[2] {
					continue
				}
				out = append(out, CarriedAt{Pos: [3]int32{x, y, z}, Tag: c})
			}
		}
	}
	return out
}

// CaptureTemplate reads the box of size from origin into a template. read
// answers the state and the block entity compound of a cell; blocks whose
// state is in skip (structure voids) are left out.
// vanilla: StructureTemplate.fillFromWorld
func CaptureTemplate(origin, size [3]int32, read func(x, y, z int32) (uint16, nbt.Compound), skip func(uint16) bool) *Template {
	t := &Template{Author: "?"}
	if size[0] < 1 || size[1] < 1 || size[2] < 1 {
		return t
	}
	t.Size = size
	var full, other, entities []TemplateBlock
	for y := int32(0); y < size[1]; y++ {
		for x := int32(0); x < size[0]; x++ {
			for z := int32(0); z < size[2]; z++ {
				state, tag := read(origin[0]+x, origin[1]+y, origin[2]+z)
				if skip(state) {
					continue
				}
				b := TemplateBlock{Pos: [3]int32{x, y, z}, State: state}
				switch {
				case tag != nil:
					b.NBT = tag
					entities = append(entities, b)
				case registry.CollisionFlat(state) && registry.CollisionHeight(state) == 1:
					full = append(full, b)
				default:
					other = append(other, b)
				}
			}
		}
	}
	// The scan is already by y, x, z, so each list is in placement order.
	t.Palettes = [][]TemplateBlock{slices.Concat(full, other, entities)}
	return t
}

// Encode is the template as the gzip-compressed .nbt file the game saves.
// vanilla: StructureTemplate.save
func (t *Template) Encode(dataVersion int32) ([]byte, error) {
	var palette []nbt.Tag
	index := map[uint16]int32{}
	var blocks []nbt.Tag
	if len(t.Palettes) > 0 {
		for _, b := range t.Palettes[0] {
			id, seen := index[b.State]
			if !seen {
				id = int32(len(palette))
				index[b.State] = id
				name, raw, _ := registry.BlockStateName(b.State)
				palette = append(palette, registry.BlockStateTag(name, raw))
			}
			e := nbt.Compound{"pos": intList(b.Pos[0], b.Pos[1], b.Pos[2]), "state": nbt.Int(id)}
			if b.NBT != nil {
				e["nbt"] = b.NBT
			}
			blocks = append(blocks, e)
		}
	}
	var entities []nbt.Tag
	for _, e := range t.Entities {
		entry := nbt.Compound{
			"pos":      nbt.List{Elem: nbt.TypeDouble, Items: []nbt.Tag{nbt.Double(e.Pos[0]), nbt.Double(e.Pos[1]), nbt.Double(e.Pos[2])}},
			"blockPos": intList(e.BlockPos[0], e.BlockPos[1], e.BlockPos[2]),
		}
		if e.NBT != nil {
			entry["nbt"] = e.NBT
		}
		entities = append(entities, entry)
	}
	root := nbt.Compound{
		"blocks":      nbt.List{Elem: nbt.TypeCompound, Items: blocks},
		"palette":     nbt.List{Elem: nbt.TypeCompound, Items: palette},
		"entities":    nbt.List{Elem: nbt.TypeCompound, Items: entities},
		"size":        intList(t.Size[0], t.Size[1], t.Size[2]),
		"DataVersion": nbt.Int(dataVersion),
	}
	raw, err := nbt.Encode("", root)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if _, err := zw.Write(raw); err != nil {
		return nil, err
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func intList(v ...int32) nbt.List {
	items := make([]nbt.Tag, len(v))
	for i, x := range v {
		items[i] = nbt.Int(x)
	}
	return nbt.List{Elem: nbt.TypeInt, Items: items}
}

// StructureTrigger is a structure block a power rise reached.
type StructureTrigger struct{ X, Y, Z int32 }

// TakeStructureTriggers hands the triggers over and empties them.
func (p *Physics) TakeStructureTriggers() []StructureTrigger {
	out := p.structureTriggers
	p.structureTriggers = nil
	return out
}

// structureBlockPowerChanged re-reads the power of a structure block and
// books a trigger on a rise.
// vanilla: StructureBlock.neighborChanged
func (p *Physics) structureBlockPowerChanged(x, y, z int32) {
	s, tag, ok := p.w.StructureBlockAt(x, y, z)
	if !ok {
		return
	}
	powered := p.hasNeighborSignal(x, y, z)
	if powered == s.Powered {
		return
	}
	s.Powered = powered
	p.w.ReplaceCarried(x, y, z, MergeStructureBlock(tag, s))
	if powered {
		p.structureTriggers = append(p.structureTriggers, StructureTrigger{x, y, z})
	}
}

// MergeStructureBlock is the carried compound with the entity's fields
// written over it, keeping any key the entity does not model.
func MergeStructureBlock(old nbt.Compound, s StructureBlock) nbt.Compound {
	out := make(nbt.Compound, len(old)+20)
	maps.Copy(out, old)
	maps.Copy(out, s.Compound())
	return out
}

// StructureBlockAt reads the position's structure block entity, creating one
// from the block state if needed. ok is false for other blocks.
func (w *World) StructureBlockAt(x, y, z int32) (s StructureBlock, tag nbt.Compound, ok bool) {
	if tag, ok = w.BlockEntityTag(x, y, z); ok {
		if id, _ := nbt.Get[nbt.String](tag, "id"); id == KindStructureBlock {
			return ParseStructureBlock(tag), tag, true
		}
		return s, nil, false
	}
	state := w.BlockAt(x, y, z)
	if registry.BlockOf(state) != registry.BlockStructureBlock {
		return s, nil, false
	}
	s = NewStructureBlock(structureModeOfState(state))
	w.SetBlockEntityTag(x, y, z, KindStructureBlock, s.Compound())
	tag, ok = w.BlockEntityTag(x, y, z)
	return s, tag, ok
}

// LoadInfo takes the template's author and size onto the block.
// vanilla: StructureBlockEntity.loadStructureInfo
func (s *StructureBlock) LoadInfo(t *Template) {
	s.Author = t.Author
	s.Size = t.Size
}

// CapturedEntity is a live entity found in a box to save: its absolute
// position and its saved compound with the id, or an empty compound for
// a passenger.
type CapturedEntity struct {
	Pos [3]float64
	NBT nbt.Compound
}

// EntityReader lists the non-player entities whose boxes meet the block box
// from lo up to, not including, hi.
type EntityReader func(lo, hi [3]int32) []CapturedEntity

// templateEntity is the stored form of an entity found in the box at origin:
// the position and a painting's anchor relative to it; the UUID stays, placing
// drops it.
// vanilla: StructureTemplate.fillEntityList
func templateEntity(e CapturedEntity, origin [3]int32) TemplateEntity {
	out := TemplateEntity{NBT: nbt.Copy(e.NBT).(nbt.Compound)}
	for i := range out.Pos {
		out.Pos[i] = e.Pos[i] - float64(origin[i])
		out.BlockPos[i] = int32(math.Floor(out.Pos[i]))
	}
	if id, _ := nbt.Get[nbt.String](e.NBT, "id"); id == "minecraft:painting" {
		if anchor, ok := nbt.Get[nbt.IntArray](e.NBT, "block_pos"); ok && len(anchor) == 3 {
			for i := range out.BlockPos {
				out.BlockPos[i] = anchor[i] - origin[i]
			}
		}
	}
	return out
}

// Save captures the block's box (the block at pos) into the store's template of
// its name, writing the file if toDisk. False for a name or file the store
// cannot save.
// vanilla: StructureBlockEntity.saveStructure
func (s StructureBlock) Save(w *World, store *TemplateStore, pos [3]int32, toDisk bool, entities EntityReader) bool {
	id, ok := ParseStructureName(s.Name)
	if !ok {
		return false
	}
	origin := [3]int32{pos[0] + s.Pos[0], pos[1] + s.Pos[1], pos[2] + s.Pos[2]}
	return SaveTemplate(w, store, id, origin, s.Size, s.IgnoreEntities, s.Author, toDisk,
		func(b registry.Block) bool { return b == registry.BlockStructureVoid }, entities)
}

// SaveTemplate captures the box of size at origin into the store's template
// id, leaving out the blocks skip names, and writes the file if toDisk. Every
// chunk of the box is loaded for the read; entities come from entities unless
// ignoreEntities. False when the file cannot be written.
// vanilla: StructureBlockEntity.saveStructure (static)
func SaveTemplate(w *World, store *TemplateStore, id string, origin, size [3]int32, ignoreEntities bool, author string, toDisk bool, skip func(registry.Block) bool, entities EntityReader) bool {
	if size[0] > 0 && size[2] > 0 {
		for cx := origin[0] >> 4; cx <= (origin[0]+size[0]-1)>>4; cx++ {
			for cz := origin[2] >> 4; cz <= (origin[2]+size[2]-1)>>4; cz++ {
				cp := ChunkPos{X: cx, Z: cz}
				w.Acquire(cp)
				defer w.Release(cp)
			}
		}
	}
	t := CaptureTemplate(origin, size, func(x, y, z int32) (uint16, nbt.Compound) {
		tag, ok := w.BlockEntityTag(x, y, z)
		if !ok {
			return w.BlockAt(x, y, z), nil
		}
		out := nbt.Compound{}
		for k, v := range tag {
			if k != "x" && k != "y" && k != "z" && k != "keepPacked" {
				out[k] = v
			}
		}
		return w.BlockAt(x, y, z), out
	}, func(state uint16) bool { return skip(registry.BlockOf(state)) })
	t.Author = author
	if entities != nil && !ignoreEntities && t.Size[0] > 0 {
		hi := [3]int32{origin[0] + size[0], origin[1] + size[1], origin[2] + size[2]}
		for _, e := range entities(origin, hi) {
			t.Entities = append(t.Entities, templateEntity(e, origin))
		}
	}
	store.Put(id, t)
	return !toDisk || store.Save(id)
}

// DetectSize sets the block's offset and size from the corner blocks of its
// name around pos. It is false outside save mode and when no corner box
// results.
// vanilla: StructureBlockEntity.detectSize
func (s *StructureBlock) DetectSize(w *World, pos [3]int32) bool {
	if s.Mode != StructureSave {
		return false
	}
	lo, hi := StructureScanBox(pos, w.MinY(), w.MinY()+w.Height()-1)
	var corners [][3]int32
	for _, at := range w.CarriedOfKind(KindStructureBlock, lo, hi) {
		other := ParseStructureBlock(at.Tag)
		if other.Mode == StructureCorner && other.Name == s.Name {
			corners = append(corners, at.Pos)
		}
	}
	off, size, ok := DetectSize(pos, corners)
	if ok {
		s.Pos, s.Size = off, size
	}
	return ok
}
