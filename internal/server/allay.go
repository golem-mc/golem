package server

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
)

// allayInteractVolume is the volume of the allay's give, take and chime
// sounds. vanilla: Allay.mobInteract
const allayInteractVolume = 2

// useOnAllay applies Allay.mobInteract: the entity decides, this locks,
// moves the items and sends.
func (c *client) useOnAllay(ent *mobEntity, hand int32, held protocol.ItemStack) (bool, error) {
	s := c.srv
	s.mobsMu.Lock()
	m := ent.mob
	if !m.Alive() {
		s.mobsMu.Unlock()
		return true, nil
	}
	x, y, z := m.X, m.Y, m.Z
	use := m.UseAllay(held, hand, c.player.UUID, c.player.EntityID)
	s.mobsMu.Unlock()
	if !use.Duplicate && !use.Give && !use.Take {
		return false, nil
	}
	if use.Duplicate {
		s.broadcastWorld(ent.w, protocol.IDPlayEntityEvent, entityEventPayload(m.ID, protocol.EntityStatusLoveHearts))
		tag := nbt.Compound{"PersistenceRequired": nbt.Byte(1), "DuplicationCooldown": nbt.Long(use.DuplicateCooldown)}
		_, _ = s.spawnSummonedMobFacing(s.dimensionOfWorld(ent.w), entity.TypeAllay, x, y, z, tag, nil)
	}
	// vanilla: Level.playSound(player, ...) leaves out the player, whose own
	// client plays it.
	s.broadcastSoundIDExcept(ent.w, c, use.Sound, protocol.SoundCategoryNeutral, x, y, z, allayInteractVolume, 1)
	if use.Take {
		s.broadcastWorld(ent.w, protocol.IDPlaySwingAnimation, mobSwingArm(m.ID))
		for _, ev := range use.Thrown {
			s.throwPiglinItem(piglinThrow{w: ent.w, event: ev, thrower: ent.uuid})
		}
		if c.addToInventory(use.Given) > 0 {
			c.dropLeftover(use.Given)
		}
		return true, nil
	}
	c.spendHeld(hand)
	return true, nil
}

// allaysHearJukebox tells the allays in earshot of a jukebox that its song
// started or stopped.
// vanilla: GameEvent.JUKEBOX_PLAY, JUKEBOX_STOP_PLAY
func (s *Server) allaysHearJukebox(w *world.World, x, y, z int32, playing bool) {
	s.mobsMu.Lock()
	defer s.mobsMu.Unlock()
	for _, m := range s.herdOf(w).herd {
		if m.Type == entity.TypeAllay && m.AllayHearsJukebox(x, y, z) {
			m.SetJukeboxPlaying(x, y, z, playing)
		}
	}
}

// allaysHearNote tells the allays near a played note block.
// vanilla: GameEvent.NOTE_BLOCK_PLAY
func (s *Server) allaysHearNote(w *world.World, x, y, z int32) {
	s.mobsMu.Lock()
	defer s.mobsMu.Unlock()
	dim := s.dimensionOfWorld(w).name
	for _, m := range s.herdOf(w).herd {
		if m.Type == entity.TypeAllay {
			m.AllayHearsNote(w, dim, x, y, z)
		}
	}
}
