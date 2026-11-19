package protocol

import (
	"math"
	"strconv"
	"strings"
)

// Plain flattens the component to the plain text the vanilla console logs:
// colour and decorations gone, translation keys filled in from the vanilla
// client's en_us language. An unknown key is shown as the raw key.
func (t Text) Plain() string {
	w := textWriter{}
	w.component(t, style{})
	return w.String()
}

// LogComponentKey is the log attribute a line carries the component its
// message was rendered from under, for a handler that draws it in colour.
const LogComponentKey = "component"

// ANSI is Plain with the component's colours and decorations drawn as ANSI
// escape codes, for a terminal. The text's own control characters are
// escaped (EscapeControls), so only the codes ANSI draws reach the terminal.
func (t Text) ANSI() string {
	w := textWriter{ansi: true}
	w.component(t, style{})
	return w.String()
}

// style is what a run of text is drawn in. A component's style is its
// parent's with its own set over it; the booleans can only be turned on, as
// Text has no way to turn one off.
type style struct {
	color             string
	italic, underline bool
}

func (s style) with(t Text) style {
	if t.Color != "" {
		s.color = t.Color
	}
	s.italic = s.italic || t.Italic
	s.underline = s.underline || t.Underline
	return s
}

// textWriter collects rendered text, and with ansi set, the escape codes
// that switch between the styles of its runs.
type textWriter struct {
	b    strings.Builder
	ansi bool
	// drawn is the style the last escape code set.
	drawn style
}

func (w *textWriter) write(s string, st style) {
	if s == "" {
		return
	}
	if !w.ansi {
		w.b.WriteString(s)
		return
	}
	if st != w.drawn {
		w.b.WriteString(sgr(st))
		w.drawn = st
	}
	w.b.WriteString(EscapeControls(s))
}

// String is the text written, ending with a reset when a style is drawn.
func (w *textWriter) String() string {
	if w.ansi && w.drawn != (style{}) {
		return w.b.String() + "\x1b[0m"
	}
	return w.b.String()
}

// component renders one component in its style over the inherited one and,
// after it, its siblings, which inherit its style.
func (w *textWriter) component(t Text, inherited style) {
	st := inherited.with(t)
	switch {
	case t.Translate != "":
		w.translation(t, st)
	case t.Float != nil:
		w.write(javaFloat(*t.Float), st)
	case t.Number != nil:
		w.write(strconv.FormatInt(*t.Number, 10), st)
	case t.Long != nil:
		w.write(strconv.FormatInt(*t.Long, 10), st)
	case t.Raw != nil:
		w.write(PlainTextOf(t.Raw), st)
	default:
		w.write(t.Text, st)
	}
	for _, extra := range t.Extra {
		w.component(extra, st)
	}
}

// translation renders a translation: the template the language holds for
// its key, with the arguments in the places its %s placeholders name.
func (w *textWriter) translation(t Text, st style) {
	w.template(translationTemplate(t.Translate, t.Translate), t.With, st)
}

func translationTemplate(key, fallback string) string {
	template, ok := vanillaLanguage[key]
	if !ok {
		return fallback
	}
	return template
}

// nextTemplateArg finds a placeholder with the client's template pattern:
// %(?:(\d+)\$)?([A-Za-z%]|$). An unmatched percent remains literal text,
// which template refuses even if a later placeholder would match.
func nextTemplateArg(t string) (start, end int, index string, kind string, ok bool) {
	start = strings.IndexByte(t, '%')
	if start < 0 {
		return
	}
	end = start + 1
	digits := end
	for end < len(t) && t[end] >= '0' && t[end] <= '9' {
		end++
	}
	if end > digits && end < len(t) && t[end] == '$' {
		index = t[digits:end]
		end++
	} else {
		end = digits
	}
	if end == len(t) {
		ok = true
		return
	}
	c := t[end]
	if c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c == '%' {
		kind = t[end : end+1]
		end++
		ok = true
	}
	return
}

// template writes template with its arguments substituted, literal parts in
// the translation's style. One the reader refuses (unknown format, stray
// percent, missing argument) is written as is, as the client shows one it
// cannot decompose.
func (w *textWriter) template(template string, args []Text, st style) {
	out := textWriter{ansi: w.ansi, drawn: w.drawn}
	refuse := func() { w.write(template, st) }
	next, pos := 0, 0
	for pos < len(template) {
		start, end, argIndex, kind, ok := nextTemplateArg(template[pos:])
		if !ok {
			break
		}
		start, end = pos+start, pos+end
		prefix := template[pos:start]
		if strings.Contains(prefix, "%") {
			// The percent of a format the pattern does not match is
			// left in the literal text, which the reader refuses.
			refuse()
			return
		}
		out.write(prefix, st)
		switch kind {
		case "%":
			// vanilla: TranslatableContents.decomposeTemplate accepts only %%.
			if template[start:end] != "%%" {
				refuse()
				return
			}
			out.write("%", st)
		case "s":
			index := next
			if argIndex != "" {
				index, _ = strconv.Atoi(argIndex)
				index--
			} else {
				next++
			}
			if index < 0 || index >= len(args) {
				refuse()
				return
			}
			out.component(args[index], st)
		default:
			// A format the reader does not support — a %d that slipped past
			// the language's rewrite, or the percent ending a template,
			// whose kind is empty — refuses the whole template.
			refuse()
			return
		}
		pos = end
	}
	tail := template[pos:]
	if strings.Contains(tail, "%") {
		refuse()
		return
	}
	out.write(tail, st)
	w.b.WriteString(out.b.String())
	w.drawn = out.drawn
}

// javaFloat renders a float the way Java's Float.toString does, which is
// what the vanilla client's argument rendering falls back to: the shortest
// digits that read back as the same number, with a fraction always shown.
func javaFloat(f float32) string {
	s := strconv.FormatFloat(float64(f), 'g', -1, 32)
	if !strings.ContainsAny(s, ".eE") {
		s += ".0"
	}
	return s
}

// javaDouble keeps Double.toString's precision, fraction and exponent form.
// vanilla: TranslatableContents.getArgument
func javaDouble(f float64) string {
	if math.IsInf(f, 1) {
		return "Infinity"
	}
	if math.IsInf(f, -1) {
		return "-Infinity"
	}
	format := byte('f')
	if a := math.Abs(f); a != 0 && (a < 1e-3 || a >= 1e7) {
		format = 'E'
	}
	s := strconv.FormatFloat(f, format, -1, 64)
	mantissa, exponent, scientific := strings.Cut(s, "E")
	if scientific && !strings.Contains(mantissa, ".") {
		// Double.toString chooses the closest decimal with at least two
		// significant digits, even when one digit would round-trip.
		mantissa, exponent, _ = strings.Cut(strconv.FormatFloat(f, 'E', 1, 64), "E")
	}
	if !strings.Contains(mantissa, ".") && !math.IsNaN(f) {
		mantissa += ".0"
	}
	if scientific {
		n, _ := strconv.Atoi(exponent)
		return mantissa + "E" + strconv.Itoa(n)
	}
	return mantissa
}
