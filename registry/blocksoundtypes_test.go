package registry

import "testing"

// TestEveryBlockHasASoundTypeOfRegisteredEvents checks the generated table
// against the registries: each block has a type, and each event a type names
// is a sound event the Sound Effect packet can refer to.
func TestEveryBlockHasASoundTypeOfRegisteredEvents(t *testing.T) {
	for b := range Block(BlockCount) {
		sound := BlockSoundType(b)
		if sound.Step == SoundNone || sound.Place == SoundNone {
			t.Errorf("%s has no sound type", b.Name())
			continue
		}
		group := BlockSoundGroup(b)
		ids := [...]Sound{group.Break, group.Step, group.Place, group.Hit, group.Fall}
		for i, event := range []string{sound.Break.Name(), sound.Step.Name(), sound.Place.Name(), sound.Hit.Name(), sound.Fall.Name()} {
			if ids[i].Name() != event {
				t.Errorf("%s: sound ID %d names %q, want %q", b.Name(), ids[i], ids[i].Name(), event)
			}
			if _, ok := SoundEventID(event); !ok {
				t.Errorf("%s: sound event %q is not in the sound event registry", b.Name(), event)
			}
		}
	}
	if BlockSoundGroup(BlockCount) != (SoundGroup{}) {
		t.Fatal("invalid block has sound IDs")
	}
	if grass := BlockSoundType(BlockGrassBlock); grass.Step != SoundBlockGrassStep {
		t.Errorf("grass steps %q, want minecraft:block.grass.step", grass.Step)
	}
	if got := BlockSoundType(BlockCount); got != (SoundType{}) {
		t.Errorf("BlockSoundType(BlockCount) = %v, want the zero SoundType", got)
	}
}
