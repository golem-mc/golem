package registry

import (
	"reflect"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/nbt"
)

func TestPartialBookTextKeepsSafeStyle(t *testing.T) {
	for _, tc := range []struct{ input, want string }{
		{`{text:"T",bold:"yes",italic:false,color:"red"}`, `{text:"T",italic:false,color:"red"}`},
		{`{text:"T",bold:true,font:"Bad Font",shadow_color:[1.0f]}`, `{text:"T",bold:true}`},
		{`{text:"T",extra:[{text:"child",bold:"yes",underlined:true}]}`, `{text:"T",extra:[{text:"child",underlined:true}]}`},
		{`{text:"T",extra:5}`, `{text:"T"}`},
		{`{text:"T",extra:[]}`, `{text:"T"}`},
		{`{text:"T",extra:[{nbt:"v"}]}`, `{text:"T"}`},
		{`{text:"T",extra:[{text:"child",extra:5}]}`, `{text:"T",extra:[{text:"child"}]}`},
		{`{text:"T",hover_event:{action:"show_text",value:{text:"hover",bold:"yes"}}}`, `{text:"T",hover_event:{action:"show_text",value:{text:"hover"}}}`},
		{`{nbt:"v",storage:"t:s",bold:"yes"}`, `{nbt:"v",storage:"t:s"}`},
		{`{selector:"@s",bold:"yes"}`, `{selector:"@s"}`},
		{`{score:{name:"*",objective:"o"},bold:"yes"}`, `{score:{name:"*",objective:"o"}}`},
		{`{text:"T",bold:"yes",hover_event:{action:"show_text",value:{nbt:"v",storage:"t:s"}}}`, `{text:"T",hover_event:{action:"show_text",value:{nbt:"v",storage:"t:s"}}}`},
		{`{translate:"%s",with:[{nbt:"v",storage:"t:s",bold:"yes"}]}`, `{translate:"%s",with:[{nbt:"v",storage:"t:s"}]}`},
		{`{translate:"%s",with:[7],bold:"yes"}`, `{translate:"%s",with:[7]}`},
		{`[{text:"prefix"},{nbt:"v",storage:"t:s",bold:"yes"}]`, `[{text:"prefix"},{nbt:"v",storage:"t:s"}]`},
	} {
		t.Run(tc.input, func(t *testing.T) {
			input, _, err := nbt.ParseValueTag(tc.input)
			if err != nil {
				t.Fatal(err)
			}
			original, _, _ := nbt.ParseValueTag(tc.input)
			wantTag, _, err := nbt.ParseValueTag(tc.want)
			if err != nil {
				t.Fatal(err)
			}
			want, ok := protocol.ComponentTextOf(wantTag)
			if !ok || !ComponentDecodes(wantTag) {
				t.Fatal("expected component is invalid")
			}
			got, ok := partialBookTextOf(input)
			if !ok || !protocol.TextEqual(got, want) {
				t.Fatalf("partial text = %+v, %v; want %+v", got, ok, want)
			}
			if !reflect.DeepEqual(input, original) {
				t.Fatal("partial decoding modified the stored tag")
			}
		})
	}
	for _, bad := range []string{`{nbt:"v"}`, `{text:1,bold:"yes"}`, `{nbt:"[",storage:"t:s",bold:"yes"}`, `{selector:"@invalid",bold:"yes"}`} {
		tag, err := nbt.ParseSNBT(bad)
		if err != nil {
			t.Fatal(err)
		}
		if got, ok := partialBookTextOf(tag); ok {
			t.Fatalf("partial decoding retained invalid content: %+v", got)
		}
	}
}

func TestContainerBookPartialsKeepDynamicPages(t *testing.T) {
	for _, page := range []string{
		`{nbt:"v",storage:"t:s"}`,
		`{selector:"@s"}`,
		`{score:{name:"*",objective:"o"}}`,
		`{text:"Hover",hover_event:{action:"show_text",value:{nbt:"v",storage:"t:s"}}}`,
		`{translate:"%s",with:[{nbt:"v",storage:"t:s"}]}`,
		`[{text:"prefix"},{nbt:"v",storage:"t:s"}]`,
	} {
		t.Run(page, func(t *testing.T) {
			tag, err := nbt.ParseSNBT(`{pages:[{nbt:"invalid"},{raw:` + page + `,filtered:` + page + `}]}`)
			if err != nil {
				t.Fatal(err)
			}
			stack, ok := StoredContainerStack(writtenBookEntry(tag))
			if !ok {
				t.Fatal("container refused partial book")
			}
			want, _, parseErr := nbt.ParseValueTag(page)
			if parseErr != nil {
				t.Fatal(parseErr)
			}
			for range 2 {
				book, ok := stack.WrittenBookOf()
				if !ok || len(book.Pages) != 1 || book.Pages[0].Filtered == nil {
					t.Fatalf("book = %+v, %v; want one dynamic page with a filtered twin", book, ok)
				}
				for _, text := range []protocol.Text{book.Pages[0].Raw, *book.Pages[0].Filtered} {
					if got := protocol.TextTag(text); !nbt.Equal(got, want) {
						t.Errorf("page = %v, want %v", got, want)
					}
				}
				stack, ok = StoredStack(StackTag(stack))
				if !ok {
					t.Fatal("recovered dynamic book did not round-trip through strict decoding")
				}
			}
		})
	}
}

func TestContainerBookPartialsKeepReadableChildren(t *testing.T) {
	for _, tc := range []struct{ input, want string }{
		{`{text:"Root",extra:[{text:"Good"},{nbt:"v"}]}`, `{text:"Root",extra:[{text:"Good"}]}`},
		{`[{nbt:"v"},{text:"Good"},{text:"Last",bold:"yes"}]`, `[{text:"Good"},{text:"Last"}]`},
		{`{text:"Root",extra:[[{nbt:"v"},{text:"Good"}],[{nbt:"v"}]]}`, `{text:"Root",extra:[[{text:"Good"}]]}`},
		{`{translate:"%s",with:[{nbt:"v"},{text:"Good"}]}`, `{translate:"%s",with:[{text:"Good"}]}`},
		{`{translate:"%s",with:[7,{nbt:"v"}]}`, `{translate:"%s",with:[7]}`},
		{`{text:"Root",hover_event:{action:"show_text",value:[{text:"Good"},{nbt:"v"}]}}`, `{text:"Root",hover_event:{action:"show_text",value:[{text:"Good"}]}}`},
	} {
		t.Run(tc.input, func(t *testing.T) {
			tag, err := nbt.ParseSNBT(`{pages:[{raw:` + tc.input + `,filtered:` + tc.input + `}]}`)
			if err != nil {
				t.Fatal(err)
			}
			entry := writtenBookEntry(tag)
			if _, ok := StoredStack(entry); ok {
				t.Fatal("strict decoding accepted invalid children")
			}
			content := entry["components"].(nbt.Compound)["minecraft:written_book_content"]
			var scratch protocol.ItemStack
			if ReadComponent(&scratch, "minecraft:written_book_content", content) || !ComponentRefused("minecraft:written_book_content", content) {
				t.Fatal("strict component validation accepted invalid children")
			}
			stack, ok := StoredContainerStack(entry)
			if !ok {
				t.Fatal("container refused partial book")
			}
			want, _, parseErr := nbt.ParseValueTag(tc.want)
			if parseErr != nil {
				t.Fatal(parseErr)
			}
			wantText, ok := protocol.ComponentTextOf(want)
			if !ok {
				t.Fatal("expected component is invalid")
			}
			for range 2 {
				book, ok := stack.WrittenBookOf()
				if !ok || len(book.Pages) != 1 || book.Pages[0].Filtered == nil {
					t.Fatalf("book = %+v, %v; want one recovered page with a filtered twin", book, ok)
				}
				for _, text := range []protocol.Text{book.Pages[0].Raw, *book.Pages[0].Filtered} {
					if !protocol.TextEqual(text, wantText) {
						t.Errorf("page = %+v, want %+v", text, wantText)
					}
				}
				saved, ok := StoredStack(StackTag(stack))
				if !ok || !saved.Equal(stack) {
					t.Fatal("recovered children did not round-trip through strict decoding")
				}
				stack = saved
			}
		})
	}
}
