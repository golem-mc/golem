package server

import (
	"log/slog"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/registry"
)

// handleUsePlaceOnWater is Use Item for a lily pad or frogspawn: the ray
// stops at the first block outline or water source, and the block goes in
// the cell above, or beside it if taken. vanilla: PlaceOnWaterBlockItem.use
func (c *client) handleUsePlaceOnWater(hand int32) bool {
	held := c.heldItem(hand)
	item := registry.Item(held.ItemID)
	block, known := item.Block()
	if !known || held.Empty() || !rules.PlacesOnWater(item) {
		return false
	}
	mode := c.player.GameMode()
	if mode == protocol.GameModeSpectator || c.player.Vitals().Dead {
		return false
	}
	state := block.DefaultState()
	w := c.theWorld()
	pos := c.player.Position()
	dx, dy, dz := entity.LookVector(pos.Yaw, pos.Pitch)
	reach := c.blockRange()
	x, y, z, ok := rules.PlaceOnWaterTarget(w, state,
		pos.X, pos.Y+c.dims(c.player.Appearance().Pose).EyeHeight, pos.Z, dx*reach, dy*reach, dz*reach)
	if !ok {
		return false
	}
	if !registry.BlockOf(state).NoCollision() && c.srv.obstructed(w, x, y, z, state) {
		return false
	}
	old := w.BlockAt(x, y, z)
	if !w.SetBlock(x, y, z, state) {
		return false
	}
	c.srv.markChanged(w, x, y, z)
	c.srv.relightAsync(w, x, z)
	volume, pitch := rules.BlockPlaceSoundLevels(registry.BlockOf(state))
	c.placedBlockTail(w, held, rules.BlockPlaceSound(registry.BlockOf(state)), volume, pitch, x, y, z, old)
	c.spendHeld(hand)
	slog.Debug("placed on water", "block", block.Name(), "x", x, "y", y, "z", z, "by", c.player.Name)
	return true
}
