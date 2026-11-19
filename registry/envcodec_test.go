package registry

import (
	"testing"

	"github.com/golem-mc/golem/nbt"
)

func TestEnvironmentDiscreteModifiersAndConstants(t *testing.T) {
	attr, _ := EnvAttributeByName("minecraft:gameplay/monsters_burn")
	for op, want := range map[string][4]float32{
		"and": {0, 0, 0, 1}, "nand": {1, 1, 1, 0}, "or": {0, 1, 1, 1},
		"nor": {1, 0, 0, 0}, "xor": {0, 1, 1, 0}, "xnor": {1, 0, 0, 1},
	} {
		for a := range 2 {
			for b := range 2 {
				e, err := decodeEnvEntry(attr, nbt.String(op), nbt.Byte(b))
				if err != nil || e.Apply(float32(a)) != want[a*2+b] {
					t.Errorf("%s(%d,%d) = %v, %v", op, a, b, e.Apply(float32(a)), err)
				}
			}
		}
	}
	t.Cleanup(func() { ApplyPackRegistries(nil) })
	ApplyPackRegistries(map[string]map[string]nbt.Tag{"minecraft:dimension_type": {
		"pk:constant": nbt.Compound{"attributes": nbt.Compound{
			"minecraft:gameplay/monsters_burn":   nbt.Byte(1),
			"minecraft:gameplay/eyeblossom_open": nbt.String("default"),
			"minecraft:visual/sun_angle":         nbt.Compound{"modifier": nbt.String("add"), "argument": nbt.Float(90)},
		}},
	}})
	d, err := DecodeEnvDimension("pk:constant")
	if err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]float32{"minecraft:gameplay/monsters_burn": 1, "minecraft:gameplay/eyeblossom_open": -1, "minecraft:visual/sun_angle": 90} {
		a, _ := EnvAttributeByName(name)
		if got := d.Attributes[name].Apply(a.Default); got != want {
			t.Errorf("%s = %v, want %v", name, got, want)
		}
	}
	if len(d.Timelines) != 0 {
		t.Fatal("type without timelines gained a timeline")
	}
	if _, err := decodeEnvEntry(attr, nbt.String("add"), nbt.Byte(1)); err == nil {
		t.Fatal("boolean attribute accepted float modifier")
	}
	if _, err := decodeEnvEntry(attr, nil, nbt.Float(1)); err == nil {
		t.Fatal("boolean attribute accepted number")
	}
}
