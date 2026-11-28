package server

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// honeySlide is one body's slide down a honey block's side this tick.
type honeySlide struct {
	w       *world.World
	id      int32
	x, y, z float64
}

// slideHoney rolls the slide's sound and particle event, each at one in five.
// vanilla: HoneyBlock.maybeDoSlideEffects
func (s *Server) slideHoney(h honeySlide) {
	if s.levelRng.Intn(5) == 0 {
		s.broadcastSoundID(h.w, registry.SoundBlockHoneyBlockSlide, protocol.SoundCategoryNeutral, h.x, h.y, h.z, 1, 1)
	}
	if s.levelRng.Intn(5) == 0 {
		s.broadcastWorld(h.w, protocol.IDPlayEntityEvent, entityEventPayload(h.id, protocol.EntityEventHoneySlideParticles))
	}
}

// appendSlides adds n slides of one body.
func appendSlides(list []honeySlide, h honeySlide, n int) []honeySlide {
	for range n {
		list = append(list, h)
	}
	return list
}
