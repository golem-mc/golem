package world

import (
	"testing"

	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
)

// TestWriteSign checks that only the edit's holder writes, the edit is
// spent, and wax refuses.
func TestWriteSign(t *testing.T) {
	me, other := uuid.New(), uuid.New()
	b := newBlockEntity(KindSign, 0, 64, 0)
	lines := [4]string{"a", "b", "c", "d"}
	if b.WriteSign(me, true, lines) {
		t.Fatal("a write without the edit stood")
	}
	b.Editor = me
	if b.WriteSign(other, true, lines) {
		t.Fatal("another player's write stood")
	}
	if !b.WriteSign(me, false, lines) || b.SignBack.Messages != lines || b.SignFront.Messages == lines {
		t.Fatal("the holder's back-side write")
	}
	if b.Editor != uuid.Nil {
		t.Error("the edit is spent")
	}
	b.Editor, b.Waxed = me, true
	if b.WriteSign(me, true, lines) {
		t.Error("a waxed sign refuses")
	}
	b.ReleaseSignEditor(other)
	if b.Editor != me {
		t.Error("only the checked edit is released")
	}
	b.ReleaseSignEditor(me)
	if b.Editor != uuid.Nil {
		t.Error("the holder's edit is released")
	}
}

// TestApplySignItem checks the dye, the glow and its removal, the wax, and
// the sides and signs they leave alone.
func TestApplySignItem(t *testing.T) {
	me, other := uuid.New(), uuid.New()
	b := newBlockEntity(KindSign, 0, 64, 0)
	if _, _, ok := b.ApplySignItem(registry.ItemRedDye, "red", me, true); ok {
		t.Fatal("a dye on an empty side")
	}
	b.SignFront.SetMessages([4]string{"hi"})
	sound, wax, ok := b.ApplySignItem(registry.ItemRedDye, "red", me, true)
	if !ok || wax || sound.Name() != "minecraft:item.dye.use" || b.SignFront.Color != "red" {
		t.Fatalf("dye: %q %v %v %q", sound, wax, ok, b.SignFront.Color)
	}
	if _, _, ok := b.ApplySignItem(registry.ItemRedDye, "red", me, true); ok {
		t.Error("the same colour again")
	}
	if _, _, ok := b.ApplySignItem(registry.ItemRedDye, "red", me, false); ok {
		t.Error("the empty back side")
	}
	if _, _, ok := b.ApplySignItem(registry.ItemInkSac, "", me, true); ok {
		t.Error("ink on a side that does not glow")
	}
	if sound, _, ok := b.ApplySignItem(registry.ItemGlowInkSac, "", me, true); !ok || !b.SignFront.Glowing || sound.Name() != "minecraft:item.glow_ink_sac.use" {
		t.Error("glow")
	}
	if sound, _, ok := b.ApplySignItem(registry.ItemInkSac, "", me, true); !ok || b.SignFront.Glowing || sound.Name() != "minecraft:item.ink_sac.use" {
		t.Error("ink")
	}
	if _, _, ok := b.ApplySignItem(registry.ItemStick, "", me, true); ok {
		t.Error("a stick")
	}
	b.Editor = other
	if _, _, ok := b.ApplySignItem(registry.ItemHoneycomb, "", me, true); ok {
		t.Error("another player's edit holds the sign")
	}
	b.Editor = uuid.Nil
	if _, wax, ok := b.ApplySignItem(registry.ItemHoneycomb, "", me, false); !ok || !wax || !b.Waxed {
		t.Error("honeycomb waxes whatever the side says")
	}
	if _, _, ok := b.ApplySignItem(registry.ItemBlueDye, "blue", me, true); ok {
		t.Error("a waxed sign is left alone")
	}
}
