package levelgen

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
)

// The golden values in testdata/structure_golden.txt come from the vanilla
// classes on the vanilla server jar (tools/java/StructureGolden.java, mise run
// gen:structure-golden): the structures' settings, the structure sets'
// placements and entries, the chunks each random_spread set passes
// isStructureChunk for (and a synthetic one with the default reducer), and
// the order a multi-structure set tries its entries in.

var adjustmentNames = map[string]TerrainAdjustment{
	"none": AdjustNone, "bury": AdjustBury, "beard_thin": AdjustBeardThin, "beard_box": AdjustBeardBox, "encapsulate": AdjustEncapsulate,
}

func digestLines(lines []string) string {
	sum := sha256.Sum256([]byte(strings.Join(lines, "\n")))
	return hex.EncodeToString(sum[:])
}

func TestStructureGolden(t *testing.T) {
	t.Parallel()
	f, err := os.Open("testdata/structure_golden.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	atoi := func(s string) int {
		v, err := strconv.Atoi(s)
		if err != nil {
			t.Fatalf("bad integer %q", s)
		}
		return v
	}
	var structures, sets int
	entries := map[string][]string{}
	checked := map[string]int{}
	sc := bufio.NewScanner(f)
	sc.Buffer(nil, 1<<20)
	for sc.Scan() {
		text := sc.Text()
		if strings.HasPrefix(text, "#") {
			continue
		}
		fl := strings.Fields(text)
		checked[fl[0]]++
		switch fl[0] {
		case "structure":
			if structures >= len(Structures) {
				t.Fatalf("vanilla has more structures than %d", len(Structures))
			}
			s := Structures[structures]
			structures++
			got := fmt.Sprintf("%s #%s %d %d", s.Name, s.Biomes, s.Step, s.Adaptation)
			want := fmt.Sprintf("%s %s %s %d", fl[1], fl[2], fl[3], adjustmentNames[fl[4]])
			if got != want {
				t.Errorf("structure %d = %s, vanilla %s", structures-1, got, want)
			}
		case "set":
			if sets >= len(StructureSets) {
				t.Fatalf("vanilla has more structure sets than %d", len(StructureSets))
			}
			s := &StructureSets[sets]
			sets++
			p := s.Placement
			var got string
			if fl[2] == "spread" {
				reducers := []string{"default", "legacy_type_1", "legacy_type_2", "legacy_type_3"}
				spreads := []string{"linear", "triangular"}
				ex := p.ExclusionSet
				if ex == "" {
					ex = "-"
				}
				got = fmt.Sprintf("set %s spread %d %d %s %d %s %s %d %d %d %s %d", s.Name, p.Spacing, p.Separation, spreads[p.Spread], p.Salt,
					strconv.FormatFloat(float64(p.Frequency), 'f', -1, 32), reducers[p.Reducer], p.LocateOffset[0], p.LocateOffset[1], p.LocateOffset[2], ex, p.ExclusionChunks)
				if p.Frequency == 1 {
					got = strings.Replace(got, " 1 default", " 1.0 default", 1)
				}
			} else {
				got = fmt.Sprintf("set %s rings %d %d %d #%s", s.Name, p.RingDistance, p.RingSpread, p.RingCount, p.RingPreferred)
			}
			if got != text {
				t.Errorf("%s\nvanilla %s", got, text)
			}
			for _, e := range s.Entries {
				entries[s.Name] = append(entries[s.Name], fmt.Sprintf("entry %s %s %d", s.Name, e.Structure, e.Weight))
			}
		case "entry":
			list := entries[fl[1]]
			if len(list) == 0 || list[0] != text {
				t.Errorf("next entry of %s is %v, vanilla %s", fl[1], list, text)
			} else {
				entries[fl[1]] = list[1:]
			}
		case "chunks", "select":
			seed, _ := strconv.ParseInt(fl[1], 10, 64)
			set := StructureSetNamed(fl[2])
			lo, hi := int32(atoi(fl[3])), int32(atoi(fl[4]))
			var lines []string
			for x := lo; x <= hi; x++ {
				for z := lo; z <= hi; z++ {
					if !set.IsStructureChunk(seed, x, z, func(int32, int32) bool { return false }) {
						continue
					}
					line := fmt.Sprintf("%d %d", x, z)
					if fl[0] == "select" {
						var order []string
						set.TryEntries(seed, x, z, func(e StructureSetEntry) bool {
							order = append(order, e.Structure)
							return false
						})
						line += " " + strings.Join(order, ",")
					}
					lines = append(lines, line)
				}
			}
			if got := fmt.Sprintf("%d %s", len(lines), digestLines(lines)); got != fl[5]+" "+fl[6] {
				t.Errorf("%s seed %d: %s, vanilla %s %s", fl[0], seed, got, fl[5], fl[6])
			}
		case "thinned":
			seed, _ := strconv.ParseInt(fl[1], 10, 64)
			freq, _ := strconv.ParseFloat(fl[5], 32)
			set := StructureSet{Placement: StructurePlacement{Spacing: int32(atoi(fl[2])), Separation: int32(atoi(fl[3])), Salt: int32(atoi(fl[4])), Frequency: float32(freq)}}
			var lines []string
			for x := int32(atoi(fl[6])); x <= int32(atoi(fl[7])); x++ {
				for z := int32(atoi(fl[6])); z <= int32(atoi(fl[7])); z++ {
					if set.IsStructureChunk(seed, x, z, nil) {
						lines = append(lines, fmt.Sprintf("%d %d", x, z))
					}
				}
			}
			if got := fmt.Sprintf("%d %s", len(lines), digestLines(lines)); got != fl[8]+" "+fl[9] {
				t.Errorf("thinned seed %d: %s, vanilla %s %s", seed, got, fl[8], fl[9])
			}
		default:
			t.Fatalf("unrecognized structure record %q", fl[0])
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	if structures != len(Structures) || sets != len(StructureSets) {
		t.Errorf("vanilla has %d structures and %d sets, ours %d and %d", structures, sets, len(Structures), len(StructureSets))
	}
	for _, kind := range []string{"structure", "set", "entry", "chunks", "select", "thinned"} {
		if checked[kind] == 0 {
			t.Errorf("no %s lines checked", kind)
		}
	}
}
