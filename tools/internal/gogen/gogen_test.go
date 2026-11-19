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
