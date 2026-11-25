package registry

import (
	"maps"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/nbt"
)

// The NBT forms of the two book components. A page is a Filterable: the
// codec writes it as {raw, filtered?} and reads a bare value as a page
// without a filtered twin.
// vanilla: Filterable.codec, WritableBookContent.CODEC, WrittenBookContent.CODEC

// filterableTag writes one page or title as the full Filterable form.
func filterableTag(raw nbt.Tag, filtered nbt.Tag) *nbt.OrderedCompound {
	tag := nbt.NewOrderedCompound(1)
	tag.Set("raw", raw)
	if filtered != nil {
		tag.Set("filtered", filtered)
	}
	return tag
}

// filterableOf splits a Filterable value into its raw and filtered tags,
// accepting the bare form.
func filterableOf(value nbt.Tag) (raw, filtered nbt.Tag) {
	entry, isCompound := nbt.CompoundView(value)
	if !isCompound {
		return value, nil
	}
	// The full form is tried first and ignores keys it does not name, so
	// any compound with raw is one; a text component without raw is bare.
	raw, hasRaw := entry["raw"]
	if !hasRaw {
		return value, nil
	}
	return raw, entry["filtered"]
}

// boolValue reads a boolean field: any number, true when it is not zero.
// vanilla: NbtOps.getBooleanValue
func boolValue(tag nbt.Tag) (value, ok bool) {
	switch v := tag.(type) {
	case nbt.Byte:
		return v != 0, true
	case nbt.Short:
		return v != 0, true
	case nbt.Int:
		return v != 0, true
	case nbt.Long:
		return v != 0, true
	case nbt.Float:
		return v != 0, true
	case nbt.Double:
		return v != 0, true
	}
	return false, false
}

func writableBookTag(book protocol.WritableBook) *nbt.OrderedCompound {
	tag := nbt.NewOrderedCompound(0)
	if len(book.Pages) == 0 {
		return tag
	}
	pages := make([]nbt.Tag, 0, len(book.Pages))
	for _, page := range book.Pages {
		var filtered nbt.Tag
		if page.Filtered != nil {
			filtered = nbt.String(*page.Filtered)
		}
		pages = append(pages, filterableTag(nbt.String(page.Raw), filtered))
	}
	tag.Set("pages", nbt.List{Elem: nbt.TypeCompound, Items: pages})
	return tag
}

func writableBookOf(value nbt.Tag) (protocol.WritableBook, bool) {
	tag, ok := nbt.CompoundView(value)
	if !ok {
		return protocol.WritableBook{}, false
	}
	var book protocol.WritableBook
	if pages, present := tag["pages"]; present {
		list, ok := pages.(nbt.List)
		if !ok || len(list.Items) > 100 || (len(list.Items) > 0 && list.Elem != nbt.TypeCompound && list.Elem != nbt.TypeString) {
			return protocol.WritableBook{}, false
		}
		for _, item := range list.Items {
			raw, filtered := filterableOf(item)
			text, ok := raw.(nbt.String)
			if !ok {
				return protocol.WritableBook{}, false
			}
			page := protocol.FilterablePage{Raw: string(text)}
			if filtered != nil {
				twin, ok := filtered.(nbt.String)
				if !ok {
					return protocol.WritableBook{}, false
				}
				s := string(twin)
				page.Filtered = &s
			}
			book.Pages = append(book.Pages, page)
		}
	}
	return book, true
}

func writtenBookTag(book protocol.WrittenBook) *nbt.OrderedCompound {
	var filteredTitle nbt.Tag
	if book.FilteredTitle != nil {
		filteredTitle = nbt.String(*book.FilteredTitle)
	}
	tag := nbt.NewOrderedCompound(2)
	tag.Set("title", filterableTag(nbt.String(book.Title), filteredTitle))
	tag.Set("author", nbt.String(book.Author))
	if book.Generation != 0 {
		tag.Set("generation", nbt.Int(book.Generation))
	}
	if len(book.Pages) > 0 {
		pages := make([]nbt.Tag, 0, len(book.Pages))
		for _, page := range book.Pages {
			var filtered nbt.Tag
			if page.Filtered != nil {
				filtered = protocol.TextTag(*page.Filtered)
			}
			pages = append(pages, filterableTag(protocol.TextTag(page.Raw), filtered))
		}
		tag.Set("pages", nbt.List{Elem: nbt.TypeCompound, Items: pages})
	}
	if book.Resolved {
		tag.Set("resolved", nbt.Byte(1))
	}
	return tag
}

func (lookup *TagLookup) writtenBookOf(value nbt.Tag) (protocol.WrittenBook, bool) {
	return lookup.readWrittenBook(value, false)
}

// readWrittenBook optionally keeps the page codecs' safe partial values.
// vanilla: WrittenBookContent.CODEC, Filterable.codec
func (lookup *TagLookup) readWrittenBook(value nbt.Tag, partial bool) (protocol.WrittenBook, bool) {
	tag, ok := nbt.CompoundView(value)
	if !ok {
		return protocol.WrittenBook{}, false
	}
	var book protocol.WrittenBook
	title, present := tag["title"]
	if !present {
		return protocol.WrittenBook{}, false
	}
	raw, filtered := filterableOf(title)
	text, ok := raw.(nbt.String)
	if !ok {
		return protocol.WrittenBook{}, false
	}
	book.Title = string(text)
	if filtered != nil {
		twin, ok := filtered.(nbt.String)
		if !ok {
			return protocol.WrittenBook{}, false
		}
		s := string(twin)
		book.FilteredTitle = &s
	}
	author, ok := nbt.Get[nbt.String](tag, "author")
	if !ok {
		return protocol.WrittenBook{}, false
	}
	book.Author = string(author)
	if generation, present := tag["generation"]; present {
		g, ok := nbt.IntValue(generation)
		if !ok || g < 0 || g > 3 {
			return protocol.WrittenBook{}, false
		}
		book.Generation = g
	}
	if pages, present := tag["pages"]; present {
		list, ok := pages.(nbt.List)
		if !ok {
			return protocol.WrittenBook{}, false
		}
		readText := func(tag nbt.Tag) (protocol.Text, bool) {
			if !lookup.componentDecodes(tag) {
				return protocol.Text{}, false
			}
			return protocol.ComponentTextOf(tag)
		}
		if partial {
			readText = lookup.partialBookTextOf
		}
		for _, item := range list.Items {
			raw, filtered := filterableOf(nbt.ListElement(item))
			text, ok := readText(raw)
			if !ok {
				if partial {
					// ListCodec's partial list contains only readable pages.
					continue
				}
				return protocol.WrittenBook{}, false
			}
			page := protocol.WrittenPage{Raw: text}
			if filtered != nil {
				twin, ok := readText(filtered)
				if !ok {
					if !partial {
						return protocol.WrittenBook{}, false
					}
				} else {
					page.Filtered = &twin
				}
			}
			book.Pages = append(book.Pages, page)
		}
	}
	if resolved, present := tag["resolved"]; present {
		b, ok := boolValue(resolved)
		if !ok {
			return protocol.WrittenBook{}, false
		}
		book.Resolved = b
	}
	return book, true
}

// partialBookTextOf reads a stored component's partial value, dropping
// invalid optional style fields while still requiring valid content.
// vanilla: Style.Serializer.MAP_CODEC, OptionalFieldCodec.decode
func (lookup *TagLookup) partialBookTextOf(tag nbt.Tag) (protocol.Text, bool) {
	if !protocol.RawTextFits(tag) {
		return protocol.Text{}, false
	}
	tag = lookup.partialBookTextTag(tag)
	if !lookup.componentDecodes(tag) {
		return protocol.Text{}, false
	}
	return protocol.ComponentTextOf(tag)
}

// partialBookTextTag copies readable children and optional fields.
func (lookup *TagLookup) partialBookTextTag(tag nbt.Tag) nbt.Tag {
	switch v := tag.(type) {
	case nbt.List:
		return lookup.partialBookTextList(v, lookup.componentDecodes)
	case nbt.CompoundTag:
		out := maps.Clone(v.Entries())
		for key, value := range v.Entries() {
			switch key {
			case "extra":
				// OptionalFieldCodec retains the empty default when the
				// non-empty sibling list has no readable partial value.
				// vanilla: ComponentSerialization.createCodec
				delete(out, key)
				if list, ok := value.(nbt.List); ok {
					if siblings := lookup.partialBookTextList(list, lookup.componentDecodes); len(siblings.Items) > 0 {
						out[key] = siblings
					}
				}
			case "separator":
				out[key] = lookup.partialBookTextTag(value)
			case "with":
				if list, ok := value.(nbt.List); ok {
					out[key] = lookup.partialBookTextList(list, lookup.textContentArgDecodes)
				}
			case "hover_event":
				if event, ok := nbt.CompoundView(value); ok {
					event = maps.Clone(event)
					for _, field := range [...]string{"value", "name"} {
						if nested, present := event[field]; present {
							event[field] = lookup.partialBookTextTag(nested)
						}
					}
					out[key] = event
				}
			}
		}
		for _, key := range [...]string{
			"color", "shadow_color", "bold", "italic", "underlined",
			"strikethrough", "obfuscated", "click_event", "hover_event", "insertion", "font",
		} {
			if value, present := out[key]; present {
				// Validate through the strict reader so partial recovery never
				// relays a style field that the normal decoder would reject.
				if !lookup.componentDecodes(nbt.Compound{"text": nbt.String(""), key: value}) {
					delete(out, key)
				}
			}
		}
		return out
	}
	return tag
}

// partialBookTextList keeps the recursive list codec's readable partial values.
// vanilla: ComponentSerialization.createCodec, TranslatableContents.ARG_CODEC
func (lookup *TagLookup) partialBookTextList(list nbt.List, decodes func(nbt.Tag) bool) nbt.List {
	items := make([]nbt.Tag, 0, len(list.Items))
	for _, item := range list.Items {
		out := lookup.partialBookTextTag(nbt.ListElement(item))
		if !decodes(out) {
			continue
		}
		if wrapper, ok := nbt.CompoundView(item); ok && len(wrapper) == 1 && wrapper[""] != nil {
			out = nbt.Compound{"": out}
		}
		items = append(items, out)
	}
	return nbt.List{Elem: list.Elem, Items: items}
}
