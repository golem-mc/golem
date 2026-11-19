package blocks

import (
	"strconv"
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/nbt"
)

// TestASignSideTheCodecRefusesLoadsBlank lands signs whose front side the
// text codec refuses (a message that is no component, a colour that is no
// dye, a glow that is no number) beside a back side it reads: the refused
// side loads as a fresh empty black one and saves as four empty strings,
// while the back keeps its lines. A line in a component's list form is kept,
// and the side saves as a list whose every entry is a compound.
// vanilla: SignText.DIRECT_CODEC, SignBlockEntity.loadAdditional
func TestASignSideTheCodecRefusesLoadsBlank(t *testing.T) {
	t.Parallel()
	srv, _ := servertest.AttachActor(t)
	servertest.HoldTicks(t, srv)
	back := `back_text:{messages:["a","b","c","d"]}`
	for i, front := range []string{
		`{messages:[1,2,3,4],color:"red"}`,
		`{messages:["w","x","y","z"],color:"not_a_dye"}`,
		`{messages:["w","x","y","z"],has_glowing_text:"yes"}`,
	} {
		x := int32(2 + i)
		srv.RunLine("setblock " + strconv.Itoa(int(x)) + " 64 3 minecraft:oak_sign{front_text:" + front + "," + back + "}")
		tag, ok := srv.BlockEntityTagAt(x, 64, 3)
		if !ok {
			t.Fatalf("the sign with front %s has no block entity", front)
		}
		side, _ := nbt.Get[nbt.Compound](tag, "front_text")
		messages, _ := nbt.Get[nbt.List](side, "messages")
		if len(messages.Items) != 4 || messages.Elem != nbt.TypeString {
			t.Fatalf("front %s saved messages %v, want four strings", front, side["messages"])
		}
		for _, line := range messages.Items {
			if line != nbt.String("") {
				t.Errorf("front %s saved line %v, want it blank", front, line)
			}
		}
		if color, _ := nbt.Get[nbt.String](side, "color"); color != "black" {
			t.Errorf("front %s saved color %q, want black", front, color)
		}
		backSide, _ := nbt.Get[nbt.Compound](tag, "back_text")
		lines, _ := nbt.Get[nbt.List](backSide, "messages")
		if len(lines.Items) != 4 || lines.Items[0] != nbt.String("a") {
			t.Errorf("front %s took the back's lines with it: %v", front, backSide["messages"])
		}
	}

	srv.RunLine(`setblock 6 64 3 minecraft:oak_sign{front_text:{messages:[["a","b"],"","",""]}}`)
	tag, ok := srv.BlockEntityTagAt(6, 64, 3)
	if !ok {
		t.Fatal("the sign with a list-form line has no block entity")
	}
	side, _ := nbt.Get[nbt.Compound](tag, "front_text")
	messages, _ := nbt.Get[nbt.List](side, "messages")
	if len(messages.Items) != 4 || messages.Elem != nbt.TypeCompound {
		t.Fatalf("the list-form side saved messages %v, want four compounds", side["messages"])
	}
	for i, line := range messages.Items {
		if _, ok := line.(nbt.Compound); !ok {
			t.Errorf("the list-form side saved line %d as %T in a compound list", i, line)
		}
	}
}
