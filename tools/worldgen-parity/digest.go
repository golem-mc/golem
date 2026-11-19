package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"sort"

	"github.com/golem-mc/golem/registry"
)

// digestHeight is how many blocks up from y 0 a chunk's digest covers: the
// nether's generated height, which is what testdata/nether_features.txt pins.
const digestHeight = 128

// chunkDigest is the SHA-256 of a chunk's blocks in y 0..digestHeight-1, each
// as "name[properties]\n" in y, z, x order.
func chunkDigest(c *chunkData) string {
	h := sha256.New()
	for y := range digestHeight {
		for z := range 16 {
			for x := range 16 {
				s := c.Blocks[c.index(x, y, z)]
				if s == unknownState {
					fmt.Fprintln(h, "unknown")
					continue
				}
				name, props, _ := registry.BlockStateName(s)
				fmt.Fprintf(h, "%s[%s]\n", name, props)
			}
		}
	}
	return hex.EncodeToString(h.Sum(nil))
}

// writeDigests writes one "seed x z vanilla ours" line per chunk of the range.
func writeDigests(path string, seed int64, vanilla, ours map[[2]int32]*chunkData) error {
	var keys [][2]int32
	for k := range vanilla {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i][0] != keys[j][0] {
			return keys[i][0] < keys[j][0]
		}
		return keys[i][1] < keys[j][1]
	})
	var b []byte
	for _, k := range keys {
		b = fmt.Appendf(b, "%d %d %d %s %s\n", seed, k[0], k[1], chunkDigest(vanilla[k]), chunkDigest(ours[k]))
	}
	return os.WriteFile(path, b, 0o644)
}
