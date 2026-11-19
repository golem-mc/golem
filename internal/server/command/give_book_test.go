package command

import (
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
