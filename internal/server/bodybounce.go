package server

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
)

// bodyBounce is a boat, minecart or primed TNT that bounced off the block
// below, noted under its lock and answered once the lock is down.
type bodyBounce struct {
	w         *world.World
	source    uuid.UUID
	x, y, z   float64
	halfWidth float64
	// quiet bodies (primed TNT) make no block sound.
	quiet bool
}

// runBodyBounces plays the bounce sound of the block each body landed on and
// posts its bounce game event.
// vanilla: Entity.restituteMovementAfterCollisions, ShelfMushroomBlock.bounceOn
func (s *Server) runBodyBounces(bounces []bodyBounce) {
	for _, b := range bounces {
		if !b.quiet {
			bx, by, bz, state := rules.OnPosCell(b.w, b.x, b.y, b.z, b.halfWidth)
			if sound := rules.BounceSound(state); sound != registry.SoundNone {
				s.broadcastSoundID(b.w, sound, protocol.SoundCategoryBlock, float64(bx)+0.5, float64(by)+0.5, float64(bz)+0.5, 1, 1)
			}
		}
		s.gameEvent(b.w, rules.GameEventBounce, b.x, b.y, b.z, rules.GameEventContext{Mob: b.source})
	}
}
