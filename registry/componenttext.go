package registry

import (
	"strconv"
	"strings"

	"github.com/golem-mc/golem/internal/commandsyntax"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/nbt"
)

// vanilla: ComponentSerialization.StrictEither, FuzzyCodec
func textContentDiagnostic(fields nbt.Compound) (string, bool) {
	raw, typed := fields["type"]
	if !typed {
		for _, name := range []string{"text", "translatable", "keybind", "score", "selector", "nbt", "object"} {
			if msg, _ := textTypedContentDiagnostic(name, fields); msg == "" {
				return "", true
			}
		}
		return componentCodecRefusal, false
	}
	if msg := textStringMalformed(raw); msg != "" {
		return msg, false
	}
	return textTypedContentDiagnostic(string(raw.(nbt.String)), fields)
}

func textTypedContentDiagnostic(name string, fields nbt.Compound) (string, bool) {
	switch name {
	case "text", "keybind":
		msg := textFieldMalformed(fields, name, textStringMalformed)
		return msg, msg == ""
	case "translatable":
		required := textFieldMalformed(fields, "translate", textStringMalformed)
		args := ""
		if value, present := fields["with"]; present {
			if list, ok := value.(nbt.List); ok {
				for _, item := range list.Items {
					tag := nbt.ListElement(item)
					if !textContentArgDecodes(tag) {
						msg, partial := textComponentDiagnostic(tag)
						if !partial {
							msg = profileEitherMalformed("This value needs to be parsed as component", msg)
						}
						args = joinFieldErrors(msg, args)
					}
				}
			} else {
				args = "Not a list: " + nbt.Stringify(value)
			}
		}
		return joinFieldErrors(args, required), required == ""
	case "selector":
		required := textFieldMalformed(fields, "selector", textSelectorMalformed)
		sep := ""
		if value, present := fields["separator"]; present {
			sep = TextComponentMalformed(value)
		}
		return joinFieldErrors(sep, required), required == ""
	case "score":
		tag, present := fields["score"]
		if !present {
			return textFieldMalformed(fields, "score", textStringMalformed), false
		}
		score, ok := nbt.CompoundView(tag)
		if !ok {
			return "Not a map: " + nbt.Stringify(tag), false
		}
		holder := textFieldMalformed(score, "name", textStringMalformed)
		if holder == "Not a string" {
			holder = profileEitherMalformed(holder, holder)
		}
		msg := joinFieldErrors(textFieldMalformed(score, "objective", textStringMalformed), holder)
		return msg, msg == ""
	case "nbt":
		path := textFieldMalformed(fields, "nbt", func(tag nbt.Tag) string {
			if msg := textStringMalformed(tag); msg != "" {
				return msg
			}
			s := string(tag.(nbt.String))
			if _, err := commandsyntax.ParseNBTPath(s); err != nil {
				return "Invalid NBT path: " + s + ": " + textSyntaxMessage(s, err)
			}
			return ""
		})
		source := textDataSourceMalformed(fields)
		msg := joinFieldErrors(source, path)
		if msg == "" && protocol.FlagOn(fields["interpret"]) && protocol.FlagOn(fields["plain"]) {
			msg = "'interpret' and 'plain' flags can't be both on"
		}
		return msg, msg == ""
	case "object":
		msg, partial := textObjectDiagnostic(fields)
		fallback := ""
		if tag, present := fields["fallback"]; present {
			fallback = TextComponentMalformed(tag)
		}
		return joinFieldErrors(fallback, msg), partial
	}
	return "Unknown element id: " + name, false
}

func textSelectorMalformed(tag nbt.Tag) string {
	if msg := textStringMalformed(tag); msg != "" {
		return msg
	}
	s := string(tag.(nbt.String))
	_, _, n, err := commandsyntax.ParseSelector(s, 0, true, func(id string) bool { _, ok := EntityTypeID(id); return ok })
	if err != nil {
		return "Invalid selector component: " + s + ": " + textSyntaxMessage(s, err)
	}
	if n != len(s) {
		return "Invalid selector component: " + s + ": " + (protocol.Text{
			Translate: "command.trailing_data", With: []protocol.Text{{Text: s[:n] + "[" + s[n:] + "]"}},
		}).Plain()
	}
	return ""
}

// textSyntaxMessage includes Brigadier's ten-character context in codec errors.
func textSyntaxMessage(s string, err *commandsyntax.Error) string {
	args := make([]protocol.Text, len(err.With))
	for i, value := range err.With {
		args[i].Text = value
	}
	msg := (protocol.Text{Translate: err.Key, With: args}).Plain()
	if err.Text != "" {
		msg = err.Text
	}
	cursor := min(err.Cursor, len(s))
	context := s[max(0, cursor-10):cursor]
	if cursor > 10 {
		context = "..." + context
	}
	return msg + " at position " + strconv.Itoa(cursor) + ": " + context + "<--[HERE]"
}

// vanilla: DataSources.CODEC
func textDataSourceMalformed(fields nbt.Compound) string {
	raw, typed := fields["source"]
	if !typed {
		for _, source := range []string{"entity", "block", "storage"} {
			if textDataSourceFieldMalformed(source, fields) == "" {
				return ""
			}
		}
		return componentCodecRefusal
	}
	if msg := textStringMalformed(raw); msg != "" {
		return msg
	}
	return textDataSourceFieldMalformed(string(raw.(nbt.String)), fields)
}

func textDataSourceFieldMalformed(source string, fields nbt.Compound) string {
	switch source {
	case "entity":
		return textFieldMalformed(fields, source, textSelectorMalformed)
	case "storage":
		return textFieldMalformed(fields, source, resourceLocationMalformed)
	case "block":
		return textFieldMalformed(fields, source, textBlockPositionMalformed)
	default:
		return "Unknown element id: " + source
	}
}

// vanilla: ObjectInfos.CODEC
func textObjectDiagnostic(fields nbt.Compound) (string, bool) {
	raw, typed := fields["object"]
	if !typed {
		for _, kind := range []string{"atlas", "player"} {
			if msg, _ := textObjectFieldsDiagnostic(kind, fields); msg == "" {
				return "", true
			}
		}
		return componentCodecRefusal, false
	}
	if msg := textStringMalformed(raw); msg != "" {
		return msg, false
	}
	return textObjectFieldsDiagnostic(string(raw.(nbt.String)), fields)
}

func textObjectFieldsDiagnostic(kind string, fields nbt.Compound) (string, bool) {
	switch kind {
	case "atlas":
		required := textFieldMalformed(fields, "sprite", resourceLocationMalformed)
		msg := required
		if atlas, present := fields["atlas"]; present {
			msg = joinFieldErrors(msg, resourceLocationMalformed(atlas))
		}
		return msg, required == ""
	case "player":
		msg := ""
		if hat, present := fields["hat"]; present {
			msg = textNumberMalformed(hat)
		}
		required := textFieldMalformed(fields, "player", ProfileComponentMalformed)
		return joinFieldErrors(msg, required), required == ""
	default:
		return "Unknown element id: " + kind, false
	}
}

// vanilla: TranslatableContents.ARG_CODEC
func textContentArgDecodes(tag nbt.Tag) bool {
	return textNumberMalformed(tag) == "" || textStringMalformed(tag) == "" || TextComponentMalformed(tag) == ""
}

// vanilla: ExtraCodecs.UNTRUSTED_URI, Util.parseAndValidateUntrustedUri
func textURIMalformed(tag nbt.Tag) string {
	if msg := textStringMalformed(tag); msg != "" {
		return msg
	}
	return protocol.UntrustedURIMalformed(string(tag.(nbt.String)))
}

// vanilla: BlockDataSource.BLOCK_POS_CODEC, WorldCoordinates.parseInt, LocalCoordinates.parse
func textBlockPositionMalformed(tag nbt.Tag) string {
	if msg := textStringMalformed(tag); msg != "" {
		return msg
	}
	s := string(tag.(nbt.String))
	pos := 0
	local := strings.HasPrefix(s, "^")
	fail := func(key string, at int, args ...string) string {
		return "Invalid coordinates path: " + s + ": " + textSyntaxMessage(s, &commandsyntax.Error{Key: key, Cursor: at, With: args})
	}
	for axis := range 3 {
		if pos >= len(s) {
			key := "argument.pos.missing.int"
			if local {
				key = "argument.pos.missing.double"
			}
			return fail(key, pos)
		}
		relative := s[pos] == '~'
		if local {
			if s[pos] != '^' {
				return fail("argument.pos.mixed", 0)
			}
			pos++
		} else if s[pos] == '^' {
			return fail("argument.pos.mixed", pos)
		} else if relative {
			pos++
		}
		start := pos
		if pos < len(s) && s[pos] != ' ' {
			for pos < len(s) && (s[pos] >= '0' && s[pos] <= '9' || s[pos] == '-' || s[pos] == '.') {
				pos++
			}
			kind := "int"
			if local || relative {
				kind = "double"
			}
			if start == pos {
				return fail("parsing."+kind+".expected", start)
			}
			number := s[start:pos]
			var err error
			if kind == "int" {
				_, err = strconv.ParseInt(number, 10, 32)
			} else {
				_, err = strconv.ParseFloat(number, 64)
			}
			if err != nil {
				return fail("parsing."+kind+".invalid", start, number)
			}
		}
		if axis < 2 {
			if pos >= len(s) || s[pos] != ' ' {
				return fail("argument.pos3d.incomplete", 0)
			}
			pos++
		}
	}
	if pos < len(s) {
		return "Invalid coordinates path: " + s + ": " + (protocol.Text{
			Translate: "command.trailing_data", With: []protocol.Text{{Text: s[:pos] + "[" + s[pos:] + "]"}},
		}).Plain()
	}
	return ""
}
