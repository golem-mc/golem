package world

import (
	"testing"
	"testing/synctest"
)

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
