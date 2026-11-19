package rules

import (
	"maps"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/registry"
)

func toolStateOf(t *testing.T, name string, props map[string]string) uint16 {
	t.Helper()
	s, ok := registry.FindState(name, props)
	if !ok {
		t.Fatalf("no state of %s", name)
	}
	return s
}

func TestAxeStripsEveryLogKeepingTheAxis(t *testing.T) {
	for from, to := range registry.AxeStrippables {
		use, ok := AxeConversion(toolStateOf(t, from, map[string]string{"axis": "z"}))
		if !ok || use.State != toolStateOf(t, to, map[string]string{"axis": "z"}) {
			t.Errorf("%s stripped = %+v, %v; want %s axis=z", from, use, ok, to)
		}
		if use.Sound.Name() != "minecraft:item.axe.strip" || use.LevelEvent != 0 {
			t.Errorf("%s strip plays %q and event %d", from, use.Sound, use.LevelEvent)
		}
	}
}

func TestTransformHeldBackByShield(t *testing.T) {
	shield := protocol.ItemStack{ItemID: registry.MustItem("minecraft:shield"), Count: 1}
	torch := protocol.ItemStack{ItemID: registry.MustItem("minecraft:torch"), Count: 1}
	cases := []struct {
		hand     int32
		off      protocol.ItemStack
		sneaking bool
		want     bool
	}{
		{protocol.HandMain, shield, false, true},
		{protocol.HandMain, shield, true, false},
		{protocol.HandOff, shield, false, false},
		{protocol.HandMain, torch, false, false},
		{protocol.HandMain, protocol.ItemStack{}, false, false},
	}
	for _, c := range cases {
		if got := TransformHeldBackByShield(c.hand, c.off, c.sneaking); got != c.want {
			t.Errorf("TransformHeldBackByShield(%d, %+v, %v) = %v, want %v", c.hand, c.off, c.sneaking, got, c.want)
		}
	}
}

func TestShovelConversion(t *testing.T) {
	path := registry.BlockStates["minecraft:dirt_path"]
	for from := range registry.ShovelFlattenables {
		w := testWorld{{0, 0, 0}: registry.BlockStates[from]}
		if use, ok := ShovelConversion(w, 0, 0, 0, protocol.FaceNorth, false); !ok || use.State != path ||
			use.Sound.Name() != "minecraft:item.shovel.flatten" {
			t.Errorf("%s flattened = %+v, %v", from, use, ok)
		}
		if _, ok := ShovelConversion(w, 0, 0, 0, protocol.FaceNorth, true); ok {
			t.Errorf("%s flattened behind a shield", from)
		}
		if _, ok := ShovelConversion(w, 0, 0, 0, protocol.FaceBottom, false); ok {
			t.Errorf("%s flattened from below", from)
		}
		w[[3]int32{0, 1, 0}] = registry.BlockStates["minecraft:snow"]
		if _, ok := ShovelConversion(w, 0, 0, 0, protocol.FaceTop, false); ok {
			t.Errorf("%s flattened under snow", from)
		}
	}
	lit := map[string]string{"facing": "east", "lit": "true", "signal_fire": "true", "waterlogged": "false"}
	w := testWorld{{0, 0, 0}: toolStateOf(t, "minecraft:soul_campfire", lit)}
	if use, ok := ShovelConversion(w, 0, 0, 0, protocol.FaceTop, false); ok {
		t.Errorf("a shovel converted a lit campfire to %+v; the campfire's own use douses it", use)
	}
}

func TestPumpkinCarve(t *testing.T) {
	cases := []struct {
		face   int32
		yaw    float32
		facing string
		dx, dz int32
	}{
		{protocol.FaceWest, 0, "west", -1, 0},
		{protocol.FaceSouth, 90, "south", 0, 1},
		{protocol.FaceTop, 0, "north", 0, -1},   // looking south
		{protocol.FaceBottom, 90, "east", 1, 0}, // looking west
		{protocol.FaceTop, -90, "west", -1, 0},  // looking east
		{protocol.FaceTop, 180, "south", 0, 1},  // looking north
	}
	for _, c := range cases {
		state, dx, dz := PumpkinCarve(c.face, c.yaw)
		if want := toolStateOf(t, "minecraft:carved_pumpkin", map[string]string{"facing": c.facing}); state != want || dx != c.dx || dz != c.dz {
			t.Errorf("PumpkinCarve(%d, %v) = %d, %d, %d; want facing %s, %d, %d", c.face, c.yaw, state, dx, dz, c.facing, c.dx, c.dz)
		}
	}
}

func TestFlintLight(t *testing.T) {
	cases := []struct {
		name  string
		props map[string]string
		ok    bool
	}{
		{"minecraft:campfire", map[string]string{"lit": "false", "waterlogged": "false"}, true},
		{"minecraft:soul_campfire", map[string]string{"lit": "false", "waterlogged": "true"}, false},
		{"minecraft:campfire", map[string]string{"lit": "true", "waterlogged": "false"}, false},
		{"minecraft:red_candle", map[string]string{"candles": "3", "lit": "false", "waterlogged": "false"}, true},
		{"minecraft:candle", map[string]string{"lit": "false", "waterlogged": "true"}, false},
		{"minecraft:lime_candle_cake", map[string]string{"lit": "false"}, true},
		{"minecraft:redstone_lamp", map[string]string{"lit": "false"}, false},
	}
	for _, c := range cases {
		lit, ok := FlintLight(toolStateOf(t, c.name, c.props))
		if ok != c.ok {
			t.Errorf("FlintLight(%s %v) ok = %v, want %v", c.name, c.props, ok, c.ok)
			continue
		}
		if ok {
			want := map[string]string{}
			maps.Copy(want, c.props)
			want["lit"] = "true"
			if lit != toolStateOf(t, c.name, want) {
				t.Errorf("FlintLight(%s %v) = %d, want it lit", c.name, c.props, lit)
			}
		}
	}
}
