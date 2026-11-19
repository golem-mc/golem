package protocol

import (
	"runtime"
	"testing"
	"time"

	"github.com/golem-mc/golem/nbt"
)

// nestedText is a component whose only sibling is the next one down: a chain
// of extra at the given depth.
func nestedText(depth int) nbt.Tag {
	var tag nbt.Tag = nbt.Compound{"text": nbt.String("a")}
	for range depth {
		tag = nbt.Compound{
			"text":  nbt.String("a"),
			"extra": nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{tag}},
		}
	}
	return tag
}

// TestTextOfChargesTheTagOnce pins that a deep extra chain reads in linear
// time: charging every subtree at every level — the exported TextOf recursing
// into itself rather than the uncharged textOf — re-walks each level and costs
// the square of the depth. Comparing with one charge makes the bound what the
// depth costs, not the clock speed.
func TestTextOfChargesTheTagOnce(t *testing.T) {
	tag := nestedText(500)
	const runs = 25
	measure := func(f func()) time.Duration {
		best := time.Duration(1 << 62)
		for range 7 {
			runtime.GC() // a collection mid-run would swamp the shorter measurement
			start := time.Now()
			f()
			if d := time.Since(start); d < best {
				best = d
			}
		}
		return best
	}
	read := measure(func() {
		for range runs {
			if _, ok := TextOf(tag); !ok {
				t.Fatal("TextOf refused a component inside the quota")
			}
		}
	})
	var charge int64
	base := measure(func() {
		for range runs {
			charge += textCharge(tag)
		}
	})
	if charge <= 0 {
		t.Fatal("the accounter charge underflowed")
	}
	// A read costs a few charges; the quadratic re-walk at this depth is a
	// few hundred, so the threshold splits them with room either way.
	if read > 64*base {
		t.Errorf("TextOf over a 500-deep chain took %v, %d times one charge (%v); want the tag charged once",
			read, read/base, base)
	}
}
