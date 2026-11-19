package world

import (
	"strconv"
	"sync"

	"github.com/golem-mc/golem/internal/server/world/levelgen"
	"github.com/golem-mc/golem/internal/server/world/terrain"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// netherFortress holds the fortress's block states and grown starts, the same
// for every chunk reading them.
type netherFortress struct {
	states  levelgen.FortressStates
	chest   [4]uint16
	chests  map[uint16]bool
	liquids map[uint16]bool
	spawner uint16
	mu      sync.Mutex
	starts  map[[2]int32]*levelgen.FortressStart
}

const (
	fortressLootTable = "minecraft:chests/nether_bridge"
	// netherFortressReach is how far, in chunks, a fortress's pieces reach from
	// its start chunk: 112 blocks plus a piece's length.
	netherFortressReach = 14
	// fortressWritingStep is the decoration step the fortress places in.
	// vanilla: Structure.step, GenerationStep.Decoration.UNDERGROUND_DECORATION
	fortressWritingStep = levelgen.NetherComplexStep
)

var fenceDirNames = [4]string{"north", "east", "south", "west"}

// DisableStructures stops the nether from generating fortresses, as
// generate-structures=false does. vanilla: ChunkGenerator.applyBiomeDecoration
func (g *NetherGenerator) DisableStructures() { g.noStructures = true }

func (g *NetherGenerator) initFortress() {
	f := &g.fortress
	mustState := func(name string, overrides map[string]string) uint16 {
		state, ok := registry.FindState(name, overrides)
		if !ok {
			panic("the block registry has no " + name)
		}
		return state
	}
	f.states.Air = airState
	f.states.NetherBricks = mustState("minecraft:nether_bricks", nil)
	f.states.SoulSand = g.soulSandState
	f.states.NetherWart = mustState("minecraft:nether_wart", nil)
	f.states.Lava = g.lavaState
	for mask := range f.states.Fence {
		props := map[string]string{}
		for d, name := range fenceDirNames {
			props[name] = strconv.FormatBool(mask&(1<<d) != 0)
		}
		f.states.Fence[mask] = mustState("minecraft:nether_brick_fence", props)
	}
	for d, name := range fenceDirNames {
		f.states.Stairs[d] = mustState("minecraft:nether_brick_stairs", map[string]string{"facing": name})
		f.chest[d] = mustState("minecraft:chest", map[string]string{"facing": name})
	}
	f.spawner = mustState("minecraft:spawner", nil)
	f.chests = map[uint16]bool{}
	for _, st := range registry.AllStates["minecraft:chest"] {
		f.chests[st.ID] = true
	}
	f.liquids = map[uint16]bool{}
	for _, name := range []string{"minecraft:lava", "minecraft:water"} {
		for _, st := range registry.AllStates[name] {
			f.liquids[st.ID] = true
		}
	}
	f.starts = map[[2]int32]*levelgen.FortressStart{}
}

// fortressInCell returns the fortress starting in a nether_complexes cell, or
// nil. When the weighted pick takes the bastion remnant instead, there is no
// fortress there; the bastion is grown separately.
// vanilla: ChunkGenerator.createStructures
func (g *NetherGenerator) fortressInCell(gx, gz int32) *levelgen.FortressStart {
	f := &g.fortress
	if g.noStructures {
		return nil
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	key := [2]int32{gx, gz}
	if s, ok := f.starts[key]; ok {
		return s
	}
	sx, sz := levelgen.RandomSpreadChunk(g.seed, gx*levelgen.NetherComplexSpacing, gz*levelgen.NetherComplexSpacing,
		levelgen.NetherComplexSpacing, levelgen.NetherComplexSeparation, levelgen.NetherComplexSalt)
	var s *levelgen.FortressStart
	if g.complexIsFortress(sx, sz) {
		s = levelgen.NewFortressStart(g.seed, sx, sz)
	}
	f.starts[key] = s
	return s
}

// complexIsFortress reports whether the nether_complexes start in placement
// chunk (sx, sz) is a fortress. The bastion remnant needs a
// #minecraft:has_structure/bastion_remnant biome (all but basalt deltas) at y
// 33 of its start piece's centre.
func (g *NetherGenerator) complexIsFortress(sx, sz int32) bool {
	bx, bz := levelgen.BastionStartCentre(g.seed, sx, sz)
	bastionOK := terrain.NetherBiomeAt(g.state.Router, bx>>2, 33>>2, bz>>2) != levelgen.BasaltDeltas
	return levelgen.NetherComplexIsFortress(g.seed, sx, sz, bastionOK)
}

// FortressAt reports whether the block is in a fortress's bounds and in one of
// its pieces; only fortresses in cells whose reach covers the chunk are
// candidates.
// vanilla: StructureManager.getStructureAt, StructureManager.structureHasPieceAt
func (g *NetherGenerator) FortressAt(x, y, z int32) (inBounds, inPiece bool) {
	cx, cz := x>>4, z>>4
	const reach = netherFortressReach
	for gz := floorDiv(cz-reach, levelgen.NetherComplexSpacing); gz <= floorDiv(cz+reach, levelgen.NetherComplexSpacing); gz++ {
		for gx := floorDiv(cx-reach, levelgen.NetherComplexSpacing); gx <= floorDiv(cx+reach, levelgen.NetherComplexSpacing); gx++ {
			s := g.fortressInCell(gx, gz)
			if s == nil {
				continue
			}
			in, piece := s.Contains(int(x), int(y), int(z))
			inBounds = inBounds || in
			if piece {
				return true, true
			}
		}
	}
	return inBounds, false
}

// placeFortress writes the parts of fortresses in chunk (nx, nz), before the
// underground_decoration features. Overlapping starts go in cell order; vanilla
// iterates a set, so its order is unspecified.
// vanilla: ChunkGenerator.applyBiomeDecoration
func (r *decorRegion) placeFortress(decorationSeed int64, nx, nz int32) {
	g := r.g
	const reach = netherFortressReach
	r.rng.SetFeatureSeed(decorationSeed, levelgen.NetherFortressIndex, levelgen.NetherComplexStep)
	lw := &fortressLevel{r: r}
	for gz := floorDiv(nz-reach, levelgen.NetherComplexSpacing); gz <= floorDiv(nz+reach, levelgen.NetherComplexSpacing); gz++ {
		for gx := floorDiv(nx-reach, levelgen.NetherComplexSpacing); gx <= floorDiv(nx+reach, levelgen.NetherComplexSpacing); gx++ {
			if s := g.fortressInCell(gx, gz); s != nil && s.Intersects(nx, nz) {
				s.PlaceInChunk(lw, &g.fortress.states, r.rng, nx, nz)
			}
		}
	}
}

// fortressLevel is the region as a fortress writes it.
type fortressLevel struct{ r *decorRegion }

// owner is the proto of the chunk a position is in, which keeps what the
// fortress placed there.
func (l *fortressLevel) owner(x, z int) *decorProto {
	return l.r.protoAt(int32(x), int32(z))
}

func (l *fortressLevel) Block(x, y, z int) uint16 { return l.r.get(int32(x), int32(y), int32(z)) }

func (l *fortressLevel) SetBlock(x, y, z int, state uint16) {
	l.r.set(int32(x), int32(y), int32(z), state)
}

func (l *fortressLevel) ReplaceableByStructures(state uint16) bool {
	return l.r.g.isAir(state) || l.r.g.fortress.liquids[state]
}

func (l *fortressLevel) SolidRender(state uint16) bool { return registry.IsSolidRender(state) }

func (l *fortressLevel) IsChest(state uint16) bool { return l.r.g.fortress.chests[state] }

func (l *fortressLevel) PlaceChest(x, y, z, facing int) {
	l.SetBlock(x, y, z, l.r.g.fortress.chest[facing])
	p := l.owner(x, z)
	p.entities = append(p.entities, protoEntity{x: int32(x) & 15, y: int32(y), z: int32(z) & 15, lootSeed: l.r.rng.NextLong()})
}

func (l *fortressLevel) PlaceSpawner(x, y, z int) {
	l.SetBlock(x, y, z, l.r.g.fortress.spawner)
	p := l.owner(x, z)
	p.entities = append(p.entities, protoEntity{spawner: true, x: int32(x) & 15, y: int32(y), z: int32(z) & 15})
}

func (l *fortressLevel) FenceAt(x, y, z int) {
	l.r.markShape(int32(x), int32(y), int32(z))
}

func (l *fortressLevel) ScheduleFluid(x, y, z int) {
	p := l.owner(x, z)
	p.sources = append(p.sources, posKey{int32(x) & 15, int32(y), int32(z) & 15})
}

// fortressStartNBT is the start as a chunk saves it under structures.starts.
// vanilla: StructureStart.createTag, StructurePiece.createTag
func fortressStartNBT(s *levelgen.FortressStart) nbt.Compound {
	saved := s.SavedPieces()
	children := make([]nbt.Tag, len(saved))
	for i, p := range saved {
		piece := nbt.Compound{
			"id": nbt.String(p.ID),
			"BB": nbt.IntArray{int32(p.BB.MinX), int32(p.BB.MinY), int32(p.BB.MinZ), int32(p.BB.MaxX), int32(p.BB.MaxY), int32(p.BB.MaxZ)},
			"O":  nbt.Int(p.Orientation),
			"GD": nbt.Int(p.GenDepth),
		}
		if p.Flag != "" {
			flag := nbt.Byte(0)
			if p.FlagValue {
				flag = 1
			}
			piece[p.Flag] = flag
		}
		if p.HasSeed {
			piece["Seed"] = nbt.Int(p.Seed)
		}
		children[i] = piece
	}
	return structureStartNBT(fortressID, s.ChunkX, s.ChunkZ, children)
}
