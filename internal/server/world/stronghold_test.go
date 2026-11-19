package world

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// TestStrongholdPieces compares the pieces of two strongholds with the ones a
// vanilla server saved in its chunks (testdata/stronghold_pieces.txt:
// each piece's id, BB, O and GD).
func TestStrongholdPieces(t *testing.T) {
	data, err := os.ReadFile("testdata/stronghold_pieces.txt")
	if err != nil {
		t.Fatal(err)
	}
	var seed int64
	var cx, cz int32
	want := map[string][]string{}
	var key string
	for line := range strings.SplitSeq(strings.TrimSpace(string(data)), "\n") {
		if strings.HasPrefix(line, "# seed ") {
			if _, err := fmt.Sscanf(line, "# seed %d chunk %d,%d", &seed, &cx, &cz); err != nil {
				t.Fatal(err)
			}
			key = fmt.Sprintf("%d %d %d", seed, cx, cz)
		} else if !strings.HasPrefix(line, "#") {
			want[key] = append(want[key], line)
		}
	}
	if len(want) != 2 {
		t.Fatalf("golden has %d strongholds", len(want))
	}
	for key, lines := range want {
		if _, err := fmt.Sscanf(key, "%d %d %d", &seed, &cx, &cz); err != nil {
			t.Fatal(err)
		}
		starts := NewOverworldGenerator(seed).structureStartsIn(cx, cz)
		if len(starts) != 1 {
			t.Fatalf("%s: %d starts", key, len(starts))
		}
		pieces := starts[0].pieces
		if len(pieces) != len(lines) {
			t.Fatalf("%s: %d pieces, vanilla %d", key, len(pieces), len(lines))
		}
		for i, p := range pieces {
			c := p.nbt()
			got := fmt.Sprintf("%s %v %d %d", c["id"].(nbt.String), []int32(c["BB"].(nbt.IntArray)), int32(c["O"].(nbt.Int)), int32(c["GD"].(nbt.Int)))
			if got != lines[i] {
				t.Fatalf("%s piece %d: %s, vanilla %s", key, i, got, lines[i])
			}
		}
	}
}

// TestStrongholdBlocks generates the two strongholds' start chunks and checks
// the blocks that neither the random stream nor the terrain decides (slabs,
// doors, bars) against what a vanilla server saved
// (testdata/stronghold_blocks.txt: x y z block props).
func TestStrongholdBlocks(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("testdata/stronghold_blocks.txt")
	if err != nil {
		t.Fatal(err)
	}
	var g *OverworldGenerator
	var c *Chunk
	var n int
	for line := range strings.SplitSeq(strings.TrimSpace(string(data)), "\n") {
		if strings.HasPrefix(line, "# seed ") {
			var seed int64
			var cx, cz int32
			if _, err := fmt.Sscanf(line, "# seed %d chunk %d,%d", &seed, &cx, &cz); err != nil {
				t.Fatal(err)
			}
			g = NewOverworldGenerator(seed)
			c = newChunk(cx, cz)
			g.Generate(c)
			continue
		}
		if strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Fields(line)
		var x, y, z int32
		if _, err := fmt.Sscanf(strings.Join(f[:3], " "), "%d %d %d", &x, &y, &z); err != nil {
			t.Fatal(err)
		}
		props := map[string]string{}
		for kv := range strings.SplitSeq(f[4], ",") {
			k, v, _ := strings.Cut(kv, "=")
			props[k] = v
		}
		want, ok := registry.FindState(f[3], props)
		if !ok {
			t.Fatalf("unknown block %s", f[3])
		}
		if got := c.BlockAt(x&15, y, z&15); got != want {
			t.Errorf("%d %d %d: %s %v, vanilla %s %v", x, y, z, registry.BlockOf(got).Name(), registry.StateProps(got), f[3], props)
		}
		n++
	}
	if n < 50 {
		t.Fatalf("golden has %d blocks", n)
	}
}

// BenchmarkStrongholdGenerate measures generating the two chunks
// TestStrongholdBlocks checks, which hold a stronghold's start.
func BenchmarkStrongholdGenerate(b *testing.B) {
	for _, tc := range []struct {
		seed   int64
		cx, cz int32
	}{{0, -13, -106}, {12345, -35, -103}} {
		b.Run(fmt.Sprintf("seed%d", tc.seed), func(b *testing.B) {
			g := NewOverworldGenerator(tc.seed)
			b.ReportAllocs()
			for b.Loop() {
				g.Generate(newChunk(tc.cx, tc.cz))
			}
		})
	}
}
