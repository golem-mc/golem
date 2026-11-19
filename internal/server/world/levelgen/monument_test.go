package levelgen

import (
	"bufio"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"
)

// Pins include every graph edge and opening, ordered piece kind, origin room,
// design, rotated box and subsequent RNG draw from the vanilla classes.
func TestMonumentGolden(t *testing.T) {
	t.Parallel()
	file, err := os.Open("testdata/monument_golden.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	scan := bufio.NewScanner(file)
	checked := 0
	var kinds [11]int
	var designs [3]int
	var cores [4]int
	for scan.Scan() {
		line := scan.Text()
		if strings.HasPrefix(line, "#") {
			continue
		}
		var seed int64
		var dir, samples, wantCount int
		var want string
		if _, err := fmt.Sscanf(line, "layout %d %d %d %d %s", &seed, &dir, &samples, &wantCount, &want); err != nil {
			t.Fatal(err)
		}
		var text strings.Builder
		count := 0
		box := func(b BoundingBox) {
			fmt.Fprintf(&text, "%d %d %d %d %d %d\n", b.MinX, b.MinY, b.MinZ, b.MaxX, b.MaxY, b.MaxZ)
		}
		for sample := range samples {
			rng := NewLegacyRandom(seed + int64(sample)*goldenRatio64)
			m := GenerateMonument(rng, -137+sample, 211-sample, dir)
			box(m.BB)
			fmt.Fprintf(&text, "%d %d\n", m.Source.Index, m.Core.Index)
			rooms := slices.Clone(m.Rooms)
			slices.SortFunc(rooms, func(a, b *MonumentRoom) int { return a.Index - b.Index })
			fmt.Fprintln(&text, len(rooms))
			for _, r := range rooms {
				fmt.Fprint(&text, r.Index)
				for _, other := range r.Connections {
					i := -1
					if other != nil {
						i = other.Index
					}
					fmt.Fprintf(&text, " %d", i)
				}
				mask := 0
				for d, open := range r.Openings {
					if open {
						mask |= 1 << d
					}
				}
				fmt.Fprintf(&text, " %d %t %t\n", mask, r.claimed, r.source)
			}
			fmt.Fprintln(&text, len(m.Pieces))
			count += len(m.Pieces)
			cores[m.Core.Index%5]++
			for _, p := range m.Pieces {
				i := -1
				if p.Room != nil {
					i = p.Room.Index
				}
				fmt.Fprintf(&text, "%d %d %d\n", p.Kind, i, p.Design)
				box(p.BB)
				kinds[p.Kind]++
				if p.Kind == MonumentSimple {
					designs[p.Design]++
				}
			}
			fmt.Fprintln(&text, rng.NextInt())
		}
		if got := digestLines([]string{text.String()}); got != want || count != wantCount {
			t.Errorf("seed %d dir %d: %d pieces, digest %s; vanilla %d, %s", seed, dir, count, got, wantCount, want)
		}
		checked += samples
	}
	if err := scan.Err(); err != nil {
		t.Fatal(err)
	}
	if checked != 5120 {
		t.Fatalf("checked %d layouts, want 5120", checked)
	}
	for kind, count := range kinds {
		if count == 0 {
			t.Errorf("piece kind %d not exercised", kind)
		}
	}
	for design, count := range designs {
		if count == 0 {
			t.Errorf("simple room design %d not exercised", design)
		}
	}
	for core, count := range cores {
		if count == 0 {
			t.Errorf("core x=%d not exercised", core)
		}
	}
}

func BenchmarkMonumentGenerate(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		GenerateMonument(NewLegacyRandom(12345), -137, 211, 0)
	}
}
