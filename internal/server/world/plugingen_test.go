package world

import (
	"testing"

	"github.com/golem-mc/golem/registry"
)

// startsIn counts the structure starts of g in the chunks around the origin.
func startsIn(g Generator) (n int) {
	og, ok := g.(*OverworldGenerator)
	if !ok {
		return 0
	}
	for x := int32(-24); x < 24; x++ {
		for z := int32(-24); z < 24; z++ {
			n += len(og.structureStartsIn(x, z))
		}
	}
	return n
}

func TestPluginFlatNoStructures(t *testing.T) {
	shape := overworldShapeForTest(t)
	with, err := NewPluginOverworldGenerator(PluginGenSpec{Type: "flat"}, shape, 7)
	if err != nil {
		t.Fatal(err)
	}
	if startsIn(with) == 0 {
		t.Fatal("the default flat world grew no structure start near the origin")
	}
	without, err := NewPluginOverworldGenerator(PluginGenSpec{Type: "flat", NoStructures: true}, shape, 7)
	if err != nil {
		t.Fatal(err)
	}
	if n := startsIn(without); n != 0 {
		t.Fatalf("NoStructures left %d starts", n)
	}
}

func TestPluginGeneratorSpecErrors(t *testing.T) {
	shape := overworldShapeForTest(t)
	for _, spec := range []PluginGenSpec{
		{Type: "flat", Settings: "not json"},
		{Type: "flat", Settings: `{"biome":"minecraft:plains"}`},
		{Settings: `{"layers":[]}`},
		{Type: "bogus"},
		{Biome: "minecraft:nope"},
		{Biome: "minecraft:nether_wastes"},
	} {
		if _, err := NewPluginOverworldGenerator(spec, shape, 1); err == nil {
			t.Errorf("%+v was accepted", spec)
		}
	}
}

func TestPluginSingleBiome(t *testing.T) {
	g, err := NewPluginOverworldGenerator(PluginGenSpec{Biome: "minecraft:desert"}, overworldShapeForTest(t), 3)
	if err != nil {
		t.Fatal(err)
	}
	want, _ := registry.BiomeByName("minecraft:desert")
	if b, ok := g.(*OverworldGenerator).fixedBiome(); !ok || b != want {
		t.Fatalf("fixed biome = %v %v, want desert", b, ok)
	}
}

func overworldShapeForTest(t *testing.T) DimensionShape {
	t.Helper()
	shape, err := ShapeOfType(DimensionType, nil)
	if err != nil {
		t.Fatal(err)
	}
	return shape
}
