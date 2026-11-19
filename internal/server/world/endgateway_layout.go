package world

import "github.com/golem-mc/golem/registry"

// EndGatewayState is the end gateway block's only state.
var EndGatewayState = registry.MustState("minecraft:end_gateway", nil)

var bedrockLayoutState = registry.MustState("minecraft:bedrock", nil)

// EndGatewayPortalCell is one write of the gateway structure.
type EndGatewayPortalCell struct {
	X, Y, Z int32
	State   uint16
}

// EndGatewayPortalCells is the structure around a new gateway: the gateway
// at the origin, bedrock caps two above and below, a bedrock pillar through
// the middle rows and air between.
// vanilla: EndGatewayFeature.place
func EndGatewayPortalCells(ox, oy, oz int32) []EndGatewayPortalCell {
	var out []EndGatewayPortalCell
	for z := oz - 1; z <= oz+1; z++ {
		for y := oy - 2; y <= oy+2; y++ {
			for x := ox - 1; x <= ox+1; x++ {
				sameX, sameY, sameZ := x == ox, y == oy, z == oz
				end := y == oy-2 || y == oy+2
				state := bedrockLayoutState
				switch {
				case sameX && sameY && sameZ:
					state = EndGatewayState
				case sameY, !sameX && !sameZ || end:
					state = airState
				}
				if end && sameX && sameZ {
					state = bedrockLayoutState
				}
				out = append(out, EndGatewayPortalCell{x, y, z, state})
			}
		}
	}
	return out
}
