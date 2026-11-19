// Package gogen builds the Go source files the generators write.
//
// A file is assembled as text and formatted in-process with go/format, so a
// generator needs no gofmt on its PATH and its output matches `gofmt` of the
// toolchain that built it.
package gogen

import (
	"fmt"
	"go/format"
	"math"
	"strconv"
	"strings"
)

// File is a generated Go source file under construction.
type File struct {
	b strings.Builder
}

// New starts a file in package pkg. from completes the line "// Code
// generated <from>. DO NOT EDIT." and must be one line: the Go tools
// recognise generated code by matching that whole line, so a wrapped header
// would leave the file treated as hand-written.
func New(pkg, from string) *File {
	if strings.ContainsAny(from, "\n\r") {
		panic("gogen: the generated-code header must fit on one line: " + from)
	}
	f := &File{}
	fmt.Fprintf(&f.b, "// Code generated %s. DO NOT EDIT.\n\npackage %s\n\n", from, pkg)
	return f
}

// Printf appends formatted text.
func (f *File) Printf(format string, args ...any) {
	fmt.Fprintf(&f.b, format, args...)
}

// WriteString appends text as it is.
func (f *File) WriteString(s string) {
	f.b.WriteString(s)
}

// Strings appends `var name = []string{...}` with one literal per line.
func (f *File) Strings(name string, values []string) {
	fmt.Fprintf(&f.b, "var %s = []string{\n", name)
	for _, v := range values {
		fmt.Fprintf(&f.b, "\t%s,\n", Quote(v))
	}
	f.b.WriteString("}\n")
}

// Set appends `var name = map[string]bool{...}` holding every value as true.
func (f *File) Set(name string, values []string) {
	fmt.Fprintf(&f.b, "var %s = map[string]bool{\n", name)
	for _, v := range values {
		fmt.Fprintf(&f.b, "\t%s: true,\n", Quote(v))
	}
	f.b.WriteString("}\n")
}

// Source returns the file formatted as gofmt would.
func (f *File) Source() ([]byte, error) {
	return format.Source([]byte(f.b.String()))
}

// Quote returns s as a Go string literal in which everything outside printable
// ASCII is a \u escape, so the generated files stay plain ASCII. Code points
// beyond the BMP use \U, since a Go literal has no surrogate pairs.
func Quote(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch {
		case r == '"' || r == '\\':
			b.WriteByte('\\')
			b.WriteRune(r)
		case r >= ' ' && r <= '~':
			b.WriteRune(r)
		case r == '\n':
			b.WriteString(`\n`)
		case r == '\r':
			b.WriteString(`\r`)
		case r == '\t':
			b.WriteString(`\t`)
		case r == '\b':
			b.WriteString(`\b`)
		case r == '\f':
			b.WriteString(`\f`)
		case r > 0xFFFF:
			fmt.Fprintf(&b, `\U%08x`, r)
		default:
			fmt.Fprintf(&b, `\u%04x`, r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

// Int formats an integer literal.
func Int[T ~int | ~int8 | ~int16 | ~int32 | ~int64 | ~uint8 | ~uint16 | ~uint32 | ~uint64](v T) string {
	return strconv.FormatInt(int64(v), 10)
}

// Float formats f as a Go float literal: the shortest digits that read back as
// the same value, fixed notation from 1e-4 up to 1e16 with a ".0" on a whole
// number (so the literal is a float even where Go would otherwise take an
// integer), and exponent notation (1e-05, 1e+16) beyond that.
func Float(f float64) string {
	if math.IsNaN(f) || math.IsInf(f, 0) {
		panic("gogen: " + strconv.FormatFloat(f, 'g', -1, 64) + " has no Go literal")
	}
	s := strconv.FormatFloat(f, 'e', -1, 64)
	neg := strings.HasPrefix(s, "-")
	s = strings.TrimPrefix(s, "-")
	mant, expText, _ := strings.Cut(s, "e")
	exp, _ := strconv.Atoi(expText)
	digits := strings.Replace(mant, ".", "", 1)
	decpt := exp + 1 // the value is 0.<digits> * 10^decpt
	var out string
	switch {
	case decpt <= -4 || decpt > 16:
		out = digits[:1]
		if len(digits) > 1 {
			out += "." + digits[1:]
		}
		sign := "+"
		if exp < 0 {
			sign, exp = "-", -exp
		}
		out += fmt.Sprintf("e%s%02d", sign, exp)
	case decpt <= 0:
		out = "0." + strings.Repeat("0", -decpt) + digits
	case decpt >= len(digits):
		out = digits + strings.Repeat("0", decpt-len(digits)) + ".0"
	default:
		out = digits[:decpt] + "." + digits[decpt:]
	}
	if neg {
		out = "-" + out
	}
	return out
}
