package items

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

func TestContainerLoadsPartialBookPages(t *testing.T) {
	t.Parallel()
	cfg := servertest.ItemCreativeConfig()
	cfg.ViewDistance = config.MinDistance
	cfg.SimulationDistance = config.MinDistance
	srv, c := servertest.Join(t, cfg, "Scribe", servertest.OpsListing(t, "Scribe"))
	servertest.HoldTicks(t, srv)

	for i, tc := range []struct {
		name  string
		pages string
		want  []protocol.WrittenPage
	}{
		{"invalid content", `{nbt:"v"}`, nil},
		{"invalid style", `{text:"T",bold:"yes"}`, []protocol.WrittenPage{{Raw: protocol.Text{Text: "T"}}}},
		{"invalid filtered content", `{raw:"T",filtered:{nbt:"v"}}`, []protocol.WrittenPage{{Raw: protocol.Text{Text: "T"}}}},
		{"mixed pages", `{text:"good"},{nbt:"v"},{text:"last"}`, []protocol.WrittenPage{{Raw: protocol.Text{Text: "good"}}, {Raw: protocol.Text{Text: "last"}}}},
		{"invalid filtered style", `{raw:"T",filtered:{text:"F",bold:"yes"}}`, []protocol.WrittenPage{{Raw: protocol.Text{Text: "T"}, Filtered: &protocol.Text{Text: "F"}}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv.RunLine(fmt.Sprintf(`setblock 2 64 %d chest{Items:[{Slot:0b,id:"minecraft:written_book",count:1,components:{"minecraft:written_book_content":{title:"Invalid",author:"Tester",pages:[%s]},"minecraft:custom_data":{kept:1}}}]}`, i+2, tc.pages))
			want := servertest.ItemStack(t, "minecraft:written_book", 1)
			if !want.SetWrittenBook(protocol.WrittenBook{Title: "Invalid", Author: "Tester", Pages: tc.want}) {
				t.Fatal("expected book could not be encoded")
			}
			wantBook, _ := want.WrittenBookOf()
			got := srv.InventoryAt(2, 64, int32(i+2))[0]
			if got.Empty() {
				t.Fatal("container discarded the book instead of keeping its partial content")
			}
			book, ok := got.WrittenBookOf()
			if !ok || book.Title != "Invalid" || book.Author != "Tester" || !reflect.DeepEqual(book.Pages, wantBook.Pages) {
				t.Fatalf("container book = %+v, %v, want pages %+v with title and author retained", book, ok, tc.want)
			}
			if data := registry.StackComponents(got)["minecraft:custom_data"]; !nbt.Equal(data, nbt.Compound{"kept": nbt.Int(1)}) {
				t.Fatalf("custom data = %v, want unrelated component retained", data)
			}
			c.ChatCommand(fmt.Sprintf("item replace entity @s hotbar.%d from block 2 64 %d container.0", i+1, i+2))
			r := protocol.NewReader(c.ExpectWithinTicks(srv, 20, protocol.IDPlayContainerSlot))
			r.ReadVarInt()
			r.ReadVarInt()
			slot := r.ReadI16()
			transferred, ok := r.ReadSlot()
			if !ok || r.Err() != nil || slot != int16(player.HotbarSlot+i+1) || !transferred.Equal(got) {
				t.Fatalf("transferred slot %d = %+v, %v (%v), want %+v", slot, transferred, ok, r.Err(), got)
			}
		})
	}
	c.Disconnect()
}
