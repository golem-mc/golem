package redstone

import (
	"fmt"
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
)

// plateEntityCases are the three sensitivities a pressing body meets: a
// wooden plate counts every entity, a stone plate the living only, and a
// light weighted plate one entity in its fifteen.
var plateEntityCases = []struct {
	plate string
	prop  string
	want  string
}{
	{"minecraft:oak_pressure_plate", "powered", "true"},
	{"minecraft:stone_pressure_plate", "powered", "false"},
	{"minecraft:light_weighted_pressure_plate", "power", "1"},
}

// TestPrimedTNTPressesPlates stands a primed TNT on each plate: vanilla's
// PressurePlateBlock counts every entity, the stone plate living only.
// vanilla: PressurePlateBlock.getSignalStrength, WeightedPressurePlateBlock.getSignalStrength
func TestPrimedTNTPressesPlates(t *testing.T) {
	t.Parallel()
	for _, tc := range plateEntityCases {
		t.Run(tc.plate, func(t *testing.T) {
			t.Parallel()
			srv, _ := plateServer(t, tc.plate)
			// A long fuse, so the press is watched for as long as it takes
			// and the blast never races the check.
			if srv.SpawnTNT(plateX+0.5, plateY, plateZ+0.5, 600) == 0 {
				t.Fatal("SpawnTNT came to nothing")
			}
			expectPlateSettles(t, srv, tc.prop, tc.want)
		})
	}
}

// TestBoatPressesPlates floats a boat on each plate. The boat rests on the
// plate and counts like any other body.
func TestBoatPressesPlates(t *testing.T) {
	t.Parallel()
	for _, tc := range plateEntityCases {
		t.Run(tc.plate, func(t *testing.T) {
			t.Parallel()
			srv, _ := plateServer(t, tc.plate)
			if srv.SpawnBoat(plateX+0.5, plateY, plateZ+0.5, "minecraft:oak_boat") == 0 {
				t.Fatal("SpawnBoat came to nothing")
			}
			expectPlateSettles(t, srv, tc.prop, tc.want)
		})
	}
}

// TestArmorStandPressesEveryPlate summons an armour stand on each plate: a
// living body for every plate kind unless it is a marker.
// vanilla: ArmorStand.isIgnoringBlockTriggers
func TestArmorStandPressesEveryPlate(t *testing.T) {
	t.Parallel()
	cases := []struct {
		plate, prop, want, nbt string
	}{
		{"minecraft:oak_pressure_plate", "powered", "true", ""},
		{"minecraft:stone_pressure_plate", "powered", "true", ""},
		{"minecraft:light_weighted_pressure_plate", "power", "1", ""},
		{"minecraft:stone_pressure_plate", "powered", "false", " {Marker:1b}"},
	}
	for _, tc := range cases {
		t.Run(tc.plate+tc.nbt, func(t *testing.T) {
			t.Parallel()
			srv, c := plateServer(t, tc.plate)
			c.ChatCommand(fmt.Sprintf("summon minecraft:armor_stand %d.5 %d %d.5%s", plateX, plateY, plateZ, tc.nbt))
			c.Settle()
			servertest.AwaitTicks(t, srv, 10)
			if got := plateProps(srv)[tc.prop]; got != tc.want {
				t.Errorf("%s = %q, want %q", tc.prop, got, tc.want)
			}
		})
	}
}

// TestBatDoesNotPressPlates hangs a bat on each plate; vanilla's bat ignores
// block triggers.
// vanilla: Bat.isIgnoringBlockTriggers
func TestBatDoesNotPressPlates(t *testing.T) {
	t.Parallel()
	for _, plate := range []string{"minecraft:oak_pressure_plate", "minecraft:stone_pressure_plate"} {
		t.Run(plate, func(t *testing.T) {
			t.Parallel()
			srv, c := plateServer(t, plate)
			c.ChatCommand(fmt.Sprintf("summon minecraft:bat %d.5 %d %d.5 {NoAI:1b}", plateX, plateY, plateZ))
			c.Settle()
			servertest.AwaitTicks(t, srv, 10)
			if got := plateProps(srv)["powered"]; got != "false" {
				t.Errorf("powered = %q, want false", got)
			}
		})
	}
}
