package entity

import (
	"math/rand"
	"slices"
	"strings"
	"testing"

	"github.com/golem-mc/golem/registry"
)

func TestBoatKindSeats(t *testing.T) {
	for _, tc := range []struct {
		kind        registry.EntityType
		chest, raft bool
		capacity    int
	}{
		{registry.EntityTypeOakBoat, false, false, 2},
		{registry.EntityTypeOakChestBoat, true, false, 1},
		{registry.EntityTypeBambooRaft, false, true, 2},
		{registry.EntityTypeBambooChestRaft, true, true, 1},
	} {
		if got := IsChestBoat(tc.kind); got != tc.chest {
			t.Errorf("IsChestBoat(%s) = %v, want %v", tc.kind, got, tc.chest)
		}
		if got := IsRaft(tc.kind); got != tc.raft {
			t.Errorf("IsRaft(%s) = %v, want %v", tc.kind, got, tc.raft)
		}
		if got := BoatCapacity(tc.kind); got != tc.capacity {
			t.Errorf("BoatCapacity(%s) = %d, want %d", tc.kind, got, tc.capacity)
		}
	}
	// Cover every wood family, including additions to the item/registry tables.
	for typ := range registry.EntityType(registry.EntityTypeCount) {
		name := typ.Name()
		if IsChestBoat(typ) != (strings.HasSuffix(name, "_chest_boat") || strings.HasSuffix(name, "_chest_raft")) || IsRaft(typ) != strings.HasSuffix(name, "_raft") {
			t.Errorf("seat family differs for %s", name)
		}
	}
}

func TestBodiesGridMatchesFullScan(t *testing.T) {
	rng := rand.New(rand.NewSource(7))
	rbox := func(span float64) AABB {
		x, y, z := rng.Float64()*100-50, rng.Float64()*10, rng.Float64()*100-50
		return NewAABB(x, y, z, x+rng.Float64()*span+0.1, y+rng.Float64()*span+0.1, z+rng.Float64()*span+0.1)
	}
	boxes := make([]AABB, 300)
	var grid BoxGrid
	for i := range boxes {
		boxes[i] = rbox(3)
		grid.Add(int32(i), boxes[i])
	}
	for range 200 {
		var skip []int32
		for range rng.Intn(4) {
			skip = append(skip, int32(rng.Intn(len(boxes))))
		}
		q := rbox(40)
		got := Bodies{Boxes: boxes, Grid: &grid, Skip: skip}.BodyBoxes(q)
		want := Bodies{Boxes: boxes, Skip: skip}.BodyBoxes(q)
		if !slices.Equal(got, want) {
			t.Fatalf("grid %v, full scan %v", got, want)
		}
	}
}
