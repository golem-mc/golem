package world

import (
	"reflect"
	"testing"

	"github.com/golem-mc/golem/nbt"
)

// TestUpgradeEntitiesAppliesTheEntityFixesAfter1_21 feeds each entity of the
// shape before a data fixer through upgradeEntities.
// vanilla: BoatSplitFix, EntitySalmonSizeFix, EntityFieldsRenameFix,
// AreaEffectCloudDurationScaleFix, EntityFallDistanceFloatToDoubleFix,
// ThrownPotionSplitFix, CopperGolemWeatherStateFix, VillagerSetVillagerDataFinalized
func TestUpgradeEntitiesAppliesTheEntityFixesAfter1_21(t *testing.T) {
	item := func(id string) nbt.Compound { return nbt.Compound{"id": nbt.String(id), "count": nbt.Int(1)} }
	str := func(s string) nbt.Tag { return nbt.String(s) }
	for _, tc := range []struct {
		name    string
		version int32
		in      nbt.Compound
		want    nbt.Compound
	}{
		{"falling anvil", 4903, nbt.Compound{"id": str("minecraft:falling_block"), "BlockState": nbt.Compound{"Name": str("minecraft:anvil"), "Properties": nbt.Compound{"facing": str("east")}}},
			nbt.Compound{"id": str("minecraft:falling_block"), "BlockState": nbt.Compound{"id": str("minecraft:anvil"), "properties": nbt.Compound{"facing": str("east")}}}},
		{"carried block", 4903, nbt.Compound{"id": str("minecraft:enderman"), "carriedBlockState": nbt.Compound{"Name": str("minecraft:stone")}},
			nbt.Compound{"id": str("minecraft:enderman"), "carriedBlockState": nbt.Compound{"id": str("minecraft:stone")}}},
		{"boat", 3955, nbt.Compound{"id": str("minecraft:boat"), "Type": str("birch")},
			nbt.Compound{"id": str("minecraft:birch_boat")}},
		{"boat without type", 3955, nbt.Compound{"id": str("minecraft:boat")},
			nbt.Compound{"id": str("minecraft:oak_boat")}},
		{"unknown boat type", 3955, nbt.Compound{"id": str("minecraft:boat"), "Type": str("x")},
			nbt.Compound{"id": str("minecraft:oak_boat")}},
		{"bamboo raft", 3955, nbt.Compound{"id": str("minecraft:boat"), "Type": str("bamboo")},
			nbt.Compound{"id": str("minecraft:bamboo_raft")}},
		{"chest boat", 3955, nbt.Compound{"id": str("minecraft:chest_boat"), "Type": str("dark_oak")},
			nbt.Compound{"id": str("minecraft:dark_oak_chest_boat")}},
		{"chest raft", 3955, nbt.Compound{"id": str("minecraft:chest_boat"), "Type": str("bamboo")},
			nbt.Compound{"id": str("minecraft:bamboo_chest_raft")}},
		{"small salmon", 4000, nbt.Compound{"id": str("minecraft:salmon"), "type": str("small")},
			nbt.Compound{"id": str("minecraft:salmon"), "type": str("medium")}},
		{"large salmon", 4000, nbt.Compound{"id": str("minecraft:salmon"), "type": str("large")},
			nbt.Compound{"id": str("minecraft:salmon"), "type": str("large")}},
		{"tnt minecart", 4100, nbt.Compound{"id": str("minecraft:tnt_minecart"), "TNTFuse": nbt.Int(40)},
			nbt.Compound{"id": str("minecraft:tnt_minecart"), "fuse": nbt.Int(40)}},
		{"cloud", 4200, nbt.Compound{"id": str("minecraft:area_effect_cloud")},
			nbt.Compound{"id": str("minecraft:area_effect_cloud"), "potion_duration_scale": nbt.Float(0.25)}},
		{"fall distance", 4300, nbt.Compound{"id": str("minecraft:cow"), "FallDistance": nbt.Float(2.5)},
			nbt.Compound{"id": str("minecraft:cow"), "fall_distance": nbt.Double(2.5)}},
		{"splash potion", 4305, nbt.Compound{"id": str("minecraft:potion"), "Item": item("minecraft:splash_potion")},
			nbt.Compound{"id": str("minecraft:splash_potion"), "Item": item("minecraft:splash_potion")}},
		{"lingering potion", 4305, nbt.Compound{"id": str("minecraft:potion"), "Item": item("minecraft:lingering_potion")},
			nbt.Compound{"id": str("minecraft:lingering_potion"), "Item": item("minecraft:lingering_potion")}},
		{"copper golem", 4500, nbt.Compound{"id": str("minecraft:copper_golem"), "weather_state": nbt.Int(2)},
			nbt.Compound{"id": str("minecraft:copper_golem"), "weather_state": str("weathered")}},
		{"phantom", 4189, nbt.Compound{"id": str("minecraft:phantom"), "Size": nbt.Int(5), "AX": nbt.Int(1), "AY": nbt.Int(80), "AZ": nbt.Int(2)},
			nbt.Compound{"id": str("minecraft:phantom"), "size": nbt.Int(5), "anchor_pos": nbt.IntArray{1, 80, 2}}},
		{"turtle", 4189, nbt.Compound{"id": str("minecraft:turtle"), "HomePosX": nbt.Int(10), "HomePosY": nbt.Int(63), "HomePosZ": nbt.Int(-4),
			"HasEgg": nbt.Byte(1), "TravelPosX": nbt.Int(1), "TravelPosY": nbt.Int(2), "TravelPosZ": nbt.Int(3)},
			nbt.Compound{"id": str("minecraft:turtle"), "home_pos": nbt.IntArray{10, 63, -4}, "has_egg": nbt.Byte(1)}},
		{"vex", 4189, nbt.Compound{"id": str("minecraft:vex"), "BoundX": nbt.Int(0), "BoundY": nbt.Int(64), "BoundZ": nbt.Int(0), "LifeTicks": nbt.Int(200)},
			nbt.Compound{"id": str("minecraft:vex"), "bound_pos": nbt.IntArray{0, 64, 0}, "life_ticks": nbt.Int(200)}},
		{"sleeping villager", 4189, nbt.Compound{"id": str("minecraft:villager"), "SleepingX": nbt.Int(3), "SleepingY": nbt.Int(64), "SleepingZ": nbt.Int(3)},
			nbt.Compound{"id": str("minecraft:villager"), "sleeping_pos": nbt.IntArray{3, 64, 3}, "VillagerDataFinalized": nbt.Byte(1)}},
		{"painting", 4189, nbt.Compound{"id": str("minecraft:painting"), "TileX": nbt.Int(5), "TileY": nbt.Int(65), "TileZ": nbt.Int(7)},
			nbt.Compound{"id": str("minecraft:painting"), "block_pos": nbt.IntArray{5, 65, 7}}},
		{"partial tile pos kept", 4189, nbt.Compound{"id": str("minecraft:painting"), "TileX": nbt.Int(5)},
			nbt.Compound{"id": str("minecraft:painting"), "TileX": nbt.Int(5)}},
		{"villager", 4700, nbt.Compound{"id": str("minecraft:villager")},
			nbt.Compound{"id": str("minecraft:villager"), "VillagerDataFinalized": nbt.Byte(1)}},
		{"zombie villager", 4700, nbt.Compound{"id": str("minecraft:zombie_villager")},
			nbt.Compound{"id": str("minecraft:zombie_villager"), "VillagerDataFinalized": nbt.Byte(1)}},
		{"current boat untouched", 4800, nbt.Compound{"id": str("minecraft:boat"), "Type": str("birch")},
			nbt.Compound{"id": str("minecraft:boat"), "Type": str("birch")}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			list := nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{tc.in}}
			upgradeEntities(list, tc.version)
			got, _ := nbt.CompoundView(list.Items[0])
			if len(got) != len(tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
			for k, w := range tc.want {
				if g := got[k]; !tagEqual(g, w) {
					t.Errorf("%s = %v, want %v", k, g, w)
				}
			}
		})
	}
}

func tagEqual(a, b nbt.Tag) bool {
	if ca, ok := nbt.CompoundView(a); ok {
		cb, ok := nbt.CompoundView(b)
		if !ok || len(ca) != len(cb) {
			return false
		}
		for k, v := range ca {
			if !tagEqual(v, cb[k]) {
				return false
			}
		}
		return true
	}
	return reflect.DeepEqual(a, b)
}

// TestUpgradeEntitiesFixesPassengers checks a boat's passenger shape.
func TestUpgradeEntitiesFixesPassengers(t *testing.T) {
	rider := nbt.Compound{"id": nbt.String("minecraft:villager")}
	boat := nbt.Compound{
		"id": nbt.String("minecraft:boat"), "Type": nbt.String("jungle"),
		"Passengers": nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{rider}},
	}
	upgradeEntities(nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{boat}}, 4000)
	if boat["id"] != nbt.String("minecraft:jungle_boat") || rider["VillagerDataFinalized"] != nbt.Byte(1) {
		t.Errorf("boat %v rider %v", boat, rider)
	}
}
