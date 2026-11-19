package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestCaptureCPUProfileWindow(t *testing.T) {
	for _, status := range []int{http.StatusOK, http.StatusBadRequest, http.StatusInternalServerError} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			const profile = "cpu profile bytes"
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/debug/pprof/profile" {
					if status != http.StatusOK {
						http.Error(w, "CPU profiling already in use", status)
						return
					}
					_, _ = w.Write([]byte(profile))
				}
			}))
			defer srv.Close()
			e := &env{
				o:   &options{out: t.TempDir()},
				srv: &serverProc{kind: "golem", pprof: strings.TrimPrefix(srv.URL, "http://")},
			}
			before := time.Now()
			start, end := e.capture("test", time.Second)
			after := time.Now()
			path := filepath.Join(e.o.out, "profile-test", "cpu.pb.gz")
			if status != http.StatusOK {
				if !start.IsZero() || !end.IsZero() {
					t.Errorf("failed capture returned window %v to %v", start, end)
				}
				if _, err := os.Stat(path); !os.IsNotExist(err) {
					t.Errorf("failed capture created a CPU profile: stat error = %v", err)
				}
				return
			}
			if start.Before(before) || end.Before(start) || end.After(after) {
				t.Errorf("capture window %v to %v outside request bounds %v to %v", start, end, before, after)
			}
			if data, err := os.ReadFile(path); err != nil || string(data) != profile {
				t.Errorf("CPU profile = %q, %v; want %q", data, err, profile)
			}
		})
	}
}

func TestProfileTickWindow(t *testing.T) {
	start := time.Unix(1_700_000_000, 0)
	var samples []tickSample
	for i, ms := range []int64{800, 900, 10, 20, 40, 90, 700} {
		samples = append(samples, tickSample{
			at: start.Add(time.Duration(i-1) * time.Second), tick: ms * 1e6,
			full: 1_000 * 1e6,
		})
	}
	for _, tt := range []struct {
		name       string
		start, end time.Time
		ticks      []float64
		stats      stats
	}{
		{
			name: "boundaries", start: start, end: start.Add(4 * time.Second),
			ticks: []float64{10, 20, 40, 90},
			stats: stats{Mean: 40, P50: 20, P95: 90, P99: 90, Max: 90},
		},
		{
			name: "short capture", start: start.Add(2 * time.Second), end: start.Add(4 * time.Second),
			ticks: []float64{40, 90},
			stats: stats{Mean: 65, P50: 40, P95: 90, P99: 90, Max: 90},
		},
		{name: "no samples", start: start.Add(10 * time.Second), end: start.Add(20 * time.Second)},
		{name: "empty", start: start, end: start},
		{name: "reversed", start: start.Add(4 * time.Second), end: start},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got := profileTickWindow(samples, tt.start, tt.end)
			if len(got) != len(tt.ticks) || !slices.Equal(got, tt.ticks) {
				t.Fatalf("ticks = %v, want %v", got, tt.ticks)
			}
			if gotStats := statsOf(got); gotStats != tt.stats {
				t.Errorf("stats = %+v, want %+v", gotStats, tt.stats)
			}
		})
	}
}
