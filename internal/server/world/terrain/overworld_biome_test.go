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
			chunkSource := *source(number(v[1]))
			r := &chunkSource.router
			for _, field := range []*Sampler{&r.Temperature, &r.Vegetation, &r.Continents, &r.Erosion, &r.Depth, &r.Ridges} {
				*field = volumeOnlyClimate{*field}
			}
			got, want = fillChunkHash(&chunkSource, int32(number(v[2])), int32(number(v[3]))), v[4]
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

type biomeChunkFixture struct {
	seed   int64
	cx, cz int32
	hash   string
}

func fillChunkHash(s *OverworldBiomeSource, cx, cz int32) string {
	h := sha256.New()
	s.FillChunk(cx, cz, func(_, _, _ int32, b OverworldBiome) {
		h.Write([]byte(b.Name() + "\n"))
	})
	return hex.EncodeToString(h.Sum(nil))
}

func loadBiomeChunkFixtures(t *testing.T) []biomeChunkFixture {
	t.Helper()
	f, err := os.Open("testdata/overworld_biome_golden.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var fixtures []biomeChunkFixture
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		v := strings.Fields(sc.Text())
		if len(v) != 5 || v[0] != "biomechunk" {
			continue
		}
		seed, err := strconv.ParseInt(v[1], 10, 64)
		if err != nil {
			t.Fatal(err)
		}
		cx, err := strconv.ParseInt(v[2], 10, 32)
		if err != nil {
			t.Fatal(err)
		}
		cz, err := strconv.ParseInt(v[3], 10, 32)
		if err != nil {
			t.Fatal(err)
		}
		fixtures = append(fixtures, biomeChunkFixture{seed: seed, cx: int32(cx), cz: int32(cz), hash: v[4]})
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	return fixtures
}

func TestOverworldBiomeFillChunkReuse(t *testing.T) {
	bySeed := map[int64][]biomeChunkFixture{}
	var seeds []int64
	for _, fx := range loadBiomeChunkFixtures(t) {
		list := bySeed[fx.seed]
		if len(list) == 0 {
			seeds = append(seeds, fx.seed)
		}
		if len(list) >= 2 || (len(list) == 1 && list[0].hash == fx.hash) {
			continue
		}
		bySeed[fx.seed] = append(list, fx)
	}
	var jobs []biomeChunkFixture
	for round := range 2 {
		for _, seed := range seeds {
			if round < len(bySeed[seed]) {
				jobs = append(jobs, bySeed[seed][round])
			}
		}
	}
	if len(jobs) < 4 {
		t.Fatalf("expected at least 4 biomechunk fixtures, got %d", len(jobs))
	}

	sources := make(map[int64]*OverworldBiomeSource, len(seeds))
	for _, seed := range seeds {
		sources[seed] = NewOverworldBiomeSource(NewRandomState(Overworld, seed))
	}

	// Sequential alternating fills reuse recycled contexts across chunks and seeds.
	for range 3 {
		for i, j := range jobs {
			got := fillChunkHash(sources[j.seed], j.cx, j.cz)
			if got != j.hash {
				t.Fatalf("sequential job %d (seed=%d chunk=%d,%d) got %s, want %s", i, j.seed, j.cx, j.cz, got, j.hash)
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
				got := fillChunkHash(sources[j.seed], j.cx, j.cz)
				if got != j.hash {
					t.Errorf("concurrent goroutine %d job %d (seed=%d chunk=%d,%d) got %s, want %s", offset, idx, j.seed, j.cx, j.cz, got, j.hash)
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
