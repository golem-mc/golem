package world

import "testing"

// vanilla: data/minecraft/timeline/day.json,
// EnvironmentAttributeSystem.addDynamicLayers
func TestDayTimelineAttributesFollowDimensionType(t *testing.T) {
	t.Parallel()
	for _, typ := range []string{DimensionType, "minecraft:overworld_caves", NetherType, EndType} {
		for _, name := range []string{DimensionName, "pack:sky"} {
			t.Run(typ+"/"+name, func(t *testing.T) {
				w := &World{dimName: name, dim: dimensionShape{typ: typ}, clocks: defaultClocks(), level: &LevelData{}}
				day := typ == DimensionType || typ == "minecraft:overworld_caves"
				for _, tc := range []struct {
					time                      int64
					burn, hive, flower, heart bool
					hatch                     float64
					gift                      float32
				}{
					{6000, true, false, false, false, 0.002, 0},
					{14000, false, true, true, true, 0.002, 0},
					{21500, false, true, true, true, 1, 0},
					{23800, true, false, false, false, 0.002, 0.7},
				} {
					w.SetClockTime(OverworldClock, tc.time)
					if got := w.MonstersBurn(); got != (day && tc.burn) {
						t.Errorf("time %d: MonstersBurn = %v", tc.time, got)
					}
					if got := w.BeesStayInHive(); got != (day && tc.hive) {
						t.Errorf("time %d: BeesStayInHive = %v", tc.time, got)
					}
					if open, ok := w.eyeblossomOpen(); open != (day && tc.flower) || ok != day {
						t.Errorf("time %d: eyeblossomOpen = %v, %v", tc.time, open, ok)
					}
					if got := w.CreakingActive(); got != (day && tc.heart) {
						t.Errorf("time %d: CreakingActive = %v", tc.time, got)
					}
					hatch, gift := float64(float32(tc.hatch)), tc.gift
					if !day {
						hatch, gift = float64(float32(turtleEggHatchChance)), 0
					}
					if got := w.turtleEggHatchChanceNow(); got != hatch {
						t.Errorf("time %d: turtleEggHatchChanceNow = %v, want %v", tc.time, got, hatch)
					}
					if got := w.CatGiftChance(); got != gift {
						t.Errorf("time %d: CatGiftChance = %v, want %v", tc.time, got, gift)
					}
				}
			})
		}
	}
}
