package levelgen

import "strconv"

// MansionRoomKind selects a room collection's template category.
// vanilla: WoodlandMansionPieces.FloorRoomCollection
type MansionRoomKind uint8

const (
	MansionSmallRoom MansionRoomKind = iota
	MansionSmallSecret
	MansionMediumSide
	MansionMediumFront
	MansionMediumSecret
	MansionBigRoom
	MansionBigSecret
)

// MansionRoomTemplate chooses a template name without the woodland_mansion/
// prefix. Floor zero uses the first-floor collection; floors one and two
// share the second-floor collection. Only upper-floor medium rooms use stairs.
// vanilla: WoodlandMansionPieces.FirstFloorRoomCollection, SecondFloorRoomCollection
func MansionRoomTemplate(rng RandomSource, floor int, kind MansionRoomKind, stairs bool) string {
	prefix, count := "", int32(0)
	switch kind {
	case MansionSmallRoom:
		prefix, count = "1x1_a", 5
		if floor != 0 {
			prefix = "1x1_b"
		}
	case MansionSmallSecret:
		prefix, count = "1x1_as", 4
	case MansionMediumSide:
		prefix, count = "1x2_a", 9
		if floor != 0 {
			if stairs {
				return "1x2_c_stairs"
			}
			prefix, count = "1x2_c", 4
		}
	case MansionMediumFront:
		prefix, count = "1x2_b", 5
		if floor != 0 {
			if stairs {
				return "1x2_d_stairs"
			}
			prefix = "1x2_d"
		}
	case MansionMediumSecret:
		prefix, count = "1x2_s", 2
		if floor != 0 {
			prefix, count = "1x2_se", 1
		}
	case MansionBigRoom:
		prefix, count = "2x2_a", 4
		if floor != 0 {
			prefix, count = "2x2_b", 5
		}
	case MansionBigSecret:
		return "2x2_s1"
	default:
		panic("levelgen: invalid mansion room kind")
	}
	// Even a one-template collection consumes nextInt(1).
	return prefix + strconv.Itoa(int(rng.NextIntN(count))+1)
}
