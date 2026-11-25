package rules

import (
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// FullHiveAt reads the hive or nest in the cell given and reports its state
// and whether its honey level is full.
// vanilla: BeehiveBlock.HONEY_LEVEL, MAX_HONEY_LEVELS
func FullHiveAt(w BlockReader, x, y, z int32) (state uint16, full bool) {
	state = w.BlockAt(x, y, z)
	if !registry.BlockOf(state).InTag(registry.TagIDBeehives) {
		return 0, false
	}
	level, _ := registry.StateInt(state, registry.PropHoneyLevel)
	return state, level == hiveMaxHoneyLevel
}

// EmptiedHive is the state of a hive with its honey level back to 0.
// vanilla: BeehiveBlock.resetHoneyLevel
func EmptiedHive(state uint16) uint16 {
	state, _ = registry.WithInt(state, registry.PropHoneyLevel, 0)
	return state
}

// HiveSedated reports whether campfire smoke reaches the hive at (x, y, z),
// reading blocks through blockAt.
// vanilla: CampfireBlock.isSmokeyPos
func HiveSedated(blockAt func(x, y, z int32) uint16, x, y, z int32) bool {
	return world.SmokeyColumn(blockAt, x, y, z)
}

// HiveFireNearby reports whether a fire block stands within one cell of the
// hive, which empties it at once.
// vanilla: BeehiveBlockEntity.isFireNearby
func HiveFireNearby(w BlockReader, x, y, z int32) bool {
	for dx := int32(-1); dx <= 1; dx++ {
		for dy := int32(-1); dy <= 1; dy++ {
			for dz := int32(-1); dz <= 1; dz++ {
				if registry.BlockOf(w.BlockAt(x+dx, y+dy, z+dz)) == registry.BlockFire {
					return true
				}
			}
		}
	}
	return false
}

// HiveReleaseSpot is where a bee leaves a hive facing (dx, dz): in the front
// cell's far side unless the front cell is solid, centred at the box's
// middle height.
// vanilla: BeehiveBlockEntity.releaseOccupant
func HiveReleaseSpot(x, y, z, dx, dz int32, frontBlocked bool, width, height float64) (float64, float64, float64) {
	delta := 0.0
	if !frontBlocked {
		delta = 0.55 + width/2
	}
	return float64(x) + 0.5 + delta*float64(dx), float64(y) + 0.5 - height/2, float64(z) + 0.5 + delta*float64(dz)
}

// HiveFacingStep is the horizontal step toward the side a hive faces.
func HiveFacingStep(facing string) (dx, dz int32) {
	switch facing {
	case "north":
		return 0, -1
	case "south":
		return 0, 1
	case "west":
		return -1, 0
	case "east":
		return 1, 0
	}
	return 0, 0
}

// Hive occupancy numbers.
// vanilla: BeehiveBlockEntity.MIN_OCCUPATION_TICKS_NECTAR, MIN_OCCUPATION_TICKS_NECTARLESS, MAX_OCCUPANTS
const (
	HiveMaxOccupants        = 3
	HiveMinTicksNectar      = 2400
	HiveMinTicksNectarless  = 600
	hiveMaxHoneyLevel       = 5
	hiveStayOutAfterRelease = 400
)

// HiveStayOutTicks is the stay-out countdown of a bee a sedated hive frees
// at a player instead of angering it.
// vanilla: BeehiveBlockEntity.emptyAllLivingFromHive
const HiveStayOutTicks = hiveStayOutAfterRelease

// HiveOccupantDue advances an occupant's clock by one tick and reports
// whether it has stayed long enough to leave; the comparison reads the count
// from before the tick.
// vanilla: BeehiveBlockEntity.BeeData.tick
func HiveOccupantDue(ticksInHive, minTicks int) (next int, due bool) {
	return ticksInHive + 1, ticksInHive > minTicks
}

// HoneyAfterDelivery is the honey_level a hive has once a bee that carried
// nectar leaves it. One delivery in a hundred adds two levels, trimmed to
// the five the block holds. roll is a uniform draw below 100.
// vanilla: BeehiveBlockEntity.releaseOccupant
func HoneyAfterDelivery(level, roll int) int {
	if level >= hiveMaxHoneyLevel {
		return level
	}
	add := 1
	if roll == 0 {
		add = 2
	}
	if level+add > hiveMaxHoneyLevel {
		add--
	}
	return level + add
}
