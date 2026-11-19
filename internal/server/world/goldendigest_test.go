package world

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sync"
	"testing"

	"github.com/golem-mc/golem/registry"
)

var goldenStateText = sync.OnceValue(func() [][]byte {
	text := make([][]byte, 1<<16)
	unknown := []byte("[]\n")
	for state := range text {
		name, props, ok := registry.BlockStateName(uint16(state))
		text[state] = unknown
		if ok {
			text[state] = []byte(name + "[" + props + "]\n")
		}
	}
	return text
})

// chunkBlockDigest keeps the oracle's y/z/x order and exact state text.
func chunkBlockDigest(c *Chunk, minY, maxY int32) string {
	h := sha256.New()
	text := goldenStateText()
	for y := minY; y < maxY; y++ {
		for z := range int32(sectionSize) {
			for x := range int32(sectionSize) {
				h.Write(text[c.BlockAt(x, y, z)])
			}
		}
	}
	return hex.EncodeToString(h.Sum(nil))
}

func TestChunkGoldenDigestMatchesStateText(t *testing.T) {
	t.Parallel()
	c := newChunk(0, 0)
	for state := range uint16(registry.StateCount) {
		c.SetBlock(int32(state&15), worldMinY+int32(state>>8), int32(state>>4&15), state)
	}
	h := sha256.New()
	for y := int32(worldMinY); y < worldMinY+worldHeight; y++ {
		for z := range int32(sectionSize) {
			for x := range int32(sectionSize) {
				name, props, _ := registry.BlockStateName(c.BlockAt(x, y, z))
				fmt.Fprintf(h, "%s[%s]\n", name, props)
			}
		}
	}
	if got, want := chunkBlockDigest(c, worldMinY, worldMinY+worldHeight), hex.EncodeToString(h.Sum(nil)); got != want {
		t.Fatalf("digest %s, want unchanged oracle text's %s", got, want)
	}
}

func BenchmarkChunkBlockDigest(b *testing.B) {
	c := flatChunk(0, 0)
	chunkBlockDigest(c, 0, netherTop)
	for b.Loop() {
		chunkBlockDigest(c, 0, netherTop)
	}
}
