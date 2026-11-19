package main

import (
	"testing"
	"time"
)

func TestViewTimesCompletion(t *testing.T) {
	complete := &bot{name: "complete", x: 1600, z: -1600}
	for x := int32(90); x <= 110; x++ {
		for z := int32(-110); z <= -90; z++ {
			complete.chunkLog = append(complete.chunkLog, chunkArrival{
				at: time.Duration(len(complete.chunkLog)+1) * time.Second, x: x, z: z,
			})
		}
	}
	partial := &bot{name: "partial", x: complete.x, z: complete.z}
	for _, a := range complete.chunkLog[:285] {
		a.at *= 10 // A slow partial view must not contribute a t100 sample.
		partial.chunkLog = append(partial.chunkLog, a)
	}
	// Repeated chunks and chunks beyond the configured view do not count.
	partial.chunkLog = append(partial.chunkLog, partial.chunkLog...)
	partial.chunkLog = append(partial.chunkLog, chunkArrival{at: time.Hour, x: 111, z: -100})

	e := &env{o: &options{view: 10}}
	empty := &bot{name: "empty"}
	got := e.viewTimes([]*bot{partial, empty, complete})
	checkBotTimes(t, got, []string{"partial", "empty", "complete"}, []*float64{nil, nil, new(float64(441))})
	for key, want := range map[string]any{
		"bots_complete":           1,
		"chunks_total":            726,
		"view_chunks_median":      float64(285),
		"t50_median_s":            float64(221),
		"t90_median_s":            float64(397),
		"t90_max_s":               float64(397),
		"t100_median_s":           float64(441),
		"t100_max_s":              float64(441),
		"chunks_per_s_until_last": float64(726) / 2850,
	} {
		if got[key] != want {
			t.Errorf("%s = %v, want %v", key, got[key], want)
		}
	}
	got = e.viewTimes([]*bot{partial})
	if got["t50_median_s"] != float64(2210) {
		t.Errorf("partial view t50_median_s = %v, want 2210", got["t50_median_s"])
	}
	for _, key := range []string{"t90_median_s", "t90_max_s", "t100_median_s", "t100_max_s"} {
		if got[key] != float64(0) {
			t.Errorf("partial view %s = %v, want 0 (unreached)", key, got[key])
		}
	}
	if got["bots_complete"] != 0 {
		t.Errorf("partial view bots_complete = %v, want 0", got["bots_complete"])
	}
}

func TestViewTimesMilestones(t *testing.T) {
	e := &env{o: &options{view: 2}}
	b := &bot{}
	for x := int32(-2); x <= 2; x++ {
		for z := int32(-2); z <= 2; z++ {
			b.chunkLog = append(b.chunkLog, chunkArrival{at: time.Duration(len(b.chunkLog)+1) * time.Second, x: x, z: z})
		}
	}
	for _, count := range []int{0, 12, 13, 22, 23, 24, 25} {
		partial := &bot{name: "milestone", chunkLog: b.chunkLog[:count]}
		got := e.viewTimes([]*bot{partial})
		var seconds *float64
		if count == 25 {
			seconds = new(float64(25))
		}
		checkBotTimes(t, got, []string{"milestone"}, []*float64{seconds})
		for key, threshold := range map[string]int{"t50_median_s": 13, "t90_median_s": 23, "t100_median_s": 25} {
			var want float64
			if count >= threshold {
				want = float64(threshold)
			}
			if got[key] != want {
				t.Errorf("%d chunks: %s = %v, want %v", count, key, got[key], want)
			}
		}
	}
}

func TestViewTimesFirstChunk(t *testing.T) {
	e := &env{o: &options{view: 10}}
	bots := []*bot{
		{name: "first", x: 1600, chunkLog: []chunkArrival{
			{at: time.Second, x: 0}, // Old area must not satisfy the teleport.
			{at: 3 * time.Second, x: 100},
			{at: 4 * time.Second, x: 101},
		}},
		{name: "second", x: 3200, chunkLog: []chunkArrival{{at: 11 * time.Second, x: 200}}},
		{name: "empty", x: 4800}, // Receiving nothing cannot satisfy a milestone.
	}
	got := e.viewTimes(bots)
	checkBotTimes(t, got, []string{"first", "second", "empty"}, []*float64{nil, nil, nil})
	if got["first_max_s"] != float64(11) || got["bots_complete"] != 0 {
		t.Fatalf("first chunk summary = %v", got)
	}
}

func checkBotTimes(t *testing.T, got map[string]any, names []string, seconds []*float64) {
	t.Helper()
	entries := got["t100_by_bot"].([]botViewTime)
	if len(entries) != len(names) {
		t.Fatalf("entries=%v", entries)
	}
	for i, entry := range entries {
		if entry.Name != names[i] || (entry.Seconds == nil) != (seconds[i] == nil) {
			t.Fatalf("entry %d=%+v", i, entry)
		}
		if seconds[i] != nil && *entry.Seconds != *seconds[i] {
			t.Fatalf("seconds=%v, want %v", *entry.Seconds, *seconds[i])
		}
	}
}
