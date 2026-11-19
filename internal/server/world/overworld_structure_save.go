package world

import (
	"errors"
	"log/slog"

	"github.com/golem-mc/golem/internal/server/world/levelgen"
	"github.com/golem-mc/golem/nbt"
)

// structureStartLoader restores pieces whose placement state cannot be regenerated from the seed.
type structureStartLoader interface {
	loadPiece(nbt.Compound) structurePiece
}

func (s *structureStart) nbt() nbt.Compound {
	children := make([]nbt.Tag, len(s.pieces))
	for i, p := range s.pieces {
		children[i] = p.nbt()
	}
	return structureStartNBT(s.def.Name, s.chunkX, s.chunkZ, children)
}

// vanilla: StructureStart.loadStaticStart, PiecesContainer.load
func (g *OverworldGenerator) loadStructureStart(loader structureStartLoader, tag nbt.Compound) *structureStart {
	id, _ := nbt.Get[nbt.String](tag, "id")
	def := levelgen.StructureNamed(string(id))
	if def == nil {
		return nil
	}
	children, _ := nbt.Get[nbt.List](tag, "Children")
	if len(children.Items) == 0 {
		return nil
	}
	pieces := make([]structurePiece, 0, len(children.Items))
	for _, child := range children.Items {
		compound, _ := nbt.CompoundView(child)
		p := loader.loadPiece(compound)
		if p == nil {
			return nil
		}
		pieces = append(pieces, p)
	}
	box := pieces[0].bounds()
	for _, p := range pieces[1:] {
		box = box.union(p.bounds())
	}
	cx, _ := nbt.IntField(tag, "ChunkX")
	cz, _ := nbt.IntField(tag, "ChunkZ")
	return &structureStart{def: def, typ: g.structures.types[string(id)], chunkX: cx, chunkZ: cz, pieces: pieces, box: box}
}

const structureProtosFile = "structure_starts.dat"

// OpenProtos restores mutable starts before terrain or decoration asks for them.
func (g *OverworldGenerator) OpenProtos(dir string) {
	g.decorState.OpenProtos(dir)
	data, err := readSavedData(dir, structureProtosFile)
	if errors.Is(err, errNoSavedData) {
		return
	}
	if err != nil {
		slog.Warn("could not read unfinished structure starts", "error", err)
		return
	}
	starts, _ := nbt.Get[nbt.List](data, "starts")
	g.structures.mu.Lock()
	defer g.structures.mu.Unlock()
	for _, entry := range starts.Items {
		tag, _ := nbt.CompoundView(entry)
		id, _ := nbt.Get[nbt.String](tag, "id")
		if _, ok := g.structures.types[string(id)].(structureStartLoader); !ok {
			continue
		}
		cx, _ := nbt.IntField(tag, "ChunkX")
		cz, _ := nbt.IntField(tag, "ChunkZ")
		g.structures.keepSavedStart([2]int32{cx, cz}, string(id), tag)
	}
}

// FlushProtos keeps mutable starts even when their start chunk has not been handed out.
// Vanilla saves these with its proto chunks; our block-only protos need a companion file.
func (g *OverworldGenerator) FlushProtos() {
	g.claimAll()
	defer g.releaseAll()
	g.protos.flush()
	if g.protos.dir == "" {
		return
	}
	g.structures.mu.Lock()
	for key, chunk := range g.structures.starts {
		for _, start := range chunk {
			if _, ok := start.typ.(structureStartLoader); ok {
				g.structures.keepSavedStart(key, start.def.Name, start.nbt())
			}
		}
	}
	var starts []nbt.Tag
	for _, chunk := range g.structures.saved {
		for _, start := range chunk {
			starts = append(starts, start)
		}
	}
	g.structures.mu.Unlock()
	if err := writeSavedData(g.protos.dir, structureProtosFile, nbt.Compound{"starts": nbt.List{Elem: nbt.TypeCompound, Items: starts}}); err != nil {
		slog.Warn("could not save unfinished structure starts", "error", err)
	}
}

// keepSavedStart is called with mu held; other starts in the same chunk still grow normally.
func (s *overworldStructures) keepSavedStart(key [2]int32, id string, tag nbt.Compound) {
	if s.saved == nil {
		s.saved = map[[2]int32]nbt.Compound{}
	}
	if s.saved[key] == nil {
		s.saved[key] = nbt.Compound{}
	}
	s.saved[key][id] = tag
}
