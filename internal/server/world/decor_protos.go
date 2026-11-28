package world

import (
	"bytes"
	"cmp"
	"encoding/binary"
	"errors"
	"io"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
	"github.com/klauspost/compress/zlib"
)

// The proto chunks of each dimension: what surrounding
// decorations have written into a chunk so far. A chunk is decorated once, when a neighbour is requested; its
// features write into its 3x3, and it is handed out once its own 3x3 is
// decorated.
// vanilla: ChunkStatus.FEATURES, ChunkGenerationTask.scheduleLayer

// decorProtoCap bounds the protos in memory when a world is attached; the
// least recently used go to disk and read back exactly. A proto is dropped from
// memory and disk once its chunk and eight neighbours are final, as vanilla
// drops a proto chunk once its neighbours are FULL: nothing reads or writes it
// again and the world holds the chunk. Several players exploring at once keep
// more than a few hundred protos live, and a spilled proto costs a deflate and
// an inflate, so the cap leaves room for them.
const decorProtoCap = 2048

// decorProtoComplete is the neighbours mask of a proto whose chunk and eight
// neighbours are final.
const decorProtoComplete = 1<<9 - 1

// decorProtoSections is the number of sections a decoration may write into:
// the overworld's, which holds the nether's 16. A proto is indexed from its
// dimension's lowest block.
const decorProtoSections = sectionCount

// decorProto holds decorations' blocks in one chunk as state+1 (0 untouched),
// and whether the chunk's own features have run.
type decorProto struct {
	decorated bool
	// final is set once the chunk was handed out: its 3x3 is decorated and
	// nothing writes into it.
	final bool
	// finals has bit (dz+1)*3+dx+1 set for each final chunk at offset (dx, dz),
	// itself included.
	finals uint16
	blocks [decorProtoSections]*[blocksPerSection]uint16
	// sources are generated fluid visits, as local positions.
	sources []posKey
	// fluidTicks are delayed water ticks booked by post-processing.
	fluidTicks []decorFluidTick
	// shapes are the local positions (y from the region's lowest block) of
	// placed blocks that need a shape update, entities its chests and spawners.
	shapes   []posKey
	entities []protoEntity
	// used orders the protos for eviction.
	used uint64
}

type decorFluidTick struct {
	pos  posKey
	kind fluidType
}

func (p *decorProto) transferFluidTicks(c *Chunk, minY int32) {
	for _, t := range p.fluidTicks {
		c.loadedTicks = append(c.loadedTicks, loadedTick{
			id: t.kind.name(), x: c.x*16 + t.pos[0], y: minY + t.pos[1], z: c.z*16 + t.pos[2],
			delay: waterTickDelay, priority: tickPriorityNormal,
			fluid: true,
		})
	}
}

// protoEntity is a block entity the decoration of a chunk placed in it.
type protoEntity struct {
	spawner bool
	// hive is a bee nest; lootSeed then holds its bees' ticks_in_hive, 10 bits
	// each from the lowest, and the count above them.
	hive     bool
	x, y, z  int32 // local x and z; y from the dimension's lowest block
	lootSeed int64
	// tag is the block entity as a structure template carries it, with its loot
	// seed; nil for the fortress's chests and spawners.
	tag nbt.Compound
}

// isCart reports whether the entity is a mineshaft's chest minecart rather
// than a block entity.
func (e protoEntity) isCart() bool {
	id, _ := nbt.Get[nbt.String](e.tag, "id")
	return id == "minecraft:chest_minecart"
}

// isBlockEntity reports whether the entity is a block entity rather than a
// structure mob (which carries a Pos) or a chest minecart.
func (e protoEntity) isBlockEntity() bool {
	return e.tag == nil || e.tag["Pos"] == nil && !e.isCart()
}

// build is the fortress's spawner or chest; a tagged entity goes through place.
func (e protoEntity) build(cx, cz, minY int32) *BlockEntity {
	x, y, z := cx*sectionSize+e.x, e.y+minY, cz*sectionSize+e.z
	if e.spawner {
		be := newBlockEntity(KindSpawner, x, y, z)
		be.SetSpawnEntityID("minecraft:blaze")
		return be
	}
	be := newBlockEntity(KindChest, x, y, z)
	be.LootTable, be.LootTableSeed = fortressLootTable, e.lootSeed
	return be
}

// resolve is the entity at its place in chunk (cx, cz): a block entity
// compound with its id and absolute x/y/z, or else an entity compound with its
// id and Pos. seed is the world seed a chest minecart's UUID derives from.
func (e protoEntity) resolve(cx, cz, minY int32, seed int64) (blockEntity, entity nbt.Compound) {
	x, y, z := cx*sectionSize+e.x, e.y+minY, cz*sectionSize+e.z
	switch {
	case e.hive:
		return hiveEntry(x, y, z, e.lootSeed), nil
	case e.isCart():
		return nil, cartTag(seed, x, y, z, e.tag)
	case e.tag == nil:
		return e.build(cx, cz, minY).EncodeNBT(), nil
	case e.tag["Pos"] != nil:
		return nil, nbt.Copy(e.tag).(nbt.Compound)
	}
	entry := nbt.Copy(e.tag).(nbt.Compound)
	entry["x"], entry["y"], entry["z"] = nbt.Int(x), nbt.Int(y), nbt.Int(z)
	return entry, nil
}

// place puts the block entity e in chunk c.
func (e protoEntity) place(c *Chunk, minY int32) {
	be, _ := e.resolve(c.x, c.z, minY, 0)
	c.placeGeneratedBlockEntity(be)
}

// placeGeneratedBlockEntity puts a block entity compound generation placed in
// the chunk; a kind the server does not model (trial spawner, vault, beehive)
// is kept as its carried compound.
func (c *Chunk) placeGeneratedBlockEntity(entry nbt.Compound) {
	if be, ok := decodeBlockEntity(entry); ok {
		c.setBlockEntity(be)
	} else {
		c.setCarriedBlockEntity(asSaved(entry))
	}
}

// asSaved shapes a placed carried compound as vanilla saves the
// entity it loads it into: components even when empty and, for a vault, its
// config without default fields and its server and shared data even when
// empty. It modifies entry and returns it.
// vanilla: BlockEntity.saveWithoutMetadata, VaultBlockEntity.saveAdditional,
// BeehiveBlockEntity.saveAdditional, TrialSpawnerBlockEntity.saveAdditional
func asSaved(entry nbt.Compound) nbt.Compound {
	if _, ok := entry["components"]; !ok {
		entry["components"] = nbt.Compound{}
	}
	id, _ := nbt.Get[nbt.String](entry, "id")
	if id == "minecraft:beehive" {
		if _, ok := entry["bees"]; !ok {
			entry["bees"] = nbt.List{Elem: nbt.TypeCompound}
		}
	}
	if id != "minecraft:vault" {
		return entry
	}
	// vanilla: VaultConfig.CODEC's lenientOptionalFieldOf defaults
	if cfg, ok := nbt.Get[nbt.Compound](entry, "config"); ok {
		cfg = maps.Clone(cfg)
		for key, def := range map[string]nbt.Tag{
			"loot_table":         nbt.String("minecraft:chests/trial_chambers/reward"),
			"activation_range":   nbt.Double(4),
			"deactivation_range": nbt.Double(4.5),
		} {
			if cfg[key] == def {
				delete(cfg, key)
			}
		}
		entry["config"] = cfg
	} else {
		// An absent config loads DEFAULT; key_item itself has no default.
		entry["config"] = nbt.Compound{"key_item": registry.StackTag(protocol.ItemStack{
			ItemID: int32(registry.ItemTrialKey), Count: 1,
		})}
	}
	for _, key := range []string{"server_data", "shared_data"} {
		if _, ok := entry[key]; !ok {
			entry[key] = nbt.Compound{}
		}
	}
	return entry
}

// dummyEntityKinds is, per block, the entity generation gives the block when
// it placed it with no data: a modelled kind, or a banner or skull, whose fresh
// entity saves no fields. KindNone for a block without one, or whose fresh
// entity is not modelled.
var dummyEntityKinds = func() *[registry.BlockCount]BlockEntityKind {
	t := new([registry.BlockCount]BlockEntityKind)
	for b := range registry.Block(registry.BlockCount) {
		kind, _ := BlockEntityKindByName(registry.BlockEntityTypes[b.Name()])
		if containerKind(kind) || kind == KindBanner || kind == KindSkull {
			t[b] = kind
		}
	}
	return t
}()

// dummyEntity is a generated block that generation gave no data, waiting for
// its fresh block entity until the chunk's tagged ones are placed.
type dummyEntity struct {
	x, y, z int32
	kind    BlockEntityKind
}

// appendDummy appends the block at world position (x, y, z) if its state has a
// fresh entity to promote.
func appendDummy(dummies []dummyEntity, x, y, z int32, state uint16) []dummyEntity {
	if kind := dummyEntityKinds[registry.BlockOf(state)]; kind != KindNone {
		dummies = append(dummies, dummyEntity{x, y, z, kind})
	}
	return dummies
}

// promoteDummies gives each generated block a fresh block entity unless one
// was placed with it.
// vanilla: WorldGenRegion.setBlock (the DUMMY tag), LevelChunk.promotePendingBlockEntity
func (c *Chunk) promoteDummies(dummies []dummyEntity) {
	for _, d := range dummies {
		if c.blockEntity(d.x, d.y, d.z) != nil {
			continue
		}
		if _, ok := c.carriedBlockEntityAt(d.x, d.y, d.z); ok {
			continue
		}
		if containerKind(d.kind) {
			c.setBlockEntity(newBlockEntity(d.kind, d.x, d.y, d.z))
			continue
		}
		// vanilla: BlockEntity.saveMetadata writes components even when empty
		c.setCarriedBlockEntity(nbt.Compound{
			"id": nbt.String(d.kind.Name()), "x": nbt.Int(d.x), "y": nbt.Int(d.y), "z": nbt.Int(d.z),
			"components": nbt.Compound{},
		})
	}
}

func protoIndex(x, y, z int32) int32 { return ((y&15)*sectionSize+z&15)*sectionSize + x&15 }

func (p *decorProto) get(x, y, z int32) (uint16, bool) {
	s := p.blocks[y>>4]
	if s == nil {
		return 0, false
	}
	v := s[protoIndex(x, y, z)]
	return v - 1, v != 0
}

func (p *decorProto) set(x, y, z int32, state uint16) {
	s := p.blocks[y>>4]
	if s == nil {
		s = new([blocksPerSection]uint16)
		p.blocks[y>>4] = s
	}
	s[protoIndex(x, y, z)] = state + 1
}

// dropBlockEntity removes the block entity at local position (x, y, z), y from
// the dimension's lowest block, once a block without one replaced its block.
// vanilla: WorldGenRegion.setBlock
func (p *decorProto) dropBlockEntity(x, y, z int32) {
	p.entities = slices.DeleteFunc(p.entities, func(e protoEntity) bool {
		return e.x == x && e.y == y && e.z == z && e.isBlockEntity()
	})
}

// decorProtoStore guards membership and eviction; claims guard proto contents.
type decorProtoStore struct {
	mu  sync.Mutex
	mem map[ChunkPos]*decorProto
	// spilling holds the protos a trim is writing out, one write per chunk at a
	// time; spilled lists the protos on disk, which are read back on first use.
	spilling map[ChunkPos]*spillEntry
	spilled  map[ChunkPos]spilledProto
	// reading holds the spilled protos a lookup is reading back without s.mu.
	reading map[ChunkPos]*readEntry
	// readHook, when set, runs in a read-back after s.mu is released.
	readHook func(ChunkPos)
	dir      string
	clock    uint64
	// cap is the most protos kept in memory with a directory attached.
	cap int
	// peak is the most protos held in memory at once.
	peak              int
	spills, readbacks atomic.Int64
}

// spilledProto is what a proto on disk keeps in memory: its flags, with finals
// current where the file holds the value from spill time; dirty means they
// differ, and a flush saves them.
type spilledProto struct {
	decorated, final, dirty bool
	finals                  uint16
}

func flagsOf(p *decorProto) spilledProto {
	return spilledProto{decorated: p.decorated, final: p.final, finals: p.finals}
}

// spillEntry is a proto a trim is writing out. done closes once p is encoded,
// so no one writes into p before; cancelled means a lookup took p back or it
// was dropped, and the write's file goes.
type spillEntry struct {
	pos       ChunkPos
	p         *decorProto
	flags     spilledProto
	done      chan struct{}
	cancelled bool
}

// readEntry is a proto being read back. done closes once the read finishes;
// flags collects the finals markFinal records meanwhile, and dropped means the
// proto is discarded instead of stored.
type readEntry struct {
	done    chan struct{}
	flags   spilledProto
	dropped bool
}

func (s *decorProtoStore) init() {
	if s.mem == nil {
		s.mem = map[ChunkPos]*decorProto{}
		s.spilling = map[ChunkPos]*spillEntry{}
		s.spilled = map[ChunkPos]spilledProto{}
		s.reading = map[ChunkPos]*readEntry{}
		s.cap = decorProtoCap
	}
}

// lookup returns a chunk's proto, reading it back from disk if spilled, nil if
// none.
func (s *decorProtoStore) lookup(pos ChunkPos) *decorProto {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lookupLocked(pos)
}

// lookupLocked may release s.mu while a trim encodes the proto or while the
// proto is read back from disk.
func (s *decorProtoStore) lookupLocked(pos ChunkPos) *decorProto {
	s.init()
	for {
		if p := s.mem[pos]; p != nil {
			return p
		}
		e := s.spilling[pos]
		if e == nil || e.cancelled {
			break
		}
		select {
		case <-e.done:
		default:
			s.mu.Unlock()
			<-e.done
			s.mu.Lock()
			continue
		}
		e.cancelled = true
		e.p.finals = e.flags.finals
		s.mem[pos] = e.p
		s.peak = max(s.peak, len(s.mem))
		return e.p
	}
	for {
		if p := s.mem[pos]; p != nil {
			return p
		}
		re := s.reading[pos]
		if re == nil {
			break
		}
		s.mu.Unlock()
		<-re.done
		s.mu.Lock()
	}
	flags, ok := s.spilled[pos]
	if !ok {
		return nil
	}
	delete(s.spilled, pos)
	re := &readEntry{done: make(chan struct{}), flags: flags}
	s.reading[pos] = re
	s.readbacks.Add(1)
	s.mu.Unlock()
	if s.readHook != nil {
		s.readHook(pos)
	}
	p, err := s.readFile(pos)
	s.mu.Lock()
	delete(s.reading, pos)
	close(re.done)
	if err != nil {
		slog.Warn("could not read a proto chunk, decorating it again", "x", pos.X, "z", pos.Z, "error", err)
		return nil
	}
	if re.dropped {
		return nil
	}
	p.finals = re.flags.finals
	s.mem[pos] = p
	s.peak = max(s.peak, len(s.mem))
	return p
}

// readFile reads, removes and decodes a spilled proto's file.
func (s *decorProtoStore) readFile(pos ChunkPos) (*decorProto, error) {
	path := s.path(pos)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	os.Remove(path)
	buf := decorProtoBufs.Get().(*decorProtoBuf)
	defer decorProtoBufs.Put(buf)
	return decodeDecorProto(buf.inflate(data))
}

// flags reports a chunk's proto flags without reading it back; ok is false
// if it has no proto.
func (s *decorProtoStore) flags(pos ChunkPos) (f spilledProto, ok bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if p := s.mem[pos]; p != nil {
		return flagsOf(p), true
	}
	if e := s.spilling[pos]; e != nil && !e.cancelled {
		return e.flags, true
	}
	if re := s.reading[pos]; re != nil && !re.dropped {
		return re.flags, true
	}
	f, ok = s.spilled[pos]
	return f, ok
}

// decorated reports whether a chunk's own features have run.
func (s *decorProtoStore) decorated(pos ChunkPos) bool {
	f, _ := s.flags(pos)
	return f.decorated
}

// final reports whether a chunk was handed out with its proto still kept.
func (s *decorProtoStore) final(pos ChunkPos) bool {
	f, _ := s.flags(pos)
	return f.final
}

// ensure returns the proto of a chunk, creating it empty.
func (s *decorProtoStore) ensure(pos ChunkPos) *decorProto {
	s.mu.Lock()
	defer s.mu.Unlock()
	if p := s.lookupLocked(pos); p != nil {
		return p
	}
	p := &decorProto{}
	s.mem[pos] = p
	s.peak = max(s.peak, len(s.mem))
	return p
}

func (s *decorProtoStore) path(pos ChunkPos) string {
	return filepath.Join(s.dir, strconv.Itoa(int(pos.X))+"."+strconv.Itoa(int(pos.Z)))
}

// drop forgets the proto of a chunk, in memory and on disk.
func (s *decorProtoStore) drop(pos ChunkPos) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.dropLocked(pos)
}

func (s *decorProtoStore) dropLocked(pos ChunkPos) {
	delete(s.mem, pos)
	if e := s.spilling[pos]; e != nil {
		e.cancelled = true
	}
	if re := s.reading[pos]; re != nil {
		re.dropped = true
	}
	if _, ok := s.spilled[pos]; ok {
		delete(s.spilled, pos)
		os.Remove(s.path(pos))
	}
}

// markFinal records that pos was handed out: each surrounding chunk learns it,
// and a proto with all nine final is dropped. A spilled proto learns it in its
// flags, without a read-back.
func (s *decorProtoStore) markFinal(pos ChunkPos) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for dx := int32(-1); dx <= 1; dx++ {
		for dz := int32(-1); dz <= 1; dz++ {
			q := ChunkPos{X: pos.X + dx, Z: pos.Z + dz}
			// pos is at offset (-dx, -dz) from q.
			bit := uint16(1) << ((1-dz)*3 + 1 - dx)
			var finals uint16
			if p := s.mem[q]; p != nil {
				p.finals |= bit
				finals = p.finals
			} else if e := s.spilling[q]; e != nil && !e.cancelled {
				e.flags.finals |= bit
				e.flags.dirty = true
				finals = e.flags.finals
			} else if re := s.reading[q]; re != nil && !re.dropped {
				re.flags.finals |= bit
				re.flags.dirty = true
				finals = re.flags.finals
			} else if f, ok := s.spilled[q]; ok {
				f.finals |= bit
				f.dirty = true
				s.spilled[q] = f
				finals = f.finals
			}
			if finals == decorProtoComplete {
				s.dropLocked(q)
			}
		}
	}
}

// pick parks the least recently used protos over the cap in s.spilling for
// spillVictims; without a directory they stay, since dropping one would lose
// blocks neighbours placed.
func (s *decorProtoStore) pick(skip func(ChunkPos) bool) []*spillEntry {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.dir == "" || len(s.mem) <= s.cap {
		return nil
	}
	order := make([]ChunkPos, 0, len(s.mem))
	for pos := range s.mem {
		// A chunk whose last write is in flight waits for the next trim, so two
		// writes never race on one file.
		if s.spilling[pos] == nil && (skip == nil || !skip(pos)) {
			order = append(order, pos)
		}
	}
	slices.SortFunc(order, func(a, b ChunkPos) int {
		return cmp.Compare(s.mem[a].used, s.mem[b].used)
	})
	order = order[:min(len(order), len(s.mem)-s.cap*3/4)]
	victims := make([]*spillEntry, len(order))
	for i, pos := range order {
		p := s.mem[pos]
		victims[i] = &spillEntry{pos: pos, p: p, flags: flagsOf(p), done: make(chan struct{})}
		s.spilling[pos] = victims[i]
		delete(s.mem, pos)
	}
	return victims
}

// spillVictims encodes and writes pick's victims without s.mu.
func (s *decorProtoStore) spillVictims(victims []*spillEntry) {
	if len(victims) == 0 {
		return
	}
	// Encode every victim before the first write, so a lookup waits for one
	// encode and never for the disk.
	buf := decorProtoBufs.Get().(*decorProtoBuf)
	defer decorProtoBufs.Put(buf)
	buf.out.Reset()
	ends := make([]int, len(victims))
	for i, e := range victims {
		buf.deflate(e.p)
		ends[i] = buf.out.Len()
		close(e.done)
	}
	errs := make([]error, len(victims))
	mkdirErr := os.MkdirAll(s.dir, 0o755)
	for i, e := range victims {
		start := 0
		if i > 0 {
			start = ends[i-1]
		}
		if errs[i] = mkdirErr; errs[i] == nil {
			errs[i] = writeDecorProtoFile(s.path(e.pos), buf.out.Bytes()[start:ends[i]], 0o644)
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	for i, e := range victims {
		delete(s.spilling, e.pos)
		switch {
		case e.cancelled:
			if errs[i] == nil {
				os.Remove(s.path(e.pos))
			}
		case errs[i] != nil:
			slog.Warn("could not save a proto chunk", "x", e.pos.X, "z", e.pos.Z, "error", errs[i])
			s.mem[e.pos] = e.p
		default:
			s.spilled[e.pos] = e.flags
			s.spills.Add(1)
		}
	}
}

func (s *decorProtoStore) spill(pos ChunkPos) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.spillLocked(pos)
}

func (s *decorProtoStore) spillLocked(pos ChunkPos) {
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		slog.Warn("could not save a proto chunk", "error", err)
		return
	}
	p := s.mem[pos]
	buf := decorProtoBufs.Get().(*decorProtoBuf)
	defer decorProtoBufs.Put(buf)
	buf.out.Reset()
	buf.deflate(p)
	if err := writeDecorProtoFile(s.path(pos), buf.out.Bytes(), 0o644); err != nil {
		slog.Warn("could not save a proto chunk", "x", pos.X, "z", pos.Z, "error", err)
		return
	}
	delete(s.mem, pos)
	s.spilled[pos] = flagsOf(p)
	s.spills.Add(1)
}

// attach points the store at a directory and learns which protos are in it,
// with their flags.
func (s *decorProtoStore) attach(dir string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.init()
	s.dir = dir
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		xs, zs, ok := strings.Cut(e.Name(), ".")
		x, errX := strconv.Atoi(xs)
		z, errZ := strconv.Atoi(zs)
		if ok && errX == nil && errZ == nil {
			s.spilled[ChunkPos{X: int32(x), Z: int32(z)}] = readDecorProtoFlags(filepath.Join(dir, e.Name()))
		}
	}
}

// readDecorProtoFlags inflates a saved proto's header; an unreadable one reads
// as undecorated, so its chunk is read back, fails and is decorated again.
func readDecorProtoFlags(path string) spilledProto {
	f, err := os.Open(path)
	if err != nil {
		return spilledProto{}
	}
	defer f.Close()
	var h [4]byte
	zr, err := zlib.NewReader(f)
	if err != nil {
		return spilledProto{}
	}
	if _, err := io.ReadFull(zr, h[:]); err != nil || h[0] != decorProtoVersion {
		return spilledProto{}
	}
	return spilledProto{decorated: h[1]&1 != 0, final: h[1]&2 != 0, finals: binary.LittleEndian.Uint16(h[2:])}
}

// writeDecorProtoFile is os.WriteFile; a test blocks it to hold a spill.
var writeDecorProtoFile = os.WriteFile

// decorProtoBuf is the scratch of a spill: raw holds one proto's encoding,
// out the deflated protos, in the inflated one. Pooled, since a proto encodes
// to over 100 kB.
type decorProtoBuf struct {
	raw []byte
	out bytes.Buffer
	in  bytes.Buffer
}

var decorProtoBufs = sync.Pool{New: func() any { return new(decorProtoBuf) }}

// decorProtoZlib holds deflate state: a fresh writer allocates about a megabyte
// of tables. BestSpeed, since most of a proto is zeros; inflate reads any level.
var decorProtoZlib = sync.Pool{New: func() any {
	zw, _ := zlib.NewWriterLevel(nil, zlib.BestSpeed)
	return zw
}}

// deflate appends p's saved form to b.out: most of its sections are zeros.
func (b *decorProtoBuf) deflate(p *decorProto) {
	b.raw = appendDecorProto(b.raw[:0], p)
	zw := decorProtoZlib.Get().(*zlib.Writer)
	defer decorProtoZlib.Put(zw)
	zw.Reset(&b.out)
	// Writes to a bytes.Buffer do not fail.
	_, _ = zw.Write(b.raw)
	_ = zw.Close()
}

// inflate undoes deflate; non-stream bytes come back empty and fail to decode.
// The result is valid until the next inflate on b.
func (b *decorProtoBuf) inflate(data []byte) []byte {
	b.in.Reset()
	src := bytes.NewReader(data)
	var zr io.ReadCloser
	if r, ok := zlibReaders.Get().(io.ReadCloser); ok {
		if r.(zlib.Resetter).Reset(src, nil) != nil {
			return nil
		}
		zr = r
	} else {
		var err error
		if zr, err = zlib.NewReader(src); err != nil {
			return nil
		}
	}
	_, _ = b.in.ReadFrom(zr)
	_ = zr.Close()
	zlibReaders.Put(zr)
	return b.in.Bytes()
}

// decorProtoVersion is the first byte of a saved proto.
const decorProtoVersion = 7

// appendDecorProto appends p to b: version, flags (decorated, final),
// neighbours mask, springs, shapes and entities, a section mask, each
// present section's 4096 states, then optional delayed fluid ticks. Positions
// are x, y, z as 1, 2, 1 bytes.
func appendDecorProto(b []byte, p *decorProto) []byte {
	var flags byte
	if p.decorated {
		flags |= 1
	}
	if p.final {
		flags |= 2
	}
	b = append(b, decorProtoVersion, flags)
	b = binary.LittleEndian.AppendUint16(b, p.finals)
	pos := func(b []byte, x, y, z int32) []byte {
		b = append(b, byte(x))
		b = binary.LittleEndian.AppendUint16(b, uint16(y))
		return append(b, byte(z))
	}
	b = binary.LittleEndian.AppendUint16(b, uint16(len(p.sources)))
	for _, k := range p.sources {
		b = pos(b, k[0], k[1], k[2])
	}
	b = binary.LittleEndian.AppendUint16(b, uint16(len(p.shapes)))
	for _, k := range p.shapes {
		b = pos(b, k[0], k[1], k[2])
	}
	b = binary.LittleEndian.AppendUint16(b, uint16(len(p.entities)))
	for _, e := range p.entities {
		kind := byte(0)
		switch {
		case e.tag != nil:
			kind = 2
		case e.spawner:
			kind = 1
		case e.hive:
			kind = 3
		}
		b = append(b, kind)
		b = pos(b, e.x, e.y, e.z)
		b = binary.LittleEndian.AppendUint64(b, uint64(e.lootSeed))
		if e.tag != nil {
			data, _ := nbt.Encode("", e.tag)
			b = binary.LittleEndian.AppendUint32(b, uint32(len(data)))
			b = append(b, data...)
		}
	}
	var mask uint32
	for i, s := range p.blocks {
		if s != nil {
			mask |= 1 << i
		}
	}
	b = binary.LittleEndian.AppendUint32(b, mask)
	for _, s := range p.blocks {
		if s == nil {
			continue
		}
		for _, v := range s {
			b = binary.LittleEndian.AppendUint16(b, v)
		}
	}
	if len(p.fluidTicks) > 0 {
		b = binary.LittleEndian.AppendUint16(b, uint16(len(p.fluidTicks)))
		for _, t := range p.fluidTicks {
			b = pos(b, t.pos[0], t.pos[1], t.pos[2])
			b = append(b, byte(t.kind))
		}
	}
	return b
}

func decodeDecorProto(b []byte) (*decorProto, error) {
	bad := errors.New("truncated or unknown proto chunk")
	if len(b) < 6 || b[0] != decorProtoVersion {
		return nil, bad
	}
	p := &decorProto{decorated: b[1]&1 != 0, final: b[1]&2 != 0, finals: binary.LittleEndian.Uint16(b[2:])}
	b = b[4:]
	count := func(size int) (int, bool) {
		if len(b) < 2 {
			return 0, false
		}
		n := int(binary.LittleEndian.Uint16(b))
		b = b[2:]
		return n, len(b) >= n*size
	}
	pos := func() (x, y, z int32) {
		x, y, z = int32(b[0]), int32(binary.LittleEndian.Uint16(b[1:])), int32(b[3])
		b = b[4:]
		return
	}
	n, ok := count(4)
	if !ok {
		return nil, bad
	}
	for range n {
		x, y, z := pos()
		p.sources = append(p.sources, posKey{x, y, z})
	}
	if n, ok = count(4); !ok {
		return nil, bad
	}
	for range n {
		x, y, z := pos()
		p.shapes = append(p.shapes, posKey{x, y, z})
	}
	if n, ok = count(0); !ok {
		return nil, bad
	}
	for range n {
		if len(b) < 13 {
			return nil, bad
		}
		e := protoEntity{spawner: b[0] == 1, hive: b[0] == 3}
		withTag := b[0] == 2
		b = b[1:]
		e.x, e.y, e.z = pos()
		e.lootSeed = int64(binary.LittleEndian.Uint64(b))
		b = b[8:]
		if withTag {
			if len(b) < 4 {
				return nil, bad
			}
			size := int(binary.LittleEndian.Uint32(b))
			if len(b) < 4+size {
				return nil, bad
			}
			_, tag, err := nbt.Decode(b[4 : 4+size])
			if err != nil {
				return nil, bad
			}
			e.tag, _ = tag.(nbt.Compound)
			b = b[4+size:]
		}
		p.entities = append(p.entities, e)
	}
	if len(b) < 4 {
		return nil, bad
	}
	mask := binary.LittleEndian.Uint32(b)
	b = b[4:]
	for i := range p.blocks {
		if mask&(1<<i) == 0 {
			continue
		}
		if len(b) < blocksPerSection*2 {
			return nil, bad
		}
		s := new([blocksPerSection]uint16)
		for j := range s {
			s[j] = binary.LittleEndian.Uint16(b[j*2:])
		}
		p.blocks[i] = s
		b = b[blocksPerSection*2:]
	}
	if len(b) > 0 {
		n, ok := count(5)
		if !ok {
			return nil, bad
		}
		for range n {
			x, y, z := pos()
			p.fluidTicks = append(p.fluidTicks, decorFluidTick{pos: posKey{x, y, z}, kind: fluidType(b[0])})
			b = b[1:]
		}
	}
	return p, nil
}

// decorClaimGap separates the read halos of concurrent decorations.
// vanilla: ChunkPyramid FEATURES (write radius 1, reads radius 1)
const decorClaimGap = 6

// decorState keeps terrain and protos shared by neighbouring decorations.
type decorState struct {
	// mu guards claims, all, lightReplay and lightDirty. Lock order: mu, then protos.mu.
	mu     sync.Mutex
	free   sync.Cond // L = &mu, lazily
	claims []ChunkPos
	all    bool // claimAll holds or waits for the whole dimension
	protos decorProtoStore
	cache  shapedTerrainCache
	// A nil lightReplay publishes initialization immediately after FEATURES.
	lightReplay map[ChunkPos]decorLight
	lightDirty  map[ChunkPos]bool
}

func (g *decorState) initCond() {
	if g.free.L == nil {
		g.free.L = &g.mu
	}
}

func (g *decorState) claim(pos ChunkPos) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.initCond()
	for g.all || (g.lightReplay != nil && len(g.claims) > 0) || g.nearClaim(pos, decorClaimGap) {
		g.free.Wait()
	}
	g.claims = append(g.claims, pos)
}

func (g *decorState) nearClaim(pos ChunkPos, gap int32) bool {
	for _, p := range g.claims {
		if max(absInt32(p.X-pos.X), absInt32(p.Z-pos.Z)) <= gap {
			return true
		}
	}
	return false
}

func (g *decorState) release(pos ChunkPos) {
	g.mu.Lock()
	defer g.mu.Unlock()
	for i, p := range g.claims {
		if p == pos {
			g.claims = slices.Delete(g.claims, i, i+1)
			break
		}
	}
	g.free.Broadcast()
}

func (g *decorState) claimAll() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.initCond()
	for g.all {
		g.free.Wait()
	}
	g.all = true
	for len(g.claims) > 0 {
		g.free.Wait()
	}
}

func (g *decorState) releaseAll() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.all = false
	g.free.Broadcast()
}

// trim prevents eviction of any proto that a current claim can read. It picks
// under g.mu, so a later claim finds its victims in spilling or on disk.
func (g *decorState) trim() {
	g.mu.Lock()
	victims := g.protos.pick(func(pos ChunkPos) bool { return g.nearClaim(pos, 3) })
	g.mu.Unlock()
	g.protos.spillVictims(victims)
}

func (s *decorProtoStore) touch(p *decorProto) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.clock++
	p.used = s.clock
}

// BeginLightReplay separates FEATURES completion from light publication.
func (g *decorState) BeginLightReplay() {
	g.claimAll()
	defer g.releaseAll()
	g.lightReplay = map[ChunkPos]decorLight{}
	g.lightDirty = map[ChunkPos]bool{}
}

type decorLight struct {
	lit, initialized bool
	top              int32
	occupied         bool
	sources          *[256]int32
}

// OpenProtos attaches the generator to the directory holding its unfinished
// chunks between runs.
func (g *decorState) OpenProtos(dir string) {
	g.claimAll()
	defer g.releaseAll()
	g.protos.attach(dir)
}

// FlushProtos saves every in-memory proto so the next run resumes; vanilla
// keeps proto chunks in the region files.
func (g *decorState) FlushProtos() {
	g.claimAll()
	defer g.releaseAll()
	g.protos.flush()
}

// flush saves every in-memory proto, and the finals of a spilled one that
// changed since its spill.
func (s *decorProtoStore) flush() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.dir == "" {
		return
	}
	var dirty []ChunkPos
	for pos, f := range s.spilled {
		if f.dirty {
			dirty = append(dirty, pos)
		}
	}
	// A deflated header cannot be patched: read back and spill again.
	for _, pos := range dirty {
		s.lookupLocked(pos)
	}
	for pos := range s.mem {
		s.spillLocked(pos)
	}
}

// ProtoStats reports the protos in memory, the peak held at once, and those on
// disk.
func (g *decorState) ProtoStats() (mem, peak, spilled int) {
	g.claimAll()
	defer g.releaseAll()
	g.protos.mu.Lock()
	defer g.protos.mu.Unlock()
	return len(g.protos.mem), g.protos.peak, len(g.protos.spilled)
}
