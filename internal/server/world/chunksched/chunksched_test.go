package chunksched

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"testing"
)

type vanillaRun struct {
	rect Rect
	seq  []Pos
}

// readVanillaRuns reads testdata/vanilla_features.txt.
func readVanillaRuns(t *testing.T) []vanillaRun {
	t.Helper()
	f, err := os.Open("testdata/vanilla_features.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var runs []vanillaRun
	var rect Rect
	sc := bufio.NewScanner(f)
	sc.Buffer(nil, 1<<20)
	for sc.Scan() {
		line := sc.Text()
		switch {
		case strings.HasPrefix(line, "#") || line == "":
		case strings.HasPrefix(line, "range "):
			if _, err := fmt.Sscan(line[len("range "):], &rect.X0, &rect.Z0, &rect.X1, &rect.Z1); err != nil {
				t.Fatalf("%q: %v", line, err)
			}
		case strings.HasPrefix(line, "run "):
			var seq []Pos
			for f := range strings.FieldsSeq(line[len("run "):]) {
				var p Pos
				if _, err := fmt.Sscanf(f, "%d,%d", &p.X, &p.Z); err != nil {
					t.Fatalf("%q: %v", f, err)
				}
				seq = append(seq, p)
			}
			runs = append(runs, vanillaRun{rect, seq})
		default:
			t.Fatalf("unexpected line %q", line)
		}
	}
	return runs
}

// decorationOrder is the order chunks are decorated when each task decorates
// the chunks of its 3x3 that are not yet, x outer and z inner.
func decorationOrder(tasks []Pos) []Pos {
	done := map[Pos]bool{}
	var seq []Pos
	for _, c := range tasks {
		for dx := int32(-1); dx <= 1; dx++ {
			for dz := int32(-1); dz <= 1; dz++ {
				p := Pos{c.X + dx, c.Z + dz}
				if !done[p] {
					done[p] = true
					seq = append(seq, p)
				}
			}
		}
	}
	return seq
}

func chebyshev(a, b Pos) int32 {
	return max(abs32(a.X-b.X), abs32(a.Z-b.Z))
}

// TestForceloadAgreesWithVanillaOrder checks every pair of chunks within two of
// each other (their features can touch the same blocks) that sit in or next to
// the rectangle: where all recorded vanilla runs order the pair the same way,
// the model must too.
func TestForceloadAgreesWithVanillaOrder(t *testing.T) {
	t.Parallel()
	runs := readVanillaRuns(t)
	if len(runs) == 0 {
		t.Fatal("no vanilla runs")
	}
	byRect := map[Rect][]vanillaRun{}
	for _, r := range runs {
		byRect[r.rect] = append(byRect[r.rect], r)
	}
	for rect, rs := range byRect {
		model := indexOf(decorationOrder(Forceload([]Rect{rect})))
		var near []Pos
		for x := rect.X0 - 1; x <= rect.X1+1; x++ {
			for z := rect.Z0 - 1; z <= rect.Z1+1; z++ {
				near = append(near, Pos{x, z})
			}
		}
		pos := make([]map[Pos]int, len(rs))
		for i, r := range rs {
			pos[i] = indexOf(r.seq)
		}
		stable, wrong := 0, 0
		for i, a := range near {
			for _, b := range near[i+1:] {
				if chebyshev(a, b) > 2 {
					continue
				}
				first := pos[0][a] < pos[0][b]
				agreed := true
				for _, p := range pos[1:] {
					agreed = agreed && (p[a] < p[b]) == first
				}
				if !agreed {
					continue
				}
				stable++
				if (model[a] < model[b]) != first {
					wrong++
					t.Errorf("rect %v: vanilla runs order %v and %v the other way", rect, a, b)
				}
			}
		}
		if stable < 800 && (rect.X1-rect.X0 >= 7) {
			t.Errorf("rect %v: only %d pairs all runs agree on", rect, stable)
		}
		t.Logf("rect %v: %d stable pairs, %d differ", rect, stable, wrong)
	}
}

func indexOf(seq []Pos) map[Pos]int {
	m := make(map[Pos]int, len(seq))
	for i, p := range seq {
		if _, ok := m[p]; !ok {
			m[p] = i
		}
	}
	return m
}

// TestPyramidMatchesVanilla pins the accumulated dependencies of a FULL chunk:
// the statuses at each distance (the radii the layers and levels come from).
// vanilla: ChunkPyramid.GENERATION_PYRAMID.getStepTo(FULL).accumulatedDependencies
func TestPyramidMatchesVanilla(t *testing.T) {
	t.Parallel()
	got := generationPyramid()[statusFull].accumulated
	want := dependencies{statusSpawn, statusInitializeLight, statusTerrain, statusBiomes, statusStructureStarts, statusStructureStarts,
		statusStructureStarts, statusStructureStarts, statusStructureStarts, statusStructureStarts, statusStructureStarts, statusStructureStarts}
	if len(got) != len(want) {
		t.Fatalf("accumulated dependencies %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("accumulated dependencies %v, want %v", got, want)
		}
	}
	if len(got)-1+33 != maxLevel {
		t.Fatalf("max level %d, the pyramid gives %d", maxLevel, len(got)-1+33)
	}
}
