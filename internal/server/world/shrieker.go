package world

import (
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
)

// The sculk shrieker: a sensor's click (or a step) is a vibration to it; it
// shrieks for 90 ticks and answers when the shriek ends. The warden trackers
// that set the warning level live with the players, so the engine sends a
// ShriekRequest and the server answers with StartShriek.
// vanilla: SculkShriekerBlock, SculkShriekerBlockEntity

// vanilla: SculkShriekerBlockEntity.SHRIEKING_TICKS
const shriekingTicks = 90

// ShriekRequest asks the server to try a shriek at the cell for the player
// the event traces back to.
type ShriekRequest struct {
	X, Y, Z int32
	// Player is the UUID of the player.
	Player uuid.UUID
}

// ShriekResponse is a shriek ending: the server plays the warning, summons
// and darkens as the warning level says.
// vanilla: SculkShriekerBlockEntity.tryRespond
type ShriekResponse struct {
	X, Y, Z int32
	Warning int
	// CanSummon is the state's can_summon at the end of the shriek.
	CanSummon bool
}

// ShriekerAt reports the shrieker at the cell: whether it is shrieking and
// may summon. ok is false for any other block.
func (p *Physics) ShriekerAt(x, y, z int32) (shrieking, canSummon, ok bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	state := p.w.BlockAt(x, y, z)
	if registry.BlockOf(state) != registry.BlockSculkShrieker {
		return false, false, false
	}
	return registry.StateBool(state, registry.PropShrieking), registry.StateBool(state, registry.PropCanSummon), true
}

// StartShriek starts a shriek at warning, kept on the block entity for the
// answer, with a flag-2 write (clients only).
// vanilla: SculkShriekerBlockEntity.shriek
func (p *Physics) StartShriek(x, y, z int32, warning int) ([]BlockChange, []Effect) {
	p.mu.Lock()
	defer p.mu.Unlock()
	state := p.w.BlockAt(x, y, z)
	if registry.BlockOf(state) != registry.BlockSculkShrieker || registry.StateBool(state, registry.PropShrieking) {
		return nil, nil
	}
	var changes []BlockChange
	p.effects = nil
	if be := p.w.EnsureBlockEntity(x, y, z, KindSculkShrieker); be != nil {
		be.Use(func(b *BlockEntity) { b.WarningLevel = warning })
	}
	next, _ := registry.WithBool(state, registry.PropShrieking, true)
	p.setClientShaped(x, y, z, next, &changes)
	p.scheduleBooked(p.sculkBooked, x, y, z, p.now+shriekingTicks, sculkTickEvent)
	// vanilla: LevelEvent.PARTICLES_SCULK_SHRIEK
	p.effects = append(p.effects, Effect{LevelEvent: 3007, X: x, Y: y, Z: z})
	effects := p.effects
	p.effects = nil
	return changes, effects
}

// shriekerTick ends a shriek: the block stops shrieking and the answer goes
// to the server.
// vanilla: SculkShriekerBlock.tick
func (p *Physics) shriekerTick(x, y, z int32, state uint16, changes *[]BlockChange) {
	if !registry.StateBool(state, registry.PropShrieking) {
		return
	}
	next, _ := registry.WithBool(state, registry.PropShrieking, false)
	p.setClient(x, y, z, next, changes)
	p.wireWakeNeighbors(x, y, z, changes)
	p.shapeUpdateNeighbors(x, y, z, changes)
	warning := 0
	if be := p.w.BlockEntity(x, y, z); be != nil {
		be.Use(func(b *BlockEntity) { warning = b.WarningLevel })
	}
	p.effects = append(p.effects, Effect{ShriekResponse: &ShriekResponse{
		X: x, Y: y, Z: z, Warning: warning, CanSummon: registry.StateBool(state, registry.PropCanSummon),
	}})
}
