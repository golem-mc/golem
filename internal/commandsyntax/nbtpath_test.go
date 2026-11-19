package commandsyntax

import (
	"reflect"
	"testing"

	"github.com/golem-mc/golem/nbt"
)

func TestParseNBTPathNodes(t *testing.T) {
	text := `{root:1}.items[{id:"a"}]."quoted\u0020key"{v:2}[-1][]`
	path, err := ParseNBTPath(text)
	if err != nil {
		t.Fatal(err)
	}
	compound := func(key string, value nbt.Tag) nbt.CompoundTag {
		c := &nbt.OrderedCompound{}
		c.Set(key, value)
		return c
	}
	want := []PathNode{MatchRootObjectNode{compound("root", nbt.Int(1))}, CompoundChildNode{"items"}, MatchElementNode{compound("id", nbt.String("a"))}, MatchObjectNode{"quoted key", compound("v", nbt.Int(2))}, IndexedElementNode{-1}, AllElementsNode{}}
	if path.Original != text || !reflect.DeepEqual(path.Nodes, want) || len(path.Ends) != len(want) {
		t.Fatalf("path=%+v", path)
	}
	for i, end := range path.Ends {
		prefix, err := ParseNBTPath(text[:end])
		if err != nil || len(prefix.Nodes) != i+1 {
			t.Fatalf("node %d end %d: %+v, %+v", i, end, prefix, err)
		}
	}
	if path.Ends[len(path.Ends)-1] != len(text) {
		t.Fatal("last node did not consume the path")
	}
}

func TestParseNBTPathErrors(t *testing.T) {
	for _, tc := range []struct {
		text, key, prose string
		cursor           int
	}{
		{"", pathNodeInvalidKey, "", 0}, {"a[", pathNodeInvalidKey, "", 2},
		{"a[1", "", "expected ]", 3}, {"a b", "", "expected .", 1},
		{`"unterminated`, pathNodeInvalidKey, "", 0},
		{`a{v:"\q"}`, pathNodeInvalidKey, "", 7},
	} {
		_, err := ParseNBTPath(tc.text)
		if err == nil || err.Key != tc.key || err.Text != tc.prose || err.Cursor != tc.cursor {
			t.Errorf("%q: %+v", tc.text, err)
		}
	}
}
