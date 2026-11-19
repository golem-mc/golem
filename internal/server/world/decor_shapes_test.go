package world

import (
	"testing"

	"github.com/golem-mc/golem/registry"
)

func TestTemplateShapesDoorsAndFences(t *testing.T) {
	for _, upperFirst := range []bool{false, true} {
		r := newDecorRegion(newGoldenTerrain(), newChunk(0, 0), worldMinY, worldHeight)
		lower, _ := registry.WithProp(registry.BlockOakDoor.DefaultState(), registry.PropHalf, "lower")
		upper, _ := registry.WithProps(lower,
			registry.PropValue{P: registry.PropHalf, V: "upper"},
			registry.PropValue{P: registry.PropFacing, V: "west"},
			registry.PropValue{P: registry.PropHinge, V: "left"},
			registry.PropValue{P: registry.PropOpen, V: "true"},
			registry.PropValue{P: registry.PropPowered, V: "true"})
		r.set(4, 80, 4, lower)
		r.set(4, 81, 4, upper)
		placed := []posKey{{4, 80, 4}, {4, 81, 4}}
		want, other := upper, "lower"
		if upperFirst {
			placed[0], placed[1] = placed[1], placed[0]
			want, other = lower, "upper"
		}
		r.templateShapes(placed)
		settled, _ := registry.WithProp(want, registry.PropHalf, other)
		for _, k := range placed {
			expect := want
			if k[1] == 80 && other == "lower" || k[1] == 81 && other == "upper" {
				expect = settled
			}
			if got := r.get(k[0], k[1], k[2]); got != expect {
				t.Errorf("upper first %v, door at %v: got %d, want %d", upperFirst, k, got, expect)
			}
		}
	}

	r := newDecorRegion(newGoldenTerrain(), newChunk(0, 0), worldMinY, worldHeight)
	fence, _ := registry.WithBool(registry.BlockAcaciaFence.DefaultState(), registry.PropWaterlogged, true)
	fence, _ = registry.WithBool(fence, registry.PropNorth, true)
	r.set(8, 80, 8, fence)
	r.set(9, 80, 8, registry.BlockOakFence.DefaultState())
	r.templateShapes([]posKey{{8, 80, 8}, {9, 80, 8}})
	want, _ := registry.WithBool(fence, registry.PropNorth, false)
	want, _ = registry.WithBool(want, registry.PropEast, true)
	if got := r.get(8, 80, 8); got != want {
		t.Errorf("fence: got %d, want %d", got, want)
	}
	if !registry.StateBool(r.get(9, 80, 8), registry.PropWest) {
		t.Error("neighbour fence did not connect west")
	}
}

func TestReconnectShapesGrowingPlantEnds(t *testing.T) {
	r := newDecorRegion(newGoldenTerrain(), newChunk(0, 0), worldMinY, worldHeight)
	r.worldSeed = 42
	const y = -14
	body, _ := registry.WithBool(registry.BlockCaveVinesPlant.DefaultState(), registry.PropBerries, true)
	r.set(6, y+1, 6, registry.BlockStone.DefaultState())
	r.set(6, y, 6, body)
	r.set(6, y-1, 6, registry.BlockCaveAir.DefaultState())
	// A head with its body still below it becomes body.
	head, _ := registry.WithInt(registry.BlockCaveVines.DefaultState(), registry.PropAge, 7)
	head, _ = registry.WithBool(head, registry.PropBerries, true)
	r.set(9, y+1, 9, registry.BlockStone.DefaultState())
	r.set(9, y, 9, head)
	r.set(9, y-1, 9, body)
	r.markPost(6, y, 6)
	r.markPost(9, y, 9)
	r.reconnectShapes(r.protoAt(6, 6))

	got := r.get(6, y, 6)
	age, ok := registry.StateInt(got, registry.PropAge)
	if registry.BlockOf(got) != registry.BlockCaveVines || !registry.StateBool(got, registry.PropBerries) || !ok || age < 0 || age > 24 {
		t.Fatalf("body over cave air became %s, want cave_vines with berries and age 0..24", stateString(got))
	}
	if got := r.get(9, y, 9); got != body {
		t.Errorf("head over its body became %s, want %s", stateString(got), stateString(body))
	}
}
