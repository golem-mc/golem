package registry

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/nbt"
)

// vanilla: ClickEvent.ShowDialog.CODEC, Style.Serializer.MAP_CODEC
func TestDialogNamesRoundTripThroughStacks(t *testing.T) {
	t.Parallel()
	for _, name := range []string{
		`{text:"Named",click_event:{action:"show_dialog",dialog:{type:"notice",title:"Title"}}}`,
		`{text:"Registered",click_event:{action:"show_dialog",dialog:"minecraft:server_links"}}`,
		`{text:"Nested",bold:1b,extra:[{text:" child",click_event:{action:"show_dialog",dialog:{type:"notice",title:"Title"}}}]}`,
		`{translate:"chat.type.text",with:[{text:"Arg",click_event:{action:"show_dialog",dialog:"minecraft:server_links"}}]}`,
		`{text:"Hover",hover_event:{action:"show_text",value:{text:"Child",click_event:{action:"show_dialog",dialog:{type:"notice",title:"Title"}}}}}`,
	} {
		t.Run(name, func(t *testing.T) {
			tag, err := nbt.ParseSNBT(name)
			if err != nil {
				t.Fatal(err)
			}
			text := protocol.Text{Raw: tag}
			stack := protocol.ItemStack{ItemID: int32(ItemStone), Count: 1, CustomName: &text}
			for _, untrusted := range []bool{false, true} {
				var b protocol.Buffer
				if untrusted {
					b.PutUntrustedSlot(stack)
				} else {
					b.PutSlot(stack)
				}
				r := protocol.NewReader(b.Bytes())
				read := r.ReadSlot
				if untrusted {
					read = r.ReadUntrustedSlot
				}
				got, ok := read()
				if !ok || r.Err() != nil || len(r.Rest()) != 0 || !got.Equal(stack) {
					t.Errorf("untrusted=%v: decoded %+v, %v, %v; want %+v", untrusted, got, ok, r.Err(), stack)
				}
			}
			if got, ok := StoredStack(StackTag(stack)); !ok || !got.Equal(stack) {
				t.Errorf("stored stack = %+v, %v; want %+v", got, ok, stack)
			}
		})
	}
}
