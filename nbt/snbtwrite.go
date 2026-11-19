package nbt

import (
	"maps"
	"math"
	"slices"
	"strconv"
	"strings"
)

// Stringify renders a tag the way the codec error messages read it back.
// vanilla: StringTagVisitor
func Stringify(tag Tag) string {
	if tag == nil {
		return ""
	}
	switch v := tag.(type) {
	case Byte:
		return strconv.FormatInt(int64(v), 10) + "b"
	case Short:
		return strconv.FormatInt(int64(v), 10) + "s"
	case Int:
		return strconv.FormatInt(int64(v), 10)
	case Long:
		return strconv.FormatInt(int64(v), 10) + "L"
	case Float:
		return JavaFloatText(dropZeroSign(float64(v)), 32) + "f"
	case Double:
		return JavaFloatText(dropZeroSign(float64(v)), 64) + "d"
	case String:
		return quoteNBTString(string(v))
	case ByteArray:
		parts := make([]string, len(v))
		for i, b := range v {
			parts[i] = strconv.FormatInt(int64(int8(b)), 10) + "B"
		}
		return "[B;" + strings.Join(parts, ",") + "]"
	case IntArray:
		parts := make([]string, len(v))
		for i, b := range v {
			parts[i] = strconv.FormatInt(int64(b), 10)
		}
		return "[I;" + strings.Join(parts, ",") + "]"
	case LongArray:
		parts := make([]string, len(v))
		for i, b := range v {
			parts[i] = strconv.FormatInt(b, 10) + "L"
		}
		return "[L;" + strings.Join(parts, ",") + "]"
	case List:
		parts := make([]string, len(v.Items))
		for i, item := range v.Items {
			// A list whose elements do not share a type is held as {"":x}
			// wrappers on disk; the writer reads the wrapped value back.
			parts[i] = Stringify(ListElement(item))
		}
		return "[" + strings.Join(parts, ",") + "]"
	case CompoundTag:
		keys := slices.Sorted(maps.Keys(v.Entries()))
		parts := make([]string, len(keys))
		for i, k := range keys {
			parts[i] = StringifyKey(k) + ":" + Stringify(v.Entries()[k])
		}
		return "{" + strings.Join(parts, ",") + "}"
	}
	return ""
}

// StringifyKey writes a compound key the way the writer does: bare where it reads
// as a plain word, quoted otherwise, so that keys like true and false stay
// quoted.
func StringifyKey(key string) string {
	if key != "" {
		i := 0
		for ; i < len(key); i++ {
			if !isNBTKeyHead(key[i]) {
				break
			}
		}
		if i > 0 {
			for ; i < len(key); i++ {
				if !isNBTKeyTail(key[i]) {
					break
				}
			}
			if i == len(key) && !strings.EqualFold(key, "true") && !strings.EqualFold(key, "false") {
				return key
			}
		}
	}
	return quoteNBTString(key)
}

// isNBTKeyHead reports whether c may open the bare form of a compound key:
// the letters, dot and underscore the writer's key pattern starts on.
func isNBTKeyHead(c byte) bool {
	return c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c == '.' || c == '_'
}

// isNBTKeyTail reports whether c may continue the bare form of a compound key.
func isNBTKeyTail(c byte) bool {
	return isNBTKeyHead(c) || c >= '0' && c <= '9' || c == '+' || c == '-'
}

// quoteNBTString quotes and escapes a string the way the writer does: the
// quote is the one the string does not already use where it uses either, a
// backslash and the control characters take their escapes, and a string of
// neither quote is double quoted.
func quoteNBTString(s string) string {
	quote := byte(0)
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '"', '\'':
			if quote == 0 {
				if s[i] == '"' {
					quote = '\''
				} else {
					quote = '"'
				}
			}
		}
	}
	if quote == 0 {
		quote = '"'
	}
	var b strings.Builder
	b.WriteByte(quote)
	for i := 0; i < len(s); i++ {
		switch c := s[i]; c {
		case '\\':
			b.WriteString(`\\`)
		case '"', '\'':
			if c == quote {
				b.WriteByte('\\')
			}
			b.WriteByte(c)
		case '\b':
			b.WriteString(`\b`)
		case '\f':
			b.WriteString(`\f`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			b.WriteByte(c)
		}
	}
	b.WriteByte(quote)
	return b.String()
}

// dropZeroSign turns a negative zero into the zero the SNBT reader keeps.
// vanilla: StringTagVisitor.visitDouble
func dropZeroSign(f float64) float64 {
	if f == 0 {
		return 0
	}
	return f
}

// JavaFloatText renders a float as Java's Float.toString and Double.toString
// do (before the writer's f or d suffix): the shortest round-tripping
// decimal, plain from 1e-3 up to 1e7 and scientific outside, always with a
// digit after the point.
func JavaFloatText(f float64, bits int) string {
	switch {
	case math.IsNaN(f):
		return "NaN"
	case math.IsInf(f, 1):
		return "Infinity"
	case math.IsInf(f, -1):
		return "-Infinity"
	}
	neg := math.Signbit(f)
	f = math.Abs(f)
	mantissa, exp, _ := strings.Cut(strconv.FormatFloat(f, 'e', -1, bits), "e")
	digits := strings.Replace(mantissa, ".", "", 1)
	e, _ := strconv.Atoi(exp)
	var out string
	switch {
	case e >= -3 && e < 7:
		switch {
		case e >= 0 && e+1 >= len(digits):
			out = digits + strings.Repeat("0", e+1-len(digits)) + ".0"
		case e >= 0:
			out = digits[:e+1] + "." + digits[e+1:]
		default:
			out = "0." + strings.Repeat("0", -e-1) + digits
		}
	default:
		frac := digits[1:]
		if frac == "" {
			frac = "0"
		}
		out = digits[:1] + "." + frac + "E" + strconv.Itoa(e)
	}
	if neg {
		return "-" + out
	}
	return out
}
