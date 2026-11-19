package nbt

import (
	"bytes"
	"encoding/binary"
	"math"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"testing"
)

// TestRoundTrip writes a tree holding every tag type and reads it back.
func TestRoundTrip(t *testing.T) {
	tree := Compound{
		"byte":   Byte(-128),
		"short":  Short(-32768),
		"int":    Int(math.MinInt32),
		"long":   Long(math.MinInt64),
		"float":  Float(0.5),
		"double": Double(-60.0625),
		"string": String("hello"),
		"bytes":  ByteArray{0, 1, 0xFF},
		"ints":   IntArray{1, -1, math.MaxInt32},
		"longs":  LongArray{0, math.MinInt64},
		"list": List{Elem: TypeDouble, Items: []Tag{
			Double(4.5), Double(-60), Double(-8.5),
		}},
		"empty": List{},
		"nested": Compound{
			"inner": Compound{"deep": String("value")},
			"lists": List{Elem: TypeList, Items: []Tag{
				List{Elem: TypeString, Items: []Tag{String("a")}},
			}},
		},
	}

	data, err := Encode("root", tree)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	name, tag, err := Decode(data)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if name != "root" {
		t.Errorf("root name = %q, want %q", name, "root")
	}
	if !reflect.DeepEqual(tag, tree) {
		t.Errorf("decoded tree does not match:\n got %#v\nwant %#v", tag, tree)
	}
}

// TestEncodeIsStable checks the same tree always produces the same bytes,
// which map iteration in Go would otherwise make untrue.
func TestEncodeIsStable(t *testing.T) {
	names := []string{"zebra", "yak", "xylem", "walrus", "vole", "umbra", "tiger"}
	tree := Compound{}
	for _, name := range names {
		tree[name] = String(name)
	}

	first, err := Encode("", tree)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	for range 10 {
		again, err := Encode("", tree)
		if err != nil {
			t.Fatalf("Encode: %v", err)
		}
		if !bytes.Equal(first, again) {
			t.Fatal("encoding the same tree twice produced different bytes")
		}
	}

	// Sorted: the names run backwards through the alphabet, so an unsorted
	// file would keep the order they were added in.
	at := -1
	for _, name := range slices.Sorted(slices.Values(names)) {
		next := bytes.Index(first, []byte(name))
		if next < at {
			t.Errorf("field %q was written out of order", name)
		}
		at = next
	}
}

// TestEmptyListRoundTrip checks an empty list comes back as one, whatever
// element type it was built with: the game writes its element type as end,
// so the type it once held is not in the file.
func TestEmptyListRoundTrip(t *testing.T) {
	data, err := Encode("", Compound{"empty": List{Elem: TypeCompound}})
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	_, tag, err := Decode(data)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	list, ok := Get[List](tag.(Compound), "empty")
	if !ok {
		t.Fatal("empty list is missing")
	}
	if list.Elem != TypeEnd || len(list.Items) != 0 {
		t.Errorf("empty list = %+v, want an empty list of end tags", list)
	}
}

// TestModifiedUTF8 checks the two places Java's encoding differs from real
// UTF-8, both of which turn up in player data: a name with an emoji, and any
// string holding a NUL.
func TestModifiedUTF8(t *testing.T) {
	for _, s := range []string{
		"",
		"plain",
		"nul\x00inside",
		"emoji \U0001F642 and \U0001F600",
		"latin1 éü, cyrillic Ж, cjk 中文",
	} {
		data, err := Encode("", String(s))
		if err != nil {
			t.Fatalf("Encode(%q): %v", s, err)
		}
		_, tag, err := Decode(data)
		if err != nil {
			t.Fatalf("Decode(%q): %v", s, err)
		}
		if got := string(tag.(String)); got != s {
			t.Errorf("round trip of %q = %q", s, got)
		}
	}

	// NUL is written the long way round so that no byte of a string is
	// zero, and anything outside the basic plane as its two surrogates.
	data, err := Encode("", String("\x00\U0001F642"))
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	want := []byte{0xC0, 0x80, 0xED, 0xA0, 0xBD, 0xED, 0xB9, 0x82}
	if got := data[len(data)-len(want):]; !bytes.Equal(got, want) {
		t.Errorf("encoded string = % X, want % X", got, want)
	}
}

// TestDecodeModifiedUTF8 decodes raw string bytes: ASCII, two- and
// three-byte runes, a surrogate pair, the two-byte NUL and a raw NUL byte.
func TestDecodeModifiedUTF8(t *testing.T) {
	for _, c := range []struct {
		raw  []byte
		want string
	}{
		{[]byte("plain ascii"), "plain ascii"},
		{[]byte{'a', 0x00, 'b'}, "a\x00b"},
		{[]byte{'c', 'a', 'f', 0xC3, 0xA9}, "café"},
		{[]byte{0xE2, 0x82, 0xAC, '5'}, "€5"},
		{[]byte{0xED, 0xA0, 0xB4, 0xED, 0xB4, 0x9E}, "\U0001D11E"},
		{[]byte{'x', 0xC0, 0x80, 'y'}, "x\x00y"},
	} {
		data := []byte{byte(TypeString), 0, 0, byte(len(c.raw) >> 8), byte(len(c.raw))}
		data = append(data, c.raw...)
		_, tag, err := Decode(data)
		if err != nil {
			t.Fatalf("Decode(% X): %v", c.raw, err)
		}
		if got := string(tag.(String)); got != c.want {
			t.Errorf("Decode(% X) = %q, want %q", c.raw, got, c.want)
		}
	}
}

// TestAppendString checks the contract the packet encoder relies on: the
// bytes are appended to whatever is already there, and a string too long to
// have a length leaves the buffer exactly as it was.
func TestAppendString(t *testing.T) {
	prefix := []byte{0xDE, 0xAD}

	got, ok := AppendString(prefix, "hi")
	if !ok {
		t.Fatal("AppendString refused an ordinary string")
	}
	if want := []byte{0xDE, 0xAD, 0, 2, 'h', 'i'}; !bytes.Equal(got, want) {
		t.Errorf("AppendString = % X, want % X", got, want)
	}

	// The longest string there is, which is a length of exactly MaxStringLen.
	longest := strings.Repeat("x", MaxStringLen)
	if got, ok := AppendString(nil, longest); !ok || len(got) != MaxStringLen+2 {
		t.Errorf("a string of %d bytes was refused (ok=%v, wrote %d)", MaxStringLen, ok, len(got))
	}

	// One byte more than a length can describe; the buffer must come back
	// untouched, or a caller would write a length that wrapped.
	for _, s := range []string{
		strings.Repeat("x", MaxStringLen+1),
		strings.Repeat("€", MaxStringLen/3+1), // three bytes apiece
		strings.Repeat("\x00", MaxStringLen/2+1),
	} {
		got, ok := AppendString(prefix, s)
		if ok {
			t.Errorf("a string of %d bytes was accepted", len(got)-2)
		}
		if !bytes.Equal(got, prefix) {
			t.Errorf("a refused string left % X behind, want % X", got, prefix)
		}
	}
}

// TestDecodeRejects checks a file that is not valid NBT is reported as such
// rather than acted on: player data is read off disk, where a half-written
// or edited file is a real possibility.
func TestDecodeRejects(t *testing.T) {
	good, err := Encode("root", Compound{
		"pos": List{Elem: TypeDouble, Items: []Tag{Double(1), Double(2), Double(3)}},
	})
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	deep := []byte{byte(TypeList), 0, 0}
	for range maxDepth + 2 {
		deep = append(deep, byte(TypeList), 0, 0, 0, 1)
	}
	deep = append(deep, byte(TypeEnd), 0, 0, 0, 0)

	tests := []struct {
		name string
		data []byte
	}{
		{"empty file", nil},
		{"root is an end tag", []byte{byte(TypeEnd)}},
		{"unknown tag", []byte{99, 0, 0}},
		{"truncated name", []byte{byte(TypeCompound), 0, 8, 'h', 'i'}},
		{"truncated payload", good[:len(good)-4]},
		{"unterminated compound", []byte{byte(TypeCompound), 0, 0}},
		{"negative array length", []byte{byte(TypeIntArray), 0, 0, 0xFF, 0xFF, 0xFF, 0xFF}},
		{"array longer than the file", []byte{byte(TypeIntArray), 0, 0, 0x00, 0x10, 0x00, 0x00}},
		{"list of end tags with entries", []byte{byte(TypeList), 0, 0, byte(TypeEnd), 0, 0, 0, 1}},
		{"nested past the depth limit", deep},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, _, err := Decode(tc.data); err == nil {
				t.Error("Decode accepted a malformed file")
			}
		})
	}
}

// TestDecodeNetworkRejectsClaimedLists checks what a packet's NBT can be
// pushed to: a nesting of lists whose every level claims far more entries
// than follow it. The pad behind the chain puts every claim inside the
// truncation check, so what stops the tree is the depth limit alone. A
// reader that presized its slice from the count would pay for each claim
// in full, sixteen bytes an element, level after level, so the decode is
// measured against a bound that presizing cannot meet.
// vanilla: ListTag.load charges the accounter before sizing its list.
func TestDecodeNetworkRejectsClaimedLists(t *testing.T) {
	const claim = 4_000_000
	var data []byte
	data = append(data, byte(TypeList))
	for range maxDepth + 2 {
		data = append(data, byte(TypeList))
		data = binary.BigEndian.AppendUint32(data, claim)
	}
	// The pad backs every level's claim; nothing of it is ever read.
	data = append(data, make([]byte, claim+4096)...)

	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	if _, _, err := DecodeNetwork(data); err == nil {
		t.Error("DecodeNetwork accepted a tree nested past the limit")
	}
	runtime.ReadMemStats(&after)
	// Presizing each of the maxDepth+2 lists would cost claim*16 bytes
	// apiece, tens of gigabytes; refusing the tree costs a few small ones.
	if grew := after.TotalAlloc - before.TotalAlloc; grew > 1<<20 {
		t.Errorf("DecodeNetwork allocated %d bytes over the decode, want the list not presized from its count", grew)
	}
}

// TestEncodeRejects checks a tree that cannot be written as NBT is reported
// rather than producing a file no reader can make sense of.
func TestEncodeRejects(t *testing.T) {
	tests := []struct {
		name string
		tag  Tag
	}{
		{"nil root", nil},
		{"nil field", Compound{"missing": nil}},
		{"nil in a list", List{Elem: TypeInt, Items: []Tag{Int(1), nil}}},
		{"string too long", String(strings.Repeat("x", math.MaxUint16+1))},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := Encode("", tc.tag); err == nil {
				t.Error("Encode accepted a tree it cannot write")
			}
		})
	}
}

// TestHeterogeneousList checks a list whose entries are not all the same
// tag saves the way the game saves one, wrapped as compounds with a single
// empty key, and reads back the tree that went in.
// vanilla: NbtList.write, NbtList.read
func TestHeterogeneousList(t *testing.T) {
	list := List{Elem: TypeInt, Items: []Tag{Int(1), String("two")}}
	data, err := Encode("", list)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	_, tag, err := Decode(data)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	want := List{Elem: TypeCompound, Items: []Tag{Int(1), String("two")}}
	if !reflect.DeepEqual(tag, want) {
		t.Fatalf("Decode = %v, want %v", tag, want)
	}
	again, err := Encode("", tag)
	if err != nil {
		t.Fatalf("re-Encode: %v", err)
	}
	if !slices.Equal(data, again) {
		t.Errorf("re-Encode wrote %x, want the bytes it read", again)
	}
}

// TestLongValueFloorsDoubles checks the one narrowing where a double differs
// from a float: DoubleTag.longValue floors, FloatTag.longValue truncates
// toward zero.
func TestLongValueFloorsDoubles(t *testing.T) {
	if got, ok := LongValue(Double(-1.5)); !ok || got != -2 {
		t.Errorf("LongValue(-1.5d) = %d, %v; want -2, true", got, ok)
	}
	if got, ok := LongValue(Float(-1.5)); !ok || got != -1 {
		t.Errorf("LongValue(-1.5f) = %d, %v; want -1, true", got, ok)
	}
}

// TestGet checks that a field of the wrong type reads as absent.
func TestGet(t *testing.T) {
	c := Compound{"health": Float(20), "name": String("Steve")}

	if health, ok := Get[Float](c, "health"); !ok || health != 20 {
		t.Errorf("Get[Float](health) = %v, %v; want 20, true", health, ok)
	}
	if _, ok := Get[Int](c, "health"); ok {
		t.Error("Get[Int](health) found a float")
	}
	if _, ok := Get[String](c, "absent"); ok {
		t.Error("Get[String](absent) found something")
	}
}

func TestAppendEncodeMatchesEncode(t *testing.T) {
	tag := Compound{"a": Int(1), "b": List{Elem: TypeString, Items: []Tag{String("x"), String("y")}}, "c": LongArray{1, 2, 3}}
	want, err := Encode("n", tag)
	if err != nil {
		t.Fatal(err)
	}
	prefix := []byte{9, 9}
	got, err := AppendEncode(prefix, "n", tag)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got[:2], prefix) || !bytes.Equal(got[2:], want) {
		t.Fatalf("AppendEncode = %x, want prefix then %x", got, want)
	}

	bad, err := AppendEncode(prefix, "", Compound{"x": nil})
	if err == nil || len(bad) != 2 {
		t.Fatalf("AppendEncode of a bad tag = %x, %v; want dst unchanged and an error", bad, err)
	}
}
