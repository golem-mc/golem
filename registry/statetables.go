package registry

// Per-state property tables indexed by state ID, built once from
// StateProps so the redstone hot path hashes no names. A state without the
// property, or no state, reads as the accessor's documented default.

// facingOrder is the Direction.values order the facing table encodes.
// vanilla: Direction.values
var facingOrder = map[string]uint8{"down": 0, "up": 1, "north": 2, "south": 3, "west": 4, "east": 5}

// WireSide values of the north/south/east/west wire properties.
const (
	WireSideNone uint8 = iota
	WireSideSide
	WireSideUp
)

// Wire side indexes for StateWireSide.
const (
	WireNorth = iota
	WireSouth
	WireEast
	WireWest
)

var (
	stateFacing    []uint8
	statePower     []uint8
	statePowered   []bool
	stateEnabled   []bool
	stateLit       []bool
	stateHasFacing []bool
	stateWire      [][4]uint8
)

func init() {
	n := len(stateByID)
	stateFacing = make([]uint8, n)
	statePower = make([]uint8, n)
	statePowered = make([]bool, n)
	stateEnabled = make([]bool, n)
	stateLit = make([]bool, n)
	stateHasFacing = make([]bool, n)
	stateWire = make([][4]uint8, n)
	sides := [4]string{"north", "south", "east", "west"}
	for id := range stateByID {
		props := stateByID[id].parsed
		if len(props) == 0 {
			stateFacing[id] = facingOrder["east"]
			continue
		}
		f, ok := facingOrder[props["facing"]]
		if !ok {
			f = facingOrder["east"]
		}
		stateFacing[id] = f
		_, stateHasFacing[id] = facingOrder[props["facing"]]
		stateEnabled[id] = props["enabled"] == "true"
		stateLit[id] = props["lit"] == "true"
		if v := props["power"]; v != "" {
			p := 0
			for _, c := range v {
				p = p*10 + int(c-'0')
			}
			statePower[id] = uint8(p)
		}
		statePowered[id] = props["powered"] == "true"
		for i, s := range sides {
			switch props[s] {
			case "side":
				stateWire[id][i] = WireSideSide
			case "up":
				stateWire[id][i] = WireSideUp
			}
		}
	}
}

// StateFacing returns the "facing" property as a Direction.values index
// (down, up, north, south, west, east); a state without the property reads as
// east.
func StateFacing(id uint16) int {
	if int(id) >= len(stateFacing) {
		return int(facingOrder["east"])
	}
	return int(stateFacing[id])
}

// StateHasFacing reports whether the state has a "facing" property.
func StateHasFacing(id uint16) bool {
	return int(id) < len(stateHasFacing) && stateHasFacing[id]
}

// StateEnabled reports whether the "enabled" property is true.
func StateEnabled(id uint16) bool {
	return int(id) < len(stateEnabled) && stateEnabled[id]
}

// StateLit reports whether the "lit" property is true.
func StateLit(id uint16) bool {
	return int(id) < len(stateLit) && stateLit[id]
}

// StatePower returns the "power" property, 0 for a state without it.
func StatePower(id uint16) int {
	if int(id) >= len(statePower) {
		return 0
	}
	return int(statePower[id])
}

// StatePowered reports whether the "powered" property is true.
func StatePowered(id uint16) bool {
	return int(id) < len(statePowered) && statePowered[id]
}

// StateWireSide returns a wire's connection on side (WireNorth, WireSouth,
// WireEast, WireWest) as WireSideNone, WireSideSide or WireSideUp.
func StateWireSide(id uint16, side int) uint8 {
	if int(id) >= len(stateWire) {
		return WireSideNone
	}
	return stateWire[id][side]
}

// FacingIndex maps a facing name to its Direction.values index; a name that
// is no direction reads as east.
func FacingIndex(name string) int {
	if f, ok := facingOrder[name]; ok {
		return int(f)
	}
	return int(facingOrder["east"])
}
