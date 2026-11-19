package protocol

import (
	"encoding/hex"
	"testing"
)

// The minecraft:lodestone_tracker component: how one stack's compass target
// rides the slot the raw bytes carry it through, and what its accessor says.

// TestLodestoneTrackerRidesTheSlot checks a stack with the component
// through the untrusted shape it arrives in and both shapes it goes back
// out as: the value read as the raw bytes of the form its codec takes, and
// the tracker the accessor answers from them the one they say.
func TestLodestoneTrackerRidesTheSlot(t *testing.T) {
	// A compass pointing at the overworld's (2, 63, 1), tracked: count 1,
	// item 1151 (minecraft:compass), the tracker added — component 69 — as
	// its type, the byte length of its value, and the value: the target's
	// present flag, the dimension's name, the position long, the tracked
	// flag.
	value := "01" + // the target is present
		"13" + "6d696e6563726166743a6f766572776f726c64" + // "minecraft:overworld"
		"000000800000103f" + // (2, 63, 1) as the position long
		"01" // tracked
	data, err := hex.DecodeString("01ff080100451e" + value)
	if err != nil {
		t.Fatal(err)
	}
	want := ItemStack{ItemID: 1151, Count: 1, Raw: []RawComponent{{
		Type: DataComponentLodestoneTracker,
		Data: mustHex(t, value),
	}}}
	got, ok := NewReader(data).ReadUntrustedSlot()
	if !ok || !got.Equal(want) {
		t.Errorf("ReadUntrustedSlot = %+v (%v), want %+v", got, ok, want)
	}
	checkTracker(t, got)

	// And the stack rides back out both ways, the component's bytes where
	// they were: bare in the trusted shape the server sends, behind their
	// length in the untrusted one it received.
	var b Buffer
	b.PutSlot(got)
	if wantTrusted := "01ff08010045" + value; hex.EncodeToString(b.Bytes()) != wantTrusted {
		t.Errorf("PutSlot = %s, want %s", hex.EncodeToString(b.Bytes()), wantTrusted)
	}
	b.Reset()
	b.PutUntrustedSlot(got)
	if hex.EncodeToString(b.Bytes()) != hex.EncodeToString(data) {
		t.Errorf("PutUntrustedSlot = %s, want the bytes it arrived as %s",
			hex.EncodeToString(b.Bytes()), hex.EncodeToString(data))
	}
}

// TestLodestoneTrackerSurvivesTheAccessor checks the stack's own setter and
// reader: what a compass used on a lodestone carries comes back out of the
// stack as it went in, and a stack carrying nothing reports no tracker at
// all. A tracker without a target travels as the absent flag and the tracked
// one alone.
func TestLodestoneTrackerSurvivesTheAccessor(t *testing.T) {
	stack := ItemStack{ItemID: 1151, Count: 2}
	if _, ok := stack.LodestoneTrackerOf(); ok {
		t.Error("a plain compass already carries a tracker")
	}
	want := LodestoneTracker{
		Target:  &LodestoneTarget{Dimension: "minecraft:the_nether", X: -5, Y: 100, Z: 42},
		Tracked: true,
	}
	stack.SetLodestoneTracker(want)
	got, ok := stack.LodestoneTrackerOf()
	if !ok {
		t.Fatal("the stack carries the component but answers none")
	}
	if got.Target == nil || got.Target.Dimension != want.Target.Dimension ||
		got.Target.X != want.Target.X || got.Target.Y != want.Target.Y ||
		got.Target.Z != want.Target.Z || got.Tracked != want.Tracked {
		t.Errorf("tracker = %+v, want %+v", got, want)
	}

	empty := ItemStack{ItemID: 1151, Count: 1}
	empty.SetLodestoneTracker(LodestoneTracker{Tracked: true})
	if len(empty.Raw) != 1 || empty.Raw[0].Type != DataComponentLodestoneTracker ||
		hex.EncodeToString(empty.Raw[0].Data) != "0001" {
		t.Errorf("the targetless tracker = %+v, want the absent flag and the tracked one", empty.Raw)
	}
	back, ok := empty.LodestoneTrackerOf()
	if !ok || back.Target != nil || !back.Tracked {
		t.Errorf("tracker = %+v, %v; want no target, tracked", back, ok)
	}
}

// TestUntrustedSlotRefusesATrackerWithNoIdentifier checks the codec's
// refusal: a dimension the game's own parser would not read — here by its
// uppercase — refuses the stack rather than relaying it.
func TestUntrustedSlotRefusesATrackerWithNoIdentifier(t *testing.T) {
	data, err := hex.DecodeString("01ff0801004515010a5468655f4e6574686572000000800000103f01")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := NewReader(data).ReadUntrustedSlot(); ok {
		t.Error("ReadUntrustedSlot took a tracker whose dimension is no identifier")
	}
}

// checkTracker holds a stack's tracker against the compass a lock at the
// overworld's (2, 63, 1) leaves.
func checkTracker(t *testing.T, stack ItemStack) {
	t.Helper()
	tracker, ok := stack.LodestoneTrackerOf()
	if !ok {
		t.Fatal("the stack carries the component but answers none")
	}
	if tracker.Target == nil {
		t.Fatal("the tracker points nowhere, want the lodestone's block")
	}
	if tracker.Target.Dimension != "minecraft:overworld" ||
		tracker.Target.X != 2 || tracker.Target.Y != 63 || tracker.Target.Z != 1 {
		t.Errorf("tracker = %+v, want the overworld's (2, 63, 1)", tracker)
	}
	if !tracker.Tracked {
		t.Error("tracker = untracked, want the game keeping the point")
	}
}

// mustHex decodes the test's own bytes, the one place a hex string is not
// checked before it is used.
func mustHex(t *testing.T, s string) []byte {
	t.Helper()
	data, err := hex.DecodeString(s)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
