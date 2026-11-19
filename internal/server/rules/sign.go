package rules

import (
	"math"
	"strings"

	"github.com/golem-mc/golem/registry"
)

// The yaw each horizontal direction's front text faces, the yaw the two
// wall-sign kinds read off their facing property. vanilla: Direction.toYRot
var signFacingYaw = map[string]float64{
	"north": 180,
	"south": 0,
	"west":  90,
	"east":  270,
}

// signPanelCentre is where a wall sign's panel sits in its cell, per the
// facing the text looks along: the slice against the wall behind, a shape
// the standing and ceiling-hanging signs do not have and so do not need.
// vanilla: WallSignBlock.getSignHitboxCenterPosition
var signPanelCentre = map[string][2]float64{
	"north": {0.5, 0.9375},
	"south": {0.5, 0.0625},
	"west":  {0.9375, 0.5},
	"east":  {0.0625, 0.5},
}

// SignFrontFacing reports whether the player at (px, pz) faces the sign's
// front: the bearing from the panel centre to the player is within a
// quarter turn of the front's yaw. Standing and ceiling-hanging signs use
// their rotation segment, wall ones their facing.
// vanilla: SignBlockEntity.isFacingFrontText
func SignFrontFacing(state uint16, x, z int32, px, pz float64) bool {
	frontYaw := 0.0
	cx, cz := 0.5, 0.5
	if segment, ok := registry.StateInt(state, registry.PropRotation); ok {
		// vanilla: RotationSegment.convertToDegrees
		frontYaw = float64(segment) * 22.5
	} else if facing := registry.StateValue(state, registry.PropFacing); facing != "" {
		yaw, known := signFacingYaw[facing]
		if !known {
			return false
		}
		frontYaw = yaw
		// Only a wall sign's panel sits against its wall; a wall-hanging
		// sign stays at the cell centre.
		// vanilla: WallSignBlock.getSignHitboxCenterPosition
		if registry.BlockOf(state).InGroup(registry.GroupWallSign) {
			if centre, ok := signPanelCentre[facing]; ok {
				cx, cz = centre[0], centre[1]
			}
		}
	} else {
		return false
	}
	// atan2 measures from east while yaws run from south.
	playerYaw := math.Atan2(pz-(float64(z)+cz), px-(float64(x)+cx))*180/math.Pi - 90
	// The difference wrapped to [0, 180]: within a half turn is the front.
	// vanilla: Mth.degreesDifferenceAbs
	return math.Abs(math.Remainder(playerYaw-frontYaw, 360)) <= 90
}

// StripFormatting removes legacy § codes, the only sanitising vanilla's
// sign and book handlers do. § is two bytes in UTF-8, so a code is that
// pair plus a code character.
// vanilla: ChatFormatting.stripFormatting
func StripFormatting(line string) string {
	if !strings.Contains(line, "§") {
		return line
	}
	var out strings.Builder
	out.Grow(len(line))
	for i := 0; i < len(line); i++ {
		if i+2 < len(line) && line[i] == 0xC2 && line[i+1] == 0xA7 && isFormatCode(line[i+2]) {
			i += 2
			continue
		}
		out.WriteByte(line[i])
	}
	return out.String()
}

// isFormatCode reports whether the byte names one of ChatFormatting's codes:
// a digit, one of A-F's colours, one of K-O's styles, or the reset.
func isFormatCode(c byte) bool {
	lower := c | 0x20
	return (c >= '0' && c <= '9') || (lower >= 'a' && lower <= 'f') ||
		(lower >= 'k' && lower <= 'o') || lower == 'r'
}
