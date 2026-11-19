package command

import (
	"strings"
	"testing"
)

func TestGiveWrittenBookParses(t *testing.T) {
	stack, ok := parseItemStack(`written_book[written_book_content={title:"T",author:"A",generation:1,pages:['"p1"','"p2"']}]`)
	if !ok {
		t.Fatal("did not parse")
	}
	book, ok := stack.WrittenBookOf()
	if !ok || book.Title != "T" || book.Author != "A" || book.Generation != 1 || len(book.Pages) != 2 {
		t.Fatalf("book = %+v ok=%v", book, ok)
	}
}

func TestGiveRefusesMalformedBook(t *testing.T) {
	for _, tc := range []struct{ key, value string }{
		{"minecraft:written_book_content", `{title:"` + strings.Repeat("x", 33) + `",author:"A"}`},
		{"minecraft:written_book_content", `{title:"T"}`},
		{"minecraft:written_book_content", `{title:"T",author:"A",generation:4}`},
		{"minecraft:written_book_content", `{title:"T",author:"A",pages:[{nbt:"v"}]}`},
		{"minecraft:written_book_content", `{title:"T",author:"A",pages:[{nbt:"[",storage:"t:s"}]}`},
		{"minecraft:written_book_content", `{title:"T",author:"A",pages:[{nbt:"v",storage:"Not Valid"}]}`},
		{"minecraft:written_book_content", `{title:"T",author:"A",pages:[{text:"T",bold:"yes"}]}`},
		{"minecraft:written_book_content", `{title:"T",author:"A",pages:[{raw:"T",filtered:{nbt:"v"}}]}`},
		{"minecraft:written_book_content", `{title:"T",author:"A",pages:[{text:"T",hover_event:{action:"show_text",value:{nbt:"v"}}}]}`},
		{"minecraft:written_book_content", `{title:"T",author:"A",pages:[{selector:"@invalid"}]}`},
		{"minecraft:written_book_content", `{title:"T",author:"A",pages:[42]}`},
		{"minecraft:written_book_content", `{title:"T",author:"A",pages:[[]]}`},
		{"minecraft:writable_book_content", `{pages:[` + strings.Repeat(`"a",`, 101) + `"a"]}`},
	} {
		if msg := componentValueMalformed(tc.key, tc.value); msg == "" {
			t.Errorf("%s %.50s accepted", tc.key, tc.value)
		}
	}
	if msg := componentValueMalformed("minecraft:written_book_content", `{title:"T",author:"A"}`); msg != "" {
		t.Errorf("valid book refused: %s", msg)
	}
}
