package world

import (
	"math/rand"
	"testing"
)

// TestSetNibbleBoxMatchesLightSet checks the row-wise box write against a
// lightSet per cell, over every x and z run a box can have (single cells, and
// runs starting and ending on either nibble of a byte), on sections holding
// random light.
func TestSetNibbleBoxMatchesLightSet(t *testing.T) {
	rng := rand.New(rand.NewSource(7))
	for lx0 := range int32(sectionSize) {
		for lx1 := lx0; lx1 < sectionSize; lx1++ {
			lz0 := int32(rng.Intn(sectionSize))
			lz1 := lz0 + int32(rng.Intn(int(sectionSize-lz0)))
			v := uint8(rng.Intn(16))

			var got, want [lightArrayLen]byte
			rng.Read(got[:])
			want = got
			setNibbleBox(&got, lx0, lx1, lz0, lz1, v)

			c := &Chunk{}
			c.skyLight[1] = &want
			for ly := range int32(sectionSize) {
				for lz := lz0; lz <= lz1; lz++ {
					for lx := lx0; lx <= lx1; lx++ {
						c.lightSet(skyLayer, lx, worldMinY+ly, lz, v)
					}
				}
			}
			if got != want {
				t.Fatalf("x [%d, %d], z [%d, %d], v %d: the box write differs from a lightSet per cell", lx0, lx1, lz0, lz1, v)
			}
		}
	}
}
