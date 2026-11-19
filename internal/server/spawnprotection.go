package server

import (
	"strconv"

	"github.com/google/uuid"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
)

// underSpawnProtection reports whether the player may not edit the block
// column.
func (c *client) underSpawnProtection(x, z int32) bool {
	return c.srv.spawnProtected(c.theWorld(), c.player.UUID, x, z)
}

// spawnProtected reports whether the column is within spawn-protection of
// the world spawn, in its dimension, for a player not on a non-empty
// ops.json. vanilla: DedicatedServer.isUnderSpawnProtection
func (s *Server) spawnProtected(w *world.World, id uuid.UUID, x, z int32) bool {
	radius := int(s.spawnProtection.Load())
	if radius <= 0 {
		return false
	}
	dim, spawn := s.worldSpawn()
	if w != dim.w {
		return false
	}
	if len(s.ops.list.snapshot()) == 0 || s.ops.listed(id) {
		return false
	}
	return rules.UnderSpawnProtection(x, z, spawn.X, spawn.Z, radius)
}

// sendSpawnProtection shows the red spawn-protection message above the
// hotbar. vanilla: ServerPlayer.sendSpawnProtectionMessage
func (c *client) sendSpawnProtection(x, y, z int32) {
	c.send(protocol.IDPlaySystemChat, systemChatPayload(protocol.Text{
		Translate: "build.spawn_protection",
		With:      []protocol.Text{{Text: strconv.Itoa(int(x)) + ", " + strconv.Itoa(int(y)) + ", " + strconv.Itoa(int(z))}},
		Color:     protocol.ColorRed,
	}, true))
}
