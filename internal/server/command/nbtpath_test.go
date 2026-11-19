package command

import (
	"testing"

	"github.com/golem-mc/golem/nbt"
)

// The NBT path argument's own facts: what a path's text reads as, and what
// the four operations through one do to a tag.

// TestNBTPathParse reads the path forms the vanilla grammar takes: the
// plain keys with their dots, the quoted ones, the selectors, the
// matchers and the sevens that refuse.
// vanilla: NbtPathArgument.parse
func TestNBTPathParse(t *testing.T) {
	t.Parallel()
	type nodeShape struct {
		kind  string
		name  string
		index int
	}
	nodeOf := func(path nbtPath, i int) nodeShape {
		switch n := path.nodes[i].(type) {
		case compoundChildNode:
			return nodeShape{kind: "child", name: n.name}
		case indexedElementNode:
			return nodeShape{kind: "index", index: n.index}
		case allElementsNode:
			return nodeShape{kind: "all"}
		case matchObjectNode:
			return nodeShape{kind: "matcher"}
		case matchElementNode:
			return nodeShape{kind: "element-matcher"}
		case matchRootObjectNode:
			return nodeShape{kind: "root-matcher"}
		}
		return nodeShape{kind: "unknown"}
	}
	for _, testCase := range []struct {
		text  string
		nodes []nodeShape
	}{
		{text: "foo", nodes: []nodeShape{{kind: "child", name: "foo"}}},
		{text: "foo.bar", nodes: []nodeShape{
			{kind: "child", name: "foo"}, {kind: "child", name: "bar"}}},
		{text: `f."oo ba".r`, nodes: []nodeShape{
			// A quoted key may carry the spaces the tokenizer splits on;
			// its element ends where its quote does.
			{kind: "child", name: "f"}, {kind: "child", name: "oo ba"}, {kind: "child", name: "r"}}},
		{text: "foo[]", nodes: []nodeShape{{kind: "child", name: "foo"}, {kind: "all"}}},
		{text: "foo[0]", nodes: []nodeShape{{kind: "child", name: "foo"}, {kind: "index"}}},
		{text: "foo[0].bar", nodes: []nodeShape{
			{kind: "child", name: "foo"}, {kind: "index"}, {kind: "child", name: "bar"}}},
		{text: "foo[0]", nodes: []nodeShape{{kind: "child", name: "foo"}, {kind: "index", index: 0}}},
		{text: "foo[-1]", nodes: []nodeShape{{kind: "child", name: "foo"}, {kind: "index", index: -1}}},
		{text: `[0].foo`, nodes: []nodeShape{{kind: "index"}, {kind: "child", name: "foo"}}},
		{text: `{foo:"bar"}.item`, nodes: []nodeShape{{kind: "root-matcher"}, {kind: "child", name: "item"}}},
		{text: "items[{id:\"a\"}]", nodes: []nodeShape{
			{kind: "child", name: "items"}, {kind: "element-matcher"}}},
		{text: "item{id:\"a\"}", nodes: []nodeShape{{kind: "matcher"}}},
	} {
		path, err := parseNBTPath(testCase.text)
		if err != nil {
			t.Errorf("parse %q: %v", testCase.text, err)
			continue
		}
		if len(path.nodes) != len(testCase.nodes) {
			t.Errorf("parse %q: %d nodes, want %d", testCase.text, len(path.nodes), len(testCase.nodes))
			continue
		}
		for i, want := range testCase.nodes {
			if got := nodeOf(path, i); got != want {
				t.Errorf("parse %q node %d = %+v, want %+v", testCase.text, i, got, want)
			}
		}
	}

	// The refusals: what the vanilla grammar will not take.
	texts := []string{"", "foo[", "foo]", ".", "foo..bar", "foo[bar]"}
	for _, text := range texts {
		if _, err := parseNBTPath(text); err == nil {
			t.Errorf("parse %q: accepted, want a refusal", text)
		}
	}

	// The not-found refusal spells the path up to the failing element.
	root := nbt.Compound{"m": nbt.Compound{"q": nbt.Int(9)}}
	missing, err := parseNBTPath("m.q.x")
	if err != nil {
		t.Fatal(err)
	}
	_, refusal := missing.get(root)
	if refusal == nil || refusal.key != pathNothingFoundKey ||
		len(refusal.with) != 1 || refusal.with[0] != "m.q.x" {
		t.Errorf("get of a missing path refused with %+v, want nothing found of %q", refusal, "m.q.x")
	}
	_, refusal = parseRefusingHelper(t, "m[].x")
	if refusal == nil || refusal.with[0] != "m[]" {
		// The prefix ends where the failing element did, not at the
		// whole path.
		t.Errorf("get over a compound refused with %+v, want nothing found of %q", refusal, "m[]")
	}
}

// parseRefusingHelper reads a path only to ask it over a root that holds
// a compound, so its all-elements finds nothing.
func parseRefusingHelper(t *testing.T, text string) ([]nbt.Tag, *pathError) {
	t.Helper()
	path, err := parseNBTPath(text)
	if err != nil {
		t.Fatal(err)
	}
	return path.get(nbt.Compound{"m": nbt.Int(1)})
}

// TestNBTPathOperations walks the four operations a /data command takes
// through a path over one compound.
// vanilla: NbtPathArgument$NbtPath
func TestNBTPathOperations(t *testing.T) {
	t.Parallel()
	root := nbt.Compound{"m": nbt.Compound{"q": nbt.Int(9)}}
	deep, err := parseNBTPath("m.q")
	if err != nil {
		t.Fatal(err)
	}

	// The read of what is there and of what is not.
	if tags, refusal := deep.get(root); refusal != nil || len(tags) != 1 || tags[0] != nbt.Int(9) {
		t.Errorf("get = (%#v, %+v), want the one int", tags, refusal)
	}
	if n := deep.countMatching(root); n != 1 {
		t.Errorf("countMatching = %d, want 1", n)
	}
	gone, _ := parseNBTPath("gone.deep")
	if n := gone.countMatching(root); n != 0 {
		t.Errorf("countMatching of a gone path = %d, want 0", n)
	}

	// The set: through one that is there, one that lands where nothing
	// is, and the parents a deeper one creates.
	changed, refusal := deep.set(root, nbt.Int(3))
	if refusal != nil || changed != 1 {
		t.Fatalf("set: (%d, %+v)", changed, refusal)
	}
	m := root["m"].(nbt.Compound)
	if m["q"] != nbt.Int(3) {
		t.Fatalf("set left %#v", root)
	}
	changed, refusal = deep.set(root, nbt.Int(3))
	if refusal != nil || changed != 0 {
		t.Fatalf("re-set: (%d, %+v), want no change", changed, refusal)
	}
	fresh, err := parseNBTPath("a.b.c")
	if err != nil {
		t.Fatal(err)
	}
	if changed, refusal := fresh.set(root, nbt.String("x")); refusal != nil || changed != 1 {
		t.Fatalf("set into the missing: (%d, %+v)", changed, refusal)
	}
	abc, ok := root["a"].(nbt.CompoundTag).Entries()["b"].(nbt.CompoundTag)
	if !ok || abc.Entries()["c"] != nbt.String("x") {
		t.Errorf("set into the missing left %#v", root)
	}

	// The set through an index and through the all-elements pair.
	list := nbt.Compound{"list": nbt.List{Elem: nbt.TypeInt, Items: []nbt.Tag{nbt.Int(7), nbt.Int(8)}}}
	byIndex, _ := parseNBTPath("list[-1]")
	if changed, refusal := byIndex.set(list, nbt.Int(1)); refusal != nil || changed != 1 {
		t.Errorf("index set: (%d, %+v)", changed, refusal)
	}
	items := list["list"].(nbt.List)
	if items.Items[1] != nbt.Int(1) || items.Items[0] != nbt.Int(7) {
		t.Errorf("index set left %#v", items)
	}
	all, _ := parseNBTPath("list[]")
	if changed, refusal := all.set(list, nbt.Int(5)); refusal != nil || changed != 2 {
		t.Errorf("all set: (%d, %+v)", changed, refusal)
	}
	if items := list["list"].(nbt.List); items.Items[0] != nbt.Int(5) || items.Items[1] != nbt.Int(5) || items.Elem != nbt.TypeInt {
		t.Errorf("all set left %#v", items)
	}

	// The insert: the append, the prepend and one in the middle; a list
	// the path grows where nothing was; a list that is no list.
	single, _ := parseNBTPath("list")
	if changed, refusal := single.insert(1, list, []nbt.Tag{nbt.Int(6)}); refusal != nil || changed != 1 {
		t.Errorf("insert: (%d, %+v)", changed, refusal)
	}
	if got := itemsOf(list["list"]); len(got) != 3 || got[0] != nbt.Int(5) || got[1] != nbt.Int(6) || got[2] != nbt.Int(5) {
		t.Errorf("insert left %v", got)
	}
	// An insert into a place that holds no list refuses with the list's
	// own refusal; into a place nothing holds it grows one.
	notAList := nbt.Compound{"m": nbt.Compound{"q": nbt.Int(9)}}
	compoundPath, _ := parseNBTPath("m.q")
	if _, refusal := compoundPath.insert(0, notAList, []nbt.Tag{nbt.Int(1)}); refusal == nil || refusal.key != dataExpectedListKey {
		t.Errorf("insert into a compound refused with %+v, want the list's refusal", refusal)
	}
	if changed, refusal := compoundPath.insert(0, nbt.Compound{}, []nbt.Tag{nbt.Int(1)}); refusal != nil || changed != 1 {
		t.Errorf("insert into the missing: (%d, %+v)", changed, refusal)
	}
	beyond, _ := parseNBTPath("list")
	if _, refusal := beyond.insert(9, list, []nbt.Tag{nbt.Int(1)}); refusal == nil || refusal.key != dataInvalidIndexKey || refusal.with[0] != "9" {
		t.Errorf("insert past the end refused with %+v, want the index's", refusal)
	}
	empty, _ := parseNBTPath("grown")
	freshRoot := nbt.Compound{}
	if changed, refusal := empty.insert(-1, freshRoot, []nbt.Tag{nbt.Int(1), nbt.Int(2)}); refusal != nil || changed != 1 {
		t.Errorf("insert into the missing: (%d, %+v)", changed, refusal)
	}
	if got := itemsOf(freshRoot["grown"]); len(got) != 2 || got[0] != nbt.Int(1) || got[1] != nbt.Int(2) {
		t.Errorf("insert into the missing left %v", got)
	}

	// The removal: what is there, what is not, and one of a list.
	if n := deep.remove(root); n != 1 {
		t.Errorf("remove = %d, want 1", n)
	}
	if n := deep.remove(root); n != 0 {
		t.Errorf("re-remove = %d, want 0", n)
	}
	if n := gone.remove(root); n != 0 {
		t.Errorf("remove of a gone path = %d, want 0", n)
	}
	one, _ := parseNBTPath("list[0]")
	if n := one.remove(list); n != 1 {
		t.Errorf("remove of an element = %d, want 1", n)
	}
	if got := itemsOf(list["list"]); len(got) != 2 || got[0] != nbt.Int(6) {
		t.Errorf("element removal left %v", got)
	}

	// The matchers: the entries a pattern names, and the sets through
	// them.
	patternRoot := nbt.Compound{"items": nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{
		nbt.Compound{"id": nbt.String("a"), "v": nbt.Int(1)},
		nbt.Compound{"id": nbt.String("b"), "v": nbt.Int(2)},
	}}}
	matched, err := parseNBTPath(`items[{id:"b"}].v`)
	if err != nil {
		t.Fatal(err)
	}
	tags, refusal := matched.get(patternRoot)
	if refusal != nil || len(tags) != 1 || tags[0] != nbt.Int(2) {
		t.Fatalf("matcher get = (%#v, %+v)", tags, refusal)
	}
	if changed, refusal := matched.set(patternRoot, nbt.Int(9)); refusal != nil || changed != 1 {
		t.Errorf("matcher set: (%d, %+v)", changed, refusal)
	}
	second := patternRoot["items"].(nbt.List).Items[1].(nbt.Compound)
	if second["v"] != nbt.Int(9) || len(second) != 2 {
		t.Errorf("matcher set left %#v", patternRoot)
	}
	named, err := parseNBTPath(`item{id:"b"}`)
	if err != nil {
		t.Fatal(err)
	}
	if tags, refusal := named.get(nbt.Compound{"item": nbt.Compound{"id": nbt.String("b"), "v": nbt.Int(2)}}); refusal != nil || len(tags) != 1 {
		t.Errorf("name matcher get = (%#v, %+v)", tags, refusal)
	}
}

// itemsOf reads a list's own items.
func itemsOf(tag nbt.Tag) []nbt.Tag {
	if list, ok := tag.(nbt.List); ok {
		return list.Items
	}
	return nil
}

// TestNBTPathArrayOperations checks the four operations over the typed
// arrays, which the game counts among its collections.
func TestNBTPathArrayOperations(t *testing.T) {
	t.Parallel()
	// An insert grows the array at the index given.
	root := nbt.Compound{"arr": nbt.IntArray{1, 2}}
	arr, _ := parseNBTPath("arr")
	if changed, refusal := arr.insert(1, root, []nbt.Tag{nbt.Int(5)}); refusal != nil || changed != 1 {
		t.Fatalf("array insert: (%d, %+v)", changed, refusal)
	}
	if got := root["arr"].(nbt.IntArray); len(got) != 3 || got[0] != 1 || got[1] != 5 || got[2] != 2 {
		t.Errorf("array insert left %v", got)
	}
	// An element set narrows the value; one no number reads changes nothing.
	first, _ := parseNBTPath("arr[0]")
	if changed, refusal := first.set(root, nbt.Int(9)); refusal != nil || changed != 1 {
		t.Errorf("array element set: (%d, %+v)", changed, refusal)
	}
	if got := root["arr"].(nbt.IntArray); got[0] != 9 {
		t.Errorf("array element set left %v", got)
	}
	if changed, _ := first.set(root, nbt.String("s")); changed != 0 {
		t.Errorf("string set into an int array changed %d, want 0", changed)
	}
	if got := root["arr"].(nbt.IntArray); got[0] != 9 {
		t.Errorf("string set left %v", got)
	}
	// A removal shrinks the array.
	if n := first.remove(root); n != 1 {
		t.Errorf("array element remove = %d, want 1", n)
	}
	if got := root["arr"].(nbt.IntArray); len(got) != 2 || got[0] != 5 {
		t.Errorf("array element remove left %v", got)
	}
	// The all-elements pair sets every element and clears them all.
	all, _ := parseNBTPath("arr[]")
	if changed, refusal := all.set(root, nbt.Int(7)); refusal != nil || changed != 2 {
		t.Errorf("array all set: (%d, %+v)", changed, refusal)
	}
	if got := root["arr"].(nbt.IntArray); got[0] != 7 || got[1] != 7 {
		t.Errorf("array all set left %v", got)
	}
	if n := all.remove(root); n != 2 {
		t.Errorf("array all remove = %d, want 2", n)
	}
	if got := root["arr"].(nbt.IntArray); len(got) != 0 {
		t.Errorf("array all remove left %v", got)
	}
}

func TestNBTPathMatchersCreateAndCompare(t *testing.T) {
	t.Parallel()
	item := func(id string, v int32) nbt.Tag {
		return nbt.Compound{"id": nbt.String(id), "v": nbt.Int(v)}
	}
	run := func(root nbt.Compound, path string, value nbt.Tag) (int, *pathError) {
		p, err := parseNBTPath(path)
		if err != nil {
			t.Fatal(err)
		}
		return p.set(root, value)
	}
	root := nbt.Compound{"items": nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{item("a", 1)}}}
	n, ref := run(root, `items[{id:"b"}].v`, nbt.Int(9))
	if n != 1 || ref != nil || nbt.Stringify(root) != `{items:[{id:"a",v:1},{id:"b",v:9}]}` {
		t.Errorf("create: %d %v %s", n, ref, nbt.Stringify(root))
	}
	root = nbt.Compound{"a": nbt.Compound{"b": nbt.Int(2)}}
	_, ref = run(root, "a{b:1}.c", nbt.Int(5))
	if ref == nil || ref.key != pathNothingFoundKey || nbt.Stringify(root) != "{a:{b:2}}" {
		t.Errorf("mismatch: %v %s", ref, nbt.Stringify(root))
	}
	same := nbt.Compound{"b": nbt.Int(1)}
	root = nbt.Compound{"a": nbt.Compound{"b": nbt.Int(1)}}
	if n, ref = run(root, "a{b:1}", same); n != 0 || ref != nil {
		t.Errorf("object equal: %d %v", n, ref)
	}
	root = nbt.Compound{"l": nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{nbt.Compound{"b": nbt.Int(1)}}}}
	if n, ref = run(root, "l[{b:1}]", same); n != 0 || ref != nil {
		t.Errorf("element equal: %d %v", n, ref)
	}
}

// vanilla: NbtPathArgument.NbtPath.set, AllElementsNode.setTag
func TestNBTPathSetCopiesEachValue(t *testing.T) {
	t.Parallel()
	for _, path := range []string{"rows[]", "rows[].child", "rows[{a:1}].child"} {
		root := nbt.Compound{"rows": nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{
			nbt.Compound{"a": nbt.Int(1)}, nbt.Compound{"a": nbt.Int(1)},
		}}}
		source := nbt.Compound{"nested": nbt.Compound{"v": nbt.Int(1)}}
		p, err := parseNBTPath(path)
		if err != nil {
			t.Fatal(err)
		}
		if changed, refusal := p.set(root, source); changed != 2 || refusal != nil {
			t.Fatalf("%s: set = %d, %v", path, changed, refusal)
		}
		values, refusal := p.get(root)
		if refusal != nil || len(values) != 2 {
			t.Fatalf("%s: get = %v, %v", path, values, refusal)
		}
		first := values[0].(nbt.CompoundTag).Entries()["nested"].(nbt.CompoundTag)
		first.Set("v", nbt.Int(2))
		second := values[1].(nbt.CompoundTag).Entries()["nested"].(nbt.CompoundTag)
		if second.Entries()["v"] != nbt.Int(1) || source["nested"].(nbt.Compound)["v"] != nbt.Int(1) {
			t.Fatalf("%s: set shares mutable state with its source or another destination", path)
		}
	}
}
