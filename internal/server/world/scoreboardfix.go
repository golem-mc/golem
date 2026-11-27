package world

import (
	"strconv"
	"strings"

	"github.com/golem-mc/golem/nbt"
)

// UpgradeScoreboard brings a scoreboard.dat's data compound saved at a
// DataVersion to the current shape.
// vanilla: DataFixTypes.SAVED_DATA_SCOREBOARD
func UpgradeScoreboard(data nbt.Compound, version int32) {
	if version < 3566 {
		fixDisplaySlots(data)
	}
	if version < 4290 {
		unflattenScoreboardText(data)
	}
}

// slotColors are the team colors of display slots 3..18.
var slotColors = []string{
	"black", "dark_blue", "dark_green", "dark_aqua", "dark_red", "dark_purple",
	"gold", "gray", "dark_gray", "blue", "green", "aqua", "red", "light_purple",
	"yellow", "white",
}

// fixDisplaySlots renames the slot_N keys of DisplaySlots.
// vanilla: ScoreboardDisplaySlotFix.SLOT_RENAMES
func fixDisplaySlots(data nbt.Compound) {
	slots, ok := nbt.Get[nbt.Compound](data, "DisplaySlots")
	if !ok {
		return
	}
	for key, tag := range slots {
		n, found := strings.CutPrefix(key, "slot_")
		if !found {
			continue
		}
		i, err := strconv.Atoi(n)
		var name string
		switch {
		case err != nil || i < 0 || i > 18:
			continue
		case i == 0:
			name = "list"
		case i == 1:
			name = "sidebar"
		case i == 2:
			name = "below_name"
		default:
			name = "sidebar.team." + slotColors[i-3]
		}
		delete(slots, key)
		slots[name] = tag
	}
}

// unflattenScoreboardText turns the JSON text fields of objectives and teams
// into components.
// vanilla: UnflattenTextComponentFix
func unflattenScoreboardText(data nbt.Compound) {
	for list, keys := range map[string][]string{
		"Objectives": {"DisplayName"},
		"Teams":      {"DisplayName", "MemberNamePrefix", "MemberNameSuffix"},
	} {
		items, _ := nbt.Get[nbt.List](data, list)
		for _, tag := range items.Items {
			if entry, ok := tag.(nbt.Compound); ok {
				for _, key := range keys {
					unflattenText(entry, key)
				}
			}
		}
	}
}
