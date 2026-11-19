package terrain

import (
	"bufio"
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// The values in testdata/sampler_golden.txt come from the vanilla classes on
// the 26.3 server jar (tools/java/SamplerGolden.java, mise run
// gen:sampler-golden); every float is compared by its bits.

// goldenRecord is one line: the fields before " -> " and the results after.
type goldenRecord struct {
	kind   string
	fields []string
	values []string
}

var (
	goldenOnce    sync.Once
	goldenRecords map[string][]goldenRecord
)

// goldenOf returns the records of one kind.
func goldenOf(t *testing.T, kind string) []goldenRecord {
	t.Helper()
	goldenOnce.Do(func() {
		goldenRecords = map[string][]goldenRecord{}
		f, err := os.Open("testdata/sampler_golden.txt")
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 1<<20), 1<<24)
		for sc.Scan() {
			line := sc.Text()
			if line == "" || line[0] == '#' {
				continue
			}
			head, tail, _ := strings.Cut(line, " -> ")
			fields := strings.Fields(head)
			r := goldenRecord{kind: fields[0], fields: fields[1:], values: strings.Fields(tail)}
			goldenRecords[r.kind] = append(goldenRecords[r.kind], r)
		}
		if err := sc.Err(); err != nil {
			t.Fatal(err)
		}
	})
	records := goldenRecords[kind]
	if len(records) == 0 {
		t.Fatalf("no %q records in the golden file", kind)
	}
	return records
}

func (r goldenRecord) int(t *testing.T, i int) int64 {
	t.Helper()
	v, err := strconv.ParseInt(r.fields[i], 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func (r goldenRecord) float(t *testing.T, i int) float64 {
	t.Helper()
	v, err := strconv.ParseFloat(r.fields[i], 64)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func (r goldenRecord) String() string { return r.kind + " " + strings.Join(r.fields, " ") }

func bitsHex(v float32) string { return fmt.Sprintf("%08x", math.Float32bits(v)) }

// checkBits fails when a float's bits differ from the golden hex.
func checkBits(t *testing.T, r goldenRecord, got float32) {
	t.Helper()
	if want := r.values[0]; bitsHex(got) != want {
		t.Errorf("%s: got %s (%v), want %s", r, bitsHex(got), got, want)
	}
}
