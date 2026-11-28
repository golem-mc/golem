package world

import (
	"testing"

	"github.com/golem-mc/golem/nbt"
)

func parseSNBT(t *testing.T, s string) nbt.Compound {
	t.Helper()
	tag, err := nbt.ParseSNBT(s)
	if err != nil {
		t.Fatal(err)
	}
	c, ok := nbt.CompoundView(tag)
	if !ok {
		t.Fatalf("%s is not a compound", s)
	}
	return c
}

// TestUpgradePlayerDataUpgradesVehicleAndShoulders runs the entity fixes on
// the entities a player file holds.
// vanilla: DataFixTypes.PLAYER.updateToCurrentVersion (ENTITY_TREE)
func TestUpgradePlayerDataUpgradesVehicleAndShoulders(t *testing.T) {
	t.Parallel()
	root := parseSNBT(t, `{DataVersion:3465,
RootVehicle:{Entity:{id:"minecraft:horse",
SaddleItem:{id:"minecraft:saddle",Count:1b},
ArmorItem:{id:"minecraft:diamond_horse_armor",Count:1b},
CustomName:'{"text":"Ed"}',
Attributes:[{Name:"minecraft:horse.jump_strength",Base:0.7d},{Name:"minecraft:generic.max_health",Base:28.0d}]}},
ShoulderEntityLeft:{id:"minecraft:parrot",CustomName:'{"text":"Polly"}',
Attributes:[{Name:"minecraft:generic.max_health",Base:6.0d}]}}`)
	UpgradePlayerData(root)
	vehicle, _ := nbt.CompoundView(root["RootVehicle"])
	horse, _ := nbt.CompoundView(vehicle["Entity"])
	equipment, _ := nbt.CompoundView(horse["equipment"])
	for _, slot := range []string{"saddle", "body"} {
		if _, ok := equipment[slot]; !ok {
			t.Errorf("horse equipment %v lacks %s", equipment, slot)
		}
	}
	if _, ok := horse["drop_chances"]; !ok {
		t.Errorf("horse %v lacks drop_chances", horse)
	}
	if _, ok := horse["SaddleItem"]; ok {
		t.Errorf("horse keeps SaddleItem")
	}
	if _, ok := horse["CustomName"].(nbt.CompoundTag); !ok {
		t.Errorf("horse CustomName = %v, want a compound", horse["CustomName"])
	}
	attrIDs := func(e nbt.Compound) []string {
		list, _ := nbt.Get[nbt.List](e, "attributes")
		var ids []string
		for _, a := range list.Items {
			m, _ := nbt.CompoundView(a)
			id, _ := nbt.Get[nbt.String](m, "id")
			ids = append(ids, string(id))
		}
		return ids
	}
	if got := attrIDs(horse); len(got) != 2 || got[0] != "minecraft:jump_strength" || got[1] != "minecraft:max_health" {
		t.Errorf("horse attributes = %v", got)
	}
	parrot, _ := nbt.CompoundView(root["ShoulderEntityLeft"])
	if _, ok := parrot["CustomName"].(nbt.CompoundTag); !ok {
		t.Errorf("parrot CustomName = %v, want a compound", parrot["CustomName"])
	}
	if got := attrIDs(parrot); len(got) != 1 || got[0] != "minecraft:max_health" {
		t.Errorf("parrot attributes = %v", got)
	}
}

// TestUpgradePlayerDataSplitsVehicleBoat checks BoatSplitFix on the vehicle.
// vanilla: BoatSplitFix
func TestUpgradePlayerDataSplitsVehicleBoat(t *testing.T) {
	t.Parallel()
	root := parseSNBT(t, `{DataVersion:3955,RootVehicle:{Entity:{id:"minecraft:boat",Type:"spruce"}}}`)
	UpgradePlayerData(root)
	vehicle, _ := nbt.CompoundView(root["RootVehicle"])
	boat, _ := nbt.CompoundView(vehicle["Entity"])
	if id, _ := nbt.Get[nbt.String](boat, "id"); id != "minecraft:spruce_boat" {
		t.Errorf("boat id = %q", id)
	}
}

// TestFixStoredComponentsUpgradesHeldEntities covers entity_data and the
// spawner and hive entities inside item components.
// vanilla: V3818_3, V3818_5
func TestFixStoredComponentsUpgradesHeldEntities(t *testing.T) {
	t.Parallel()
	stack := parseSNBT(t, `{id:"minecraft:armor_stand",count:1,components:{
"minecraft:entity_data":{id:"minecraft:armor_stand",ArmorItems:[{},{},{},{id:"minecraft:chain",count:1}]},
"minecraft:block_entity_data":{id:"minecraft:mob_spawner",SpawnData:{entity:{id:"minecraft:zombie",CustomName:'"Z"'}}},
"minecraft:bees":[{entity_data:{id:"minecraft:bee",CustomName:'"B"'}}]}}`)
	fixStoredComponents(stack, 3839)
	comps, _ := nbt.CompoundView(stack["components"])
	data, _ := nbt.CompoundView(comps["minecraft:entity_data"])
	equipment, _ := nbt.CompoundView(data["equipment"])
	if _, ok := equipment["head"]; !ok {
		t.Errorf("armor stand data %v has no equipment.head", data)
	}
	be, _ := nbt.CompoundView(comps["minecraft:block_entity_data"])
	spawn, _ := nbt.CompoundView(be["SpawnData"])
	zombie, _ := nbt.CompoundView(spawn["entity"])
	if got, _ := nbt.Get[nbt.String](zombie, "CustomName"); got != "Z" {
		t.Errorf("spawner entity CustomName = %v, want Z unquoted", zombie["CustomName"])
	}
	bees, _ := comps["minecraft:bees"].(nbt.List)
	bee, _ := nbt.CompoundView(bees.Items[0])
	inner, _ := nbt.CompoundView(bee["entity_data"])
	if got, _ := nbt.Get[nbt.String](inner, "CustomName"); got != "B" {
		t.Errorf("bee CustomName = %v, want B unquoted", inner["CustomName"])
	}
}
