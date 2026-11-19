package world

import (
	"github.com/golem-mc/golem/nbt"
	"math/rand"
	"strings"
	"testing"

	"github.com/golem-mc/golem/registry"
)

func TestDoorSoundIDs(t *testing.T) {
	for block := range registry.Block(registry.BlockCount) {
		if !isDoorLike[block] {
			continue
		}
		name := strings.TrimPrefix(block.Name(), "minecraft:")
		kind := doorKindOf(block)
		family := "wooden_"
		if kind == "fence_gate" {
			family = ""
		}
		switch {
		case strings.HasPrefix(name, "bamboo_"):
			family = "bamboo_wood_"
		case strings.HasPrefix(name, "cherry_"):
			family = "cherry_wood_"
		case strings.HasPrefix(name, "crimson_"), strings.HasPrefix(name, "warped_"):
			family = "nether_wood_"
		case strings.Contains(name, "copper_"):
			family = "copper_"
		case strings.HasPrefix(name, "iron_"):
			family = "iron_"
		}
		for i, edge := range []string{"close", "open"} {
			want := "minecraft:block." + family + kind + "." + edge
			if got := doorSounds[block][i]; got.Name() != want {
				t.Errorf("%s %s: got %d (%s), want %s", name, edge, got, got.Name(), want)
			}
		}
	}
}

func TestDoorSoundKeepsPitchDraws(t *testing.T) {
	w, err := NewWorld(t.TempDir(), NewFlatGenerator())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = w.Close() })
	w.Acquire(ChunkPos{})
	p := NewPhysics(w)
	p.rng = rand.New(rand.NewSource(42))
	want := rand.New(rand.NewSource(42))
	state := registry.BlockCherryTrapdoor.DefaultState()
	var changes []BlockChange
	var effects []Effect
	for range 8 {
		effects = effects[:0]
		p.toggleDoor(5, GroundLevel, 5, registry.BlockCherryTrapdoor, "", state, &changes, &effects)
		pitch := want.Float32()*0.1 + 0.9
		if len(effects) != 1 || effects[0].SoundID != registry.SoundBlockCherryWoodTrapdoorOpen || effects[0].Pitch != pitch || !effects[0].ExcludeUser {
			t.Fatalf("effects %+v, want one opening at pitch %g, excluding the user", effects, pitch)
		}
	}
	if p.rng.Int63() != want.Int63() {
		t.Fatal("sound selection changed the RNG stream")
	}
}

func TestCustomNoteSoundStaysAtNBTBoundary(t *testing.T) {
	w, err := NewWorld(t.TempDir(), NewFlatGenerator())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = w.Close() })
	w.Acquire(ChunkPos{})
	p := NewPhysics(w)
	w.SetBlock(5, GroundLevel+1, 5, registry.BlockPlayerHead.DefaultState())
	state, ok := registry.WithProp(registry.BlockNoteBlock.DefaultState(), registry.PropInstrument, "custom_head")
	if !ok {
		t.Fatal("no custom head instrument")
	}
	w.SetBlock(5, GroundLevel, 5, state)
	p.playNote(5, GroundLevel, 5, state, true)
	if len(p.effects) != 0 {
		t.Fatal("a head without a sound played a note")
	}
	for _, name := range []string{"minecraft:entity.pig.ambient", "example:custom_note"} {
		w.SetBlockEntityTag(5, GroundLevel+1, 5, "minecraft:skull", nbt.Compound{"note_block_sound": nbt.String(name)})
		p.playNote(5, GroundLevel, 5, state, true)
		if len(p.effects) != 1 || p.effects[0].CustomSound != name || p.effects[0].SoundID != registry.SoundNone || p.effects[0].Volume != 3 || p.effects[0].Pitch != 1 {
			t.Fatalf("custom sound %s: %+v", name, p.effects)
		}
		p.effects = nil
	}
}
