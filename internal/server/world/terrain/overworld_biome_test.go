package terrain

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
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

func fillChunkFresh(s *OverworldBiomeSource, cx, cz int32) [4 * 96 * 4]OverworldBiome {
	v := NewVolume(4, 96, 4, cx*16, -64, cz*16)
	v.StepX, v.StepY, v.StepZ = 4, 4, 4
	ctx := &Context{caches: []cacheCell{}}
	var values [6][]float32
	for i, sampler := range [...]Sampler{s.router.Temperature, s.router.Vegetation, s.router.Continents, s.router.Erosion, s.router.Depth, s.router.Ridges} {
		values[i] = make([]float32, v.Size())
		sampler.SampleVolume(ctx, values[i], v)
	}
	var out [4 * 96 * 4]OverworldBiome
	search := overworldIndex().NewSearch()
	for section := range int32(24) {
		for x := range int32(4) {
			for y := section * 4; y < section*4+4; y++ {
				for z := range int32(4) {
					i := v.Index(x, y, z)
					var target [6]int64
					for axis := range target {
						target[axis] = quantizeClimate(values[axis][i])
					}
					out[i] = OverworldBiome(search.Find(target))
				}
			}
		}
	}
	return out
}

func fillChunkCollect(s *OverworldBiomeSource, cx, cz int32) [4 * 96 * 4]OverworldBiome {
	v := NewVolume(4, 96, 4, cx*16, -64, cz*16)
	var out [4 * 96 * 4]OverworldBiome
	s.FillChunk(cx, cz, func(x, y, z int32, b OverworldBiome) {
		out[v.Index(x, y, z)] = b
	})
	return out
}

func TestOverworldBiomeFillChunkReuse(t *testing.T) {
	seeds := []int64{12345, -987654321}
	sources := make([]*OverworldBiomeSource, len(seeds))
	for i, seed := range seeds {
		sources[i] = NewOverworldBiomeSource(NewRandomState(Overworld, seed))
	}
	type job struct {
		sourceIdx int
		cx, cz    int32
	}
	jobs := []job{
		{0, 0, 0},
		{1, 0, 0},
		{0, -3, 7},
		{1, 14, -9},
		{0, 60, 62},
		{1, -70, 5},
	}
	want := make([][4 * 96 * 4]OverworldBiome, len(jobs))
	for i, j := range jobs {
		want[i] = fillChunkFresh(sources[j.sourceIdx], j.cx, j.cz)
	}

	// Sequential alternating fills reuse recycled contexts across chunks and seeds.
	for range 3 {
		for i, j := range jobs {
			got := fillChunkCollect(sources[j.sourceIdx], j.cx, j.cz)
			if got != want[i] {
				t.Fatalf("sequential job %d (seed=%d chunk=%d,%d) mismatched fresh context", i, seeds[j.sourceIdx], j.cx, j.cz)
			}
		}
	}

	// Concurrent fills sharing biome sources must remain independent.
	var wg sync.WaitGroup
	for g := range 4 {
		wg.Add(1)
		go func(offset int) {
			defer wg.Done()
			for round := range 4 {
				idx := (offset + round) % len(jobs)
				j := jobs[idx]
				got := fillChunkCollect(sources[j.sourceIdx], j.cx, j.cz)
				if got != want[idx] {
					t.Errorf("concurrent goroutine %d job %d (seed=%d chunk=%d,%d) mismatched fresh context", offset, idx, seeds[j.sourceIdx], j.cx, j.cz)
					return
				}
			}
		}(g)
	}
	wg.Wait()

	// Fixed biome fast path still fills all quarts with the fixed biome.
	fixed := NewFixedOverworldBiomeSource(OverworldBiome(1))
	count := 0
	fixed.FillChunk(2, -5, func(_, _, _ int32, b OverworldBiome) {
		if b != OverworldBiome(1) {
			t.Fatalf("fixed biome got %d, want 1", b)
		}
		count++
	})
	if count != 4*96*4 {
		t.Fatalf("fixed biome visited %d quarts, want %d", count, 4*96*4)
	}
}
