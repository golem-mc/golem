package main

import (
	"testing"

	"github.com/golem-mc/golem/nbt"
)

func TestShipwreckTemplateRejectsUnsupportedData(t *testing.T) {
	for _, name := range []string{"supported", "oversized", "entity", "marker"} {
		t.Run(name, func(t *testing.T) {
			tag := nbt.Compound{"size": nbt.List{Elem: nbt.TypeInt, Items: []nbt.Tag{nbt.Int(9), nbt.Int(9), nbt.Int(32)}}}
			switch name {
			case "oversized":
				tag["size"] = nbt.List{Elem: nbt.TypeInt, Items: []nbt.Tag{nbt.Int(33), nbt.Int(9), nbt.Int(32)}}
			case "entity":
				tag["entities"] = nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{nbt.Compound{}}}
			case "marker":
				tag["blocks"] = nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{nbt.Compound{"nbt": nbt.Compound{"id": nbt.String("minecraft:structure_block"), "mode": nbt.String("DATA"), "metadata": nbt.String("unknown")}}}}
			}
			err := checkShipwreckTemplate(tag)
			if (err == nil) != (name == "supported") {
				t.Fatalf("validation error = %v", err)
			}
		})
	}
}
