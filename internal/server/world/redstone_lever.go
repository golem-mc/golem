package world

import "github.com/golem-mc/golem/registry"

// Player-use actions, called once the server's block-use path has decided the
// block is usable; the server plays the sounds they report.

// pullLever toggles the lever, wakes it and its mount, and clicks (lower when
// turning off).
// vanilla: LeverBlock.use
func (p *Physics) pullLever(x, y, z int32, state uint16, byUser bool, changes *[]BlockChange, effects *[]Effect) {
	on := !registry.StatePowered(state)
	next, _ := registry.WithBool(state, registry.PropPowered, on)
	p.setClientShaped(x, y, z, next, changes)
	pitch := float32(0.5)
	if on {
		pitch = 0.6
	}
	*effects = append(*effects, Effect{
		SoundID: registry.SoundBlockLeverClick, X: x, Y: y, Z: z, Volume: 0.3, Pitch: pitch,
		GameEvent: activateGameEvent(on), GameEventByUser: byUser, // vanilla: LeverBlock.pull
	})
	p.levitateNeighbours(x, y, z, state, changes)
}

// levitateNeighbours wakes the lever's cell and its mount, each with all six
// neighbours.
// vanilla: LeverBlock.updateNeighbours
func (p *Physics) levitateNeighbours(x, y, z int32, state uint16, changes *[]BlockChange) {
	p.redstoneNeighborChanged(x, y, z, p.w.BlockAt(x, y, z), changes, false)
	for _, d := range updateNeighborOrder {
		nx, ny, nz := x+dirDelta[d][0], y+dirDelta[d][1], z+dirDelta[d][2]
		p.redstoneNeighborChanged(nx, ny, nz, p.w.BlockAt(nx, ny, nz), changes, false)
	}
	ax, ay, az := x+dirDelta[attachedFace(state)][0], y+dirDelta[attachedFace(state)][1], z+dirDelta[attachedFace(state)][2]
	p.redstoneNeighborChanged(ax, ay, az, p.w.BlockAt(ax, ay, az), changes, false)
	for _, d := range updateNeighborOrder {
		nx, ny, nz := ax+dirDelta[d][0], ay+dirDelta[d][1], az+dirDelta[d][2]
		p.redstoneNeighborChanged(nx, ny, nz, p.w.BlockAt(nx, ny, nz), changes, false)
	}
}
