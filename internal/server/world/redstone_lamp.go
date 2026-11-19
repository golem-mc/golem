package world

import "github.com/golem-mc/golem/registry"

// lampLit and lampUnlit are the redstone lamp's two states.
var (
	lampLit, _   = registry.WithBool(registry.BlockRedstoneLamp.DefaultState(), registry.PropLit, true)
	lampUnlit, _ = registry.WithBool(registry.BlockRedstoneLamp.DefaultState(), registry.PropLit, false)
)

// copperBulbBlocks marks a copper bulb of any stage, waxed or not; all carry the
// LIT the analog output follows.
var copperBulbBlocks = registry.BlocksOf(func(b registry.Block) bool {
	kind := b.Type()
	return kind == registry.BlockTypeWeatheringCopperBulb || kind == registry.BlockTypeCopperBulbBlock
})

func isCopperBulb(block registry.Block) bool { return copperBulbBlocks[block] }

// lampTick darkens a lamp after the four-tick delay since it lost power.
// vanilla: RedstoneLampBlock.tick
func (p *Physics) lampTick(x, y, z int32, state uint16, changes *[]BlockChange) {
	if !registry.StateLit(state) {
		return
	}
	if p.hasNeighborSignal(x, y, z) {
		return
	}
	if p.redstonePower(x, y, z, 15, 0) > 0 {
		return
	}
	p.setClientShaped(x, y, z, lampUnlit, changes)
}

// copperBulbFlip moves POWERED on either edge and toggles LIT with the sound on
// a rise, so LIT holds until the next rise. wake adds the flag-3 neighbour and
// analog-output wakes; the on-place caller leaves those to the change's own
// wake.
// vanilla: CopperBulbBlock.checkAndFlip
func (p *Physics) copperBulbFlip(x, y, z int32, state uint16, changes *[]BlockChange, wake bool) {
	powered := p.hasNeighborSignal(x, y, z)
	if registry.StatePowered(state) == powered {
		return
	}
	want, _ := registry.WithBool(state, registry.PropPowered, powered)
	if !registry.StatePowered(state) {
		want, _ = registry.WithBool(want, registry.PropLit, !registry.StateLit(state))
		sound := registry.SoundBlockCopperBulbTurnOff
		if registry.StateLit(want) {
			sound = registry.SoundBlockCopperBulbTurnOn
		}
		p.effects = append(p.effects, Effect{SoundID: sound, X: x, Y: y, Z: z, Volume: 1, Pitch: 1})
	}
	old := p.w.BlockAt(x, y, z)
	p.setClientShaped(x, y, z, want, changes)
	if wake {
		p.wakeNeighbors(x, y, z, old, p.w.BlockAt(x, y, z), changes)
		p.analogOutputChanged(x, y, z, p.now)
	}
}
