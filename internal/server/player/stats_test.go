package player

import (
	"math"
	"testing"

	"github.com/golem-mc/golem/registry"
)

// TestStatsSaturateAndRoundTrip awards past the int32 ceiling and reads
// the encoded file back.
func TestStatsSaturateAndRoundTrip(t *testing.T) {
	s := NewStats()
	jump := Custom(registry.CustomStatJump)
	s.Award(jump, math.MaxInt32-1)
	s.Award(jump, 5)
	if got := s.Value(jump); got != math.MaxInt32 {
		t.Fatalf("jump = %d, want it held at %d", got, int32(math.MaxInt32))
	}
	data, err := s.Encode(4903)
	if err != nil {
		t.Fatal(err)
	}
	back, err := ParseStats(data)
	if err != nil {
		t.Fatal(err)
	}
	if got := back.Value(jump); got != math.MaxInt32 {
		t.Errorf("read back jump = %d, want %d", got, int32(math.MaxInt32))
	}
}

// TestParseStatsRenamesOnlyOldPlayTime: play_one_minute is play_time in a
// file from before 2710 and an unknown key after.
func TestParseStatsRenamesOnlyOldPlayTime(t *testing.T) {
	old, err := ParseStats([]byte(`{"stats":{"minecraft:custom":{"minecraft:play_one_minute":40}}}`))
	if err != nil {
		t.Fatal(err)
	}
	if got := old.Value(Custom(registry.CustomStatPlayTime)); got != 40 {
		t.Errorf("unversioned play_one_minute read as play_time %d, want 40", got)
	}
	recent, err := ParseStats([]byte(`{"stats":{"minecraft:custom":{"minecraft:play_one_minute":40,"minecraft:jump":"x"}},"DataVersion":4903}`))
	if err != nil {
		t.Fatal(err)
	}
	if n := len(recent.TakeDirty()) + len(recent.values); n != 0 {
		t.Errorf("a recent file's play_one_minute or non-number kept %d counters, want none", n)
	}
}

func TestParseStatsRenamesOldKeys(t *testing.T) {
	const old = `{"stats":{"minecraft:mined":{"minecraft:grass":5,"minecraft:chain":2,"minecraft:stone":1},` +
		`"minecraft:used":{"minecraft:scute":4,"minecraft:grass_path":1},"minecraft:killed":{"minecraft:zombie_pigman":3}},"DataVersion":2230}`
	want := map[[2]string]int32{
		{"minecraft:mined", "minecraft:short_grass"}:       5,
		{"minecraft:mined", "minecraft:iron_chain"}:        2,
		{"minecraft:mined", "minecraft:stone"}:             1,
		{"minecraft:used", "minecraft:turtle_scute"}:       4,
		{"minecraft:used", "minecraft:dirt_path"}:          1,
		{"minecraft:killed", "minecraft:zombified_piglin"}: 3,
	}
	s, err := ParseStats([]byte(old))
	if err != nil {
		t.Fatal(err)
	}
	for k, n := range want {
		stat, ok := StatNamed(k[0], k[1])
		if !ok || s.Value(stat) != n {
			t.Errorf("%v = %d (known %v), want %d", k, s.Value(stat), ok, n)
		}
	}
	cur, err := ParseStats([]byte(`{"stats":{"minecraft:mined":{"minecraft:iron_chain":2,"minecraft:chain":9}},"DataVersion":4600}`))
	if err != nil {
		t.Fatal(err)
	}
	stat, _ := StatNamed("minecraft:mined", "minecraft:iron_chain")
	if got := cur.Value(stat); got != 2 {
		t.Errorf("4600 iron_chain = %d, want 2", got)
	}
}
