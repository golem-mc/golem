package world

import (
	"sync"
	"testing"
	"testing/synctest"
)

func TestDecorClaims(t *testing.T) {
	synctest.Test(t, testDecorClaims)
}

func testDecorClaims(t *testing.T) {
	var state decorState
	origin, far, edge := ChunkPos{}, ChunkPos{X: 7}, ChunkPos{X: 6}
	state.claim(origin)
	state.claim(far) // disjoint read halos can coexist
	state.release(far)
	entered, acquired := make(chan struct{}), make(chan struct{})
	go func() { close(entered); state.claim(edge); close(acquired) }()
	<-entered
	synctest.Wait()
	select {
	case <-acquired:
		t.Fatal("overlapping claim acquired")
	default:
	}
	state.release(origin)
	<-acquired
	state.release(edge)
	state.claim(origin)
	state.claim(far)
	entered, acquired = make(chan struct{}), make(chan struct{})
	go func() { close(entered); state.claimAll(); close(acquired) }()
	<-entered
	synctest.Wait()
	select {
	case <-acquired:
		t.Fatal("exclusive claim acquired with active claims")
	default:
	}
	state.release(origin)
	select {
	case <-acquired:
		t.Fatal("exclusive claim acquired before last release")
	default:
	}
	state.release(far)
	<-acquired
	state.releaseAll()
}

// This generates 36 chunks to compare two independent regions and their neighbours.
func TestOverworldConcurrentDecorationMatchesSerial(t *testing.T) {
	serial, concurrent := NewOverworldGenerator(8675309), NewOverworldGenerator(8675309)
	centres := [...]ChunkPos{{}, {X: 40}}
	var want, got [2][9]*Chunk
	generate := func(g *OverworldGenerator, centre ChunkPos, chunks *[9]*Chunk) {
		i := 0
		for dx := int32(-1); dx <= 1; dx++ {
			for dz := int32(-1); dz <= 1; dz++ {
				c := newChunk(centre.X+dx, centre.Z+dz)
				g.Generate(c)
				chunks[i] = c
				i++
			}
		}
	}
	for i, centre := range centres {
		generate(serial, centre, &want[i])
	}
	var wg sync.WaitGroup
	for i, centre := range centres {
		wg.Go(func() { generate(concurrent, centre, &got[i]) })
	}
	wg.Wait()
	for i := range centres {
		for j, expected := range want[i] {
			actual := got[i][j]
			for y := int32(worldMinY); y < worldMinY+worldHeight; y++ {
				for z := range int32(16) {
					for x := range int32(16) {
						if expected.BlockAt(x, y, z) != actual.BlockAt(x, y, z) {
							t.Fatalf("chunk (%d,%d), block (%d,%d,%d) differs", expected.x, expected.z, x, y, z)
						}
					}
				}
			}
		}
	}
}

// TestDecorProtoStoreConcurrent checks independent claims can spill and drop without losing their blocks.
func TestDecorProtoStoreConcurrent(t *testing.T) {
	var store decorProtoStore
	store.attach(t.TempDir())
	var wg sync.WaitGroup
	for i := range int32(8) {
		wg.Go(func() {
			pos := ChunkPos{X: i * 7}
			p := store.ensure(pos)
			p.set(0, 0, 0, stoneState)
			store.touch(p)
			store.spill(pos)
			p = store.lookup(pos)
			if p == nil {
				t.Error("spilled proto lost")
				return
			}
			if state, ok := p.get(0, 0, 0); !ok || state != stoneState {
				t.Error("spilled block lost")
			}
			store.drop(pos)
			if store.lookup(pos) != nil {
				t.Error("dropped proto resurrected")
			}
		})
	}
	wg.Wait()
}
