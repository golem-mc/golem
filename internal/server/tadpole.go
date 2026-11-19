package server

import (
	"math"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/registry"
)

// lockAge toggles the golden dandelion age lock of a baby.
// vanilla: AgeableMob.mobInteract, AgeableMob.setAgeLocked
func (c *client) lockAge(ent *mobEntity, hand int32) error {
	s := c.srv
	s.mobsMu.Lock()
	was := mobMetadataStateOf(ent.mob)
	changed := ent.mob.ToggleAgeLock()
	locked := ent.mob.AgeLocked
	x, y, z := ent.mob.X, ent.mob.Y, ent.mob.Z
	var data []byte
	if changed {
		data = mobMetadataDeltaPayload(ent, was)
	}
	s.mobsMu.Unlock()
	if !changed {
		return nil
	}
	if c.player.GameMode() != protocol.GameModeCreative {
		if err := c.consumeHeld(hand); err != nil {
			return err
		}
	}
	s.broadcastWorld(ent.w, protocol.IDPlayEntityMetadata, data)
	sound := registry.SoundItemGoldenDandelionUnuse
	if locked {
		sound = registry.SoundItemGoldenDandelionUse
	}
	// vanilla: Level.playSound centers the mob's BlockPos on all three axes.
	s.broadcastSoundID(ent.w, sound, protocol.SoundCategoryPlayer, math.Floor(x)+0.5, math.Floor(y)+0.5, math.Floor(z)+0.5, 1, 1)
	return nil
}
