package world

import "github.com/golem-mc/golem/registry"

// buttonTick looks for an arrow holding a pressed button down: one still inside
// books the next look silently, and with none the button releases, waking its
// neighbours and clicking off.
// vanilla: ButtonBlock.checkPressed
func (p *Physics) buttonTick(x, y, z int32, block registry.Block, state uint16, changes *[]BlockChange, effects *[]Effect) {
	if !registry.StatePowered(state) {
		return
	}
	if buttonOpensByArrow(registry.BlockOf(state)) {
		if p.buttonArrows(x, y, z, state) > 0 {
			p.schedulePriority(x, y, z, p.now+buttonPressTicks(registry.BlockOf(state)), tickPriorityNormal)
			return
		}
	}
	up, _ := registry.WithBool(state, registry.PropPowered, false)
	p.setClientShaped(x, y, z, up, changes)
	*effects = append(*effects, buttonEffect(block, false, x, y, z))
	p.buttonUpdateNeighbours(x, y, z, state, changes)
}

// buttonArrows counts the arrows touching the button's own shape.
// vanilla: ButtonBlock.checkPressed
func (p *Physics) buttonArrows(x, y, z int32, state uint16) int {
	if p.toucher == nil {
		return 0
	}
	return p.toucher.ArrowsIn(buttonBounds(x, y, z, state))
}

// buttonBounds is the button's box: a 6 by 4 plate on the attached face, 2 deep
// unpressed and 1 pressed.
// vanilla: ButtonBlock.makeShapes
func buttonBounds(x, y, z int32, state uint16) Box {
	depth := 2.0
	if registry.StatePowered(state) {
		depth = 1.0
	}
	// Extents in sixteenths along x, y, z; the plate hugs the attached face.
	lo, hi := [3]float64{5, 5, 5}, [3]float64{11, 11, 11}
	facing := registry.StateValue(state, registry.PropFacing)
	alongX := facing == "north" || facing == "south"
	face := registry.StateValue(state, registry.PropFace)
	switch face {
	case "floor", "ceiling":
		if alongX {
			lo[2], hi[2] = 6, 10
		} else {
			lo[0], hi[0] = 6, 10
		}
		if face == "floor" {
			lo[1], hi[1] = 0, depth
		} else {
			lo[1], hi[1] = 16-depth, 16
		}
	default:
		lo[1], hi[1] = 6, 10
		switch facing {
		case "north":
			lo[2], hi[2] = 16-depth, 16
		case "south":
			lo[2], hi[2] = 0, depth
		case "west":
			lo[0], hi[0] = 16-depth, 16
		default:
			lo[0], hi[0] = 0, depth
		}
	}
	const u = 1.0 / 16.0
	return Box{
		MinX: float64(x) + lo[0]*u, MinY: float64(y) + lo[1]*u, MinZ: float64(z) + lo[2]*u,
		MaxX: float64(x) + hi[0]*u, MaxY: float64(y) + hi[1]*u, MaxZ: float64(z) + hi[2]*u,
	}
}

// buttonPressTicks is the press duration: 20 ticks for stone buttons, 30 for
// wooden.
func buttonPressTicks(block registry.Block) int64 {
	if block == registry.BlockStoneButton || block == registry.BlockPolishedBlackstoneButton {
		return buttonTicksStone
	}
	return buttonTicksWood
}

// buttonUpdateNeighbours wakes the button's cell and its mount.
// vanilla: ButtonBlock.updateNeighbours
func (p *Physics) buttonUpdateNeighbours(x, y, z int32, state uint16, changes *[]BlockChange) {
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

// buttonEffect is the BlockSetType's buttonClickOn/Off at volume and pitch 1.
// vanilla: ButtonBlock.playSound
func buttonEffect(block registry.Block, on bool, x, y, z int32) Effect {
	return Effect{SoundID: clickSound(buttonSounds[block], on), X: x, Y: y, Z: z, Volume: 1, Pitch: 1, GameEvent: activateGameEvent(on)}
}

func buttonOpensByArrow(block registry.Block) bool {
	return buttonPressTicks(block) != buttonTicksStone
}

// pressButton presses the button for its span, wakes its neighbours and clicks.
// byHand skips the user in the click, whose client predicted it.
// vanilla: ButtonBlock.playSound
func (p *Physics) pressButton(x, y, z int32, block registry.Block, state uint16, byHand bool, changes *[]BlockChange, effects *[]Effect) {
	down, _ := registry.WithBool(state, registry.PropPowered, true)
	p.setClientShaped(x, y, z, down, changes)
	click := buttonEffect(block, true, x, y, z)
	click.ExcludeUser = byHand
	click.GameEventByUser = byHand
	*effects = append(*effects, click)
	p.buttonUpdateNeighbours(x, y, z, state, changes)
	p.schedulePriority(x, y, z, p.now+buttonPressTicks(registry.BlockOf(state)), tickPriorityNormal)
}

// vanilla: BlockSetType.buttonClickOn, BlockSetType.buttonClickOff
var buttonSounds = func() [registry.BlockCount][2]registry.Sound {
	sounds := [registry.BlockCount][2]registry.Sound{
		registry.BlockStoneButton:              {registry.SoundBlockStoneButtonClickOff, registry.SoundBlockStoneButtonClickOn},
		registry.BlockPolishedBlackstoneButton: {registry.SoundBlockStoneButtonClickOff, registry.SoundBlockStoneButtonClickOn},
		registry.BlockCherryButton:             {registry.SoundBlockCherryWoodButtonClickOff, registry.SoundBlockCherryWoodButtonClickOn},
		registry.BlockBambooButton:             {registry.SoundBlockBambooWoodButtonClickOff, registry.SoundBlockBambooWoodButtonClickOn},
		registry.BlockCrimsonButton:            {registry.SoundBlockNetherWoodButtonClickOff, registry.SoundBlockNetherWoodButtonClickOn},
		registry.BlockWarpedButton:             {registry.SoundBlockNetherWoodButtonClickOff, registry.SoundBlockNetherWoodButtonClickOn},
	}
	for block := range sounds {
		if sounds[block][0] == registry.SoundNone && registry.Block(block).InGroup(registry.GroupButton) {
			sounds[block] = [2]registry.Sound{registry.SoundBlockWoodenButtonClickOff, registry.SoundBlockWoodenButtonClickOn}
		}
	}
	return sounds
}()

func clickSound(pair [2]registry.Sound, on bool) registry.Sound {
	if on {
		return pair[1]
	}
	return pair[0]
}
