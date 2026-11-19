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
