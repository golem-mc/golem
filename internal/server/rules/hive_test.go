package rules

import (
	"testing"

	"github.com/golem-mc/golem/registry"
)

type oneBlock uint16

func (b oneBlock) BlockAt(x, y, z int32) uint16 { return uint16(b) }

// TestHiveHarvest checks that only a full hive reads full and that emptying
// it resets the level.
func TestHiveHarvest(t *testing.T) {
	if _, full := FullHiveAt(oneBlock(stateOf(t, "minecraft:stone")), 0, 0, 0); full {
		t.Error("stone is not a hive")
	}
	if _, full := FullHiveAt(oneBlock(stateOf(t, "minecraft:bee_nest")), 0, 0, 0); full {
		t.Error("a default nest is empty")
	}
}

type column map[int32]uint16

func (c column) BlockAt(_, y, _ int32) uint16 { return c[y] }

// TestHiveSedatedByCampfireSmoke checks the five cells of smoke: a lit
// campfire in reach sedates, one behind a solid block does only when it is
// right under that block, and an unlit or distant one does not.
func TestHiveSedatedByCampfireSmoke(t *testing.T) {
	lit := stateOf(t, "minecraft:campfire")
	stone := stateOf(t, "minecraft:stone")
	air := stateOf(t, "minecraft:air")
	for _, tc := range []struct {
		name string
		col  column
		want bool
	}{
		{"campfire five below", column{-5: lit}, true},
		{"campfire six below", column{-6: lit}, false},
		{"stone then campfire", column{-1: stone, -2: lit}, true},
		{"stone, air, campfire", column{-1: stone, -2: air, -3: lit}, false},
		{"no campfire", column{}, false},
	} {
		if got := HiveSedated(tc.col, 0, 0, 0); got != tc.want {
			t.Errorf("%s: sedated = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestHoneyAfterDelivery(t *testing.T) {
	for _, c := range []struct{ level, roll, want int }{
		{0, 5, 1}, {0, 0, 2}, {4, 0, 5}, {5, 0, 5}, {3, 99, 4},
	} {
		if got := HoneyAfterDelivery(c.level, c.roll); got != c.want {
			t.Errorf("HoneyAfterDelivery(%d, %d) = %d, want %d", c.level, c.roll, got, c.want)
		}
	}
}

func TestHiveOccupantDue(t *testing.T) {
	if _, due := HiveOccupantDue(600, 600); due {
		t.Error("due at exactly the minimum")
	}
	if next, due := HiveOccupantDue(601, 600); !due || next != 602 {
		t.Errorf("got %d %v", next, due)
	}
}

func TestBeeGrownCrop(t *testing.T) {
	for _, tc := range []struct {
		name     string
		props    map[string]string
		attracts bool
	}{
		{"minecraft:sunflower", map[string]string{"half": "upper"}, true},
		{"minecraft:sunflower", map[string]string{"half": "lower"}, false},
		{"minecraft:flowering_azalea_leaves", map[string]string{"waterlogged": "true"}, false},
		{"minecraft:flowering_azalea_leaves", map[string]string{"waterlogged": "false"}, true},
	} {
		if got := BeeAttracts(registry.MustState(tc.name, tc.props)); got != tc.attracts {
			t.Errorf("%s %v attractiveness = %v, want %v", tc.name, tc.props, got, tc.attracts)
		}
	}
	wheat := func(age string) uint16 {
		id, _ := registry.FindState("minecraft:wheat", map[string]string{"age": age})
		return id
	}
	if got, ok := BeeGrownCrop(wheat("3")); !ok || got != wheat("4") {
		t.Errorf("age 3 wheat: %d %v", got, ok)
	}
	if _, ok := BeeGrownCrop(wheat("7")); ok {
		t.Error("ripe wheat grew")
	}
	if _, ok := BeeGrownCrop(stateOf(t, "minecraft:stone")); ok {
		t.Error("stone grew")
	}
	if !BeeAttracts(stateOf(t, "minecraft:dandelion")) || BeeAttracts(stateOf(t, "minecraft:stone")) {
		t.Error("attractiveness")
	}
}

func TestBeeGrowsCaveVineBerriesNotTheirAge(t *testing.T) {
	for _, name := range []string{"minecraft:cave_vines", "minecraft:cave_vines_plant"} {
		props := map[string]string{"berries": "false"}
		if name == "minecraft:cave_vines" {
			props["age"] = "5"
		}
		plain, _ := registry.FindState(name, props)
		got, ok := BeeGrownCrop(plain)
		if !ok {
			t.Fatalf("%s did not grow", name)
		}
		_, raw, _ := registry.BlockStateName(got)
		next := registry.ParseStateProps(raw)
		if next["berries"] != "true" || (name == "minecraft:cave_vines" && next["age"] != "5") {
			t.Errorf("%s grew into %v", name, next)
		}
		if _, ok := BeeGrownCrop(got); ok {
			t.Errorf("%s with berries grew again", name)
		}
	}
}

func TestBeeGrowsOnlyWhatVanillaGrows(t *testing.T) {
	st := func(name string, props map[string]string) uint16 { return registry.MustState(name, props) }
	torch1 := st("minecraft:torchflower_crop", map[string]string{"age": "1"})
	for _, tc := range []struct {
		from uint16
		to   uint16
		ok   bool
	}{
		{st("minecraft:torchflower_crop", map[string]string{"age": "0"}), torch1, true},
		{torch1, registry.BlockTorchflower.DefaultState(), true},
		{st("minecraft:beetroots", map[string]string{"age": "2"}), st("minecraft:beetroots", map[string]string{"age": "3"}), true},
		{st("minecraft:beetroots", map[string]string{"age": "3"}), 0, false},
		{st("minecraft:melon_stem", map[string]string{"age": "6"}), st("minecraft:melon_stem", map[string]string{"age": "7"}), true},
		{st("minecraft:pumpkin_stem", map[string]string{"age": "7"}), 0, false},
		{st("minecraft:sweet_berry_bush", map[string]string{"age": "2"}), st("minecraft:sweet_berry_bush", map[string]string{"age": "3"}), true},
		{st("minecraft:sweet_berry_bush", map[string]string{"age": "3"}), 0, false},
		{st("minecraft:pitcher_crop", map[string]string{"age": "0", "half": "lower"}), 0, false},
		{st("minecraft:pitcher_crop", map[string]string{"age": "1", "half": "upper"}), 0, false},
	} {
		name, props, _ := registry.BlockStateName(tc.from)
		got, ok := BeeGrownCrop(tc.from)
		if ok != tc.ok || (ok && got != tc.to) {
			t.Errorf("%s[%s] grew to %d %v, want %d %v", name, props, got, ok, tc.to, tc.ok)
		}
	}
}
