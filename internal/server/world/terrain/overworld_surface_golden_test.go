package terrain

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"strconv"
	"strings"
	"testing"
)

// goldenSurfaceHeight is the synthetic terrain the golden's Java side builds.
// Java and Go share the truncating % and / on negative numbers.
func goldenSurfaceHeight(level, a, x, z int32) int32 {
	return level + (x*a+z*(a+1))%(3*a+2) + ((x/8)*5+(z/8)*3)%4*2
}

// TestOverworldSurfaceGolden checks material rules over natural, chunk-backed
// biomes on synthetic terrain (mise run gen:overworld-surface-golden).
func TestOverworldSurfaceGolden(t *testing.T) {
	t.Parallel()
	f, err := os.Open("testdata/overworld_surface_golden.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	atoi := func(s string) int32 {
		v, err := strconv.ParseInt(s, 10, 32)
		if err != nil {
			t.Fatal(err)
		}
		return int32(v)
	}
	gens := map[int64]*RandomState{}
	biomeSources := map[int64]*naturalGoldenBiomes{}
	type chunkKey struct {
		seed             int64
		cx, cz, level, a int32
	}
	surfaces := map[chunkKey]*ProtoChunk{}
	melt := map[chunkKey]string{}
	meltScan := bufio.NewScanner(f)
	for meltScan.Scan() {
		fl := strings.Fields(meltScan.Text())
		if len(fl) == 0 || fl[0] != "surfacemelt" {
			continue
		}
		seed, err := strconv.ParseInt(fl[1], 10, 64)
		if err != nil {
			t.Fatal(err)
		}
		melt[chunkKey{seed, atoi(fl[2]), atoi(fl[3]), atoi(fl[4]), atoi(fl[5])}] = fl[6]
	}
	if err := meltScan.Err(); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Seek(0, 0); err != nil {
		t.Fatal(err)
	}
	sc := bufio.NewScanner(f)
	sc.Buffer(nil, 1<<20)
	chunks, columns, wrong := 0, 0, 0
	for sc.Scan() {
		line := sc.Text()
		if line == "" || line[0] == '#' || strings.HasPrefix(line, "surfacemelt ") {
			continue
		}
		fl := strings.Fields(line)
		seed, err := strconv.ParseInt(fl[1], 10, 64)
		if err != nil {
			t.Fatal(err)
		}
		g := gens[seed]
		if g == nil {
			g = NewRandomState(Overworld, seed)
			gens[seed] = g
			biomeSources[seed] = newNaturalGoldenBiomes(g, "", false)
		}
		cx, cz, level, a := atoi(fl[2]), atoi(fl[3]), atoi(fl[4]), atoi(fl[5])
		key := chunkKey{seed, cx, cz, level, a}
		if surfaces[key] == nil {
			if len(melt[key]) != 256 {
				t.Fatalf("missing melt decisions for %+v", key)
			}
			chunk := NewProtoChunk(-64, 384, Stone)
			for x := range int32(16) {
				for z := range int32(16) {
					h := goldenSurfaceHeight(level, a, cx*16+x, cz*16+z)
					for y := int32(-64); y <= max(h, 62); y++ {
						material := Water
						if y <= h {
							material = Stone
						}
						chunk.Set(x, y, z, material)
					}
				}
			}
			biomes := biomeSources[seed]
			biomes.melt = melt[key]
			nc := NewNoiseChunk(g, nil, g.ChunkVolume(cx, cz))
			g.Materials.BuildSurface(chunk, cx, cz, nc, biomes, nil)
			surfaces[key] = chunk
		}
		s := surfaces[key]
		switch fl[0] {
		case "surfacechunk":
			h := sha256.New()
			for x := range int32(16) {
				for z := range int32(16) {
					for y := range int32(384) {
						h.Write([]byte(s.Get(x, y-64, z).Name() + "\n"))
					}
				}
			}
			chunks++
			if got := hex.EncodeToString(h.Sum(nil)); got != fl[7] {
				wrong++
				t.Errorf("seed %d chunk (%d,%d) level %d slope %d (%s): digest %s, vanilla %s", seed, cx, cz, level, a, fl[6], got, fl[7])
			}
		case "surfacecolumn":
			columns++
			x, z := atoi(fl[6]), atoi(fl[7])
			var parts []string
			for y := int32(-64); y < 320; {
				n := y
				for n < 320 && s.Get(x, n, z) == s.Get(x, y, z) {
					n++
				}
				parts = append(parts, s.Get(x, y, z).Name()+"*"+strconv.Itoa(int(n-y)))
				y = n
			}
			if got := strings.Join(parts, ","); got != fl[8] {
				t.Errorf("seed %d chunk (%d,%d) column (%d,%d):\n got %s\nwant %s", seed, cx, cz, x, z, got, fl[8])
			}
		default:
			t.Fatalf("unknown record %q", fl[0])
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	if chunks != 318 || len(melt) != chunks || columns != 3*chunks {
		t.Fatalf("got %d chunks, %d melt records and %d columns, want 318/318/954", chunks, len(melt), columns)
	}
	t.Logf("%d chunks, %d differ", chunks, wrong)
}
