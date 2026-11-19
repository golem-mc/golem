package registry

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/nbt"
)

// TestSignTextComponentsRoundTripAStoredStack reads sign_text_front with a
// styled line, sign_text_back and waxed onto a stack, sends it over the wire,
// and saves it back in the codec's shape.
func TestSignTextComponentsRoundTripAStoredStack(t *testing.T) {
	front := nbt.Compound{
		"messages": nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{
			nbt.Compound{"": nbt.String("a")},
			nbt.Compound{"text": nbt.String("b"), "color": nbt.String("red")},
			nbt.Compound{"": nbt.String("")},
			nbt.Compound{"": nbt.String("")},
		}},
		"color":            nbt.String("blue"),
		"has_glowing_text": nbt.Byte(1),
	}
	back := nbt.Compound{"messages": nbt.List{Elem: nbt.TypeString, Items: []nbt.Tag{
		nbt.String("x"), nbt.String(""), nbt.String(""), nbt.String(""),
	}}}
	stack := protocol.ItemStack{ItemID: int32(ItemOakSign), Count: 1}
	for name, value := range map[string]nbt.Tag{
		"minecraft:sign_text_front": front,
		"minecraft:sign_text_back":  back,
		"minecraft:waxed":           nbt.Compound{},
	} {
		if !ReadComponent(&stack, name, value) {
			t.Fatalf("could not read %s", name)
		}
	}
	if ReadComponent(&stack, "minecraft:sign_text_front", nbt.Compound{"messages": nbt.List{Elem: nbt.TypeString, Items: []nbt.Tag{nbt.String("a")}}}) {
		t.Error("read a sign text of one line")
	}

	var b protocol.Buffer
	b.PutUntrustedSlot(stack)
	got, ok := protocol.NewReader(b.Bytes()).ReadUntrustedSlot()
	if !ok {
		t.Fatal("the stack did not cross the wire")
	}
	components := StackComponents(got)
	wantBack := nbt.Compound{
		"messages":         back["messages"],
		"color":            nbt.String("black"),
		"has_glowing_text": nbt.Byte(0),
	}
	// A mixed list's wrappers may differ; the text read back may not.
	gotFront, _ := nbt.CompoundView(components["minecraft:sign_text_front"])
	wantFront, _ := SignTextOf(front)
	if text, ok := SignTextOf(gotFront); !ok || !nbt.Equal(SignTextTag(text), SignTextTag(wantFront)) {
		t.Errorf("sign_text_front = %v, want %v", gotFront, front)
	}
	if !nbt.Equal(components["minecraft:sign_text_back"], wantBack) {
		t.Errorf("sign_text_back = %v, want %v", components["minecraft:sign_text_back"], wantBack)
	}
	if _, ok := components["minecraft:waxed"]; !ok {
		t.Error("the stack lost minecraft:waxed")
	}
}
