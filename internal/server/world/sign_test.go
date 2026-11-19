package world

import (
	"reflect"

	"github.com/golem-mc/golem/nbt"
	"testing"

	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
)

// TestSignEntityKind checks the two sign kinds and a non-sign.
func TestSignEntityKind(t *testing.T) {
	for name, want := range map[string]BlockEntityKind{
		"minecraft:oak_sign":              KindSign,
		"minecraft:oak_wall_sign":         KindSign,
		"minecraft:oak_hanging_sign":      KindHangingSign,
		"minecraft:oak_wall_hanging_sign": KindHangingSign,
		"minecraft:stone":                 KindNone,
	} {
		block, _ := registry.BlockByName(name)
		got, ok := SignEntityKind(block)
		if got != want || ok != (want != KindNone) {
			t.Errorf("SignEntityKind(%s) = %q, %v, want %q", name, got, ok, want)
		}
	}
	if !IsSignKind(KindSign) || !IsSignKind(KindHangingSign) || IsSignKind(KindChest) {
		t.Error("IsSignKind")
	}
	if SignWaxedFailSound(KindHangingSign) == SignWaxedFailSound(KindSign) {
		t.Error("the hanging sign has its own wobble")
	}
}

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

// TestSignUse checks the click's reading of wax, a foreign edit and the side.
func TestSignUse(t *testing.T) {
	me, other := uuid.New(), uuid.New()
	b := newBlockEntity(KindSign, 0, 64, 0)
	if waxed, held, editable := b.SignUse(me, true); waxed || held || !editable {
		t.Errorf("fresh sign: %v %v %v", waxed, held, editable)
	}
	b.Editor = other
	if _, held, _ := b.SignUse(me, true); !held {
		t.Error("another player's edit")
	}
	if _, held, _ := b.SignUse(other, true); held {
		t.Error("one's own edit is no other's")
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

// TestSetMessagesKeepsTheLineStyle checks that only a literal line's style
// keys survive an edit.
func TestSetMessagesKeepsTheLineStyle(t *testing.T) {
	var text SignText
	text.raw[0] = nbt.Compound{
		"text": nbt.String("Shop"), "color": nbt.String("red"), "bold": nbt.Byte(1),
		"extra": nbt.List{Elem: nbt.TypeString, Items: []nbt.Tag{nbt.String("x")}},
	}
	text.raw[1] = nbt.Compound{"text": nbt.String("a"), "extra": nbt.List{Elem: nbt.TypeString, Items: []nbt.Tag{nbt.String("x")}}}
	text.raw[2] = nbt.Compound{"translate": nbt.String("k"), "color": nbt.String("red")}
	text.SetMessages([4]string{"Open", "b", "c", "d"})
	want := nbt.Compound{"text": nbt.String("Open"), "color": nbt.String("red"), "bold": nbt.Byte(1)}
	if !reflect.DeepEqual(text.raw[0], want) {
		t.Errorf("raw[0] = %#v, want %#v", text.raw[0], want)
	}
	if text.raw[1] != nil || text.raw[2] != nil {
		t.Errorf("unstyled and non-literal lines = %#v, %#v, want plain", text.raw[1], text.raw[2])
	}
	if text.Messages != [4]string{"Open", "b", "c", "d"} {
		t.Errorf("Messages = %q", text.Messages)
	}
}
