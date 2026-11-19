package plugin

import "github.com/golem-mc/golem/registry"

// Text is a chat component: what messages, titles, item names, signs and
// boss bars show. Build one with Plain or Translate and the methods below;
// each answers a changed copy. vanilla: Component
type Text struct {
	// Literal is the text shown, unless Translate is set.
	Literal string
	// Translate is a translation key the client fills in from its own
	// language files, with Args in its placeholders.
	Translate string
	Args      []Text
	// Fallback is shown by a client without the translation key.
	Fallback string
	// Keybind shows the key a control is bound to, e.g. "key.jump".
	Keybind string
	// Children follow this component and inherit its style.
	Children []Text
	Style    Style
}

// Style is a component's look and behaviour; zero fields inherit.
type Style struct {
	// Color is a named colour ("red", "gold") or "#rrggbb"; empty inherits.
	Color string
	// Shadow is the shadow's ARGB colour; nil for the default.
	Shadow *uint32
	// The decorations: nil inherits, else on or off.
	Bold, Italic, Underlined, Strikethrough, Obfuscated *bool
	// Font is the resource pack font; zero for the default.
	Font Key
	// Insertion is what a shift-click puts in the chat box.
	Insertion string
	Click     *Click
	Hover     *Hover
}

// ClickAction is what clicking a component does.
type ClickAction uint8

const (
	OpenURL ClickAction = iota
	RunCommand
	SuggestCommand
	ChangePage
	CopyToClipboard
	ShowDialog
	Custom
)

// Click is a component's click event; Value is the URL, command, page,
// copied text, dialog key or custom payload ID its Action takes.
type Click struct {
	Action ClickAction
	Value  string
	// Payload is the custom action's data, sent back in a
	// PlayerCustomClickEvent.
	Payload []byte
}

// Hover is a component's tooltip: text, an item or an entity, whichever is
// set.
type Hover struct {
	Text   *Text
	Item   *ItemStack
	Entity *HoverEntity
}

// HoverEntity is the entity a hover shows.
type HoverEntity struct {
	Type registry.EntityType
	UUID UUID
	Name *Text
}

// Plain answers a literal component.
func Plain(s string) Text { return Text{Literal: s} }

// Translate answers a translated component.
func Translate(key string, args ...Text) Text { return Text{Translate: key, Args: args} }

// Append answers t with children added.
func (t Text) Append(children ...Text) Text {
	t.Children = append(append([]Text(nil), t.Children...), children...)
	return t
}

// Color answers t drawn in c, a named colour or "#rrggbb".
func (t Text) Color(c string) Text { t.Style.Color = c; return t }

// Bold answers t in bold.
func (t Text) Bold() Text { t.Style.Bold = new(true); return t }

// Italic answers t in italics; item names and lore are italic unless
// turned off with NotItalic.
func (t Text) Italic() Text { t.Style.Italic = new(true); return t }

// NotItalic answers t explicitly upright.
func (t Text) NotItalic() Text { t.Style.Italic = new(false); return t }

// Underlined answers t underlined.
func (t Text) Underlined() Text { t.Style.Underlined = new(true); return t }

// OnClick answers t with a click event.
func (t Text) OnClick(a ClickAction, value string) Text {
	t.Style.Click = &Click{Action: a, Value: value}
	return t
}

// OnHover answers t with a text tooltip.
func (t Text) OnHover(h Text) Text { t.Style.Hover = &Hover{Text: &h}; return t }

// PlainString answers the literal text of t and its children, without
// translations filled in; for logs and comparisons.
func (t Text) PlainString() string {
	s := t.Literal
	if t.Translate != "" {
		s = t.Translate
		if t.Fallback != "" {
			s = t.Fallback
		}
	}
	for _, c := range t.Children {
		s += c.PlainString()
	}
	return s
}

//go:fix inline
