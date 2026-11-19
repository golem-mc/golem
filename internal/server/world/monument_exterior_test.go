package world

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/golem-mc/golem/internal/server/world/levelgen"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

var monumentTestSea = [...]int32{63, 32, 70, 48}

type monumentTerrain struct {
	*goldenTerrain
	mode int
}

// Matches the Java oracle's synthetic columns, independently of the placer.
func (t *monumentTerrain) at(x, y, z int32) uint16 {
	if t.mode == 3 {
		switch floorMod(x+y+2*z, 10) {
		case 0:
			state, _ := registry.WithInt(registry.BlockWater.DefaultState(), registry.PropLevel, 7)
			return state
		case 1:
			return registry.BlockIce.DefaultState()
		case 2:
			return registry.BlockPackedIce.DefaultState()
		case 3:
			return registry.BlockBlueIce.DefaultState()
		case 4:
			return registry.BlockStone.DefaultState()
		case 5:
			return registry.BlockLava.DefaultState()
		case 6:
			return airState
		default:
			return registry.BlockWater.DefaultState()
		}
	}
	floor := int32(25) + floorMod(x*3+z*5, 11)
	if t.mode == 2 {
		floor = 60 + floorMod(x+z, 9)
	}
	if t.mode == 1 {
		floor = -60 + floorMod(x+z, 5)
		if floorMod(x+z, 7) == 0 {
			floor = -65
		}
	}
	if y <= floor {
		return registry.BlockStone.DefaultState()
	}
	stripe := floorMod(x+2*z, 9)
	if y == monumentTestSea[t.mode] {
		switch stripe {
		case 0:
			return registry.BlockIce.DefaultState()
		case 1:
			return registry.BlockPackedIce.DefaultState()
		case 2:
			return registry.BlockBlueIce.DefaultState()
		case 3:
			state, _ := registry.WithInt(registry.BlockWater.DefaultState(), registry.PropLevel, 7)
			return state
		default:
			return airState
		}
	}
	if y == 37 {
		switch stripe {
		case 0:
			return registry.BlockGlowLichen.DefaultState()
		case 1:
			return registry.BlockSeagrass.DefaultState()
		case 2:
			return registry.BlockTallSeagrass.DefaultState()
		case 3:
			return registry.BlockLava.DefaultState()
		case 4:
			return registry.BlockKelp.DefaultState()
		default:
			return registry.BlockWater.DefaultState()
		}
	}
	if y < monumentTestSea[t.mode] {
		return registry.BlockWater.DefaultState()
	}
	return airState
}

var monumentBaseTerrain sync.Map // [mode, cx, cz] -> immutable *shapedTerrain

func (t *monumentTerrain) cachedTerrain(cx, cz int32) *shapedTerrain {
	pos := ChunkPos{cx, cz}
	if c := t.chunks[pos]; c != nil {
		return c
	}
	key := [3]int32{int32(t.mode), cx, cz}
	if cached, ok := monumentBaseTerrain.Load(key); ok {
		s := cached.(*shapedTerrain)
		t.chunks[pos] = s
		return s
	}
	c := newChunk(cx, cz)
	for y := int32(-64); y < 80; y++ {
		for x := range int32(16) {
			for z := range int32(16) {
				c.SetBlock(x, y, z, t.at(cx*16+x, y, cz*16+z))
			}
		}
	}
	s := &shapedTerrain{}
	s.keep(c, false)
	monumentBaseTerrain.Store(key, s)
	t.chunks[pos] = s
	return s
}

func (t *monumentTerrain) terrainOf(cx, cz int32, _ *Chunk) *shapedTerrain {
	return t.cachedTerrain(cx, cz)
}

func testMonumentBlockGolden(t *testing.T, path string, rooms bool) {
	t.Helper()
	wantCases := 12
	minY, maxY := int32(-64), int32(80)
	if rooms {
		wantCases = 128
		minY, maxY = 32, 64
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	listed := 0
	seedOrdinals := map[int64]int{}
	for line := range strings.SplitSeq(string(data), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		var mode, dir, wantChanges, wantTicks int
		var wantStates, wantFluids, wantRandom, wantSpawns string
		var wantElders int
		seed := int64(12345)
		var err error
		if rooms {
			_, err = fmt.Sscanf(line, "rooms %d %d %d %d %s %d %s %s %d %s", &seed, &mode, &dir, &wantChanges, &wantStates, &wantTicks, &wantFluids, &wantRandom, &wantElders, &wantSpawns)
		} else {
			_, err = fmt.Sscanf(line, "exterior %d %d %d %s %d %s", &mode, &dir, &wantChanges, &wantStates, &wantTicks, &wantFluids)
		}
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := seedOrdinals[seed]; !ok {
			seedOrdinals[seed] = len(seedOrdinals)
		}
		// A Latin-square subset of the seed x terrain x direction grid: each
		// seed's room graph runs in 4 of its 16 (terrain, direction) pairs, and
		// across the seeds every pair runs, as does every terrain and direction.
		// The exterior's 12 pairs keep every other one.
		if rooms && (mode+dir)%4 != seedOrdinals[seed]%4 || !rooms && (mode+dir)%2 != 0 {
			listed++
			continue
		}
		t.Run(fmt.Sprintf("seed%d/terrain%d/dir%d", seed, mode, dir), func(t *testing.T) {
			t.Parallel()
			src := &monumentTerrain{newGoldenTerrain(), mode}
			m := levelgen.GenerateMonument(levelgen.NewLegacyRandom(seed), -19, 7, dir)
			frame := levelgen.PieceFrame{BB: m.BB, Dir: dir}
			ticks := map[[3]int32]bool{}
			elders := map[[3]int32]bool{}
			regions := map[ChunkPos]*decorRegion{}
			var randoms []byte
			for cz := int32(-1); cz <= 5; cz++ {
				for cx := int32(-3); cx <= 3; cx++ {
					r := newDecorRegion(src, newChunk(cx, cz), worldMinY, worldHeight)
					r.nx, r.nz = cx, cz
					r.bindProtos(ChunkPos{cx, cz})
					clip := jbox{cx * 16, -64, cz * 16, cx*16 + 15, 79, cz*16 + 15}
					if mode == 3 {
						clip.minY, clip.maxY = 43, 49
					}
					p := newMonumentPlacer(r, clip, frame, monumentTestSea[mode])
					if rooms {
						p.rng = levelgen.NewLegacyRandom(seed + int64(cx)*31 + int64(cz)*17)
						for _, piece := range m.Pieces {
							bb := piece.BB
							bounds := jbox{int32(bb.MinX), int32(bb.MinY), int32(bb.MinZ), int32(bb.MaxX), int32(bb.MaxY), int32(bb.MaxZ)}
							if bounds.intersects(clip) {
								p.frame = levelgen.PieceFrame{BB: bb, Dir: dir}
								p.room(piece)
							}
						}
						randoms = binary.BigEndian.AppendUint32(randoms, uint32(p.rng.NextInt()))
					} else {
						p.exterior()
					}
					regions[ChunkPos{cx, cz}] = r
					if ps := r.protos[len(r.protos)/2]; ps != nil {
						for _, e := range ps.entities {
							tag := e.tag
							if tag["id"] != nbt.String("minecraft:elder_guardian") || !nbt.BoolField(tag, "PersistenceRequired") {
								t.Fatalf("invalid elder: %v", tag)
							}
							if _, ok := nbt.UUIDField(tag, "UUID"); !ok {
								t.Fatal("missing elder UUID")
							}
							pos := tag["Pos"].(nbt.List).Items
							key := [3]int32{cx*16 + e.x, e.y + r.minY, cz*16 + e.z}
							if pos[0] != nbt.Double(float64(key[0])+0.5) || pos[1] != nbt.Double(key[1]) || pos[2] != nbt.Double(float64(key[2])+0.5) {
								t.Fatalf("elder position differs from owning proto: %v", pos)
							}
							if elders[key] {
								t.Fatal("duplicate elder")
							}
							elders[key] = true
						}
						for _, pos := range ps.sources {
							ticks[[3]int32{cx*16 + pos[0], pos[1] + worldMinY, cz*16 + pos[2]}] = true
						}
					}
				}
			}
			states := sha256.New()
			var fluids, spawns []byte
			changes := 0
			if rooms {
				if got := fmt.Sprintf("%x", sha256.Sum256(randoms)); got != wantRandom {
					t.Errorf("random digest %s, vanilla %s", got, wantRandom)
				}
			}
			for x := int32(-48); x < 64; x++ {
				for y := minY; y < maxY; y++ {
					for z := int32(-16); z < 96; z++ {
						r := regions[ChunkPos{x >> 4, z >> 4}]
						state := r.get(x, y, z)
						states.Write(goldenStateBytes()[state])
						if state != src.at(x, y, z) {
							changes++
						}
						v := byte(0)
						if ticks[[3]int32{x, y, z}] {
							v = 1
						}
						fluids = append(fluids, v)
						if elders[[3]int32{x, y, z}] {
							spawns = append(spawns, 1)
						} else {
							spawns = append(spawns, 0)
						}
					}
				}
			}
			if rooms && (len(elders) != wantElders || fmt.Sprintf("%x", sha256.Sum256(spawns)) != wantSpawns) {
				t.Errorf("elder positions differ: got %v; vanilla count %d digest %s", elders, wantElders, wantSpawns)
			}
			if changes != wantChanges {
				t.Errorf("%d changed blocks, vanilla %d", changes, wantChanges)
			}
			if got := fmt.Sprintf("%x", states.Sum(nil)); got != wantStates {
				t.Errorf("block digest %s, vanilla %s", got, wantStates)
			}
			if got := fmt.Sprintf("%x", sha256.Sum256(fluids)); got != wantFluids || len(ticks) != wantTicks {
				t.Errorf("%d fluid ticks, digest %s; vanilla %d, %s", len(ticks), got, wantTicks, wantFluids)
			}
		})
		listed++
	}
	if listed != wantCases {
		t.Fatalf("listed %d cases, want %d", listed, wantCases)
	}
}

func BenchmarkMonumentExterior(b *testing.B) {
	src := &monumentTerrain{newGoldenTerrain(), 0}
	m := levelgen.GenerateMonument(levelgen.NewLegacyRandom(12345), -19, 7, 0)
	r := newDecorRegion(src, newChunk(0, 2), worldMinY, worldHeight)
	r.nx, r.nz = 0, 2
	p := newMonumentPlacer(r, jbox{0, -64, 32, 15, 79, 47}, levelgen.PieceFrame{BB: m.BB, Dir: 0}, 63)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		clear(r.protos[:])
		clear(src.protos.mem)
		p.exterior()
	}
}

func TestMonumentExteriorClipsToChunk(t *testing.T) {
	for dir := range 4 {
		src := &monumentTerrain{newGoldenTerrain(), 2}
		m := levelgen.GenerateMonument(levelgen.NewLegacyRandom(0), -19, 7, dir)
		r := newDecorRegion(src, newChunk(0, 1), worldMinY, worldHeight)
		r.nx, r.nz = 0, 1
		clip := jbox{0, 45, 16, 15, 50, 31}
		p := newMonumentPlacer(r, clip, levelgen.PieceFrame{BB: m.BB, Dir: dir}, 70)
		p.exterior()
		writes := 0
		for i, ps := range r.protos {
			if ps == nil {
				continue
			}
			cx, cz := int32(i%decorRegionSpan)-2, int32(i/decorRegionSpan)-1
			for section, blocks := range ps.blocks {
				if blocks == nil {
					continue
				}
				for index, state := range blocks {
					if state == 0 {
						continue
					}
					x, y, z := cx*16+int32(index&15), worldMinY+int32(section*16+index>>8), cz*16+int32(index>>4&15)
					if !clip.inside(x, y, z) {
						t.Fatalf("dir %d: write outside clip at %d,%d,%d", dir, x, y, z)
					}
					writes++
				}
			}
		}
		if writes == 0 {
			t.Fatalf("dir %d: no clipped writes", dir)
		}
	}
}

func BenchmarkMonumentRooms(b *testing.B) {
	src := &monumentTerrain{newGoldenTerrain(), 0}
	m := levelgen.GenerateMonument(levelgen.NewLegacyRandom(18), -19, 7, 0)
	r := newDecorRegion(src, newChunk(0, 1), worldMinY, worldHeight)
	r.nx, r.nz = 0, 1
	clip := jbox{0, -64, 16, 15, 79, 31}
	p := newMonumentPlacer(r, clip, levelgen.PieceFrame{}, 63)
	rng := levelgen.NewLegacyRandom(0)
	p.rng = rng
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		clear(r.protos[:])
		clear(src.protos.mem)
		rng.SetSeed(0)
		for _, piece := range m.Pieces {
			bb := piece.BB
			bounds := jbox{int32(bb.MinX), int32(bb.MinY), int32(bb.MinZ), int32(bb.MaxX), int32(bb.MaxY), int32(bb.MaxZ)}
			if bounds.intersects(clip) {
				p.frame = levelgen.PieceFrame{BB: bb, Dir: 0}
				p.room(piece)
			}
		}
	}
}
