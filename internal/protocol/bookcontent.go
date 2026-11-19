package protocol

import "slices"

// FilterablePage is one page of a writable book: its text and the filtered
// twin the chat filter would substitute, nil when there is none.
// vanilla: Filterable
type FilterablePage struct {
	Raw      string
	Filtered *string
}

// WritableBook is minecraft:writable_book_content.
// vanilla: WritableBookContent
type WritableBook struct {
	Pages []FilterablePage
}

// WrittenPage is one page of a signed book, a text component with its
// filtered twin.
type WrittenPage struct {
	Raw      Text
	Filtered *Text
}

// WrittenBook is minecraft:written_book_content. Generation counts the
// copies made, 0 to 3.
// vanilla: WrittenBookContent
type WrittenBook struct {
	Title         string
	FilteredTitle *string
	Author        string
	Generation    int32
	Pages         []WrittenPage
	Resolved      bool
}

// stringFits reports whether s, as the reader decodes it, is at most max
// UTF-16 code units, the unit Java's String.length() and ReadString count.
func stringFits(s string, maxUnits int) bool {
	return utf16Len(utf8Replaced(s)) <= maxUnits
}

// SetWritableBook sets minecraft:writable_book_content; false for more pages
// or longer text than the codec allows.
// vanilla: WritableBookContent.STREAM_CODEC
func (s *ItemStack) SetWritableBook(book WritableBook) bool {
	if len(book.Pages) > 100 {
		return false
	}
	var value Buffer
	value.PutVarInt(int32(len(book.Pages)))
	for _, page := range book.Pages {
		if !stringFits(page.Raw, 1024) || (page.Filtered != nil && !stringFits(*page.Filtered, 1024)) {
			return false
		}
		value.PutString(page.Raw)
		value.PutBool(page.Filtered != nil)
		if page.Filtered != nil {
			value.PutString(*page.Filtered)
		}
	}
	// A copied stack shares the Raw backing array; the write must not reach
	// the stack it came from.
	s.Raw = slices.Clone(s.Raw)
	s.putRaw(RawComponent{Type: DataComponentWritableBookContent, Data: value.Bytes()})
	return true
}

// RemoveWritableBook drops the stack's minecraft:writable_book_content from
// its patch.
func (s *ItemStack) RemoveWritableBook() { s.dropRaw(DataComponentWritableBookContent) }

// WritableBookOf returns the stack's writable book content and whether it
// carries a whole one.
func (s ItemStack) WritableBookOf() (WritableBook, bool) {
	for _, raw := range s.Raw {
		if raw.Type != DataComponentWritableBookContent {
			continue
		}
		r := NewReader(raw.Data)
		book, ok := writableBookValue(r)
		if !ok || r.Remaining() != 0 {
			return WritableBook{}, false
		}
		return book, true
	}
	return WritableBook{}, false
}

// writableBookValue decodes a writable book: the pages, each a string and
// its optional filtered twin, a hundred of them at most.
// vanilla: WritableBookContent.STREAM_CODEC
func writableBookValue(r *Reader) (WritableBook, bool) {
	pages := r.ReadVarInt()
	if r.Err() != nil || pages < 0 || pages > 100 {
		return WritableBook{}, false
	}
	book := WritableBook{Pages: make([]FilterablePage, 0, pages)}
	for range pages {
		page := FilterablePage{Raw: r.ReadString(1024)}
		if r.ReadBool() {
			filtered := r.ReadString(1024)
			page.Filtered = &filtered
		}
		book.Pages = append(book.Pages, page)
	}
	if r.Err() != nil {
		return WritableBook{}, false
	}
	return book, true
}

// SetWrittenBook sets minecraft:written_book_content; false for a title,
// author or generation outside the codec's bounds.
// vanilla: WrittenBookContent.STREAM_CODEC
func (s *ItemStack) SetWrittenBook(book WrittenBook) bool {
	if !stringFits(book.Title, 32) ||
		(book.FilteredTitle != nil && !stringFits(*book.FilteredTitle, 32)) ||
		!stringFits(book.Author, 32767) ||
		book.Generation < 0 || book.Generation > 3 {
		return false
	}
	var value Buffer
	value.PutString(book.Title)
	value.PutBool(book.FilteredTitle != nil)
	if book.FilteredTitle != nil {
		value.PutString(*book.FilteredTitle)
	}
	value.PutString(book.Author)
	value.PutVarInt(book.Generation)
	value.PutVarInt(int32(len(book.Pages)))
	for _, page := range book.Pages {
		value.PutText(page.Raw)
		value.PutBool(page.Filtered != nil)
		if page.Filtered != nil {
			value.PutText(*page.Filtered)
		}
	}
	value.PutBool(book.Resolved)
	s.Raw = slices.Clone(s.Raw)
	s.putRaw(RawComponent{Type: DataComponentWrittenBookContent, Data: value.Bytes()})
	return true
}

// WrittenBookOf returns the stack's written book content and whether it
// carries a whole one.
func (s ItemStack) WrittenBookOf() (WrittenBook, bool) {
	for _, raw := range s.Raw {
		if raw.Type != DataComponentWrittenBookContent {
			continue
		}
		r := NewReader(raw.Data)
		book, ok := writtenBookValue(r, ReadComponentText)
		if !ok || r.Remaining() != 0 {
			return WrittenBook{}, false
		}
		return book, true
	}
	return WrittenBook{}, false
}

// writtenBookValue decodes title (and its optional filtered twin), author,
// copy count (at most 3, as WrittenBookContent's constructor insists),
// pages and the resolved flag, pages as strictly as the codec. The page list
// has no count limit, so each page's budget bounds it and the count does
// not preallocate. readPage is ReadText for a stack a client sent, which
// keeps only what the model holds, and ReadComponentText for one the server
// stored.
// vanilla: WrittenBookContent.STREAM_CODEC
func writtenBookValue(r *Reader, readPage func(*Reader) (Text, bool)) (WrittenBook, bool) {
	var book WrittenBook
	book.Title = r.ReadString(32)
	if r.ReadBool() {
		filtered := r.ReadString(32)
		book.FilteredTitle = &filtered
	}
	book.Author = r.ReadString(32767)
	book.Generation = r.ReadVarInt()
	pages := r.ReadVarInt()
	if r.Err() != nil || pages < 0 || book.Generation < 0 || book.Generation > 3 {
		return WrittenBook{}, false
	}
	for range pages {
		raw, ok := readPage(r)
		if !ok {
			return WrittenBook{}, false
		}
		page := WrittenPage{Raw: raw}
		if r.ReadBool() {
			filtered, ok := readPage(r)
			if !ok {
				return WrittenBook{}, false
			}
			page.Filtered = &filtered
		}
		book.Pages = append(book.Pages, page)
	}
	book.Resolved = r.ReadBool()
	if r.Err() != nil {
		return WrittenBook{}, false
	}
	return book, true
}
