package world

import (
	"bytes"
	"fmt"
	"slices"
	"sync"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

func TestChunkPacketCacheInvalidation(t *testing.T) {
	for _, dimension := range []string{DimensionName, NetherName, EndName} {
		t.Run(dimension, func(t *testing.T) {
			var w *World
			if dimension == DimensionName {
				var err error
				w, err = NewWorld(t.TempDir(), NewFlatGenerator())
				if err != nil {
					t.Fatal(err)
				}
			} else {
				w = NewDimensionWorld(t.TempDir(), dimension == NetherName, NewFlatGenerator())
			}
			defer w.Close()
			c, _ := w.Acquire(ChunkPos{})
			var previous *protocol.CachedPacket
			var previousBytes []byte
			check := func(what string, changed bool) {
				t.Helper()
				got := w.CachedChunkPacket(c)
				if previous != nil {
					if (got != previous) != changed {
						t.Fatalf("%s: changed cache = %v, want %v", what, got != previous, changed)
					}
					if !bytes.Equal(previous.Data(), previousBytes) {
						t.Fatalf("%s: old packet was modified", what)
					}
				}
				if !bytes.Equal(got.Data(), chunkPacketReference(w, c)) {
					t.Fatalf("%s: cached bytes differ from reference", what)
				}
				if w.CachedChunkPacket(c) != got {
					t.Fatalf("%s: unchanged chunk rebuilt its packet", what)
				}
				previous, previousBytes = got, slices.Clone(got.Data())
			}
			check("initial", false)
			w.SetBlock(2, 70, 3, registry.BlockStates["minecraft:stone"])
			check("terrain and heightmap", true)
			w.SetBlock(2, 70, 3, registry.BlockStates["minecraft:stone"])
			check("same block", false)
			w.SetBlock(4, 70, 5, registry.BlockStates["minecraft:oak_sign"])
			be := w.EnsureBlockEntity(4, 70, 5, KindSign)
			check("new entity", true)
			be.Use(func(be *BlockEntity) { be.SignFront.Messages[0] = "Edited without a terrain write" })
			check("update tag", true)
			be.Use(func(be *BlockEntity) { be.SignFront.Messages[0] = "edited without a terrain write" })
			check("same-size update tag", true)
			w.SetBlock(6, 70, 7, registry.BlockStates["minecraft:chest"])
			chest := w.EnsureBlockEntity(6, 70, 7, KindChest)
			check("container", true)
			chest.Use(func(be *BlockEntity) { be.Put(0, protocol.ItemStack{ItemID: int32(registry.ItemDiamond), Count: 1}) })
			check("container contents stay off the wire", false)
			w.mu.Lock()
			c.carriedBlockEntities = nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{nbt.Compound{
				"id": nbt.String("minecraft:banner"), "x": nbt.Int(8), "y": nbt.Int(70), "z": nbt.Int(9),
				"CustomName": nbt.String(`"Carried"`),
			}}}
			w.mu.Unlock()
			check("carried entity", true)
			w.mu.Lock()
			c.carriedBlockEntities = nbt.List{Elem: nbt.TypeCompound}
			w.mu.Unlock()
			check("removed carried entity", true)
			// Model a neighbouring relight publishing a fresh immutable array,
			// without changing this chunk's blocks or heightmaps.
			w.mu.Lock()
			c.blockLight[5] = new([lightArrayLen]byte)
			c.blockLight[5][0] = 15
			c.invalidateLightForms()
			c.lightGen++
			w.mu.Unlock()
			check("light only", true)
			// The wire entries sit in a 16-entry stack buffer: cross its end.
			for i := range int32(15) {
				x, z := i%8, 10+i/8
				w.SetBlock(x, 71, z, registry.BlockStates["minecraft:chest"])
				w.EnsureBlockEntity(x, 71, z, KindChest)
				if n := i + 3; n == 16 || n == 17 || n == 18 {
					check(fmt.Sprintf("%d entities", n), true)
				}
			}
			be.Use(func(be *BlockEntity) { be.SignFront.Messages[0] = "Edited past the buffer" })
			check("update tag past 16 entities", true)

			var readers sync.WaitGroup
			for range 10 {
				readers.Go(func() {
					if w.CachedChunkPacket(c) != previous {
						t.Error("concurrent reader rebuilt the packet")
					}
				})
			}
			readers.Wait()
		})
	}
}

func TestChunkPacketCacheHitAllocations(t *testing.T) {
	w, err := NewWorld(t.TempDir(), NewFlatGenerator())
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	c, _ := w.Acquire(ChunkPos{})
	check := func(name string, maxAllocs float64) {
		t.Run(name, func(t *testing.T) {
			packet := w.CachedChunkPacket(c)
			allocs := testing.AllocsPerRun(100, func() {
				if w.CachedChunkPacket(c) != packet {
					t.Fatal("unchanged chunk rebuilt its packet")
				}
			})
			if allocs > maxAllocs {
				t.Errorf("cache hit allocated %.0f times, want at most %.0f", allocs, maxAllocs)
			}
		})
	}
	check("0blockentities", 0)
	for i := range int32(10) {
		x, z := i%4, i/4
		w.SetBlock(x, GroundLevel, z, registry.BlockChest.DefaultState())
		w.EnsureBlockEntity(x, GroundLevel, z, KindChest)
	}
	check("10blockentities", 0)
}
