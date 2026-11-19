package terrain

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

// The parameter entries, independent target searches and complete chunks are
// recorded from the 26.3 jar by OverworldBiomeGolden.java.
func TestOverworldBiomeGolden(t *testing.T) {
	f, err := os.Open("testdata/overworld_biome_golden.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	sources := map[int64]*OverworldBiomeSource{}
	source := func(seed int64) *OverworldBiomeSource {
		if sources[seed] == nil {
			sources[seed] = NewOverworldBiomeSource(NewRandomState(Overworld, seed))
		}
		return sources[seed]
	}
	number := func(s string) int64 {
		n, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			t.Fatal(err)
		}
		return n
	}
	counts := map[string]int{}
	sc := bufio.NewScanner(f)
	line := 0
	for sc.Scan() {
		line++
		text := sc.Text()
		if strings.HasPrefix(text, "#") || text == "" {
			continue
		}
		v := strings.Fields(text)
		var got, want string
		switch v[0] {
		case "biomepoint":
			i := counts[v[0]]
			if i >= len(overworldBiomePoints) {
				t.Fatal("golden contains more biome parameters")
			}
			p := overworldBiomePoints[i]
			var b strings.Builder
			for _, r := range p.Bounds[:6] {
				fmt.Fprintf(&b, "%d %d ", r[0], r[1])
			}
			fmt.Fprintf(&b, "%d %s", p.Bounds[6][0], OverworldBiome(p.Biome).Name())
			got, want = b.String(), strings.Join(v[1:], " ")
		case "biometarget":
			var target [6]int64
			for i := range target {
				target[i] = number(v[i+1])
			}
			got = OverworldBiome(overworldIndex().NewSearch().Find(target)).Name()
			want = v[7]
		case "biomeat":
			got = source(number(v[1])).At(int32(number(v[2])), int32(number(v[3])), int32(number(v[4]))).Name()
			want = v[5]
		case "biomechunk":
			h := sha256.New()
			chunkSource := *source(number(v[1]))
			r := &chunkSource.router
			for _, field := range []*Sampler{&r.Temperature, &r.Vegetation, &r.Continents, &r.Erosion, &r.Depth, &r.Ridges} {
				*field = volumeOnlyClimate{*field}
			}
			chunkSource.FillChunk(int32(number(v[2])), int32(number(v[3])), func(_, _, _ int32, b OverworldBiome) { h.Write([]byte(b.Name() + "\n")) })
			got, want = hex.EncodeToString(h.Sum(nil)), v[4]
		default:
			t.Fatalf("line %d: unknown record %s", line, v[0])
		}
		counts[v[0]]++
		if got != want {
			t.Errorf("line %d: %s\n got %s\nwant %s", line, text, got, want)
		}
		if t.Failed() {
			t.FailNow()
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	if counts["biomepoint"] != len(overworldBiomePoints) {
		t.Fatal("incomplete parameter golden")
	}
	for _, kind := range []string{"biometarget", "biomeat", "biomechunk"} {
		if counts[kind] == 0 {
			t.Fatalf("no %s records", kind)
		}
	}
}

// Point and volume samplers can round differently; chunk biomes must use the
// volume path even when quantization happens to hide that difference.
type volumeOnlyClimate struct{ Sampler }

func (volumeOnlyClimate) Sample(*Context, int32, int32, int32) float32 {
	panic("chunk climate must be sampled as a volume")
}
