package nbt

import (
	"slices"
	"strings"
	"testing"
)

func TestOrderedCompoundHashOrder(t *testing.T) {
	for _, tc := range []struct{ keys, want string }{
		{"NoGravity Brain Invulnerable FallFlying ShowArms PortalCooldown AbsorptionAmount DisabledSlots DeathTime Pose Invisible UUID Tags Motion Small Health fall_distance Air OnGround Rotation current_impulse_context_reset_grace_time Pos Fire NoBasePlate attributes HurtTime", "NoGravity Brain Invulnerable FallFlying ShowArms PortalCooldown AbsorptionAmount DisabledSlots DeathTime Pose Invisible UUID Tags Motion Small Health fall_distance Air OnGround Rotation current_impulse_context_reset_grace_time Pos Fire NoBasePlate attributes HurtTime"},
		{"big s t v f w em k l m bo n", "big s t v f w em k l m bo n"},
		{"big s t v f w em k l m bo n a", "big a f em k l m bo n s t v w"},
		{"BB Aa NoGravity", "BB Aa NoGravity"},
	} {
		for _, c := range []*OrderedCompound{{}, NewOrderedCompound(64)} {
			for key := range strings.FieldsSeq(tc.keys) {
				c.Set(key, Int(1))
			}
			for _, tag := range []CompoundTag{c, Copy(c).(CompoundTag)} {
				if got := HashOrderedKeys(tag); !slices.Equal(got, strings.Fields(tc.want)) {
					t.Errorf("%s: keys = %v, want %s", tc.keys, got, tc.want)
				}
			}
		}
	}
}

func TestOrderedCompoundMutations(t *testing.T) {
	c := &OrderedCompound{}
	for key := range strings.FieldsSeq("v f m bo") {
		c.Set(key, Int(1))
	}
	c.Set("v", Int(2))
	if got := HashOrderedKeys(c); !slices.Equal(got, []string{"v", "f", "m", "bo"}) {
		t.Fatalf("replacement moved a key: %v", got)
	}
	c.Delete("v")
	c.Set("v", Int(3))
	if got := HashOrderedKeys(c); !slices.Equal(got, []string{"f", "v", "m", "bo"}) {
		t.Fatalf("reinserted key did not move: %v", got)
	}
	for key := range strings.FieldsSeq("big s t w em k l n a") {
		c.Set(key, Int(1))
	}
	c.Delete("a")
	// A HashMap does not shrink on removal; copying it builds a fresh table.
	if got := HashOrderedKeys(c); !slices.Equal(got, strings.Fields("big f em k l m bo n s t v w")) {
		t.Fatalf("removal shrank the table: %v", got)
	}
	if got := HashOrderedKeys(Copy(c).(CompoundTag)); !slices.Equal(got, strings.Fields("big s t f v w em k l m bo n")) {
		t.Fatalf("copy did not rebuild the table: %v", got)
	}
}

func TestSnapshotPreservesCompoundHistory(t *testing.T) {
	c := &OrderedCompound{}
	for key := range strings.FieldsSeq("big s t v f w em k l m bo n a") {
		c.Set(key, Int(1))
	}
	c.Delete("a")
	for _, wrapped := range []Tag{c, Compound{"nested": c}, List{Elem: TypeCompound, Items: []Tag{c}}} {
		snapshot := Snapshot(wrapped)
		var got *OrderedCompound
		switch v := snapshot.(type) {
		case *OrderedCompound:
			got = v
		case Compound:
			got = v["nested"].(*OrderedCompound)
		case List:
			got = v.Items[0].(*OrderedCompound)
		}
		if got.capacity != c.capacity || !slices.Equal(got.keys, c.keys) {
			t.Fatalf("snapshot lost table history: %+v, want %+v", got, c)
		}
		if keys := HashOrderedKeys(got); !slices.Equal(keys, strings.Fields("big f em k l m bo n s t v w")) {
			t.Fatalf("snapshot reordered keys: %v", keys)
		}
		got.Delete("big")
		got.Set("s", Int(2))
		got.Set("a", Int(3))
		if c.values["big"] != Int(1) || c.values["s"] != Int(1) || slices.Contains(c.keys, "a") || c.keys[0] != "big" {
			t.Fatal("snapshot shares mutable compound state")
		}
	}
	for _, key := range slices.Clone(c.keys) {
		c.Delete(key)
	}
	for _, empty := range []*OrderedCompound{{}, c} {
		got := Snapshot(empty).(*OrderedCompound)
		if got.capacity != empty.capacity {
			t.Fatal("empty snapshot lost capacity")
		}
		got.Set("v", Int(2))
		got.Set("f", Int(2))
		want := []string{"v", "f"}
		if empty.capacity > 16 {
			want = []string{"f", "v"}
		}
		if keys := HashOrderedKeys(got); !slices.Equal(keys, want) {
			t.Fatalf("refilled empty snapshot reordered keys: %v, want %v", keys, want)
		}
		if len(empty.Entries()) != 0 {
			t.Fatal("empty snapshot shares entries")
		}
	}
}

func TestOrderedCompoundRoundTrip(t *testing.T) {
	const text = `{v:1,f:2,m:3,bo:4,nested:{BB:1,Aa:2},list:[{v:1,f:2},3],v:5}`
	tag, n, pe := ParseOrderedValueTag(text)
	if pe != nil || n != len(text) {
		t.Fatalf("parse = %v, %d, %v", tag, n, pe)
	}
	c := tag.(*OrderedCompound)
	if c.values["v"] != Int(5) {
		t.Fatalf("duplicate key = %v", c.values["v"])
	}
	encoded, err := Encode("", c)
	if err != nil {
		t.Fatal(err)
	}
	_, decoded, err := DecodeOrdered(encoded)
	if err != nil {
		t.Fatal(err)
	}
	for _, candidate := range []Tag{Copy(c), PlainCopy(c)} {
		if !Equal(c, candidate) || !Equal(candidate, c) || !Matches(c, candidate) || !Matches(candidate, c) {
			t.Fatalf("compound operations disagree: %v", candidate)
		}
		if Stringify(candidate) != Stringify(c) {
			t.Fatalf("sorted SNBT differs: %s", Stringify(candidate))
		}
	}
	if Stringify(decoded) != Stringify(c) {
		t.Fatal("round trip changed SNBT")
	}
	again, err := Encode("", decoded)
	if err != nil || !slices.Equal(encoded, again) {
		t.Fatalf("re-encode = %x, %v; want %x", again, err, encoded)
	}
	for _, name := range []string{"", "nested"} {
		before, after := CompoundTag(c), decoded.(CompoundTag)
		if name != "" {
			before = before.Entries()[name].(CompoundTag)
			after = after.Entries()[name].(CompoundTag)
		}
		if !slices.Equal(HashOrderedKeys(before), HashOrderedKeys(after)) {
			t.Fatalf("%s: round trip reordered keys", name)
		}
	}

	network, err := EncodeNetwork(c)
	if err != nil {
		t.Fatal(err)
	}
	networkTag, consumed, err := DecodeNetworkOrdered(network)
	if err != nil || consumed != len(network) {
		t.Fatalf("network decode: %d bytes, %v", consumed, err)
	}
	roundTrip, err := EncodeNetwork(networkTag)
	if err != nil || !slices.Equal(network, roundTrip) {
		t.Fatalf("network round trip reordered compounds: %v", err)
	}
	plain := PlainCopy(c).(Compound)
	plain["nested"].(Compound)["BB"] = Int(9)
	if c.values["nested"].(*OrderedCompound).values["BB"] != Int(1) {
		t.Fatal("plain copy shares nested data")
	}
}

func TestOrderedCompoundMapRead(t *testing.T) {
	ordered := &OrderedCompound{}
	child := &OrderedCompound{}
	child.Set("Head", List{Elem: TypeFloat, Items: []Tag{Float(1), Float(2), Float(3)}})
	ordered.Set("Pose", child)
	root := Compound{"nested": ordered}
	nested, ok := Get[Compound](root, "nested")
	if !ok {
		t.Fatal("ordered compound is not readable by a map consumer")
	}
	pose, ok := Get[Compound](nested, "Pose")
	if !ok {
		t.Fatal("nested ordered compound is not readable")
	}
	head, ok := Get[List](pose, "Head")
	if !ok || len(head.Items) != 3 {
		t.Fatalf("Head = %v", head)
	}
	if _, ok := nested["Pose"].(*OrderedCompound); !ok {
		t.Fatal("read stripped the nested insertion history")
	}
}

func TestOrderedCopyPromotesMapCompounds(t *testing.T) {
	inner := Compound{"a": Int(1)}
	source := Compound{"list": List{Elem: TypeCompound, Items: []Tag{Compound{"nested": inner}}}}
	copied := OrderedCopy(source).(CompoundTag)
	list := copied.Entries()["list"].(List)
	got := list.Items[0].(CompoundTag).Entries()["nested"].(CompoundTag)
	for key := range strings.FieldsSeq("v f m bo") {
		got.Set(key, Int(2))
	}
	if keys := HashOrderedKeys(got); !slices.Equal(keys, strings.Fields("a v f m bo")) {
		t.Fatalf("copied map lost subsequent insertion history: %v", keys)
	}
	got.Set("a", Int(3))
	if len(inner) != 1 || inner["a"] != Int(1) {
		t.Fatal("ordered copy shares mutable map state")
	}
	for _, plain := range []Tag{Copy(source), Snapshot(source)} {
		if _, ok := plain.(Compound); !ok {
			t.Fatalf("ordinary map copy changed representation: %T", plain)
		}
	}
}

func TestShallowCompoundCopyRetainsHistoryAndOwnsRoot(t *testing.T) {
	source, err := ParseOrderedSNBT(`{BB:1,Aa:2,nested:{BB:3,Aa:4}}`)
	if err != nil {
		t.Fatal(err)
	}
	copied := ShallowCopyCompound(source)
	if !slices.Equal(HashOrderedKeys(copied), HashOrderedKeys(source)) {
		t.Fatal("copy lost root collision history")
	}
	copied.Delete("BB")
	copied.Set("id", String("minecraft:bee"))
	if source.Entries()["BB"] != Int(1) || source.Entries()["id"] != nil {
		t.Fatal("copy mutated source root")
	}
	if copied.Entries()["nested"] != source.Entries()["nested"] {
		t.Fatal("shallow copy duplicated children")
	}
	empty := ShallowCopyCompound(nil)
	empty.Set("id", String("minecraft:bee"))
	if len(empty.Entries()) != 1 {
		t.Fatal("nil compound did not become an editable root")
	}
}
