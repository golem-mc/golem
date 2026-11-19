package world

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/server/world/levelgen"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

func TestOverworldPortalStartsMatchVanilla(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("testdata/overworld_portal_starts.txt")
	if err != nil {
		t.Fatal(err)
	}
	generators := map[int64]*OverworldGenerator{}
	checked := 0
	for line := range strings.SplitSeq(string(data), "\n") {
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "age ") || strings.HasPrefix(line, "growth ") {
			continue
		}
		fields := strings.SplitN(line, " ", 5)
		if len(fields) != 5 {
			t.Fatal(line)
		}
		var seed int64
		var cx, cz int32
		if _, err := fmt.Sscan(strings.Join(fields[:3], " "), &seed, &cx, &cz); err != nil {
			t.Fatal(err)
		}
		g := generators[seed]
		if g == nil {
			g = NewOverworldGenerator(seed)
			generators[seed] = g
		}
		rng := levelgen.NewLegacyRandom(0)
		rng.SetLargeFeatureSeed(seed, cx, cz)
		_, build, ok := (ruinedPortalStructure{fields[3]}).generationPoint(&structureContext{g: g, chunkX: cx, chunkZ: cz, rng: rng})
		if !ok {
			t.Fatal("no generation point", line)
		}
		want, err := nbt.ParseSNBT(fields[4])
		if err != nil {
			t.Fatal(err)
		}
		got := build()[0].nbt()
		a, _ := nbt.Encode("", got)
		b, _ := nbt.Encode("", want)
		if string(a) != string(b) {
			t.Errorf("seed %d %s at %d,%d: got %v; vanilla %v", seed, fields[3], cx, cz, got, want)
		}
		checked++
	}
	if checked != 48 {
		t.Fatalf("checked %d pieces, want 48", checked)
	}
}

func TestPortalAgeMatchesVanilla(t *testing.T) {
	data, err := os.ReadFile("testdata/overworld_portal_starts.txt")
	if err != nil {
		t.Fatal(err)
	}
	blocks := []registry.Block{registry.BlockStone, registry.BlockStoneBricks, registry.BlockChiseledStoneBricks, registry.BlockStoneBrickStairs, registry.BlockMossyStoneBrickStairs, registry.BlockStoneBrickSlab, registry.BlockStoneBrickWall, registry.BlockObsidian}
	checked := 0
	for line := range strings.SplitSeq(string(data), "\n") {
		if !strings.HasPrefix(line, "age ") {
			continue
		}
		var mossiness float32
		var want string
		if _, err := fmt.Sscanf(line, "age %f %s", &mossiness, &want); err != nil {
			t.Fatal(err)
		}
		h := sha256.New()
		for i := range int32(256) {
			x, y, z := i-128, 64+i%32, 37-i*2
			for _, b := range blocks {
				state := ruinedPortalAge(b.DefaultState(), levelgen.NewLegacyRandom(blockPosSeed(x, y, z)), mossiness)
				var bytes [4]byte
				binary.BigEndian.PutUint32(bytes[:], uint32(state))
				h.Write(bytes[:])
			}
		}
		if got := fmt.Sprintf("%x", h.Sum(nil)); got != want {
			t.Errorf("mossiness %g: got %s; vanilla %s", mossiness, got, want)
		}
		checked++
	}
	if checked != 4 {
		t.Fatalf("checked %d ages, want 4", checked)
	}
}

func TestPortalGrowthMatchesVanilla(t *testing.T) {
	data, err := os.ReadFile("testdata/overworld_portal_starts.txt")
	if err != nil {
		t.Fatal(err)
	}
	blocks := []registry.Block{registry.BlockNetherrack, registry.BlockStone, registry.BlockStoneBrickStairs, registry.BlockStoneSlab, registry.BlockJungleLeaves, registry.BlockObsidian}
	checked := 0
	for line := range strings.SplitSeq(string(data), "\n") {
		if !strings.HasPrefix(line, "growth ") {
			continue
		}
		var seed int64
		var want string
		if _, err := fmt.Sscanf(line, "growth %d %s", &seed, &want); err != nil {
			t.Fatal(err)
		}
		r := newDecorRegion(newGoldenTerrain(), newChunk(0, 0), worldMinY, worldHeight)
		r.bindProtos(ChunkPos{})
		r.rng = levelgen.NewWorldgenRandom(seed)
		for x := int32(-1); x <= 1; x++ {
			for z := int32(-1); z <= 1; z++ {
				r.set(x, 200, z, blocks[((x+1)*3+z+1)%int32(len(blocks))].DefaultState())
			}
		}
		for y := int32(200); y <= 201; y++ {
			for z := int32(-2); z <= 2; z++ {
				for x := int32(-2); x <= 2; x++ {
					r.portalVines(x, y, z)
					r.portalLeaves(x, y, z)
				}
			}
		}
		h := sha256.New()
		write := func(n int32) { var b [4]byte; binary.BigEndian.PutUint32(b[:], uint32(n)); h.Write(b[:]) }
		for x := int32(-4); x <= 4; x++ {
			for y := int32(200); y <= 202; y++ {
				for z := int32(-4); z <= 4; z++ {
					if s := r.get(x, y, z); !r.src.isAir(s) {
						write(x)
						write(y)
						write(z)
						write(int32(s))
					}
				}
			}
		}
		write(r.rng.NextInt())
		if got := fmt.Sprintf("%x", h.Sum(nil)); got != want {
			t.Errorf("seed %d: got %s; vanilla %s", seed, got, want)
		}
		checked++
	}
	if checked != 2 {
		t.Fatalf("checked %d growth cases, want 2", checked)
	}
}

func TestColdPortalRules(t *testing.T) {
	r := newDecorRegion(newGoldenTerrain(), newChunk(0, 0), worldMinY, worldHeight)
	for _, tc := range []struct {
		placement string
		lava      registry.Block
	}{{"on_land_surface", registry.BlockNetherrack}, {"on_ocean_floor", registry.BlockMagmaBlock}} {
		p := &ruinedPortal{cold: true, setup: ruinedPortalSetup{Placement: tc.placement}}
		for _, b := range []registry.Block{registry.BlockLava, registry.BlockNetherrack} {
			want := b.DefaultState()
			if b == registry.BlockLava {
				want = tc.lava.DefaultState()
			}
			if got, ok := r.processPortalBlock(p, b.DefaultState(), 0, 200, 0); !ok || got != want {
				t.Errorf("%s cold %s: got %d,%t; want %d", tc.placement, b.Name(), got, ok, want)
			}
		}
	}
}

func TestPortalGrowthVisitsBetweenClosedOrder(t *testing.T) {
	t.Parallel()
	p := &ruinedPortal{box: jbox{0, 200, 0, 3, 202, 4}, setup: ruinedPortalSetup{Vines: true, Overgrown: true}}
	region := func() *decorRegion {
		r := newDecorRegion(newGoldenTerrain(), newChunk(0, 0), worldMinY, worldHeight)
		r.bindProtos(ChunkPos{})
		r.rng = levelgen.NewWorldgenRandom(5544)
		for x := int32(0); x <= 3; x++ {
			for y := int32(200); y <= 202; y++ {
				for z := int32(0); z <= 4; z++ {
					if (x*7+y*3+z*5)%3 != 0 {
						r.set(x, y, z, registry.BlockNetherrack.DefaultState())
					}
				}
			}
		}
		return r
	}
	snapshot := func(r *decorRegion) string {
		var sb strings.Builder
		for x := int32(-1); x <= 4; x++ {
			for y := int32(200); y <= 203; y++ {
				for z := int32(-1); z <= 5; z++ {
					fmt.Fprintf(&sb, "%d,", r.get(x, y, z))
				}
			}
		}
		fmt.Fprintf(&sb, "rng %d", r.rng.NextInt())
		return sb.String()
	}
	// replay visits the box with the outer, middle and inner loops over the
	// given axes (0 x, 1 y, 2 z).
	replay := func(outer, middle, inner int) string {
		r := region()
		lo := [3]int32{p.box.minX, p.box.minY, p.box.minZ}
		hi := [3]int32{p.box.maxX, p.box.maxY, p.box.maxZ}
		var c [3]int32
		for c[outer] = lo[outer]; c[outer] <= hi[outer]; c[outer]++ {
			for c[middle] = lo[middle]; c[middle] <= hi[middle]; c[middle]++ {
				for c[inner] = lo[inner]; c[inner] <= hi[inner]; c[inner]++ {
					r.portalVines(c[0], c[1], c[2])
					r.portalLeaves(c[0], c[1], c[2])
				}
			}
		}
		return snapshot(r)
	}
	r := region()
	r.growRuinedPortal(p)
	got := snapshot(r)
	if want := replay(2, 1, 0); got != want {
		t.Fatal("growth does not visit the box z outermost, then y, then x")
	}
	if replay(1, 2, 0) == got {
		t.Fatal("y, z, x order gives the same growth; the case does not tell the orders apart")
	}
}
