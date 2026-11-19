package world

import (
	"github.com/golem-mc/golem/internal/server/blocklogic"
	"github.com/golem-mc/golem/registry"
)

// The doors, the trapdoors and the fence gates.

// doorSounds indexes close/open events by block. vanilla: BlockSetType, WoodType
var doorSounds = func() *[registry.BlockCount][2]registry.Sound {
	var t [registry.BlockCount][2]registry.Sound
	wood := map[string][2]registry.Sound{
		"door":       {registry.SoundBlockWoodenDoorClose, registry.SoundBlockWoodenDoorOpen},
		"trapdoor":   {registry.SoundBlockWoodenTrapdoorClose, registry.SoundBlockWoodenTrapdoorOpen},
		"fence_gate": {registry.SoundBlockFenceGateClose, registry.SoundBlockFenceGateOpen},
	}
	for block := range registry.Block(registry.BlockCount) {
		if isDoorLike[block] {
			t[block] = wood[doorKindOf(block)]
		}
	}
	for _, set := range []struct {
		stems  []string
		sounds [3][2]registry.Sound
	}{
		{[]string{"iron"}, [3][2]registry.Sound{{registry.SoundBlockIronDoorClose, registry.SoundBlockIronDoorOpen}, {registry.SoundBlockIronTrapdoorClose, registry.SoundBlockIronTrapdoorOpen}, {}}},
		{[]string{"copper", "exposed_copper", "weathered_copper", "oxidized_copper", "waxed_copper", "waxed_exposed_copper", "waxed_weathered_copper", "waxed_oxidized_copper"}, [3][2]registry.Sound{{registry.SoundBlockCopperDoorClose, registry.SoundBlockCopperDoorOpen}, {registry.SoundBlockCopperTrapdoorClose, registry.SoundBlockCopperTrapdoorOpen}, {}}},
		{[]string{"bamboo"}, [3][2]registry.Sound{{registry.SoundBlockBambooWoodDoorClose, registry.SoundBlockBambooWoodDoorOpen}, {registry.SoundBlockBambooWoodTrapdoorClose, registry.SoundBlockBambooWoodTrapdoorOpen}, {registry.SoundBlockBambooWoodFenceGateClose, registry.SoundBlockBambooWoodFenceGateOpen}}},
		{[]string{"cherry"}, [3][2]registry.Sound{{registry.SoundBlockCherryWoodDoorClose, registry.SoundBlockCherryWoodDoorOpen}, {registry.SoundBlockCherryWoodTrapdoorClose, registry.SoundBlockCherryWoodTrapdoorOpen}, {registry.SoundBlockCherryWoodFenceGateClose, registry.SoundBlockCherryWoodFenceGateOpen}}},
		{[]string{"crimson", "warped"}, [3][2]registry.Sound{{registry.SoundBlockNetherWoodDoorClose, registry.SoundBlockNetherWoodDoorOpen}, {registry.SoundBlockNetherWoodTrapdoorClose, registry.SoundBlockNetherWoodTrapdoorOpen}, {registry.SoundBlockNetherWoodFenceGateClose, registry.SoundBlockNetherWoodFenceGateOpen}}},
	} {
		for _, stem := range set.stems {
			for i, kind := range []string{"door", "trapdoor", "fence_gate"} {
				if block, ok := registry.BlockByName("minecraft:" + stem + "_" + kind); ok {
					t[block] = set.sounds[i]
				}
			}
		}
	}
	return &t
}()

// doorLikeNeighborChanged opens or closes a door, trapdoor or fence gate on its
// input signal, POWERED following, with the swing sound (a fence gate's only if
// openness changed).
func (p *Physics) doorLikeNeighborChanged(x, y, z int32, block registry.Block, state uint16, changes *[]BlockChange, effects *[]Effect) {
	signal := p.hasNeighborSignal(x, y, z)
	kind := doorKindOf(block)
	if kind == "door" {
		// A signal at either half opens the pair.
		// vanilla: DoorBlock.neighborChanged
		if registry.StateValue(state, registry.PropHalf) == "lower" {
			signal = signal || p.hasNeighborSignal(x, y+1, z)
		} else {
			signal = signal || p.hasNeighborSignal(x, y-1, z)
		}
	}
	// Only a change of power acts, so a door opened by hand stays open through
	// neighbour changes.
	if signal == registry.StatePowered(state) {
		return
	}
	next, _ := registry.WithBool(state, registry.PropPowered, signal)
	open := registry.StateBool(state, registry.PropOpen)
	if open == signal {
		p.setClientShaped(x, y, z, next, changes)
		return
	}
	next, _ = registry.WithBool(next, registry.PropOpen, signal)
	p.setClientShaped(x, y, z, next, changes)
	if kind != "fence_gate" || open != signal {
		*effects = append(*effects, Effect{
			SoundID: clickSound(doorSounds[block], signal),
			X:       x, Y: y, Z: z, Volume: 1,
			// vanilla: DoorBlock.playSound, TrapDoorBlock.playSound
			Pitch:     p.rng.Float32()*0.1 + 0.9,
			GameEvent: doorGameEvent(signal),
		})
	}
}

func doorKindOf(block registry.Block) string {
	switch {
	case block.InGroup(registry.GroupTrapdoor):
		return "trapdoor"
	case block.InGroup(registry.GroupFenceGate):
		return "fence_gate"
	default:
		return "door"
	}
}

// facingDir turns a facing property value into a direction index.
var facingDir = map[string]int{"north": dirNorth, "south": dirSouth, "west": dirWest, "east": dirEast}

// vanilla: DoorBlock.updateShape
func doorShapeUpdate(state uint16, toward int, neighbor uint16) uint16 {
	half := registry.StateValue(state, registry.PropHalf)
	if toward == dirUp && half == "lower" || toward == dirDown && half == "upper" {
		if registry.BlockOf(neighbor).InGroup(registry.GroupDoor) && registry.StateValue(neighbor, registry.PropHalf) != half {
			// Copy the block kind too: copper weathering travels between halves.
			next, _ := registry.WithProp(neighbor, registry.PropHalf, half)
			return next
		}
		return airState
	}
	if toward == dirDown && half == "lower" && !registry.FaceSturdy(neighbor, dirUp, registry.SupportFull) {
		return airState
	}
	return state
}

// doorLikeShapeUpdate adopts a door's partner state or breaks a half that cannot stand.
func (p *Physics) doorLikeShapeUpdate(x, y, z int32, d [3]int32, changes *[]BlockChange) {
	state := p.w.BlockAt(x, y, z)
	if !registry.BlockOf(state).InGroup(registry.GroupDoor) {
		return
	}
	toward := blocklogic.Opposite(directionOf(d))
	o := dirDelta[toward]
	next := doorShapeUpdate(state, toward, p.w.BlockAt(x+o[0], y+o[1], z+o[2]))
	if next == airState {
		p.breakWithDrop(x, y, z, changes)
	} else if next != state {
		p.setClientShaped(x, y, z, next, changes)
	}
}

// bedShapeUpdate: the half whose partner direction changed takes the partner's
// occupied flag (keeping its own state, unlike doors) or breaks with a drop
// with no partner.
// vanilla: BedBlock.updateShape
func (p *Physics) bedShapeUpdate(x, y, z int32, d [3]int32, changes *[]BlockChange) {
	state := p.w.BlockAt(x, y, z)
	if int(state) >= registry.StateCount || !isBed[registry.BlockOf(state)] {
		return
	}
	// The foot's partner is the way the bed faces, the head's behind it.
	// vanilla: getNeighbourDirection
	want := facingDir[registry.StateValue(state, registry.PropFacing)]
	part := registry.StateValue(state, registry.PropPart)
	if part == "head" {
		want = blocklogic.Opposite(want)
	}
	// Vanilla's direction points toward the changed block; the delta points the
	// other way.
	if vd := blocklogic.Opposite(directionOf(d)); vd != want {
		return
	}
	px, py, pz := x+dirDelta[want][0], y+dirDelta[want][1], z+dirDelta[want][2]
	ps := p.w.BlockAt(px, py, pz)
	if int(ps) < registry.StateCount && registry.BlockOf(ps) == registry.BlockOf(state) && registry.StateValue(ps, registry.PropPart) != part {
		if occupied := registry.StateValue(ps, registry.PropOccupied); occupied != registry.StateValue(state, registry.PropOccupied) {
			next, _ := registry.WithProp(state, registry.PropOccupied, occupied)
			p.setClientShaped(x, y, z, next, changes)
		}
		return
	}
	p.breakWithDrop(x, y, z, changes)
}

// oppositeFacing flips a horizontal facing value.
// vanilla: Direction.getOpposite
func oppositeFacing(facing string) string {
	switch facing {
	case "north":
		return "south"
	case "south":
		return "north"
	case "east":
		return "west"
	case "west":
		return "east"
	}
	return facing
}

// toggleDoor flips open on a hand use, with a write that runs the shape pass
// (vanilla flags 10 and 2, so a watching observer pulses) and the sound. Iron
// doors and trapdoors ignore hands, and a gate opened from the side it faces
// turns to face away from its opener. facing is the player's horizontal look
// direction.
// vanilla: DoorBlock.useWithoutItem, TrapDoorBlock.useWithoutItem,
// FenceGateBlock.useWithoutItem
func (p *Physics) toggleDoor(x, y, z int32, block registry.Block, facing string, state uint16, changes *[]BlockChange, effects *[]Effect) {
	if !doorOpensByHand(block) {
		return
	}
	kind := doorKindOf(block)
	open := !registry.StateBool(state, registry.PropOpen)
	next, _ := registry.WithBool(state, registry.PropOpen, open)
	if kind == "fence_gate" && open &&
		facing != "" && registry.StateValue(state, registry.PropFacing) == oppositeFacing(facing) {
		next, _ = registry.WithProp(next, registry.PropFacing, facing)
	}
	p.setClientShaped(x, y, z, next, changes)
	*effects = append(*effects, Effect{
		SoundID: clickSound(doorSounds[block], open),
		X:       x, Y: y, Z: z, Volume: 1,
		// Pitch 0.9 to 1.0.
		// vanilla: DoorBlock.playSound, TrapDoorBlock.playSound
		Pitch:       p.rng.Float32()*0.1 + 0.9,
		ExcludeUser: true,
		GameEvent:   doorGameEvent(open),
	})
	if kind == "door" {
		// The other half gets the same write, without a second sound.
		var ox, oy, oz = x, y, z
		if registry.StateValue(state, registry.PropHalf) == "lower" {
			oy++
		} else {
			oy--
		}
		opState := p.w.BlockAt(ox, oy, oz)
		if doorKindOf(registry.BlockOf(opState)) == "door" {
			other, _ := registry.WithBool(opState, registry.PropOpen, open)
			p.setClientShaped(ox, oy, oz, other, changes)
		}
	}
}

// doorOpensByHand is false only for iron doors and trapdoors.
// vanilla: BlockSetType.canOpenByHand
func doorOpensByHand(block registry.Block) bool {
	return block != registry.BlockIronDoor && block != registry.BlockIronTrapdoor
}
