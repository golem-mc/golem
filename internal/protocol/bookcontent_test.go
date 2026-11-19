package protocol

import (
	"strings"
	"testing"
)

// TestBookLengthsCountUTF16Units pins the setters' length caps to the unit
// the readers count: an astral rune is two UTF-16 units, so a string within
// the cap in runes can still be over it.
func TestBookLengthsCountUTF16Units(t *testing.T) {
	emoji := func(n int) string { return strings.Repeat("\U0001F600", n) }
	var stack ItemStack
	if !stack.SetWritableBook(WritableBook{Pages: []FilterablePage{{Raw: emoji(512)}}}) {
		t.Error("SetWritableBook refused a page of 1024 units")
	}
	if _, ok := stack.WritableBookOf(); !ok {
		t.Error("WritableBookOf refused a page of 1024 units")
	}
	if stack.SetWritableBook(WritableBook{Pages: []FilterablePage{{Raw: emoji(512) + "a"}}}) {
		t.Error("SetWritableBook took a page of 1025 units")
	}
	long := emoji(512) + "a"
	if stack.SetWritableBook(WritableBook{Pages: []FilterablePage{{Raw: "a", Filtered: &long}}}) {
		t.Error("SetWritableBook took a filtered page of 1025 units")
	}
	if !stack.SetWrittenBook(WrittenBook{Title: emoji(16), Author: "a"}) {
		t.Error("SetWrittenBook refused a title of 32 units")
	}
	if _, ok := stack.WrittenBookOf(); !ok {
		t.Error("WrittenBookOf refused a title of 32 units")
	}
	if stack.SetWrittenBook(WrittenBook{Title: emoji(16) + "a", Author: "a"}) {
		t.Error("SetWrittenBook took a title of 33 units")
	}
	longTitle := emoji(16) + "a"
	if stack.SetWrittenBook(WrittenBook{Title: "t", FilteredTitle: &longTitle, Author: "a"}) {
		t.Error("SetWrittenBook took a filtered title of 33 units")
	}
	if !stack.SetWrittenBook(WrittenBook{Title: "t", Author: strings.Repeat("a", 32767)}) {
		t.Error("SetWrittenBook refused an author of 32767 units")
	}
	if stack.SetWrittenBook(WrittenBook{Title: "t", Author: emoji(16383) + "aa"}) {
		t.Error("SetWrittenBook took an author of 32768 units")
	}
}
