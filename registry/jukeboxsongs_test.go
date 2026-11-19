package registry

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/nbt"
)

func TestJukeboxSongStackCodec(t *testing.T) {
	for _, item := range []Item{ItemStick, ItemMusicDisc13} {
		stack := protocol.ItemStack{ItemID: int32(item), Count: 1}
		if !ReadComponent(&stack, "minecraft:jukebox_playable", nbt.String("cat")) {
			t.Fatal("song component refused")
		}
		if song, ok := JukeboxSongForStack(stack); !ok || song != "minecraft:cat" {
			t.Fatalf("song = %q, %v", song, ok)
		}
		again, ok := StoredStack(StackTag(stack))
		if !ok || !again.Equal(stack) {
			t.Fatal("stored song did not round trip")
		}
		stack.Remove(protocol.DataComponentJukeboxPlayable)
		if _, ok := JukeboxSongForStack(stack); ok {
			t.Fatal("removed component still plays")
		}
		again, ok = StoredStack(StackTag(stack))
		if !ok || !again.Removes(protocol.DataComponentJukeboxPlayable) {
			t.Fatal("stored removal did not round trip")
		}
	}
	stack := protocol.ItemStack{ItemID: int32(ItemMusicDisc13), Count: 1}
	if song, ok := JukeboxSongForStack(stack); !ok || song != "minecraft:13" {
		t.Fatal("disc lost its default song")
	}
	for _, value := range []nbt.Tag{nbt.String("missing:song"), nbt.Int(1), nbt.Compound{}} {
		if ReadComponent(&stack, "minecraft:jukebox_playable", value) {
			t.Errorf("accepted invalid song %v", value)
		}
	}
}

func TestJukeboxPackSongOverrides(t *testing.T) {
	t.Cleanup(func() { ApplyPackRegistries(nil) })
	ApplyPackRegistries(map[string]map[string]nbt.Tag{"minecraft:jukebox_song": {
		"minecraft:13": nbt.Compound{"length_in_seconds": nbt.Double(1.01), "comparator_output": nbt.Int(9)},
	}})
	if song, ok := JukeboxSongOf("minecraft:13"); !ok || song.LengthTicks != 21 || song.Comparator != 9 {
		t.Errorf("pack override = %+v, %v", song, ok)
	}
}
