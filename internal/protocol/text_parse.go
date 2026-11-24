package protocol

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"slices"
	"strconv"
	"strings"

	"github.com/golem-mc/golem/nbt"
)

// ParseTextJSON reads a JSON text component into its network tag tree: text,
// translatable and keybind contents with every style field, a value the
// codec would refuse failing the whole. Score, selector, nbt and object
// contents are refused; click_event and hover_event are dropped unchecked
// and named in dropped, so nothing undecodable reaches the client.
// vanilla: ComponentSerialization.CODEC
func ParseTextJSON(s string) (tag nbt.Tag, dropped []string, err error) {
	dec := json.NewDecoder(bytes.NewReader([]byte(s)))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, nil, err
	}
	// More() is false before a stray ']' or '}', so only EOF proves the
	// input ended; trailing whitespace is allowed.
	if _, err := dec.Token(); err != io.EOF {
		return nil, nil, errors.New("trailing data after the component")
	}
	p := &jsonTextParser{}
	tag, err = p.component(v, 0)
	if err != nil {
		return nil, nil, err
	}
	return tag, p.dropped, nil
}

type jsonTextParser struct {
	dropped []string
}

func (p *jsonTextParser) component(v any, depth int) (nbt.Tag, error) {
	if depth > maxTextNesting {
		return nil, errors.New("component nested too deep")
	}
	switch v := v.(type) {
	case string:
		return nbt.String(v), nil
	case []any:
		// A list is its first element with the rest appended as siblings.
		if len(v) == 0 {
			return nil, errors.New("empty component list")
		}
		first, err := p.component(v[0], depth+1)
		if err != nil {
			return nil, err
		}
		if len(v) == 1 {
			return first, nil
		}
		head, ok := first.(nbt.Compound)
		if !ok {
			head = nbt.Compound{"text": first}
		}
		var siblings []nbt.Tag
		if extra, ok := head["extra"].(nbt.List); ok {
			for _, item := range extra.Items {
				siblings = append(siblings, listElementOf(item))
			}
		}
		for _, e := range v[1:] {
			sib, err := p.component(e, depth+1)
			if err != nil {
				return nil, err
			}
			siblings = append(siblings, sib)
		}
		head["extra"] = componentList(siblings)
		return head, nil
	case map[string]any:
		return p.object(v, depth)
	}
	return nil, fmt.Errorf("a component is a string, list or object, not %s", jsonKind(v))
}

// contentKeys are the content types in the order the codec tries them, each
// with the key that names it and whether this parser reads it.
// vanilla: ComponentSerialization.bootstrap
var contentKeys = []struct {
	typ, key  string
	supported bool
}{
	{"text", "text", true},
	{"translatable", "translate", true},
	{"keybind", "keybind", true},
	{"score", "score", false},
	{"selector", "selector", true},
	{"nbt", "nbt", false},
	{"object", "object", true},
}

func (p *jsonTextParser) object(o map[string]any, depth int) (nbt.Tag, error) {
	out := nbt.Compound{}
	contentKey := ""
	if raw, ok := o["type"]; ok {
		typ, ok := raw.(string)
		if !ok {
			return nil, errors.New(`"type" is not a string`)
		}
		for _, c := range contentKeys {
			if c.typ == typ {
				if !c.supported {
					return nil, fmt.Errorf("%s components are not supported", typ)
				}
				contentKey = c.key
			}
		}
		if contentKey == "" {
			return nil, fmt.Errorf("unknown component type %q", typ)
		}
		if _, ok := o[contentKey]; !ok && (typ != "object" || !objectInfoKeysPresent(o)) {
			return nil, fmt.Errorf("%s component has no %q", typ, contentKey)
		}
	} else {
		for _, c := range contentKeys {
			if _, ok := o[c.key]; ok || c.typ == "object" && objectInfoKeysPresent(o) {
				if !c.supported {
					return nil, fmt.Errorf("%s components are not supported", c.typ)
				}
				contentKey = c.key
				break
			}
		}
		if contentKey == "" {
			return nil, errors.New("component has no text, translate or keybind")
		}
	}
	if contentKey == "object" {
		if err := p.objectContent(o, out, depth); err != nil {
			return nil, err
		}
	} else {
		content, ok := o[contentKey].(string)
		if !ok {
			return nil, fmt.Errorf("%q is not a string", contentKey)
		}
		out[contentKey] = nbt.String(content)
	}

	if contentKey == "selector" {
		// vanilla: SelectorContents.MAP_CODEC
		if raw, ok := o["separator"]; ok {
			sep, err := p.component(raw, depth+1)
			if err != nil {
				return nil, err
			}
			out["separator"] = sep
		}
	}

	if contentKey == "translate" {
		// fallback is read leniently: a non-string one is ignored.
		// vanilla: TranslatableContents.MAP_CODEC
		if fallback, ok := o["fallback"].(string); ok {
			out["fallback"] = nbt.String(fallback)
		}
		if raw, ok := o["with"]; ok {
			list, ok := raw.([]any)
			if !ok {
				return nil, errors.New(`"with" is not a list`)
			}
			args := make([]nbt.Tag, 0, len(list))
			for _, e := range list {
				arg, err := p.argument(e, depth)
				if err != nil {
					return nil, err
				}
				args = append(args, arg)
			}
			if len(args) > 0 {
				out["with"] = componentList(args)
			}
		}
	}

	if raw, ok := o["extra"]; ok {
		list, ok := raw.([]any)
		if !ok || len(list) == 0 {
			return nil, errors.New(`"extra" is not a non-empty list`)
		}
		extra := make([]nbt.Tag, 0, len(list))
		for _, e := range list {
			sib, err := p.component(e, depth+1)
			if err != nil {
				return nil, err
			}
			extra = append(extra, sib)
		}
		out["extra"] = componentList(extra)
	}

	if err := p.style(o, out); err != nil {
		return nil, err
	}
	return out, nil
}

// objectInfoKeysPresent reports whether o names an object without its object
// key: the compact form, read as an atlas sprite or a player head.
func objectInfoKeysPresent(o map[string]any) bool {
	_, sprite := o["sprite"]
	_, player := o["player"]
	return sprite || player
}

// objectContent copies an object component's fields into out: the object
// kind, an atlas sprite's identifiers, a player head's profile and hat, and
// the fallback component.
// vanilla: ObjectContents.MAP_CODEC, AtlasSprite.MAP_CODEC, PlayerSprite.MAP_CODEC
func (p *jsonTextParser) objectContent(o map[string]any, out nbt.Compound, depth int) error {
	for _, key := range []string{"object", "sprite", "atlas"} {
		raw, ok := o[key]
		if !ok {
			continue
		}
		s, ok := raw.(string)
		if !ok {
			return fmt.Errorf("%q is not a string", key)
		}
		out[key] = nbt.String(s)
	}
	if raw, ok := o["player"]; ok {
		player, err := JSONValueTag(raw)
		if err != nil {
			return err
		}
		out["player"] = player
	}
	if raw, ok := o["hat"]; ok {
		hat, err := jsonBool("hat", raw)
		if err != nil {
			return err
		}
		out["hat"] = hat
	}
	if raw, ok := o["fallback"]; ok {
		fallback, err := p.component(raw, depth+1)
		if err != nil {
			return err
		}
		out["fallback"] = fallback
	}
	_, sprite := out["sprite"]
	_, player := out["player"]
	switch out["object"] {
	case nil:
	case nbt.String("atlas"):
		player = false
	case nbt.String("player"):
		sprite = false
	default:
		return fmt.Errorf("unknown object type %s", jsonValue(o["object"]))
	}
	if !sprite && !player {
		return errors.New("object component has no sprite or player")
	}
	return nil
}

// JSONValueTag converts a decoded JSON value (numbers as json.Number) to the
// tag it reads as; a list of ints becomes an int array.
// vanilla: Dynamic.convert(JsonOps, NbtOps), NbtOps.createList
func JSONValueTag(v any) (nbt.Tag, error) {
	switch v := v.(type) {
	case string:
		return nbt.String(v), nil
	case bool:
		return BoolTag(v), nil
	case json.Number:
		return JSONNumberTag(v)
	case map[string]any:
		out := make(nbt.Compound, len(v))
		for k, e := range v {
			tag, err := JSONValueTag(e)
			if err != nil {
				return nil, err
			}
			out[k] = tag
		}
		return out, nil
	case []any:
		items := make([]nbt.Tag, 0, len(v))
		ints := make(nbt.IntArray, 0, len(v))
		for _, e := range v {
			tag, err := JSONValueTag(e)
			if err != nil {
				return nil, err
			}
			items = append(items, tag)
			if i, ok := tag.(nbt.Int); ok {
				ints = append(ints, int32(i))
			}
		}
		if len(items) > 0 && len(ints) == len(items) {
			return ints, nil
		}
		return nbt.ListOf(items), nil
	}
	return nil, fmt.Errorf("unsupported value %s", jsonValue(v))
}

// argument reads one entry of a translation's with-list: a number, boolean or
// string as itself, anything else as a component.
// vanilla: TranslatableContents.ARG_CODEC
func (p *jsonTextParser) argument(v any, depth int) (nbt.Tag, error) {
	switch v := v.(type) {
	case json.Number:
		return JSONNumberTag(v)
	case bool:
		return BoolTag(v), nil
	}
	return p.component(v, depth+1)
}

// style reads the style fields of o into out.
// vanilla: Style.Serializer.MAP_CODEC
func (p *jsonTextParser) style(o map[string]any, out nbt.Compound) error {
	if raw, ok := o["color"]; ok {
		color, ok := raw.(string)
		// Narrower than TextColor.parseColor, which also takes forms like
		// "#+12345": a colour outside the sixteen names and #RRGGBB is
		// refused rather than relayed.
		if !ok || !validColor(color) {
			return fmt.Errorf("invalid color %s", jsonValue(raw))
		}
		out["color"] = nbt.String(color)
	}
	if raw, ok := o["shadow_color"]; ok {
		tag, err := shadowColorTag(raw)
		if err != nil {
			return err
		}
		out["shadow_color"] = tag
	}
	for _, key := range []string{"bold", "italic", "underlined", "strikethrough", "obfuscated"} {
		raw, ok := o[key]
		if !ok {
			continue
		}
		b, err := jsonBool(key, raw)
		if err != nil {
			return err
		}
		out[key] = b
	}
	if raw, ok := o["insertion"]; ok {
		insertion, ok := raw.(string)
		if !ok {
			return errors.New(`"insertion" is not a string`)
		}
		out["insertion"] = nbt.String(insertion)
	}
	if raw, ok := o["font"]; ok {
		font, ok := raw.(string)
		if !ok || !ValidIdentifier(font) {
			return fmt.Errorf("invalid font %s", jsonValue(raw))
		}
		out["font"] = nbt.String(font)
	}
	for _, key := range []string{"click_event", "hover_event"} {
		if _, ok := o[key]; ok && !slices.Contains(p.dropped, key) {
			p.dropped = append(p.dropped, key)
		}
	}
	return nil
}

// jsonBool reads the boolean field key: a number reads as true when it is
// not zero.
// vanilla: JsonOps.getBooleanValue
func jsonBool(key string, raw any) (nbt.Tag, error) {
	switch v := raw.(type) {
	case bool:
		return BoolTag(v), nil
	case json.Number:
		if f, err := v.Float64(); err == nil {
			return BoolTag(int8(int64(f)) != 0), nil
		}
	}
	return nil, fmt.Errorf("%q is not a boolean", key)
}

// shadowColorTag reads a shadow colour: an ARGB int, or a list of four
// floats in alpha-last order.
// vanilla: ExtraCodecs.ARGB_COLOR_CODEC
func shadowColorTag(raw any) (nbt.Tag, error) {
	switch v := raw.(type) {
	case json.Number:
		n, err := strconv.ParseInt(v.String(), 10, 64)
		// An ARGB colour above 0x7FFFFFFF reads as its 32-bit pattern.
		if err != nil || n < math.MinInt32 || n > math.MaxUint32 {
			return nil, fmt.Errorf("invalid shadow_color %s", v)
		}
		return nbt.Int(int32(uint32(n))), nil
	case []any:
		if len(v) != 4 {
			return nil, errors.New("shadow_color list is not four floats")
		}
		items := make([]nbt.Tag, 0, 4)
		for _, e := range v {
			n, ok := e.(json.Number)
			if !ok {
				return nil, errors.New("shadow_color list is not four floats")
			}
			f, err := n.Float64()
			if err != nil {
				return nil, fmt.Errorf("invalid shadow_color component %s", n)
			}
			items = append(items, nbt.Float(float32(f)))
		}
		return nbt.List{Elem: nbt.TypeFloat, Items: items}, nil
	}
	return nil, fmt.Errorf("invalid shadow_color %s", jsonValue(raw))
}

// JSONNumberTag is the tag a JSON number becomes: an int when it is a whole
// number that fits, a long when only that fits, otherwise a double.
func JSONNumberTag(n json.Number) (nbt.Tag, error) {
	if i, err := strconv.ParseInt(n.String(), 10, 64); err == nil {
		if i >= math.MinInt32 && i <= math.MaxInt32 {
			return nbt.Int(int32(i)), nil
		}
		return nbt.Long(i), nil
	}
	f, err := n.Float64()
	if err != nil {
		return nil, fmt.Errorf("invalid number %s", n)
	}
	return nbt.Double(f), nil
}

// BoolTag is a flag as the byte a compound keeps it as.
func BoolTag(b bool) nbt.Tag {
	if b {
		return nbt.Byte(1)
	}
	return nbt.Byte(0)
}

// componentList builds a list of mixed entries, wrapping each non-compound
// under the empty name when types differ; the reader unwraps them.
// vanilla: ListTag.wrapIfNeeded
func componentList(items []nbt.Tag) nbt.List {
	elem := items[0].Type()
	same := true
	for _, item := range items[1:] {
		if item.Type() != elem {
			same = false
			break
		}
	}
	if same {
		return nbt.List{Elem: elem, Items: items}
	}
	wrapped := make([]nbt.Tag, 0, len(items))
	for _, item := range items {
		if c, ok := item.(nbt.Compound); ok {
			if _, wrapper := c[""]; !wrapper || len(c) != 1 {
				wrapped = append(wrapped, c)
				continue
			}
		}
		wrapped = append(wrapped, nbt.Compound{"": item})
	}
	return nbt.List{Elem: nbt.TypeCompound, Items: wrapped}
}

func jsonKind(v any) string {
	switch v.(type) {
	case json.Number:
		return "a number"
	case bool:
		return "a boolean"
	case nil:
		return "null"
	}
	return fmt.Sprintf("%T", v)
}

// jsonValue renders a decoded JSON value back for a warning.
func jsonValue(v any) string {
	data, err := json.Marshal(v)
	if err != nil {
		return jsonKind(v)
	}
	return strings.TrimSpace(string(data))
}
