package world

import (
	"bytes"
	"fmt"
	"math/rand/v2"
	"testing"

	"github.com/golem-mc/golem/registry"
)

func TestIncrementalRelightAcrossZero(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		loaded bool
		east   uint8
	}{
		{name: "loaded", loaded: true, east: 13},
		{name: "missing"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := newTestWorld(t, NewFlatGenerator())
			source := ChunkPos{-1, 0}
			destination := ChunkPos{0, 0}
			w.Acquire(source)
			defer w.Release(source)
			if tc.loaded {
				w.Acquire(destination)
				defer w.Release(destination)
			}

			w.SetBlock(-1, 64, 8, torchState)
			w.Relight(-1, 8)

			if got := w.ChunkLoaded(destination); got != tc.loaded {
				t.Errorf("destination chunk loaded = %t, want %t", got, tc.loaded)
			}
			for _, cell := range []struct {
				x, y int32
				want uint8
			}{
				{-1, 64, 14},
				{-2, 64, 13},
				{-1, 65, 13},
				{0, 64, tc.east},
				{-16, 64, 0}, // x=0 has the same local x in the destination chunk.
			} {
				if got := w.BlockLightAt(cell.x, cell.y, 8); got != cell.want {
					t.Errorf("block light at (%d,%d,8) = %d, want %d", cell.x, cell.y, got, cell.want)
				}
			}
		})
	}
}

func TestIncrementalRelightSkySourcesFromTheChangedCell(t *testing.T) {
	t.Parallel()
	const top = worldMinY + worldHeight
	floors := []int32{worldMinY, worldMinY + 15, worldMinY + 16, 79, 80, 81, top - 17, top - 16, top - 1, top}
	for _, oldFloor := range floors {
		for _, newFloor := range floors {
			if oldFloor == worldMinY && newFloor == worldMinY {
				continue
			}
			for _, overlay := range []bool{false, true} {
				t.Run(fmt.Sprintf("%d_to_%d/overlay_%t", oldFloor, newFloor, overlay), func(t *testing.T) {
					c := newChunk(0, 0)
					if newFloor > worldMinY {
						c.SetBlock(8, newFloor-1, 8, stoneState)
					}
					for y := oldFloor; y < top; y++ {
						c.lightSet(skyLayer, 8, y, 8, 15)
					}
					ctx := &lightCtx{alone: c}
					if overlay {
						ctx.cow = make(map[*Chunk]*lightOverlay)
						ctx.layer(c, skyLayer)
						// The scan must see the overlay, even if the chunk differs.
						c.skyLight = [lightSections]*[lightArrayLen]byte{}
					}
					var dq, iq lightQueue
					ctx.updateSkySources(8, max(oldFloor, newFloor)-1, 8, &dq, &iq)
					for y := int32(worldMinY); y < top; y++ {
						var want uint8
						if y >= newFloor {
							want = 15
						}
						if got := ctx.getLight(skyLayer, 8, y, 8); got != want {
							t.Fatalf("sky light at y=%d = %d, want %d", y, got, want)
						}
					}
				})
			}
		}
	}
	t.Run("no source above the changed cell", func(t *testing.T) {
		c := newChunk(0, 0)
		c.SetBlock(8, 100, 8, stoneState)
		c.SetBlock(8, 80, 8, airState)
		for y := int32(101); y < top; y++ {
			c.lightSet(skyLayer, 8, y, 8, 15)
		}
		ctx := &lightCtx{alone: c}
		var dq, iq lightQueue
		ctx.updateSkySources(8, 80, 8, &dq, &iq)
		for y := int32(worldMinY); y <= 100; y++ {
			if got := ctx.getLight(skyLayer, 8, y, 8); got != 0 {
				t.Fatalf("sky light at y=%d = %d, want 0", y, got)
			}
		}
		if len(dq.entries) != 0 || len(iq.entries) != 0 {
			t.Fatalf("queued %d decreases and %d increases", len(dq.entries), len(iq.entries))
		}
	})
}

// lightWorld is a flat world with the 3x3 chunks around the origin loaded.
func lightWorld(t *testing.T) *World {
	t.Helper()
	w := newTestWorld(t, NewFlatGenerator())
	for cx := int32(-1); cx <= 1; cx++ {
		for cz := int32(-1); cz <= 1; cz++ {
			_, _ = w.Acquire(ChunkPos{cx, cz})
		}
	}
	return w
}

// assertSameLight fails the test where the two worlds' light differs.
func assertSameLight(t *testing.T, got, want *World) {
	t.Helper()
	got.mu.RLock()
	defer got.mu.RUnlock()
	want.mu.RLock()
	defer want.mu.RUnlock()
	for pos, l := range got.chunks {
		g, o := l.chunk, want.chunks[pos].chunk
		if !bytes.Equal(g.encodeLight(), o.encodeLight()) {
			for y := int32(worldMinY); y < worldMinY+worldHeight; y++ {
				for x := range int32(16) {
					for z := range int32(16) {
						for layer, name := range []string{"sky", "block"} {
							a := g.lightGet([2]*[lightSections]*[lightArrayLen]byte{&g.skyLight, &g.blockLight}[layer], x, y, z)
							b := o.lightGet([2]*[lightSections]*[lightArrayLen]byte{&o.skyLight, &o.blockLight}[layer], x, y, z)
							if a != b {
								t.Fatalf("chunk %v %s light at (%d,%d,%d) = %d, a full relight gives %d", pos, name, x, y, z, a, b)
							}
						}
					}
				}
			}
			t.Fatalf("chunk %v: light differs from a full relight", pos)
		}
		if !bytes.Equal(g.lightForm(), g.encodeLight()) || !bytes.Equal(g.chunkLightForm(), g.encodeChunkLight()) {
			t.Fatalf("chunk %v: the encoded light is not the light it holds", pos)
		}
	}
}

// fullRelight recomputes all the light of the world from its blocks.
func fullRelight(w *World) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.relightBox(-16, -16, 31, 31)
}

// TestIncrementalRelightMatchesAFullRelight edits the world at random,
// relighting after every few writes the incremental way, and checks the light
// ends where a recompute of the whole world from its blocks puts it.
func TestIncrementalRelightMatchesAFullRelight(t *testing.T) {
	t.Parallel()
	blocks := []uint16{
		stoneState, torchState, airState, airState, airState,
		registry.BlockStates["minecraft:glass"],
		registry.BlockStates["minecraft:oak_leaves"],
		registry.BlockStates["minecraft:oak_fence"],
		registry.BlockStates["minecraft:water"],
		registry.BlockStates["minecraft:glowstone"],
		registry.BlockStates["minecraft:oak_slab"],
		registry.MustState("minecraft:oak_slab", map[string]string{"type": "top"}),
		registry.MustState("minecraft:lava", nil),
	}
	for seed := range uint64(4) {
		t.Run("", func(t *testing.T) {
			t.Parallel()
			w, ref := lightWorld(t), lightWorld(t)
			rng := rand.New(rand.NewPCG(seed, 11))
			for range 120 {
				minX, minZ, maxX, maxZ := int32(1<<30), int32(1<<30), int32(-1<<30), int32(-1<<30)
				for range 1 + rng.IntN(4) {
					x, z := rng.Int32N(48)-16, rng.Int32N(48)-16
					y := 40 + rng.Int32N(40)
					state := blocks[rng.IntN(len(blocks))]
					switch rng.IntN(6) {
					case 0: // a shaft down through the ground
						for dy := range int32(30) {
							w.SetBlock(x, 63-dy, z, airState)
							ref.SetBlock(x, 63-dy, z, airState)
						}
					case 1: // a roof
						for dx := range int32(3) {
							for dz := range int32(3) {
								w.SetBlock(x+dx, y+3, z+dz, stoneState)
								ref.SetBlock(x+dx, y+3, z+dz, stoneState)
							}
						}
					}
					w.SetBlock(x, y, z, state)
					ref.SetBlock(x, y, z, state)
					minX, maxX = min(minX, x-2), max(maxX, x+2)
					minZ, maxZ = min(minZ, z-2), max(maxZ, z+2)
				}
				w.RelightAreaAsync(minX, minZ, maxX, maxZ)
				<-w.RelightAreaAsync(minX, minZ, maxX, maxZ)
			}
			fullRelight(ref)
			assertSameLight(t, w, ref)
		})
	}
}
