package world

// jukeboxSignal is 15 to every face while the song plays, 0 once it ends with
// the disc still in.
// vanilla: JukeboxBlock.ownSignal
func (p *Physics) jukeboxSignal(x, y, z int32) int {
	be := p.w.BlockEntity(x, y, z)
	if be == nil || be.Kind != KindJukebox {
		return 0
	}
	playing := false
	be.Use(func(b *BlockEntity) { playing = b.JukeboxPlaying() })
	if playing {
		return 15
	}
	return 0
}

// SignalChanged wakes the neighbours of a block whose signal changed with no
// block change: a jukebox's song starting or stopping, or a test block's
// start. Only this wake tells them.
// vanilla: Level.updateNeighborsAt
func (p *Physics) SignalChanged(x, y, z int32) ([]BlockChange, []Effect, []TNTPrime) {
	p.mu.Lock()
	defer p.mu.Unlock()
	var changes []BlockChange
	p.effects, p.wakePrimes = nil, nil
	for _, d := range updateNeighborOrder {
		nx, ny, nz := x+dirDelta[d][0], y+dirDelta[d][1], z+dirDelta[d][2]
		p.redstoneNeighborChanged(nx, ny, nz, p.w.BlockAt(nx, ny, nz), &changes, false)
	}
	effects, primes := p.effects, p.wakePrimes
	p.effects, p.wakePrimes = nil, nil
	return changes, effects, primes
}
