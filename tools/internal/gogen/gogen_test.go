package gogen

import (
	"strings"
	"testing"
)

func TestQuoteMatchesJSONDumps(t *testing.T) {
	// Each want is the input as an escaped, plain-ASCII literal.
	cases := []struct{ in, want string }{
		{"minecraft:stone", `"minecraft:stone"`},
		{`a"b\c`, `"a\"b\\c"`},
		{"line\nbreak\ttab\r", `"line\nbreak\ttab\r"`},
		{"\b\f", `"\b\f"`},
		{"\x00\x1f\x7f", `"\u0000\u001f\u007f"`},
		{"caf\u00e9", `"caf\u00e9"`},
		{"\u2603", `"\u2603"`},
		// Beyond the BMP a Go literal takes \U, not a surrogate pair.
		{"\U0001f600", `"\U0001f600"`},
	}
	for _, c := range cases {
		if got := Quote(c.in); got != c.want {
			t.Errorf("Quote(%q) = %s, want %s", c.in, got, c.want)
		}
	}
}

func TestQuoteParsesBack(t *testing.T) {
	for _, s := range []string{"plain", "tab\there", "\u00e9\u2603\U0001f600", "\x00\x7f"} {
		src := "package p\n\nvar s = " + Quote(s) + "\n"
		f := &File{}
		f.WriteString(src)
		if _, err := f.Source(); err != nil {
			t.Errorf("Quote(%q) is not valid Go: %v", s, err)
		}
	}
}

func TestHeaderIsOneLine(t *testing.T) {
	f := New("registry", "from a vanilla 26.3 server")
	src, err := f.Source()
	if err != nil {
		t.Fatal(err)
	}
	want := "// Code generated from a vanilla 26.3 server. DO NOT EDIT.\n\npackage registry\n"
	if string(src) != want {
		t.Errorf("got %q, want %q", src, want)
	}
	defer func() {
		if recover() == nil {
			t.Error("a header spanning two lines was accepted")
		}
	}()
	New("registry", "from a vanilla\n26.3 server")
}

func TestTablesAreFormatted(t *testing.T) {
	f := New("p", "from test")
	f.Strings("Names", []string{"a", "b"})
	f.Set("Seen", []string{"x"})
	src, err := f.Source()
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"var Names = []string{\n\t\"a\",\n\t\"b\",\n}\n", "var Seen = map[string]bool{\n\t\"x\": true,\n}\n"} {
		if !strings.Contains(string(src), want) {
			t.Errorf("missing %q in\n%s", want, src)
		}
	}
}

func TestFloatFormat(t *testing.T) {
	// Each want is the shortest form that reads back as the same float.
	cases := []struct {
		in   float64
		want string
	}{
		{0, "0.0"}, {1, "1.0"}, {-1, "-1.0"}, {0.5, "0.5"}, {1.6, "1.6"}, {0.3, "0.3"},
		{3, "3.0"}, {100000, "100000.0"}, {123456789, "123456789.0"},
		{1e15, "1000000000000000.0"}, {1e16, "1e+16"}, {1.5e16, "1.5e+16"}, {12345678901234567, "1.2345678901234568e+16"},
		{0.001, "0.001"}, {0.0001, "0.0001"}, {0.00001, "1e-05"}, {0.000123, "0.000123"}, {1.5e-7, "1.5e-07"},
		{1e100, "1e+100"}, {1e-100, "1e-100"}, {0.30000000000000004, "0.30000000000000004"}, {2.5e-5, "2.5e-05"},
		{-0.0001234, "-0.0001234"}, {5e-324, "5e-324"},
	}
	for _, c := range cases {
		if got := Float(c.in); got != c.want {
			t.Errorf("Float(%v) = %s, want %s", c.in, got, c.want)
		}
	}
}
