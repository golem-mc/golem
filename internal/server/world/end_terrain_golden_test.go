package world

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"strings"
	"testing"
)

// testEndTerrainMatchesTerrainGolden checks the End's island terrain against
// the 26.3 server's block digests of its terrain step: every block is end
// stone where the density is above 0 and air elsewhere.
func testEndTerrainMatchesTerrainGolden(t *testing.T) {
	t.Parallel()
	f, err := os.Open("terrain/testdata/terrain_golden.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	n := 0
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var seed int64
		var cx, cz int32
		var digest string
		if !strings.HasPrefix(sc.Text(), "terrain end ") {
			continue
		}
		if _, err := fmt.Sscanf(sc.Text(), "terrain end %d %d %d -> %s", &seed, &cx, &cz, &digest); err != nil {
			t.Fatal(err)
		}
		n++
		tr := NewEndGenerator(seed).terrainOf(cx, cz)
		h := sha256.New()
		for x := range int32(sectionSize) {
			for z := range int32(sectionSize) {
				for y := range int32(256) {
					if y < endHeight && tr.solid(x, y, z) {
						h.Write([]byte("end_stone\n"))
					} else {
						h.Write([]byte("air\n"))
					}
				}
			}
		}
		if got := hex.EncodeToString(h.Sum(nil)); got != digest {
			t.Errorf("chunk (%d, %d) of seed %d: digest %s, want %s", cx, cz, seed, got, digest)
		}
	}
	if n == 0 {
		t.Fatal("the golden has no End chunks")
	}
}
