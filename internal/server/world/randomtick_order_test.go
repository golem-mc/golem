package world

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/golem-mc/golem/registry"
)

// TestEveryRandomlyTickingStateHasAVisit checks that each state a die can
// land on names a random tick, so no landed die is spent on a block the
// dispatch does not know.
func TestEveryRandomlyTickingStateHasAVisit(t *testing.T) {
	for s := range uint16(registry.StateCount) {
		if randomlyTicking(s) && randomTickKinds[registry.BlockOf(s)] == rtNone {
			t.Errorf("%s (state %d) random-ticks with no visit", registry.BlockOf(s).Name(), s)
		}
	}
}

// The random tick pass, order-side: the server gathers the chunks from a map,
// whose iteration order Go randomizes, and the dice are one stream shared by
// every chunk. The engine therefore has to throw them in an order of its own,
// or the same world and the same seed do not replay.

// physicsWorldForOrder builds a flat world with the chunks around the origin
// held, the newPhysicsWorld layout without its one t.Parallel per test.
func physicsWorldForOrder(t *testing.T) (*World, *Physics) {
	t.Helper()
	w := newTestWorld(t, NewFlatGenerator())
	t.Cleanup(func() { w.Close() })
	for cx := int32(-1); cx <= 2; cx++ {
		for cz := int32(-1); cz <= 2; cz++ {
			w.Acquire(ChunkPos{X: cx, Z: cz})
		}
	}
	return w, NewPhysics(w)
}

// TestRandomTickThrowsItsDiceInAFixedChunkOrder checks the engine's replay
// promise: two worlds of the same leaves, seeded alike and ticked once,
// produce the same changes whichever order the caller hands the two chunks
// in. The same order twice is not enough - the caller's list is the one that
// varies.
func TestRandomTickThrowsItsDiceInAFixedChunkOrder(t *testing.T) {
	t.Parallel()
	w1, p1 := physicsWorldForOrder(t)
	w2, p2 := physicsWorldForOrder(t)
	leaf := leafState(t, map[string]string{"persistent": "false", "distance": "7"})

	// A full section of leaves in each of two chunks, so every die lands on
	// a leaf and the sampled cell, which the order decides, is the change.
	for x := range int32(32) {
		for y := int32(GroundLevel); y < GroundLevel+16; y++ {
			for z := range int32(16) {
				setBlock(t, w1, p1, x, y, z, leaf)
				setBlock(t, w2, p2, x, y, z, leaf)
			}
		}
	}

	for seed := range int64(4) {
		p1.SetSeed(seed)
		c1, _ := p1.RandomTick(1, []ChunkPos{{X: 0, Z: 0}, {X: 1, Z: 0}}, 1, 1)
		p2.SetSeed(seed)
		c2, _ := p2.RandomTick(1, []ChunkPos{{X: 1, Z: 0}, {X: 0, Z: 0}}, 1, 1)
		if !reflect.DeepEqual(c1, c2) {
			t.Fatalf("seed %d: the chunks in one order changed %v, the reverse %v", seed, c1, c2)
		}
	}
}

// Snapshot a seeded pass across uniform and paletted sections. The first
// decays allocate a palette and cross its four-bit width respectively.
func randomTickPaletteOutcome(t *testing.T, speed int) []byte {
	t.Helper()
	w := newTestWorld(t, NewFlatGenerator())
	t.Cleanup(func() { w.Close() })
	w.SetClockTime(OverworldClock, 2000)
	leaf := leafState(t, map[string]string{"persistent": "false", "distance": "7"})
	chunks := []ChunkPos{{X: 1}, {X: 0}, {X: -1}}
	for _, pos := range chunks {
		c, _ := w.Acquire(pos)
		state := leaf
		if pos.X == 1 {
			state = registry.BlockPottedOpenEyeblossom.DefaultState()
		}
		// setAll is only legal before sharing a chunk; the test owns this world
		// and holds the writer lock while constructing the section fixture.
		w.mu.Lock()
		sec := &c.sections[(96-worldMinY)/sectionSize]
		sec.setAll(state)
		if pos.X == 0 {
			for i, b := range []registry.Block{registry.BlockStone, registry.BlockDirt, registry.BlockGranite, registry.BlockDiorite, registry.BlockAndesite, registry.BlockSand, registry.BlockGravel, registry.BlockBedrock, registry.BlockObsidian, registry.BlockCobblestone, registry.BlockBricks, registry.BlockGlass, registry.BlockGoldBlock, registry.BlockIronBlock, registry.BlockDiamondBlock} {
				sec.set(i, b.DefaultState(), false)
			}
			if sec.blocks.Load().bits != 4 {
				t.Fatal("fixture needs a four-bit palette")
			}
		}
		w.mu.Unlock()
	}
	p := NewPhysics(w)
	p.SetSeed(123)
	changes, decays := p.RandomTick(1, chunks, speed, 1)
	effects := p.TakeRandomEffects()
	if len(changes) == 0 || len(decays) == 0 || len(effects) == 0 {
		t.Fatal("fixture did not exercise changes, decays and effects")
	}
	w.mu.RLock()
	if w.chunks[ChunkPos{}].chunk.sections[(96-worldMinY)/sectionSize].blocks.Load().bits != 5 {
		t.Fatal("visits did not widen the palette")
	}
	w.mu.RUnlock()
	hash := sha256.New()
	for _, cx := range []int32{-1, 0, 1} {
		for y := int32(96); y < 112; y++ {
			for z := range int32(16) {
				for x := range int32(16) {
					s := w.BlockAt(cx*16+x, y, z)
					hash.Write([]byte{byte(s), byte(s >> 8)})
				}
			}
		}
	}
	out, err := json.Marshal(struct {
		Changes []BlockChange
		Decays  []Decay
		Effects []Effect
		Blocks  string
		Next    [4]int64
	}{changes, decays, effects, hex.EncodeToString(hash.Sum(nil)), [4]int64{p.rng.Int63(), p.rng.Int63(), p.rng.Int63(), p.rng.Int63()}})
	if err != nil {
		t.Fatal(err)
	}
	return dropFalseSilent(out)
}

// dropFalseSilent removes the default Silent field so the snapshots keep the
// shape they were recorded in; a true Silent still shows up and fails.
func dropFalseSilent(b []byte) []byte {
	return bytes.ReplaceAll(b, []byte(`"Silent":false,`), nil)
}

// A vine callback mutates the live section while RandomTick has dropped the
// read lock. Predict only position draws; visits below consume no extra dice.
func randomTickTransitionOutcome(t *testing.T) []byte {
	t.Helper()
	var outcomes []json.RawMessage
	for _, mode := range []string{"empty", "full", "removed", "drained"} {
		w := newTestWorld(t, NewFlatGenerator())
		t.Cleanup(func() { w.Close() })
		c, _ := w.Acquire(ChunkPos{})
		for i := range c.sections {
			c.sections[i].setAll(airState)
		}
		const speed = 8
		predict := NewPhysics(w)
		predict.SetSeed(6532)
		for range speed {
			if predict.rng.Intn(precipitationDie) == 0 {
				predict.blockRandomPos(0, 0, 0)
			}
		}
		var dice [speed][3]int32
		for i := range dice {
			x, y, z := predict.blockRandomPos(0, 96, 0)
			dice[i] = [3]int32{x, y, z}
		}
		sec := &c.sections[(96-worldMinY)/16]
		vine := registry.BlockVine.DefaultState()
		if mode == "full" {
			sec.setAll(vine)
		} else {
			q := dice[0]
			c.SetBlock(q[0], q[1], q[2], vine)
		}
		p := NewPhysics(w)
		p.SetSeed(6532)
		visits := 0
		p.SetVineSpreadRule(func() bool {
			visits++
			if visits != 1 {
				t.Fatal("unexpected extra vine visit")
			}
			w.mu.Lock()
			defer w.mu.Unlock()
			if mode == "removed" {
				delete(w.chunks, ChunkPos{})
				return false
			}
			for i := range blocksPerSection {
				sec.set(i, airState, true)
			}
			for i := range blocksPerSection {
				sec.set(i, vine, true)
			}
			for i := range blocksPerSection {
				sec.set(i, airState, true)
			}
			if mode == "drained" {
				return false
			}
			q := dice[1]
			c.SetBlock(q[0], q[1], q[2], leafState(t, map[string]string{"persistent": "false", "distance": "7"}))
			q = dice[2]
			c.SetBlock(q[0], q[1], q[2], registry.BlockPottedOpenEyeblossom.DefaultState())
			return false
		})
		w.SetClockTime(OverworldClock, 2000)
		changes, decays := p.RandomTick(1, []ChunkPos{{}}, speed, 1)
		if visits != 1 {
			t.Fatal("first die missed callback")
		}
		var blocks [blocksPerSection]uint16
		for i := range blocks {
			blocks[i] = c.sections[(96-worldMinY)/16].at(i)
		}
		digest := sha256.Sum256(func() []byte { b, _ := json.Marshal(blocks); return b }())
		outcome, err := json.Marshal(struct {
			Changes []BlockChange
			Decays  []Decay
			Effects []Effect
			Blocks  string
			Next    [4]int64
		}{changes, decays, p.TakeRandomEffects(), hex.EncodeToString(digest[:]), [4]int64{p.rng.Int63(), p.rng.Int63(), p.rng.Int63(), p.rng.Int63()}})
		if err != nil {
			t.Fatal(err)
		}
		outcomes = append(outcomes, outcome)
	}
	b, err := json.Marshal(outcomes)
	b = dropFalseSilent(b)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
