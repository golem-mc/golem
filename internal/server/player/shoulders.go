package player

import "github.com/golem-mc/golem/nbt"

// Shoulders is the entities perched on a player: each side's saved entity
// compound, empty when free, and the game time the last one landed.
// vanilla: ServerPlayer.shoulderEntityLeft, shoulderEntityRight, timeEntitySatOnShoulder
type Shoulders struct {
	Left, Right nbt.CompoundTag
	SatAt       int64
}

// Shoulders returns the perched entities.
func (p *Player) Shoulders() Shoulders {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.shoulders
}

// SetShoulders replaces the perched entities.
func (p *Player) SetShoulders(s Shoulders) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.shoulders = s
}
