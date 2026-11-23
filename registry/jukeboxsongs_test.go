package registry

import (
	"bytes"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/nbt"
)

func TestJukeboxSongDefaultPatch(t *testing.T) {
	t.Parallel()
	plain := protocol.ItemStack{ItemID: int32(ItemMusicDiscCat), Count: 1}
	var want protocol.Buffer
	want.PutSlot(plain)
	claim := protocol.HashedSlot{Stack: plain}

	for _, remove := range []bool{false, true} {
		stack := plain
		if remove {
			stack.Remove(protocol.DataComponentJukeboxPlayable)
		} else if !ReadComponent(&stack, "minecraft:jukebox_playable", nbt.String("blocks")) {
			t.Fatal("song override refused")
		}
		if !ReadComponent(&stack, "minecraft:jukebox_playable", nbt.String("cat")) {
			t.Fatal("default song refused")
		}
		if song, ok := JukeboxSongForStack(stack); !ok || song != "minecraft:cat" {
			t.Fatalf("song = %q, %v", song, ok)
		}
		var got protocol.Buffer
		got.PutSlot(stack)
		if !bytes.Equal(got.Bytes(), want.Bytes()) {
			t.Errorf("slot = %x, want %x", got.Bytes(), want.Bytes())
		}
		if !claim.Matches(stack) {
			t.Error("default disc's hashed slot claim did not match")
		}
	}
}

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
