package world

import (
	"fmt"
	"os"
	"strings"
	"testing"
)

// recordedNetherRange diagnoses a scheduler-model disagreement independently
// of block rules, using the FEATURES order saved with vanilla's chunk digests.
func recordedNetherRange(t *testing.T, r netherDigestRange) map[ChunkPos]*Chunk {
	t.Helper()
	data, err := os.ReadFile("testdata/nether_recorded_orders.txt")
	if err != nil {
		t.Fatal(err)
	}
	for line := range strings.SplitSeq(string(data), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Fields(line)
		var key netherDigestRange
		if _, err := fmt.Sscan(strings.Join(f[:5], " "), &key.seed, &key.x0, &key.z0, &key.x1, &key.z1); err != nil {
			t.Fatal(err)
		}
		if key != r {
			continue
		}
		var seq []ChunkPos
		for _, s := range f[5:] {
			var p ChunkPos
			if _, err := fmt.Sscanf(s, "%d,%d", &p.X, &p.Z); err != nil {
				t.Fatal(err)
			}
			seq = append(seq, p)
		}
		g := NewNetherGenerator(r.seed)
		g.DecorateInOrder(seq)
		out := map[ChunkPos]*Chunk{}
		for x := r.x0; x <= r.x1; x++ {
			for z := r.z0; z <= r.z1; z++ {
				c := newChunk(x, z)
				g.Generate(c)
				out[ChunkPos{X: x, Z: z}] = c
			}
		}
		return out
	}
	t.Fatalf("no recorded FEATURES order for %v", r)
	return nil
}
