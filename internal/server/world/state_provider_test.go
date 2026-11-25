package world

import (
	"bufio"
	"encoding/json"
	"math"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/server/world/levelgen"
	"github.com/golem-mc/golem/registry"
)

func TestWorldRejectsLegacySimpleStateProvider(t *testing.T) {
	_, err := NewStateProviderPicker(parseJSON(`{"type":"minecraft:simple_state_provider","state":{"id":"minecraft:sand"}}`), 0)
	if err == nil || !strings.Contains(err.Error(), "unhandled state provider \"simple_state_provider\"") {
		t.Fatalf("provider compile error = %v, want the unknown provider type", err)
	}
}

func TestStateProviderGolden(t *testing.T) {
	t.Parallel()
	f, err := os.Open("testdata/state_provider_golden.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	docs := map[string]string{}
	c := providerCompiler{lookup: func(name string) (string, bool) { doc, ok := docs[name]; return doc, ok }}
	var provider optState
	var ordinary stateProvider
	var rng, ordinaryRNG *levelgen.WorldgenRandom
	ordinaryKey := ""
	last := ""
	current := registry.MustState("minecraft:oak_stairs", map[string]string{"facing": "west"})
	shaped := &shapedTerrain{}
	for i := range shaped.fill {
		shaped.fill[i] = current
	}
	r := &decorRegion{minY: worldMinY, height: worldHeight}
	for i := range r.near {
		r.near[i] = shaped
	}
	number := func(s string) int64 {
		n, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			t.Fatal(err)
		}
		return n
	}
	decode := func(s string) any {
		var v any
		if err := json.Unmarshal([]byte(s), &v); err != nil {
			t.Fatal(err)
		}
		return v
	}
	samples, noises := 0, 0
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Split(line, "\t")
		switch fields[0] {
		case "provider":
			docs[fields[1]] = fields[2]
		case "case":
			v := decode(fields[2])
			provider, ordinary = c.optional(v), c.state(v)
		case "sample":
			key := fields[1] + "/" + fields[2]
			seed := number(fields[2])
			if key != last {
				rng = levelgen.NewWorldgenRandom(seed)
				last = key
			}
			r.rng = rng
			p := [3]int32{int32(number(fields[3])), int32(number(fields[4])), int32(number(fields[5]))}
			r.center = newChunk(p[0]>>4, p[2]>>4)
			state, ok := provider(r, p)
			want := decode(fields[6])
			if ok != (want != nil) || (ok && state != compileBlockState(want)) {
				t.Fatalf("%s: state %d, %v, want %s", line, state, ok, fields[6])
			}
			if got := rng.NextInt(); got != int32(number(fields[7])) {
				t.Fatalf("%s: next random %d", line, got)
			}
			// Replay the same stream through getState, whose null result falls back to the world.
			if key != ordinaryKey {
				ordinaryRNG = levelgen.NewWorldgenRandom(seed)
				ordinaryKey = key
			}
			r.rng = ordinaryRNG
			expected := current
			if ok {
				expected = state
			}
			if got := ordinary(r, p); got != expected {
				t.Fatalf("%s: getState = %d, want %d", line, got, expected)
			}
			if got := ordinaryRNG.NextInt(); got != int32(number(fields[7])) {
				t.Fatalf("%s: getState next random %d", line, got)
			}
			samples++
		case "noise":
			noise := compileNativeNoise(parseJSON(fields[1])).Create(levelgen.NewLegacyRandom(2345))
			got := math.Float32bits(noise.Get3D(float64(number(fields[2])), float64(number(fields[3])), float64(number(fields[4]))))
			want, err := strconv.ParseUint(fields[5], 16, 32)
			if err != nil {
				t.Fatal(err)
			}
			if got != uint32(want) {
				t.Fatalf("%s: noise bits %08x", line, got)
			}
			noises++
		default:
			t.Fatalf("unexpected row %s", line)
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	if samples < 300 || noises != 15 {
		t.Fatalf("incomplete golden: %d states, %d noises", samples, noises)
	}
}
