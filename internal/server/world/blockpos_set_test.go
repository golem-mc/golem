package world

import (
	"crypto/sha256"
	"fmt"
	"slices"
	"testing"
)

func TestBlockPosSetOrderVanilla(t *testing.T) {
	// Actual vanilla BlockPos in Java 25 HashSet; digest of UTF-8 "x y z\n" lines.
	for _, tc := range []struct {
		n       int
		reverse bool
		digest  string
	}{
		{0, false, "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"},
		{17, false, "d3e65c33e5a340e38886a012f157401daf6c12ee9531c37951a052d070b0f6a4"},
		{17, true, "aa22e40e18b81f7cab2996bf0381bf139dc4249d18ac43a21bd7ef2ab2521b51"},
		{49, false, "bc71c165602ba8cc2d0c98cc41d761dedf3a73dbdafdc5ef34cae91dd18e8274"},
		{49, true, "5e785be644b1c8293a2194bd354b7542604caec15010608bee8d3829fb24a6ef"},
		{100, false, "b1042ddf174c8f91b984f76d7386f166d4e15907f367c0154cd7b3ef34dd28aa"},
		{100, true, "e1b0a24647f30492b1dbca99b60359388bacb027e039391e361759fa10f4f136"},
	} {
		for _, duplicates := range []bool{false, true} {
			t.Run(fmt.Sprintf("n=%d/reverse=%t/duplicates=%t", tc.n, tc.reverse, duplicates), func(t *testing.T) {
				var in [][3]int32
				for j := 0; j < tc.n; j++ {
					i := j
					if tc.reverse {
						i = tc.n - 1 - j
					}
					p := [3]int32{192 + int32(i), 80, -312 - int32(i)}
					in = append(in, p)
					if duplicates {
						in = append(in, p)
					}
				}
				r := &decorRegion{}
				out := blockPosSetOrder(r, in)
				h := sha256.New()
				for _, p := range out {
					fmt.Fprintf(h, "%d %d %d\n", p[0], p[1], p[2])
				}
				if got := fmt.Sprintf("%x", h.Sum(nil)); got != tc.digest {
					t.Fatalf("digest %s, want %s; order %v", got, tc.digest, out)
				}
				if r.unorderedSet {
					t.Fatal("distinct hashes required identity tie-breaking")
				}
			})
		}
	}
}

func TestBlockPosSetIdentityTie(t *testing.T) {
	for _, n := range []int{2, 17} {
		t.Run(fmt.Sprint(n), func(t *testing.T) {
			var in [][3]int32
			// Distinct positions with the same complete Vec3i hash, not just bucket.
			for i := int32(0); i < int32(n); i++ {
				in = append(in, [3]int32{961 * i, 0, -i})
			}
			r := &decorRegion{}
			out := blockPosSetOrder(r, in)
			if len(out) != n {
				t.Fatalf("got %d positions, want %d", len(out), n)
			}
			if want := n == 17; r.unorderedSet != want {
				t.Fatalf("unorderedSet=%t, want %t", r.unorderedSet, want)
			}
			if again := blockPosSetOrder(&decorRegion{}, in); !slices.Equal(out, again) {
				t.Fatal("identity fallback is not deterministic")
			}
		})
	}
}

func TestBlockPosSetLushCaveWitness(t *testing.T) {
	r := &decorRegion{}
	out := blockPosSetOrder(r, lushCaveSetWitness)
	h := sha256.New()
	for _, p := range out {
		fmt.Fprintf(h, "%d %d %d\n", p[0], p[1], p[2])
	}
	if got := fmt.Sprintf("%x", h.Sum(nil)); got != "9cb524d74edc4a70689f5cfbf590a4e653be6777908988b75fa8676a70d8a0af" {
		t.Fatalf("Java HashSet witness digest: %s", got)
	}
	if r.unorderedSet {
		t.Fatal("witness required identity tie-breaking")
	}
	// Guard the benchmark's coverage of both treeification and tree insertion.
	s := blockPosSet{table: make([]*blockPosNode, 16), region: r}
	treeInsertions := 0
	for _, p := range lushCaveSetWitness {
		h := hashPosSpread(hashPosHash(p))
		if head := s.table[s.index(int32(h))]; head != nil && head.tree {
			treeInsertions++
		}
		s.put(&blockPosNode{pos: p, hash: int32(h)})
	}
	if treeInsertions == 0 {
		t.Fatal("benchmark witness did not insert into a tree bin")
	}
}
