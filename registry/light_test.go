package registry

import (
	"strings"
	"testing"
)

func TestLightEmissionPropertyDependent(t *testing.T) {
	for _, tc := range []struct {
		state string
		want  int
	}{
		{"minecraft:white_candle[candles=4,lit=true,waterlogged=false]", 12},
		{"minecraft:white_candle[candles=4,lit=false,waterlogged=false]", 0},
		{"minecraft:red_candle_cake[lit=true]", 3},
		{"minecraft:red_candle_cake[lit=false]", 0},
		{"minecraft:calibrated_sculk_sensor", 1},
		{"minecraft:sea_pickle[pickles=4,waterlogged=true]", 15},
		{"minecraft:sea_pickle[pickles=4,waterlogged=false]", 0},
		{"minecraft:light[level=15]", 15},
		{"minecraft:light[level=0]", 0},
		{"minecraft:trial_spawner[trial_spawner_state=active]", 8},
		{"minecraft:trial_spawner[trial_spawner_state=waiting_for_players]", 4},
		{"minecraft:trial_spawner[trial_spawner_state=cooldown]", 0},
		{"minecraft:vault[vault_state=active]", 12},
		{"minecraft:vault[vault_state=inactive]", 6},
	} {
		name, props, _ := strings.Cut(strings.TrimSuffix(tc.state, "]"), "[")
		id, ok := FindState(name, ParseStateProps(props))
		if !ok {
			t.Fatalf("no state %s", tc.state)
		}
		if got := LightEmission(id); got != tc.want {
			t.Errorf("LightEmission(%s) = %d, want %d", tc.state, got, tc.want)
		}
	}
}
