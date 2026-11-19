package registry

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/nbt"
)

// vanilla: WrittenBookContent.CONTENT_CODEC, Filterable.codec
func TestStoredBookRefusesMalformedPages(t *testing.T) {
	for _, tc := range []struct {
		name, page string
		valid      bool
	}{
		{"missing source", `{nbt:"v"}`, false},
		{"invalid path", `{nbt:"[",storage:"t:s"}`, false},
		{"invalid storage", `{nbt:"v",storage:"Not Valid"}`, false},
		{"invalid style", `{text:"T",bold:"yes"}`, false},
		{"filtered twin", `{raw:"T",filtered:{nbt:"v"}}`, false},
		{"invalid selector", `{selector:"@invalid"}`, false},
		{"selector trailing data", `{selector:"@s trailing"}`, false},
		{"unknown entity type", `{selector:"@e[type=missing]"}`, false},
		{"selector separator", `{selector:"@s",separator:42}`, false},
		{"nested show text", `{text:"T",hover_event:{action:"show_text",value:{nbt:"v"}}}`, false},
		{"nested hover book", `{text:"T",hover_event:{action:"show_item",id:"written_book",components:{written_book_content:{title:"T",author:"A",pages:[{nbt:"v"}]}}}}`, false},
		{"nested dialog title", `{text:"T",click_event:{action:"show_dialog",dialog:{type:"notice",title:{nbt:"v"}}}}`, false},
		{"nested dialog item book", `{text:"T",click_event:{action:"show_dialog",dialog:{type:"notice",title:"T",body:{type:"item",item:{id:"written_book",components:{written_book_content:{title:"T",author:"A",pages:[{raw:"T",filtered:{text:"T",bold:"yes"}}]}}}}}}}`, false},
		{"invalid profile", `{object:"player",player:{name:"invalid name"}}`, false},
		{"invalid profile UUID", `{player:{id:[I;1,2,3]}}`, false},
		{"invalid profile properties", `{player:{properties:[{name:"textures"}]}}`, false},
		{"invalid profile model", `{player:{name:"Actor",model:"invalid"}}`, false},
		{"invalid atlas sprite", `{object:"atlas",sprite:"Not Valid"}`, false},
		{"compact profile", `{player:"Actor"}`, true},
		{"typed profile", `{type:"object",object:"player",player:{name:"Actor"}}`, true},
		{"atlas", `{object:"atlas",atlas:"minecraft:items",sprite:"minecraft:item/diamond"}`, true},
		{"compact atlas", `{sprite:"minecraft:item/diamond"}`, true},
		{"typed atlas", `{type:"object",sprite:"minecraft:item/diamond"}`, true},
		{"filtered objects", `{raw:{player:"Actor"},filtered:{sprite:"minecraft:item/diamond"}}`, true},
		{"invalid filtered profile", `{raw:{sprite:"minecraft:item/diamond"},filtered:{player:{name:"invalid name"}}}`, false},
		{"dynamic", `{nbt:"v",storage:"t:s",unknown:"kept"}`, true},
		{"dynamic filtered", `{raw:{selector:"@e[type=player]"},filtered:{nbt:"v",entity:"@s"}}`, true},
		{"empty filtered", `{raw:{nbt:"v",storage:"t:s"},filtered:""}`, true},
		{"arbitrary score name", `{score:{name:"arbitrary name beyond sixteen characters",objective:"o"}}`, true},
		{"lenient NBT fields", `{nbt:"v",storage:"t:s",separator:42,interpret:"yes",plain:{}}`, true},
		{"unknown type tag", `{selector:"@e[type=#unknown]"}`, true},
		{"profile", `{object:"player",player:{name:"Actor",id:[I;1,2,3,4],properties:[{name:"textures",value:"value"}],texture:"t:skin",model:"slim"}}`, true},
		{"dialog", `{text:"T",click_event:{action:"show_dialog",dialog:{type:"notice",title:{selector:"@s"}}}}`, true},
		{"hover patch", `{text:"T",hover_event:{action:"show_item",id:"player_head",components:{profile:{name:"Actor"},custom_name:{selector:"@s"},"!damage":{}}}}`, true},
		{"hover book", `{text:"T",hover_event:{action:"show_item",id:"written_book",components:{written_book_content:{title:"T",author:"A",pages:[{nbt:"v",storage:"t:s"}]}}}}`, true},
		{"dialog item book", `{text:"T",click_event:{action:"show_dialog",dialog:{type:"notice",title:"T",body:{type:"item",item:{id:"written_book",components:{written_book_content:{title:"T",author:"A",pages:[{nbt:"v",storage:"t:s"}]}}}}}}}`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			page, n, err := nbt.ParseValueTag(tc.page)
			if err != nil || n != len(tc.page) {
				t.Fatalf("fixture: %v, consumed %d/%d", err, n, len(tc.page))
			}
			entry := writtenBookEntry(nbt.Compound{"pages": nbt.NewList([]nbt.Tag{page})})
			content := entry["components"].(nbt.Compound)["minecraft:written_book_content"]
			stack, stored := StoredStack(entry)
			var read protocol.ItemStack
			decoded := ReadComponent(&read, "minecraft:written_book_content", content)
			refused := ComponentRefused("minecraft:written_book_content", content)
			if stored != tc.valid || decoded != tc.valid || refused == tc.valid {
				t.Fatalf("StoredStack=%v ReadComponent=%v ComponentRefused=%v, want valid=%v", stored, decoded, refused, tc.valid)
			}
			if !tc.valid {
				return
			}
			for range 2 {
				book, ok := stack.WrittenBookOf()
				if !ok || book.Resolved || len(book.Pages) != 1 {
					t.Fatalf("book=%+v, ok=%v", book, ok)
				}
				raw, filtered := filterableOf(page)
				if !nbt.Equal(protocol.TextTag(book.Pages[0].Raw), raw) {
					t.Fatalf("raw page changed: %v", protocol.TextTag(book.Pages[0].Raw))
				}
				twin := book.Pages[0].Filtered
				if (twin != nil) != (filtered != nil) || (twin != nil && !nbt.Equal(protocol.TextTag(*twin), filtered)) {
					t.Fatalf("filtered page changed: %+v", twin)
				}
				stack, ok = StoredStack(StackTag(stack))
				if !ok {
					t.Fatal("round trip refused")
				}
			}
		})
	}
}
