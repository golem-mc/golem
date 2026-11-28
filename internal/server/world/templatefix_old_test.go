package world

import (
	"testing"

	"github.com/golem-mc/golem/nbt"
)

func TestParseTemplateOldDataVersions(t *testing.T) {
	for _, version := range []int32{2230, 2975, 3337, 3700, 4189} {
		var stack nbt.Compound
		if version < storedItemVersion {
			stack = nbt.Compound{"Slot": nbt.Byte(0), "id": nbt.String("minecraft:stone"), "Count": nbt.Byte(3)}
		} else {
			stack = nbt.Compound{"Slot": nbt.Byte(0), "id": nbt.String("minecraft:stone"), "count": nbt.Int(3)}
		}
		tag := nbt.Compound{
			"DataVersion": nbt.Int(version),
			"size":        nbt.List{Elem: nbt.TypeInt, Items: []nbt.Tag{nbt.Int(1), nbt.Int(1), nbt.Int(2)}},
			"palette": nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{
				nbt.Compound{"Name": nbt.String("minecraft:chest")},
				nbt.Compound{"Name": nbt.String("minecraft:grass_path")},
				nbt.Compound{"Name": nbt.String("minecraft:white_bed")},
			}},
			"blocks": nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{
				nbt.Compound{"pos": nbt.List{Elem: nbt.TypeInt, Items: []nbt.Tag{nbt.Int(0), nbt.Int(0), nbt.Int(0)}}, "state": nbt.Int(0),
					"nbt": nbt.Compound{"id": nbt.String("minecraft:chest"), "Items": nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{stack}}}},
				nbt.Compound{"pos": nbt.List{Elem: nbt.TypeInt, Items: []nbt.Tag{nbt.Int(0), nbt.Int(0), nbt.Int(1)}}, "state": nbt.Int(1)},
			}},
		}
		tag["blocks"] = nbt.List{Elem: nbt.TypeCompound, Items: append(tag["blocks"].(nbt.List).Items,
			nbt.Compound{"pos": nbt.List{Elem: nbt.TypeInt, Items: []nbt.Tag{nbt.Int(0), nbt.Int(0), nbt.Int(2)}}, "state": nbt.Int(2),
				"nbt": nbt.Compound{"id": nbt.String("minecraft:bed"), "color": nbt.Int(0)}})}
		if err := upgradeTemplate(tag); err != nil {
			t.Fatalf("v%d: %v", version, err)
		}
		palette := tag["palette"].(nbt.List)
		want := nbt.String("minecraft:grass_path")
		if version < grassPathRenameVersion {
			want = "minecraft:dirt_path"
		}
		if got := palette.Items[1].(nbt.Compound)["id"]; got != want {
			t.Errorf("v%d: grass_path became %v, want %v", version, got, want)
		}
		blocks := tag["blocks"].(nbt.List).Items
		chest := blocks[0].(nbt.Compound)["nbt"].(nbt.Compound)
		items := chest["Items"].(nbt.List).Items
		got := items[0].(nbt.Compound)
		if got["id"] != nbt.String("minecraft:stone") || got["count"] != nbt.Int(3) {
			t.Errorf("v%d: stack = %v", version, got)
		}
		if _, ok := got["Count"]; ok {
			t.Errorf("v%d: stack keeps Count: %v", version, got)
		}
		if _, ok := blocks[2].(nbt.Compound)["nbt"]; ok {
			t.Errorf("v%d: bed keeps its block entity", version)
		}
	}
	if err := upgradeTemplate(nbt.Compound{"DataVersion": nbt.Int(templateDataVersionFloor - 1)}); err == nil {
		t.Error("a structure below the floor was accepted")
	}
}
