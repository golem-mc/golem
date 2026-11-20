package world

import (
	"testing"

	"github.com/golem-mc/golem/nbt"
)

func TestShapeOfTypeEnderDragonFight(t *testing.T) {
	t.Parallel()
	endShape, err := ShapeOfType(EndType, nil)
	if err != nil || !endShape.dragonFight {
		t.Fatalf("EndType shape = %+v, err = %v, want dragonFight true", endShape, err)
	}
	overShape, err := ShapeOfType(DimensionType, nil)
	if err != nil || overShape.dragonFight {
		t.Fatalf("DimensionType shape = %+v, err = %v, want dragonFight false", overShape, err)
	}
	for _, want := range []bool{false, true} {
		flag := nbt.Byte(0)
		if want {
			flag = 1
		}
		shape, err := ShapeOfType("test:custom_end", nbt.Compound{
			"min_y":                  nbt.Int(0),
			"height":                 nbt.Int(256),
			"logical_height":         nbt.Int(256),
			"has_skylight":           nbt.Byte(1),
			"has_ceiling":            nbt.Byte(0),
			"has_ender_dragon_fight": flag,
		})
		if err != nil {
			t.Fatalf("ShapeOfType(want=%v): %v", want, err)
		}
		if shape.dragonFight != want {
			t.Errorf("custom shape dragonFight = %v, want %v", shape.dragonFight, want)
		}
	}
}
