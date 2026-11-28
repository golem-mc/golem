package protocol

import (
	"encoding/binary"
	"encoding/json"
	"maps"
	"math"
	"slices"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"github.com/golem-mc/golem/nbt"
)

// colorNames are the colour names the game's own component codec reads.
// vanilla: TextColor.NAMED_COLORS
var colorNames = map[string]bool{
	"black": true, "dark_blue": true, "dark_green": true, "dark_aqua": true,
	"dark_red": true, "dark_purple": true, "gold": true, "gray": true,
	"dark_gray": true, "blue": true, "green": true, "aqua": true,
	"red": true, "light_purple": true, "yellow": true, "white": true,
}

// validColor accepts the sixteen names and #rrggbb. The client's
// Integer.parseInt reads wider forms ("#+12345"), but anything else is
// refused here rather than relayed.
// vanilla: TextColor.parseColor
func validColor(color string) bool {
	if strings.HasPrefix(color, "#") {
		if len(color) != 7 {
			return false
		}
		for _, c := range color[1:] {
			if c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F' {
				continue
			}
			return false
		}
		return true
	}
	return colorNames[color]
}

// Colours a text component can be given; the client picks the shade from
// its own palette.
const (
	ColorRed    = "red"
	ColorYellow = "yellow"
	ColorGreen  = "green"
	ColorGray   = "gray"
	ColorAqua   = "aqua"
	ColorGold   = "gold"
)

// Text is a text component: literal text, or a translation key the client
// fills in from its own language files.
type Text struct {
	// Text is the literal content, ignored when Translate is set.
	Text string

	// Translate is a translation key, such as "multiplayer.player.joined";
	// the client substitutes With into its placeholders.
	Translate string
	With      []Text

	// Extra is the sibling components, rendered after this one; the vanilla
	// server assembles a ", "-separated list of names this way.
	Extra []Text

	// Color names the colour the message is drawn in, empty for the default.
	Color string

	// Bold, Italic and Underline are text decorations, set as byte tags the
	// way the vanilla server writes them when it styles a command error.
	Bold      bool
	Italic    bool
	Underline bool

	// Insertion is what a shift-click puts in the chat box; entity names
	// carry their UUID so it pastes as a selector.
	Insertion string

	// ClickCommand is the command a click suggests typing, what the vanilla
	// server's command error context carries; empty for no click at all.
	ClickCommand string

	// ClickCopy is the text a click puts on the clipboard (vanilla:
	// ClickEvent.CopyToClipboard); empty for none. Ignored when ClickCommand is set.
	ClickCopy string

	// ClickURL is the URL a click opens (open_url); empty for none. Only
	// written when ClickCommand is empty.
	ClickURL string

	// ClickRun is the command a click runs (run_command); empty for none.
	// Only written when no click above is set.
	ClickRun string

	// ClickPage is the book page a click turns to (change_page), from 1; 0
	// for none. Only written when no click above is set.
	ClickPage int32

	// HoverEntity is the show-entity hover the vanilla server hangs on an
	// entity's name; nil for no hover at all.
	HoverEntity *HoverEntity

	// HoverItem is the show-item hover the vanilla server hangs on an item's
	// name in command feedback; nil for no hover at all.
	HoverItem *HoverItem

	// HoverText is the show-text hover: a tooltip of the text given; nil for
	// no hover at all. Only one of the three hovers is written, entity first.
	HoverText *Text

	// Float is a translation argument shown as a fraction (an angle in a
	// feedback). A Text with a Float is only that float; nil otherwise.
	Float *float32

	// Number is a translation argument shown as a whole number (a count). A
	// Text with a Number is only that number.
	Number *int64

	// Long is a whole-number argument too wide for an int32 (the block
	// count a /fill was too big by). A Text with a Long is only that long.
	Long *int64

	// Raw keeps a component or primitive translation argument as its tag,
	// preserving styles and numeric widths the model has no field for.
	// A Text with a Raw is only that tag; see RawTextOf.
	Raw nbt.Tag
}

// HoverEntity is a show-entity hover event: the tooltip name, the entity
// type and the UUID.
type HoverEntity struct {
	Name Text
	ID   string
	UUID [4]uint32
}

// HoverItem is a show-item hover event: item, count and the components
// compound, whole, since what the tooltip shows is the caller's business.
type HoverItem struct {
	ID         string
	Count      int32
	Components nbt.Compound
}

// NBT tag types, named here so the tag numbers have one home: a text
// component is either a bare string tag or a compound holding its fields.
const (
	tagEnd      = uint8(nbt.TypeEnd)
	tagByte     = uint8(nbt.TypeByte)
	tagString   = uint8(nbt.TypeString)
	tagList     = uint8(nbt.TypeList)
	tagFloat    = uint8(nbt.TypeFloat)
	tagCompound = uint8(nbt.TypeCompound)
	tagInt      = uint8(nbt.TypeInt)
	tagLong     = uint8(nbt.TypeLong)
	tagIntArray = uint8(nbt.TypeIntArray)
)

// PutText writes a text component. Network NBT leaves the root tag unnamed,
// so the tag type is followed straight away by its payload.
func (b *Buffer) PutText(t Text) {
	if t.Raw != nil {
		b.PutU8(uint8(t.Raw.Type()))
		b.putNBTValue(t.Raw)
		return
	}
	// A component that is nothing but text is written as a string, both
	// what the vanilla server does and the shortest thing on the wire.
	if t.Translate == "" && t.Color == "" && !t.Bold && !t.Italic && !t.Underline &&
		t.Insertion == "" && !t.hasClick() && t.HoverEntity == nil &&
		t.HoverItem == nil && t.HoverText == nil && len(t.Extra) == 0 {
		if t.Float != nil {
			b.PutU8(tagFloat)
			b.PutF32(*t.Float)
			return
		}
		if t.Number != nil {
			// A number on its own is a number tag, as vanilla hands a
			// count to a translation.
			b.PutU8(tagInt)
			b.PutI32(int32(*t.Number))
			return
		}
		if t.Long != nil {
			// The same idea at its wider width, as vanilla hands the
			// block count a /fill was too big by.
			b.PutU8(tagLong)
			b.PutI64(*t.Long)
			return
		}
		b.PutU8(tagString)
		b.putModifiedUTF8(t.Text)
		return
	}
	b.PutU8(tagCompound)
	b.putTextBody(t)
}

// putTextBody writes the fields of a component and the tag that ends them,
// which is the payload of a compound tag.
func (b *Buffer) putTextBody(t Text) {
	if t.Raw != nil {
		if fields, ok := nbt.CompoundView(t.Raw); ok {
			b.putNBTCompoundBody(fields)
		} else {
			// Compound lists unwrap an element's single anonymous tag.
			// vanilla: ListTag.tryUnwrap
			b.PutU8(uint8(t.Raw.Type()))
			b.putModifiedUTF8("")
			b.putNBTValue(t.Raw)
			b.PutU8(tagEnd)
		}
		return
	}
	if t.Translate != "" {
		b.putNBTString("translate", t.Translate)
		if len(t.With) > 0 {
			// A list has one element type: counts
			// alone as numbers, plain text alone as strings, anything else
			// as compounds, where a count or plain text is wrapped as the
			// one anonymous tag and a richer component is written whole.
			b.PutU8(tagList)
			b.putModifiedUTF8("with")
			plain := true
			ints := true
			longs := true
			floats := true
			for _, with := range t.With {
				if with.Number == nil {
					ints = false
				}
				if with.Long == nil {
					longs = false
				}
				if with.Float == nil {
					floats = false
				}
				if !isPlainText(with) {
					plain = false
				}
			}
			switch {
			case ints:
				b.PutU8(tagInt)
				b.PutI32(int32(len(t.With)))
				for _, with := range t.With {
					b.PutI32(int32(*with.Number))
				}
			case longs:
				b.PutU8(tagLong)
				b.PutI32(int32(len(t.With)))
				for _, with := range t.With {
					b.PutI64(*with.Long)
				}
			case floats:
				b.PutU8(tagFloat)
				b.PutI32(int32(len(t.With)))
				for _, with := range t.With {
					b.PutF32(*with.Float)
				}
			case plain:
				b.PutU8(tagString)
				b.PutI32(int32(len(t.With)))
				for _, with := range t.With {
					b.putModifiedUTF8(with.Text)
				}
			default:
				b.PutU8(tagCompound)
				b.PutI32(int32(len(t.With)))
				for _, with := range t.With {
					b.putTextListElement(with)
				}
			}
		}
	} else {
		b.putNBTString("text", t.Text)
	}
	if t.ClickCommand != "" {
		// The click event is a compound of its own, as vanilla writes the
		// suggest-command one.
		b.PutU8(tagCompound)
		b.putModifiedUTF8("click_event")
		b.putNBTString("action", "suggest_command")
		b.putNBTString("command", t.ClickCommand)
		b.PutU8(tagEnd)
	} else if t.ClickCopy != "" {
		b.PutU8(tagCompound)
		b.putModifiedUTF8("click_event")
		b.putNBTString("action", "copy_to_clipboard")
		b.putNBTString("value", t.ClickCopy)
		b.PutU8(tagEnd)
	} else if t.ClickURL != "" {
		b.PutU8(tagCompound)
		b.putModifiedUTF8("click_event")
		b.putNBTString("action", "open_url")
		b.putNBTString("url", t.ClickURL)
		b.PutU8(tagEnd)
	} else if t.ClickRun != "" {
		b.PutU8(tagCompound)
		b.putModifiedUTF8("click_event")
		b.putNBTString("action", "run_command")
		b.putNBTString("command", t.ClickRun)
		b.PutU8(tagEnd)
	} else if t.ClickPage != 0 {
		b.PutU8(tagCompound)
		b.putModifiedUTF8("click_event")
		b.putNBTString("action", "change_page")
		b.putNBTInt("page", t.ClickPage)
		b.PutU8(tagEnd)
	}
	if t.Color != "" {
		b.putNBTString("color", t.Color)
	}
	if t.Bold {
		b.putNBTByte("bold", 1)
	}
	if t.Italic {
		b.putNBTByte("italic", 1)
	}
	if t.Underline {
		b.putNBTByte("underlined", 1)
	}
	if t.Insertion != "" {
		b.putNBTString("insertion", t.Insertion)
	}
	if t.HoverEntity != nil {
		// Name, action, entity type, and the UUID as four ints.
		b.PutU8(tagCompound)
		b.putModifiedUTF8("hover_event")
		if name := t.HoverEntity.Name; isPlainText(name) {
			// A plain name travels as the bare string vanilla writes a
			// player's name as.
			b.putNBTString("name", name.Text)
		} else {
			b.PutU8(tagCompound)
			b.putModifiedUTF8("name")
			b.putTextBody(name)
		}
		b.putNBTString("action", "show_entity")
		b.putNBTString("id", t.HoverEntity.ID)
		b.PutU8(tagIntArray)
		b.putModifiedUTF8("uuid")
		b.PutI32(4)
		for _, word := range t.HoverEntity.UUID {
			b.PutI32(int32(word))
		}
		b.PutU8(tagEnd)
	}
	if t.HoverItem != nil {
		// The show-item hover is the same kind of compound, with the item's
		// name and — where they are not one — its count and components.
		b.PutU8(tagCompound)
		b.putModifiedUTF8("hover_event")
		if t.HoverItem.Count != 1 {
			b.putNBTInt("count", t.HoverItem.Count)
		}
		if len(t.HoverItem.Components) > 0 {
			b.PutU8(tagCompound)
			b.putModifiedUTF8("components")
			b.putNBTCompoundBody(t.HoverItem.Components)
		}
		b.putNBTString("action", "show_item")
		b.putNBTString("id", t.HoverItem.ID)
		b.PutU8(tagEnd)
	}
	if t.HoverText != nil && t.HoverEntity == nil && t.HoverItem == nil {
		b.PutU8(tagCompound)
		b.putModifiedUTF8("hover_event")
		b.putNBTString("action", "show_text")
		if isPlainText(*t.HoverText) {
			b.putNBTString("value", t.HoverText.Text)
		} else if t.HoverText.Raw != nil {
			b.PutU8(uint8(t.HoverText.Raw.Type()))
			b.putModifiedUTF8("value")
			b.putNBTValue(t.HoverText.Raw)
		} else {
			b.PutU8(tagCompound)
			b.putModifiedUTF8("value")
			b.putTextBody(*t.HoverText)
		}
		b.PutU8(tagEnd)
	}
	if len(t.Extra) > 0 {
		// A list is homogeneous, so every sibling goes in as a compound
		// even where a bare string would have done.
		b.PutU8(tagList)
		b.putModifiedUTF8("extra")
		b.PutU8(tagCompound)
		b.PutI32(int32(len(t.Extra)))
		for _, e := range t.Extra {
			if e.Raw != nil {
				b.putTextListElement(e)
			} else {
				b.putTextBody(e)
			}
		}
	}
	b.PutU8(tagEnd)
}

// hasClick reports whether t carries any click event; a new click field must
// be added here.
func (t Text) hasClick() bool {
	return t.ClickCommand != "" || t.ClickCopy != "" || t.ClickURL != "" || t.ClickRun != "" || t.ClickPage != 0
}

// isPlainText says whether a component renders as its own text and nothing
// else, deciding whether it can travel as a bare string.
func isPlainText(t Text) bool {
	return t.Raw == nil && t.Translate == "" && t.Number == nil && t.Long == nil && t.Float == nil && t.Color == "" && !t.Bold && !t.Italic && !t.Underline &&
		t.Insertion == "" && !t.hasClick() && t.HoverEntity == nil && t.HoverItem == nil &&
		t.HoverText == nil && len(t.Extra) == 0 && len(t.With) == 0
}

// putTextListElement writes a translation argument or raw sibling in a
// compound list, wrapping non-compound tags under the empty key.
func (b *Buffer) putTextListElement(t Text) {
	switch {
	case t.Number != nil:
		b.PutU8(tagInt)
		b.putModifiedUTF8("")
		b.PutI32(int32(*t.Number))
		b.PutU8(tagEnd)
	case t.Long != nil:
		b.PutU8(tagLong)
		b.putModifiedUTF8("")
		b.PutI64(*t.Long)
		b.PutU8(tagEnd)
	case t.Float != nil:
		b.PutU8(tagFloat)
		b.putModifiedUTF8("")
		b.PutF32(*t.Float)
		b.PutU8(tagEnd)
	case isPlainText(t):
		b.PutU8(tagString)
		b.putModifiedUTF8("")
		b.putModifiedUTF8(t.Text)
		b.PutU8(tagEnd)
	case t.Raw != nil:
		if fields, ok := nbt.CompoundView(t.Raw); ok {
			b.putNBTCompoundBody(fields)
			return
		}
		b.PutU8(uint8(t.Raw.Type()))
		b.putModifiedUTF8("")
		b.putNBTValue(t.Raw)
		b.PutU8(tagEnd)
	default:
		b.putTextBody(t)
	}
}

// putNBTString writes a named string tag.
func (b *Buffer) putNBTString(name, value string) {
	b.PutU8(tagString)
	b.putModifiedUTF8(name)
	b.putModifiedUTF8(value)
}

// putNBTByte writes a named byte tag, used for the boolean-valued
// decorations on a text component.
func (b *Buffer) putNBTByte(name string, value byte) {
	b.PutU8(tagByte)
	b.putModifiedUTF8(name)
	b.PutU8(value)
}

// putNBTInt writes a named integer tag, used for the count an item hover
// previews.
func (b *Buffer) putNBTInt(name string, value int32) {
	b.PutU8(tagInt)
	b.putModifiedUTF8(name)
	b.PutI32(value)
}

// putNBTCompoundBody writes a compound's fields and its end tag: the payload
// of a compound, how an item hover's components travel.
func (b *Buffer) putNBTCompoundBody(fields nbt.Compound) {
	for _, name := range slices.Sorted(maps.Keys(fields)) {
		value := fields[name]
		b.PutU8(uint8(value.Type()))
		b.putModifiedUTF8(name)
		b.putNBTValue(value)
	}
	b.PutU8(tagEnd)
}

// putNBTValue writes one tag's payload, including arbitrary click and hover data.
func (b *Buffer) putNBTValue(value nbt.Tag) {
	switch v := value.(type) {
	case *nbt.OrderedCompound:
		b.putNBTValue(v.Entries())
	case nbt.Byte:
		b.PutU8(uint8(v))
	case nbt.Short:
		b.PutI16(int16(v))
	case nbt.Int:
		b.PutI32(int32(v))
	case nbt.Long:
		b.PutI64(int64(v))
	case nbt.Float:
		b.PutF32(float32(v))
	case nbt.Double:
		b.PutF64(float64(v))
	case nbt.String:
		b.putModifiedUTF8(string(v))
	case nbt.CompoundTag:
		compoundFields := v.Entries()
		b.putNBTCompoundBody(compoundFields)
	case nbt.List:
		// Decode unwraps mixed entries; rebuild their shared wire type and wrappers.
		// vanilla: ListTag.write, ListTag.identifyRawElementType
		v = nbt.NewList(v.Items)
		b.PutU8(uint8(v.Elem))
		b.PutI32(int32(len(v.Items)))
		for _, item := range v.Items {
			b.putNBTValue(item)
		}
	case nbt.ByteArray:
		b.PutI32(int32(len(v)))
		b.PutRaw(v)
	case nbt.IntArray:
		b.PutI32(int32(len(v)))
		for _, n := range v {
			b.PutI32(n)
		}
	case nbt.LongArray:
		b.PutI32(int32(len(v)))
		for _, n := range v {
			b.PutI64(n)
		}
	}
}

// PutTextComponent writes a text component holding nothing but text.
func (b *Buffer) PutTextComponent(text string) { b.PutText(Text{Text: text}) }

// putModifiedUTF8 writes a string the way NBT stores one, the same on the
// wire as in a file: see nbt.AppendString for the encoding.
func (b *Buffer) putModifiedUTF8(s string) {
	data, ok := nbt.AppendString(b.data, s)
	if !ok {
		// Put cannot fail and no length fits, so it goes out empty: a
		// wrapped length would misalign the rest of the packet.
		b.PutI16(0)
		return
	}
	b.data = data
}

// PutJSONTextComponent writes a plain text component in the JSON form still
// used by the login Disconnect packet.
func (b *Buffer) PutJSONTextComponent(text string) {
	data, err := json.Marshal(struct {
		Text string `json:"text"`
	}{text})
	if err != nil {
		// A string always marshals.
		data = []byte(`{"text":""}`)
	}
	b.PutString(string(data))
}

// JSONText serializes a component for the login Disconnect packet, which the
// client reads as JSON, not NBT. Plain text is a bare JSON string. Only text,
// translate, with and extra are written, all the login messages need.
func JSONText(t Text) string {
	data, err := json.Marshal(textJSON(t))
	if err != nil {
		return `""`
	}
	return string(data)
}

// textJSON is the value JSONText marshals: a bare string, number or float for
// a component of nothing else, an object otherwise.
func textJSON(t Text) any {
	if t.Translate == "" && t.Color == "" && !t.Bold && !t.Italic && !t.Underline &&
		t.Insertion == "" && !t.hasClick() && len(t.Extra) == 0 && len(t.With) == 0 {
		switch {
		case t.Float != nil:
			return *t.Float
		case t.Number != nil:
			return *t.Number
		case t.Long != nil:
			return *t.Long
		default:
			return t.Text
		}
	}
	m := map[string]any{}
	if t.Translate != "" {
		m["translate"] = t.Translate
	} else {
		m["text"] = t.Text
	}
	if len(t.With) > 0 {
		with := make([]any, len(t.With))
		for i, arg := range t.With {
			with[i] = textJSON(arg)
		}
		m["with"] = with
	}
	if len(t.Extra) > 0 {
		extra := make([]any, len(t.Extra))
		for i, arg := range t.Extra {
			extra[i] = textJSON(arg)
		}
		m["extra"] = extra
	}
	if t.Color != "" {
		m["color"] = t.Color
	}
	if t.Bold {
		m["bold"] = true
	}
	if t.Italic {
		m["italic"] = true
	}
	if t.Underline {
		m["underlined"] = true
	}
	if t.Insertion != "" {
		m["insertion"] = t.Insertion
	}
	if t.ClickCommand != "" {
		m["click_event"] = map[string]string{"action": "suggest_command", "command": t.ClickCommand}
	} else if t.ClickCopy != "" {
		m["click_event"] = map[string]string{"action": "copy_to_clipboard", "value": t.ClickCopy}
	} else if t.ClickURL != "" {
		m["click_event"] = map[string]string{"action": "open_url", "url": t.ClickURL}
	} else if t.ClickRun != "" {
		m["click_event"] = map[string]string{"action": "run_command", "command": t.ClickRun}
	} else if t.ClickPage != 0 {
		m["click_event"] = map[string]any{"action": "change_page", "page": t.ClickPage}
	}
	return m
}

// TextTag writes a text component as a file stores it, PutText's file-side
// twin.
func TextTag(t Text) nbt.Tag {
	if t.Raw != nil {
		return nativeTextTag(t.Raw)
	}
	// vanilla: TranslatableContents.ARG_CODEC
	switch {
	case t.Float != nil:
		return nbt.Float(*t.Float)
	case t.Number != nil:
		return nbt.Int(*t.Number)
	case t.Long != nil:
		return nbt.Long(*t.Long)
	case isPlainText(t):
		return nbt.String(t.Text)
	}
	fields := nbt.NewOrderedCompound(4)
	if t.Translate != "" {
		fields.Set("translate", nbt.String(t.Translate))
		if len(t.With) > 0 {
			items := make([]nbt.Tag, len(t.With))
			for i, arg := range t.With {
				items[i] = TextTag(arg)
			}
			fields.Set("with", nbt.NewList(items))
		}
	} else {
		fields.Set("text", nbt.String(t.Text))
	}
	if len(t.Extra) > 0 {
		extra := make([]nbt.Tag, 0, len(t.Extra))
		for _, e := range t.Extra {
			extra = append(extra, TextTag(e))
		}
		fields.Set("extra", nbt.List{Elem: nbt.TypeCompound, Items: extra})
	}
	if t.Color != "" {
		fields.Set("color", nbt.String(t.Color))
	}
	if t.Bold {
		fields.Set("bold", nbt.Byte(1))
	}
	if t.Italic {
		fields.Set("italic", nbt.Byte(1))
	}
	if t.Underline {
		fields.Set("underlined", nbt.Byte(1))
	}
	if t.ClickCommand != "" {
		fields.Set("click_event", textEventTag("suggest_command", "command", nbt.String(t.ClickCommand)))
	} else if t.ClickCopy != "" {
		fields.Set("click_event", textEventTag("copy_to_clipboard", "value", nbt.String(t.ClickCopy)))
	} else if t.ClickURL != "" {
		fields.Set("click_event", textEventTag("open_url", "url", nbt.String(t.ClickURL)))
	} else if t.ClickRun != "" {
		fields.Set("click_event", textEventTag("run_command", "command", nbt.String(t.ClickRun)))
	} else if t.ClickPage != 0 {
		fields.Set("click_event", textEventTag("change_page", "page", nbt.Int(t.ClickPage)))
	}
	if t.HoverEntity != nil {
		name := nbt.Tag(nbt.String(t.HoverEntity.Name.Text))
		if !isPlainText(t.HoverEntity.Name) {
			name = TextTag(t.HoverEntity.Name)
		}
		uuid := nbt.IntArray(make([]int32, 4))
		for i, word := range t.HoverEntity.UUID {
			uuid[i] = int32(word)
		}
		event := nbt.NewOrderedCompound(4)
		event.Set("action", nbt.String("show_entity"))
		event.Set("id", nbt.String(t.HoverEntity.ID))
		event.Set("uuid", uuid)
		event.Set("name", name)
		fields.Set("hover_event", event)
	}
	if t.HoverItem != nil {
		hover := textEventTag("show_item", "id", nbt.String(t.HoverItem.ID))
		if t.HoverItem.Count != 1 {
			hover.Set("count", nbt.Int(t.HoverItem.Count))
		}
		if len(t.HoverItem.Components) > 0 {
			hover.Set("components", t.HoverItem.Components)
		}
		fields.Set("hover_event", hover)
	}
	if t.HoverText != nil && t.HoverEntity == nil && t.HoverItem == nil {
		value := nbt.Tag(nbt.String(t.HoverText.Text))
		if !isPlainText(*t.HoverText) {
			value = TextTag(*t.HoverText)
		}
		fields.Set("hover_event", textEventTag("show_text", "value", value))
	}
	if t.Insertion != "" {
		fields.Set("insertion", nbt.String(t.Insertion))
	}
	return fields
}

// vanilla: ClickEvent.CODEC, HoverEvent.CODEC
func textEventTag(action, key string, value nbt.Tag) *nbt.OrderedCompound {
	tag := nbt.NewOrderedCompound(2)
	tag.Set("action", nbt.String(action))
	tag.Set(key, value)
	return tag
}

// vanilla: ComponentSerialization.createCodec, Style.Serializer.MAP_CODEC
func nativeTextTag(tag nbt.Tag) nbt.Tag {
	switch v := tag.(type) {
	case nbt.List:
		items := make([]nbt.Tag, len(v.Items))
		for i, child := range v.Items {
			items[i] = nativeTextTag(child)
		}
		return nbt.List{Elem: v.Elem, Items: items}
	case nbt.CompoundTag:
		return textRecord(v, "text", "translate", "keybind", "score", "selector", "nbt", "object", "atlas", "sprite", "player", "hat", "fallback", "with", "interpret", "plain", "separator", "block", "entity", "storage", "extra", "color", "shadow_color", "bold", "italic", "underlined", "strikethrough", "obfuscated", "click_event", "hover_event", "insertion", "font")
	default:
		return tag
	}
}

// textRecord orders codec fields while leaving opaque event payloads untouched.
func textRecord(tag nbt.CompoundTag, keys ...string) *nbt.OrderedCompound {
	fields := tag.Entries()
	out := nbt.NewOrderedCompound(len(fields))
	for _, key := range keys {
		value, has := fields[key]
		if !has {
			continue
		}
		switch key {
		case "extra", "with", "name", "value", "separator", "fallback":
			value = nativeTextTag(value)
		case "score":
			if score, ok := value.(nbt.CompoundTag); ok {
				value = textRecord(score, "name", "objective")
			}
		case "click_event", "hover_event":
			if event, ok := value.(nbt.CompoundTag); ok {
				value = textRecord(event, "action", "id", "uuid", "name", "count", "components", "value", "url", "command", "page", "dialog", "payload")
			}
		}
		out.Set(key, value)
	}
	if len(out.Entries()) == len(fields) {
		return out
	}
	// Unknown fields belong to the retained raw value, not to the codec schema.
	for _, key := range nbt.HashOrderedKeys(tag) {
		if _, known := out.Entries()[key]; !known {
			out.Set(key, fields[key])
		}
	}
	return out
}

// componentFields are the fields a text component compound may name, the
// set the game's component codec reads.
var componentFields = map[string]bool{
	"text": true, "translate": true, "fallback": true, "keybind": true,
	"score": true, "selector": true, "nbt": true, "with": true, "extra": true,
	"separator": true, "color": true, "font": true, "bold": true,
	"italic": true, "underlined": true, "strikethrough": true,
	"obfuscated": true, "shadow_color": true, "insertion": true,
	"click_event": true, "hover_event": true,
	"object": true, "player": true, "sprite": true, "atlas": true,
}

// IsTextComponent reports whether a tag is shaped like a text component the
// codec reads: a string, a non-empty list of components, or a compound with
// at least one component field.
// Unlike TextOf it accepts styling the model cannot represent (bold), for a
// tag relayed whole.
func IsTextComponent(tag nbt.Tag) bool {
	switch v := tag.(type) {
	case *nbt.OrderedCompound:
		return IsTextComponent(v.Entries())
	case nbt.String:
		return true
	case nbt.List:
		if len(v.Items) == 0 {
			return false
		}
		for _, item := range v.Items {
			if !IsTextComponent(listElementOf(item)) {
				return false
			}
		}
		return true
	case nbt.CompoundTag:
		compoundFields := v.Entries()
		for field := range compoundFields {
			if componentFields[field] {
				return true
			}
		}
	}
	return false
}

// maxTextNesting bounds the recursion a hand-written tag can ask for.
const maxTextNesting = 64

// TextOf reads a text component from its file or network NBT form. It is at
// least as strict as the game's codec, refusing too what this model cannot
// represent, so false (over budget among them) is the caller's cue to refuse
// the stack: what a client cannot decode takes it down when relayed.
func TextOf(tag nbt.Tag) (Text, bool) {
	if textCharge(tag) > textQuota {
		return Text{}, false
	}
	return textOf(tag)
}

// textOf is TextOf without the charge, which TextOf makes once for the whole
// tree rather than at every nesting level.
func textOf(tag nbt.Tag) (Text, bool) {
	switch v := tag.(type) {
	case *nbt.OrderedCompound:
		return textOf(v.Entries())
	case nbt.String:
		return Text{Text: string(v)}, true
	case nbt.CompoundTag:
		compoundFields := v.Entries()
		return textOfCompound(compoundFields)
	case nbt.List:
		if len(v.Items) == 0 {
			return Text{}, false
		}
		// The first component is the parent, so its style is inherited by
		// later entries and its existing siblings precede them.
		// vanilla: ComponentSerialization.createFromList
		text, ok := textOf(listElementOf(v.Items[0]))
		if !ok {
			return Text{}, false
		}
		for _, item := range v.Items[1:] {
			sibling, ok := textOf(listElementOf(item))
			if !ok {
				return Text{}, false
			}
			text.Extra = append(text.Extra, sibling)
		}
		return text, true
	default:
		return Text{}, false
	}
}

// textQuota is the codec's NBT accounter quota, 2 MiB in Java's estimated
// memory cost (a string is 36 + 2 per UTF-16 unit), which is why 60,000
// one-letter extras are over budget at only 180 KB on the wire.
// vanilla: NbtAccounter.DEFAULT_NBT_QUOTA (ComponentSerialization.STREAM_CODEC)
const textQuota = 0x200000

// textCharge is the accounter's charge for one tag.
// vanilla: NbtAccounter (NbtIo, the TagType loads)
func textCharge(tag nbt.Tag) int64 {
	switch v := tag.(type) {
	case *nbt.OrderedCompound:
		return textCharge(v.Entries())
	case nbt.Byte:
		return 9
	case nbt.Short:
		return 10
	case nbt.Int:
		return 12
	case nbt.Long:
		return 16
	case nbt.Float:
		return 12
	case nbt.Double:
		return 16
	case nbt.String:
		return 36 + 2*int64(utf16Len(string(v)))
	case nbt.ByteArray:
		return 24 + int64(len(v))
	case nbt.IntArray:
		return 24 + 4*int64(len(v))
	case nbt.LongArray:
		return 24 + 8*int64(len(v))
	case nbt.List:
		total := int64(36 + 4*len(v.Items))
		for _, item := range v.Items {
			total += textCharge(item)
		}
		return total
	case nbt.CompoundTag:
		compoundFields := v.Entries()
		total := int64(48)
		for name, value := range compoundFields {
			total += 28 + 2*int64(utf16Len(name)) + 36 + textCharge(value)
		}
		return total
	default:
		return 0
	}
}

// textArgumentOf reads one with-list argument, which may be a bare number
// tag (a count) where a full component could not.
func textArgumentOf(tag nbt.Tag) (Text, bool) {
	switch v := tag.(type) {
	case nbt.Byte, nbt.Short, nbt.Double:
		// vanilla: TranslatableContents.PRIMITIVE_ARG_CODEC
		return Text{Raw: tag}, true
	case nbt.Int:
		n := int64(v)
		return Text{Number: &n}, true
	case nbt.Float:
		f := float32(v)
		return Text{Float: &f}, true
	case nbt.Long:
		n := int64(v)
		return Text{Long: &n}, true
	default:
		return textOf(tag)
	}
}

// listElementOf unwraps a component list element from the compound a
// non-homogeneous value is wrapped in; only list elements are unwrapped.
// vanilla: ListTag.tryUnwrap
func listElementOf(tag nbt.Tag) nbt.Tag {
	fields, ok := nbt.CompoundView(tag)
	if !ok || len(fields) != 1 {
		return tag
	}
	if inner, ok := fields[""]; ok {
		return inner
	}
	return tag
}

// ValidChatString reports whether every character is one the chat box
// accepts (not the section sign, controls or DEL); a click command with one
// takes the reading client down with the component.
// vanilla: ExtraCodecs.CHAT_STRING, StringUtil.isAllowedChatCharacter
func ValidChatString(s string) bool {
	for _, r := range s {
		if r != 0xA7 && r >= ' ' && r != 0x7F {
			continue
		}
		return false
	}
	return true
}

// WithoutClick is t with the click event dropped from it and every sibling,
// how an entity's custom name is shown as its name.
// vanilla: Entity.removeAction
func (t Text) WithoutClick() Text {
	if t.Raw != nil {
		t.Raw = withoutClickTag(t.Raw)
		return t
	}
	t.ClickCommand, t.ClickCopy, t.ClickURL, t.ClickRun, t.ClickPage = "", "", "", "", 0
	if len(t.Extra) > 0 {
		extra := make([]Text, len(t.Extra))
		for i, sibling := range t.Extra {
			extra[i] = sibling.WithoutClick()
		}
		t.Extra = extra
	}
	return t
}

// withoutClickTag is a component tag with click_event dropped from it and its
// siblings; the tag is copied where it changes.
func withoutClickTag(tag nbt.Tag) nbt.Tag {
	switch v := tag.(type) {
	case *nbt.OrderedCompound:
		return withoutClickTag(v.Entries())
	case nbt.List:
		items := make([]nbt.Tag, len(v.Items))
		for i, item := range v.Items {
			inner := withoutClickTag(listElementOf(item))
			if wrapper, ok := nbt.CompoundView(item); ok && len(wrapper) == 1 && wrapper[""] != nil {
				inner = nbt.Compound{"": inner}
			}
			items[i] = inner
		}
		return nbt.List{Elem: v.Elem, Items: items}
	case nbt.Compound:
		out := make(nbt.Compound, len(v))
		for key, value := range v {
			switch key {
			case "click_event":
				continue
			case "extra":
				value = withoutClickTag(value)
			}
			out[key] = value
		}
		return out
	}
	return tag
}

// textOfCompound reads a component's compound form; a field the model or
// codec cannot handle makes it unfaithful, the caller's cue to keep the raw
// tag.
func textOfCompound(fields nbt.Compound) (Text, bool) {
	var t Text
	content := false
	for name, value := range fields {
		switch name {
		case "text":
			s, ok := value.(nbt.String)
			if !ok {
				return Text{}, false
			}
			t.Text = string(s)
			content = true
		case "translate":
			s, ok := value.(nbt.String)
			if !ok {
				return Text{}, false
			}
			t.Translate = string(s)
			content = true
		case "with":
			list, ok := value.(nbt.List)
			if !ok {
				return Text{}, false
			}
			for _, item := range list.Items {
				arg, ok := textArgumentOf(listElementOf(item))
				if !ok {
					return Text{}, false
				}
				t.With = append(t.With, arg)
			}
		case "extra":
			list, ok := value.(nbt.List)
			// An empty sibling list is one the codec refuses.
			if !ok || len(list.Items) == 0 {
				return Text{}, false
			}
			for _, item := range list.Items {
				sibling, ok := textOf(listElementOf(item))
				if !ok {
					return Text{}, false
				}
				t.Extra = append(t.Extra, sibling)
			}
		case "color":
			s, ok := value.(nbt.String)
			if !ok || !validColor(string(s)) {
				return Text{}, false
			}
			t.Color = string(s)
		case "italic":
			if !booleanTag(value, &t.Italic) {
				return Text{}, false
			}
		case "underlined":
			if !booleanTag(value, &t.Underline) {
				return Text{}, false
			}
		case "insertion":
			s, ok := value.(nbt.String)
			if !ok {
				return Text{}, false
			}
			t.Insertion = string(s)
		case "click_event":
			if !parseClickEvent(value, &t) {
				return Text{}, false
			}
		case "hover_event":
			if !parseHoverEvent(value, &t) {
				return Text{}, false
			}
		default:
			return Text{}, false
		}
	}
	// A compound with no content field — `{}` among them — is one the
	// codec dispatches nothing for, and refuses.
	if !content {
		return Text{}, false
	}
	return t, true
}

// parseClickEvent reads a click_event compound into t, returning false when
// the action or its fields are unreadable or not represented on Text.
// vanilla: ClickEvent.CODEC
func parseClickEvent(value nbt.Tag, t *Text) bool {
	event, ok := nbt.CompoundView(value)
	if !ok {
		return false
	}
	action, _ := nbt.Get[nbt.String](event, "action")
	switch string(action) {
	case "suggest_command":
		command, ok := nbt.Get[nbt.String](event, "command")
		if !ok || !ValidChatString(string(command)) {
			return false
		}
		t.ClickCommand = string(command)
	case "copy_to_clipboard":
		copied, ok := nbt.Get[nbt.String](event, "value")
		if !ok {
			return false
		}
		t.ClickCopy = string(copied)
	case "run_command":
		command, ok := nbt.Get[nbt.String](event, "command")
		if !ok || !ValidChatString(string(command)) {
			return false
		}
		t.ClickRun = string(command)
	case "open_url":
		url, ok := nbt.Get[nbt.String](event, "url")
		if !ok || !ValidUntrustedURI(string(url)) {
			return false
		}
		t.ClickURL = string(url)
	case "change_page":
		page, ok := nbt.IntValue(event["page"])
		if !ok || page < 1 {
			return false
		}
		t.ClickPage = page
	default:
		// custom and show_dialog the model has no field for;
		// open_file is refused from a server.
		return false
	}
	return true
}

// validClickEvent reports whether value is a click_event compound the client
// decodes, including unmodelled show_dialog and custom actions.
// vanilla: ClickEvent.CODEC
func validClickEvent(value nbt.Tag, validDialog func(nbt.Tag) bool) bool {
	var scratch Text
	if parseClickEvent(value, &scratch) {
		return true
	}
	event, ok := nbt.CompoundView(value)
	if !ok {
		return false
	}
	switch action, _ := nbt.Get[nbt.String](event, "action"); string(action) {
	case "show_dialog":
		// vanilla: ClickEvent.ShowDialog.CODEC
		return RawTextFits(event) && validDialog != nil && validDialog(event["dialog"])
	case "custom":
		// vanilla: ClickEvent.Custom.CODEC
		id, ok := nbt.Get[nbt.String](event, "id")
		return ok && ValidIdentifier(string(id))
	}
	return false
}

// parseHoverEvent reads a hover_event compound into t, returning false when
// the action or its fields are unreadable.
// vanilla: HoverEvent.CODEC
func parseHoverEvent(value nbt.Tag, t *Text) bool {
	event, ok := nbt.CompoundView(value)
	if !ok {
		return false
	}
	action, _ := nbt.Get[nbt.String](event, "action")
	switch string(action) {
	case "show_entity":
		name, ok := nbt.Get[nbt.Tag](event, "name")
		if !ok {
			return false
		}
		hoverName, ok := textOf(name)
		if !ok {
			return false
		}
		// The client resolves the id against its entity type
		// registry while decoding, so it must be a real type.
		// vanilla: HoverEvent.EntityTooltipInfo.CODEC
		id, ok := nbt.Get[nbt.String](event, "id")
		if !ok || !entityTypeNames[string(id)] {
			return false
		}
		id4, ok := hoverEntityUUID(event["uuid"])
		if !ok {
			return false
		}
		var words [4]uint32
		for i, word := range id4 {
			words[i] = uint32(word)
		}
		t.HoverEntity = &HoverEntity{Name: hoverName, ID: string(id), UUID: words}
	case "show_text":
		value, ok := nbt.Get[nbt.Tag](event, "value")
		if !ok {
			return false
		}
		hover, ok := textOf(value)
		if !ok {
			return false
		}
		t.HoverText = &hover
	default:
		return false
	}
	return true
}

// booleanTag reads a numeric component flag; false, leaving into unchanged,
// for anything else.
// vanilla: NbtOps.getBooleanValue
func booleanTag(value nbt.Tag, into *bool) bool {
	b, ok := nbt.NumberValue(value)
	if !ok {
		return false
	}
	*into = b != 0
	return true
}

// hoverEntityUUID reads a show_entity uuid as four ints, from an int array or
// the string form.
// vanilla: UUIDUtil.LENIENT_CODEC
func hoverEntityUUID(tag nbt.Tag) (nbt.IntArray, bool) {
	if str, ok := tag.(nbt.String); ok {
		id, reason := parseJavaUUID(string(str))
		if reason != "" {
			return nil, false
		}
		return nbt.UUIDToIntArray(id), true
	}
	words, ok := nbt.UUIDWords(tag)
	if !ok || len(words) != 4 {
		return nil, false
	}
	return words, true
}

// parseJavaUUID reads five dash-separated hex fields the way UUID.fromString
// does, including its acceptance of short fields. A refusal returns the
// exception message it throws, which is never empty.
// vanilla: UUID.fromString
func parseJavaUUID(s string) (uuid.UUID, string) {
	if len(s) > 36 {
		return uuid.Nil, "UUID string too large"
	}
	var dash [5]int
	from := 0
	for i := range dash {
		n := strings.IndexByte(s[from:], '-')
		if n < 0 {
			dash[i] = -1
			break
		}
		dash[i] = from + n
		from += n + 1
	}
	if dash[3] < 0 || dash[4] >= 0 {
		return uuid.Nil, "Invalid UUID string: " + s
	}
	var v [5]uint64
	begin := 0
	for i := range v {
		end := len(s)
		if i < 4 {
			end = dash[i]
		}
		n, reason := javaParseHexLong(s[begin:end])
		if reason != "" {
			return uuid.Nil, reason
		}
		v[i] = uint64(n)
		begin = end + 1
	}
	msb := (v[0]&0xffffffff)<<32 | (v[1]&0xffff)<<16 | v[2]&0xffff
	lsb := (v[3]&0xffff)<<48 | v[4]&0xffffffffffff
	var id uuid.UUID
	binary.BigEndian.PutUint64(id[:8], msb)
	binary.BigEndian.PutUint64(id[8:], lsb)
	return id, ""
}

// javaParseHexLong is Long.parseLong(s, 0, len, 16): signed, a leading '+'
// accepted, with the NumberFormatException message as the refusal.
func javaParseHexLong(s string) (int64, string) {
	if s == "" {
		return 0, `For input string: "" under radix 16`
	}
	fail := func(i int) (int64, string) {
		return 0, "Error at index " + strconv.Itoa(i) + ` in: "` + s + `"`
	}
	i := 0
	negative := false
	limit := int64(-math.MaxInt64)
	if s[0] < '0' {
		switch s[0] {
		case '-':
			negative = true
			limit = math.MinInt64
		case '+':
		default:
			return fail(0)
		}
		i++
		if len(s) == 1 {
			return fail(i)
		}
	}
	multmin := limit / 16
	var result int64
	for ; i < len(s); i++ {
		digit := -1
		if isHexDigit(s[i]) {
			digit = strings.IndexByte("0123456789abcdef", s[i]|0x20)
		}
		if digit < 0 || result < multmin {
			return fail(i)
		}
		result *= 16
		if result < limit+int64(digit) {
			return fail(i)
		}
		result -= int64(digit)
	}
	if negative {
		return result, ""
	}
	return -result, ""
}
