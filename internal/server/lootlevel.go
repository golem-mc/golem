package server

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/command"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
)

// lootLevelAt is w as the level of a roll at (x, y, z); nil (a roll with no
// level) for a nil w.
func (s *Server) lootLevelAt(w *world.World, x, y, z float64) rules.LootLevel {
	if w == nil {
		return nil
	}
	return rules.LevelAt(w, w.LootReads(), x, y, z)
}

// lootLevelWithoutOrigin is w as the level of a roll with no origin; nil for a
// nil w.
func (s *Server) lootLevelWithoutOrigin(w *world.World) rules.LootLevel {
	if w == nil {
		return nil
	}
	return rules.LevelWithoutOrigin(w, w.LootReads())
}

// lootLevelAtBlock is w as the level of a roll at the block's centre.
func (s *Server) lootLevelAtBlock(w *world.World, x, y, z int32) rules.LootLevel {
	return s.lootLevelAt(w, float64(x)+0.5, float64(y)+0.5, float64(z)+0.5)
}

// lootReads is the scoreboard and command storages every level's rolls read.
type lootReads struct{ s *Server }

func (r lootReads) Score(holder, objective string) (int32, bool) {
	sc, ok := r.s.scoreboard.Score(holder, objective)
	return sc.Value, ok
}

func (r lootReads) StorageNumber(storage, path string) (nbt.Tag, bool) {
	return command.StorageNumber(r.s.storages.Read, storage, path)
}

func (r lootReads) Storage(id string) nbt.Compound { return command.Storage(r.s.storages.Read, id) }

// blockInteractLootContext reads the block before an interaction changes it.
// vanilla: Block.dropFromBlockInteractLootTable
func (s *Server) blockInteractLootContext(w *world.World, x, y, z int32, state uint16, tool protocol.ItemStack, interacting rules.LootEntity) rules.LootContext {
	ctx := s.brokenBlockLootContext(w, x, y, z, state, nil)
	ctx.Tool = rules.ToolOf(tool)
	ctx.Interacting = interacting
	return ctx
}
