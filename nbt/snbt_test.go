package nbt

import (
	"reflect"
	"slices"
	"strconv"
	"testing"
)

// vanilla: SnbtGrammar.IntegerLiteral, convertFloat, unquoted_string_or_builtin
func TestParseValueStrictLiterals(t *testing.T) {
	for _, tc := range []struct {
		text string
		want Tag
	}{
		{"0xFFFFFFFF", Int(-1)},
		{"0b11111111111111111111111111111111", Int(-1)},
		{"0xFFb", Int(4091)},
		{"0xFFFFs", Short(-1)},
		{"0b11111111b", Byte(-1)},
		{"0xFFFFFFFFFFFFFFFFL", Long(-1)},
		{"-0x10si", Int(-16)},
		{"-0b10sb", Byte(-2)},
		{"[I;0xFFFFFFFF]", IntArray{-1}},
		{"[B;0b11111111]", ByteArray{255}},
		{"[I;-0x10si]", IntArray{-16}},
		{"3.4028235E38f", Float(3.4028235e38)},
		{"TRUE", Byte(1)},
		{"FaLsE", Byte(0)},
		{"bool(TRUE)", Byte(1)},
		{"TrueStory", String("TrueStory")},
	} {
		got, n, err := ParseValueTag(tc.text)
		if err != nil || n != len(tc.text) || !reflect.DeepEqual(got, tc.want) {
			t.Errorf("ParseValueTag(%q) = %#v, %d, %v; want %#v", tc.text, got, n, err, tc.want)
		}
	}
	for _, tc := range []struct {
		text string
		key  string
	}{
		{"-0x10", errNonNegative},
		{"-0b10", errNonNegative},
		{"-0x10s", errNonNegative},
		{"[I;-0x10]", errNonNegative},
		{"3.4028235E39f", errInfinity},
		{"-3.4028235E39F", errInfinity},
	} {
		_, _, err := ParseValueTag(tc.text)
		if err == nil || err.Key != tc.key || err.Offset != len(tc.text) {
			t.Errorf("ParseValueTag(%q) error = %#v; want %s at %d", tc.text, err, tc.key, len(tc.text))
		}
	}
}

// TestParseSNBT reads a spread of SNBT texts, checking the type each
// suffix named is the type the tag read as.
func TestParseSNBT(t *testing.T) {
	tests := []struct {
		text string
		want Tag
	}{
		{"{}", Compound{}},
		{"{a:1b}", Compound{"a": Byte(1)}},
		{"{a:1s}", Compound{"a": Short(1)}},
		{"{a:1}", Compound{"a": Int(1)}},
		{"{a:1l}", Compound{"a": Long(1)}},
		{"{a:1f}", Compound{"a": Float(1)}},
		{"{a:1d}", Compound{"a": Double(1)}},
		{"{a:1.5}", Compound{"a": Double(1.5)}},
		{"{a:1.5f}", Compound{"a": Float(1.5)}},
		{"{a:.5}", Compound{"a": Double(0.5)}},
		{"{a:-.5}", Compound{"a": Double(-0.5)}},
		{"{a:+1}", Compound{"a": Int(1)}},
		{"{a:1e5}", Compound{"a": Double(100000)}},
		{"{a:1.5e-2f}", Compound{"a": Float(0.015)}},
		{"{a:1e+5d}", Compound{"a": Double(100000)}},
		{"{a:0xFF}", Compound{"a": Int(255)}},
		{"{a:0xFFl}", Compound{"a": Long(255)}},
		{"{a:0b1010}", Compound{"a": Int(10)}},
		{"{a:0X1_F}", Compound{"a": Int(31)}},
		{"{a:1__2}", Compound{"a": Int(12)}},
		{"{a:_1}", Compound{"a": String("_1")}},
		{"{a:1_000_000}", Compound{"a": Int(1000000)}},
		{"{a:255ub}", Compound{"a": Byte(-1)}},
		{"{a:0xFFub}", Compound{"a": Byte(-1)}},
		{"{a:65535us}", Compound{"a": Short(-1)}},
		{"{a:1sb}", Compound{"a": Byte(1)}},
		{"{a:1 b}", Compound{"a": Byte(1)}},
		{"{a:1 ,b:2}", Compound{"a": Int(1), "b": Int(2)}},
		{"{a:0b1_0}", Compound{"a": Int(2)}},
		{"{ cooldown_ends_at : 900000000L }", Compound{"cooldown_ends_at": Long(900000000)}},
		{"{a:true}", Compound{"a": Byte(1)}},
		{"{a:false}", Compound{"a": Byte(0)}},
		{"{a:trueX}", Compound{"a": String("trueX")}},
		{"{a:hello}", Compound{"a": String("hello")}},
		{`{a:"123"}`, Compound{"a": String("123")}},
		{`{a:'it\'s'}`, Compound{"a": String("it's")}},
		{`{"a b":"c\nd"}`, Compound{"a b": String("c\nd")}},
		{`{a:"\x41é"}`, Compound{"a": String("Aé")}},
		{`{a:"\\ \" \s \b"}`, Compound{"a": String("\\ \" \x20 \b")}},
		{"{a.b-1_c:2}", Compound{"a.b-1_c": Int(2)}},
		{"{a:{b:{c:1}}}", Compound{"a": Compound{"b": Compound{"c": Int(1)}}}},
		{"{a:[1,2,3]}", Compound{"a": List{Elem: TypeInt, Items: []Tag{Int(1), Int(2), Int(3)}}}},
		{"{a:[]}", Compound{"a": List{}}},
		{"{a:[1,2,]}", Compound{"a": List{Elem: TypeInt, Items: []Tag{Int(1), Int(2)}}}},
		{"{a:[ 1 , 2 ]}", Compound{"a": List{Elem: TypeInt, Items: []Tag{Int(1), Int(2)}}}},
		{"{a:[B;1,2]}", Compound{"a": ByteArray{1, 2}}},
		{"{a:[B;]}", Compound{"a": ByteArray{}}},
		{"{a:[I;0x10,-1]}", Compound{"a": IntArray{16, -1}}},
		{"{a:[L;1_000L]}", Compound{"a": LongArray{1000}}},
		{"{a:[I;1b,2s]}", Compound{"a": IntArray{1, 2}}},
		{"{a:[L;1i,2b]}", Compound{"a": LongArray{1, 2}}},
		{"{a:[B;200ub]}", Compound{"a": ByteArray{200}}},
		{`{a:[{id:"x",count:1b},{}]}`, Compound{"a": List{Elem: TypeCompound, Items: []Tag{
			Compound{"id": String("x"), "count": Byte(1)}, Compound{},
		}}}},
		{"{a:[[1],[2]]}", Compound{"a": List{Elem: TypeList, Items: []Tag{
			List{Elem: TypeInt, Items: []Tag{Int(1)}},
			List{Elem: TypeInt, Items: []Tag{Int(2)}},
		}}}},
		{"{a:[[1],[2b]]}", Compound{"a": List{Elem: TypeList, Items: []Tag{
			List{Elem: TypeInt, Items: []Tag{Int(1)}},
			List{Elem: TypeByte, Items: []Tag{Byte(2)}},
		}}}},
		{"{a:[1,\"two\"]}", Compound{"a": List{Elem: TypeCompound, Items: []Tag{
			Int(1), String("two"),
		}}}},
		{"{a:[{},1]}", Compound{"a": List{Elem: TypeCompound, Items: []Tag{
			Compound{}, Int(1),
		}}}},
		{"{a:0b}", Compound{"a": Byte(0)}},
		{"{a:0B}", Compound{"a": Byte(0)}},
		{"{a:[B;0b]}", Compound{"a": ByteArray{0}}},
		{"{a:bool(1)}", Compound{"a": Byte(1)}},
		{"{a:bool(0)}", Compound{"a": Byte(0)}},
		{"{a:bool(-5)}", Compound{"a": Byte(1)}},
		{"{a:bool(true)}", Compound{"a": Byte(1)}},
		{"{a:bool(0.0)}", Compound{"a": Byte(0)}},
		{"{a:uuid(\"f81d4fae-7dec-11d0-a765-00a0c91e6bf6\")}", Compound{"a": IntArray{
			-132296786, 2112623056, -1486552928, -920753162,
		}}},
		{"{a:{},b:1,}", Compound{"a": Compound{}, "b": Int(1)}},
	}
	for _, test := range tests {
		got, err := ParseSNBT(test.text)
		if err != nil {
			t.Errorf("ParseSNBT(%q): %v", test.text, err)
			continue
		}
		if !reflect.DeepEqual(Tag(got), test.want) {
			t.Errorf("ParseSNBT(%q) = %v, want %v", test.text, got, test.want)
			continue
		}
		// Every tag read must survive a round-trip through the binary
		// form, which is how a carried compound comes back from a save.
		data, err := Encode("", got)
		if err != nil {
			t.Errorf("Encode(ParseSNBT(%q)): %v", test.text, err)
			continue
		}
		_, tag, err := Decode(data)
		if err != nil {
			t.Errorf("Decode(ParseSNBT(%q)): %v", test.text, err)
			continue
		}
		if !reflect.DeepEqual(tag, test.want) {
			t.Errorf("ParseSNBT(%q) did not round-trip: got %v, want %v", test.text, tag, test.want)
		}
	}
}

// TestParseSNBTErrors reads the texts the grammar refuses and the compound
// argument form demands.
func TestParseSNBTErrors(t *testing.T) {
	tests := []string{
		"",                // nothing there
		"1",               // a bare tag, not a compound
		"1b",              // a bare tag, not a compound
		"\"hi\"",          // a string, not a compound
		"[1]",             // a list, not a compound
		"{",               // unclosed
		"{a",              // unclosed after the name
		"{a:",             // unclosed before the value
		"{a:1",            // unclosed before the comma
		"{a:1,",           // unclosed after the comma
		"{a:}",            // no value
		"{:1}",            // no name
		"{a 1}",           // no colon
		"{a:1}x",          // trailing data
		"{a:1;}",          // no colon where the semicolon stood
		"{a:2147483648}",  // an int too wide
		"{a:128b}",        // a byte too wide
		"{a:32768s}",      // a short too wide
		"{a:1.5b}",        // an integer suffix on a float
		"{a:0b2}",         // a binary digit that is not one, left after the nought
		"{a:0x}",          // a hex prefix with no hex digit after it
		"{a:0xg}",         // a hex prefix with no hex digit after it
		"{a:0123}",        // a leading zero
		"{a:00}",          // a leading zero
		"{a:-1ub}",        // a minus on an unsigned number
		"{a:[B;1s]}",      // an array element of the wrong type
		"{a:[B 1]}",       // no semicolon after the array's type
		"{a:[1 2]}",       // no comma between the elements
		"{a:[}",           // a compound where a list element belongs
		`{a:"unclosed}`,   // no closing quote
		`{a:"bad\q"}`,     // an escape that is not one
		`{a:"\xZZ"}`,      // hex digits that are not
		`{a:"\x4"}`,       // a hex escape too short
		"{a:12e}",         // an exponent with no digits
		"{a:.e5}",         // no digits on either side of the dot
		"{a:--1}",         // a second sign
		"{a:1_}",          // an underscore after the last digit
		"{a:1u}",          // an unsigned mark with no width letter behind it
		"{a:0X_1}",        // an underscore before the first hex digit
		"{a:0X1_}",        // an underscore after the last hex digit
		"{a:[B;300]}",     // a byte array element too wide for a byte
		"{a:[I;128b]}",    // a byte-suffixed element too wide for a byte
		"{a:[B;300i]}",    // an int-suffixed element in a byte array
		"{a:[B;0123]}",    // a leading zero in an array element
		"{a:bool(\"x\")}", // a string has no truth
		"{a:bool(1}",      // an operation left unclosed
		"{a:uuid(1)}",     // a number is not a UUID
		"{a:uuid(\"x\")}", // a UUID is 36 characters, dashed
		"{a:foo(1)}",      // an operation that is not one
		`{a:{"":1}}`,      // an entry with no name
		"{a:1 b:2}",       // a second entry with nothing between
		"{a:[i;1]}",       // the lowercase letter the array prefix does not take
		`{a:"\N{}"}`,
		`{a:"\N{bogus nonsense}"}`,
	}
	for _, text := range tests {
		if _, err := ParseSNBT(text); err == nil {
			t.Errorf("ParseSNBT(%q) succeeded, want an error", text)
		}
	}
}

// TestParseSNBTErrorsCompareWithVanilla reads the texts a vanilla
// server refuses, checked against what it answers each with: the key the
// client renders the failure with, the values the key's placeholders take,
// and the byte the context line points into the tag. The grammar reports
// the first expectation among the entries at the furthest cursor, so a
// fault in the compound reads as the number's own readings before it. The
// rows are what the vanilla server was probed with, answer for answer.
func TestParseSNBTErrorsCompareWithVanilla(t *testing.T) {
	literal, hex_, binary, decimal, nonNegative, unquoted := literalIncorrect, errHexNumeral,
		errBinaryNumeral, errDecimalNumeral, errNonNegative, errUnquoted
	valueOut := func(sign, ds string, radix int) string {
		return "Value out of range. Value:\"" + sign + ds + "\" Radix:" + strconv.Itoa(radix)
	}
	tests := []struct {
		text   string
		key    string
		offset int
		with   []string
	}{
		// Nothing readable where a value belonged.
		{"{a:", unquoted, 3, nil},
		{"{a:}", unquoted, 3, nil},
		{"{a:[1,}", unquoted, 6, nil},
		// A decimal run's own readings: the dot first, named at the run's
		// end, the exponent second, and the compound's separator after.
		{"{a:1q}", literal, 4, []string{"."}},
		{"{a:1a}", literal, 4, []string{"."}},
		{"{a:1;}", literal, 4, []string{"."}},
		{"{a:01}", literal, 5, []string{"."}},
		{"{a:1 2:3}", literal, 5, []string{"."}},
		{"{a:[1 2]}", literal, 6, []string{"."}},
		{"{a:2147483648}", literal, 13, []string{"."}},
		{"{a:99999999999999999999}", literal, 23, []string{"."}},
		{"{\"\":1}", literal, 5, []string{"."}},
		{"{'':1}", literal, 5, []string{"."}},
		// The suffix consumed past the run leaves the compound's comma at
		// its back, and the separator the value missed behind that.
		{"{a:1 b:2}", literal, 6, []string{","}},
		{"{a:1b:2}", literal, 5, []string{","}},
		{"{a:1 b:2 c:3}", literal, 6, []string{","}},
		{"{a 1}", literal, 3, []string{":"}},
		{"{a}", literal, 2, []string{":"}},
		{"{:1}", literal, 1, []string{"\""}},
		// A float's digits name their exponent before the suffix, and the
		// exponent its sign before its digits.
		{"{a:1.5q}", literal, 6, []string{"e|E"}},
		{"{a:1.5b}", literal, 6, []string{"e|E"}},
		{"{a:1.5.5}", literal, 6, []string{"e|E"}},
		{"{a:1e}", literal, 5, []string{"+"}},
		{"{a:12e}", literal, 6, []string{"+"}},
		{"{a:1.5e}", literal, 7, []string{"+"}},
		{"{a:1e+}", decimal, 6, nil},
		{"{a:+}", decimal, 4, nil},
		{"{a:.}", decimal, 4, nil},
		{"{a:1..2}", decimal, 5, nil},
		// The based runs name their unsigned mark, and their prefixes the
		// digit that never came, which a half-number then reads as the
		// word of an operation it was not.
		{"{a:0b12}", literal, 6, []string{"u|U"}},
		{"{a:0x}", hex_, 5, nil},
		{"{a:0b2}", binary, 5, nil},
		{"{a:-1ub}", nonNegative, 7, nil},
		{"{a:--1}", literal, 6, []string{"("}},
		{"{a:--}", literal, 5, []string{"("}},
		{"{a:-e}", literal, 5, []string{"("}},
		{"{a:1_}", literal, 5, []string{"("}},
		{"{a:.e5}", literal, 6, []string{"("}},
		{"{a:..}", literal, 5, []string{"("}},
		{"{a:0X_1}", literal, 7, []string{"("}},
		{"{a:0X1_}", literal, 7, []string{"("}},
		{"{a:0x_}", literal, 6, []string{"("}},
		// The letters of an array prefix, and its semicolon.
		{"{a:[}", literal, 4, []string{"B"}},
		{"{a:[B 1]}", literal, 6, []string{";"}},
		{"{a:[b;1]}", literal, 5, []string{"("}},
		{"{a:[i;1]}", literal, 5, []string{"("}},
		{"{a:[I;1,b]}", literal, 8, []string{"+"}},
		// A range too wide for its suffix carries the vanilla message,
		// past the suffix; a quoted string's first escape letter is named
		// at the character under its backslash, and an element an array
		// cannot hold is named past its bracket.
		{"{a:300b}", errNumberParseFailure, 7, []string{valueOut("", "300", 10)}},
		{"{a:128b}", errNumberParseFailure, 7, []string{valueOut("", "128", 10)}},
		{"{a:-300b}", errNumberParseFailure, 8, []string{valueOut("-", "300", 10)}},
		{"{a:300b,}", errNumberParseFailure, 7, []string{valueOut("", "300", 10)}},
		{"{a:[B;300]}", errNumberParseFailure, 10, []string{valueOut("", "300", 10)}},
		{"{a:[B;300,301]}", errNumberParseFailure, 14, []string{valueOut("", "300", 10)}},
		{"{a:[B;70000]}", errNumberParseFailure, 12, []string{valueOut("", "70000", 10)}},
		{"{a:[I;128b]}", errNumberParseFailure, 11, []string{valueOut("", "128", 10)}},
		{"{a:[B;1s]}", errArrayElement, 9, nil},
		{"{a:[B;0123]}", errLeadingZero, 10, nil},
		{"{a:'\\q'}", literal, 5, []string{"b"}},
		{`{a:"bad\q"}`, literal, 8, []string{"b"}},
		// A Unicode name reads over vanilla's class and must meet its
		// brace: an outside character ends the name there, a missing
		// brace is a literal, and an empty name is named where it stood.
		{`{a:"\N{a_b}"}`, literal, 8, []string{"}"}},
		{`{a:"\N{ab"}`, literal, 9, []string{"}"}},
		{`{a:"\Nx"}`, literal, 6, []string{"{"}},
		{`{a:"\N{}"}`, errInvalidCharName, 7, nil},
		{`{a:"\N{NO SUCH NAME}"}`, errInvalidCharName, 20, nil},
		{`{a:"unclosed}`, errStringContents, 13, nil},
		// The operations carry their name and their arity; their argument
		// checks point at the argument.
		{"{a:foo(1)}", errNoSuchOperation, 9, []string{"foo/1"}},
		{"{a:fooo(1)}", errNoSuchOperation, 10, []string{"fooo/1"}},
		{"{a:bool(1,2)}", errNoSuchOperation, 12, []string{"bool/2"}},
		{"{a:uuid(1,2)}", errNoSuchOperation, 12, []string{"uuid/2"}},
		{"{a:uuid(1)}", errStringUUID, 10, nil},
		{`{a:uuid("x")}`, errStringUUID, 12, nil},
		{`{a:bool("x")}`, errNumberOrBoolean, 12, nil},
	}
	for _, test := range tests {
		_, err := ParseSNBT(test.text)
		pe, ok := err.(*ParseError)
		if !ok {
			t.Errorf("ParseSNBT(%q) = %v, want a *ParseError", test.text, err)
			continue
		}
		if pe.Key != test.key {
			t.Errorf("ParseSNBT(%q) key = %q, want %q", test.text, pe.Key, test.key)
		}
		if pe.Offset != test.offset {
			t.Errorf("ParseSNBT(%q) offset = %d, want %d", test.text, pe.Offset, test.offset)
		}
		if !slices.Equal(pe.With, test.with) {
			t.Errorf("ParseSNBT(%q) with = %v, want %v", test.text, pe.With, test.with)
		}
	}
}

// TestParseValue reads one value from the start of a text and checks the
// offset the reading stopped at, which is what a caller reading a value
// inside a larger text needs, and the refusal a value the grammar cannot
// read hands back.
func TestParseValue(t *testing.T) {
	tests := []struct {
		text string
		end  int
		key  string
	}{
		{"1_000]", 5, ""},
		{"1__0,", 4, ""},
		{"1.5_5,", 5, ""},
		{"0x10ub}", 6, ""},
		{"0b101,", 5, ""},
		{"bool(1),", 7, ""},
		{"{a:1,b:2},", 9, ""},
		{"'a b',", 5, ""},
		// The fraction behind a dot is optional, so the dot alone is a
		// double and the exponent may follow it.
		{"5.", 2, ""},
		{"1.e2", 4, ""},
		{"1.f", 3, ""},
		{"1..5]", 2, ""},
		// A u with no width letter behind it is no suffix, so the run ends
		// before it and the bytes behind it are the caller's.
		{"1u]", 1, ""},
		// A half number names the numeral its own rule wanted; an
		// unterminated string names its contents at the end.
		{"'a", 2, errStringContents},
		// A value that never begins records its first expectation at the
		// offset zero it stood on, which an uninitialised furthest mark
		// would otherwise swallow.
		{"", 0, errUnquoted},
		{"]", 0, errUnquoted},
		// A Unicode name escape is read for its shape, so an empty name is
		// refused, and a name reads over vanilla's class up to the brace.
		{`"\N{}"`, 3, errInvalidCharName},
		{`"\N{a_b}"`, 5, literalIncorrect},
		{`"\N{ab"`, 6, literalIncorrect},
	}
	for _, test := range tests {
		end, err := ParseValue(test.text)
		if test.key == "" {
			if err != nil {
				t.Errorf("ParseValue(%q) = %v, want no error", test.text, err)
				continue
			}
			if end != test.end {
				t.Errorf("ParseValue(%q) end = %d, want %d", test.text, end, test.end)
			}
			continue
		}
		if err == nil {
			t.Errorf("ParseValue(%q) = %d, want the %s refusal", test.text, end, test.key)
			continue
		}
		if err.Key != test.key {
			t.Errorf("ParseValue(%q) key = %q, want %q", test.text, err.Key, test.key)
		}
	}
}

// TestParseSNBTKindsKeepTheirWidths pins the suffix to the type: an f
// suffix never widens to a double, and a missing suffix never widens to a
// long, because a round-trip through the binary form would say otherwise.
func TestParseSNBTKindsKeepTheirWidths(t *testing.T) {
	tree, err := ParseSNBT("{f:1f,d:1d,i:1,l:1L,s:1s,b:1b}")
	if err != nil {
		t.Fatalf("ParseSNBT: %v", err)
	}
	want := Compound{
		"f": Float(1), "d": Double(1), "i": Int(1),
		"l": Long(1), "s": Short(1), "b": Byte(1),
	}
	if !reflect.DeepEqual(tree, want) {
		for _, name := range []string{"b", "s", "i", "l", "f", "d"} {
			t.Errorf("%s: got %T(%v), want %T(%v)", name, tree[name], tree[name], want[name], want[name])
		}
	}
}
