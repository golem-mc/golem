package world

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/world/levelgen"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

func TestDesertWellArchaeologySurvivesSave(t *testing.T) {
	world := newGoldenTerrain()
	// A chunk corner lets the sand positions cross proto boundaries.
	p := [3]int32{-1280, 71, 128}
	for seed := range int64(20) {
		clear(world.protos.mem)
		r := newDecorRegion(world, newChunk(p[0]>>4, p[2]>>4), worldMinY, worldHeight)
		r.nx, r.nz = p[0]>>4, p[2]>>4
		r.rng.SetSeed(seed)
		if !r.placeDesertWell(p) {
			t.Fatal("well was rejected on sand")
		}
		reference := levelgen.NewWorldgenRandom(seed)
		// vanilla: water center, east, south, west, north.
		offsets := [...][2]int32{{0, 0}, {1, 0}, {0, 1}, {-1, 0}, {0, -1}}
		want := map[[3]int32]bool{}
		for depth := int32(1); depth <= 2; depth++ {
			d := offsets[reference.NextIntN(5)]
			want[[3]int32{p[0] + d[0], 70 - depth, p[2] + d[1]}] = true
		}
		if r.rng.NextLong() != reference.NextLong() {
			t.Fatal("loot seeds consumed the feature random")
		}
		for pos, proto := range world.protos.mem {
			if len(proto.entities) == 0 {
				continue
			}
			saved, err := decodeDecorProto(appendDecorProto(nil, proto))
			if err != nil {
				t.Fatal(err)
			}
			c := newChunk(pos.X, pos.Z)
			for _, e := range saved.entities {
				x, y, z := pos.X*16+e.x, worldMinY+e.y, pos.Z*16+e.z
				at := [3]int32{x, y, z}
				if !want[at] || registry.BlockOf(r.get(x, y, z)) != registry.BlockSuspiciousSand {
					t.Fatalf("unexpected brushable block at %v", at)
				}
				delete(want, at)
				e.place(c, worldMinY)
				c.SetBlock(x&15, y, z&15, registry.BlockSuspiciousSand.DefaultState())
			}
			root, err := encodeChunkNBTWith(c, nil, chunkTicks{})
			if err != nil {
				t.Fatal(err)
			}
			loaded, err := decodeChunkNBT(root)
			if err != nil {
				t.Fatal(err)
			}
			for _, e := range saved.entities {
				x, y, z := pos.X*16+e.x, worldMinY+e.y, pos.Z*16+e.z
				tag, ok := loaded.carriedBlockEntityAt(x, y, z)
				table, _ := nbt.Get[nbt.String](tag, "LootTable")
				if !ok || table != "minecraft:archaeology/desert_well" {
					t.Fatalf("missing archaeology loot at %d,%d,%d: %v", x, y, z, tag)
				}
				// BlockPos.asLong: 26 bits x, 26 bits z, 12 bits y.
				wantSeed := int64(x&0x3FFFFFF)<<38 | int64(z&0x3FFFFFF)<<12 | int64(y&0xFFF)
				gotSeed, _ := nbt.LongValue(tag["LootTableSeed"])
				if gotSeed != wantSeed {
					t.Fatalf("loot seed at %d,%d,%d = %d, want %d", x, y, z, gotSeed, wantSeed)
				}
			}
		}
		if len(want) != 0 {
			t.Fatalf("missing suspicious sand: %v", want)
		}
	}
}

func TestDesertWellRejectsUnsuitableGround(t *testing.T) {
	for _, tc := range []struct {
		name string
		z    int32
		hole bool
	}{
		{"snow", -128, false},
		{"unsupported sand", 128, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			world := newGoldenTerrain()
			r := newDecorRegion(world, newChunk(-80, tc.z>>4), worldMinY, worldHeight)
			r.nx, r.nz = -80, tc.z>>4
			if tc.hole {
				r.set(-1282, 69, tc.z+2, airState)
				r.set(-1282, 68, tc.z+2, airState)
			}
			before, n := world.changes(r)
			r.rng.SetSeed(7)
			if r.placeDesertWell([3]int32{-1280, 71, tc.z}) {
				t.Fatal("well accepted unsuitable ground")
			}
			if after, count := world.changes(r); after != before || count != n {
				t.Fatal("rejected well changed blocks")
			}
			if r.rng.NextLong() != levelgen.NewWorldgenRandom(7).NextLong() {
				t.Fatal("rejected well consumed random draws")
			}
		})
	}
}
