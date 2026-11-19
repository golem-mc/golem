package entities

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// boatOverColumn boards a rider on a boat held at the top of a bubble column
// (dragging over magma, lifting over soul sand), its hull above the column's
// water so it is not sunk out of control, and reads what the rider's
// client is sent over the next 61 ticks: whether the boat's metadata showed
// a running bubble time, the boat's entity events in order, and whether the
// boat was emptied. An emptied boat must then sink below 64 within 3 ticks.
func boatOverColumn(t *testing.T, name string, drag bool) (bubbled bool, events []uint8, emptied bool) {
	t.Helper()
	srv, c, _ := servertest.BoatPlayer(t, "creative", name, 0)
	boatID := boatRidden(t, srv, c, 0, 2.5, 64.5, 2.5)
	floor, dragProp := "minecraft:soul_sand", "false"
	if drag {
		floor, dragProp = "minecraft:magma_block", "true"
	}
	base := servertest.MustState(t, floor, nil)
	column := servertest.MustState(t, "minecraft:bubble_column", map[string]string{"drag": dragProp})
	for x := int32(1); x <= 3; x++ {
		for z := int32(1); z <= 3; z++ {
			srv.SetBlock(x, 62, z, base)
			srv.SetBlock(x, 63, z, column)
			srv.SetBlock(x, 64, z, column)
		}
	}
	c.ReadTicksSkipping(srv, 61, servertest.BoatSkip, func(id int32, data []byte) bool {
		r := protocol.NewReader(data)
		switch id {
		case protocol.IDPlayEntityMetadata:
			if r.ReadVarInt() != boatID {
				return false
			}
			for r.Err() == nil {
				index := r.ReadU8()
				if index == protocol.MetadataEnd {
					break
				}
				switch r.ReadVarInt() {
				case protocol.MetadataVarInt:
					if v := r.ReadVarInt(); index == protocol.MetadataIndexBoatBubbleTime && v != 0 && len(events) == 0 {
						bubbled = true
					}
				case protocol.MetadataFloat:
					r.ReadF32()
				case protocol.MetadataBoolean:
					r.ReadBool()
				}
			}
		case protocol.IDPlayEntityEvent:
			if r.ReadI32() == boatID {
				events = append(events, r.ReadU8())
			}
		case protocol.IDPlaySetPassengers:
			if vehicle, riders := servertest.ReadPassengers(c, data); vehicle == boatID && len(riders) == 0 {
				emptied = true
			}
		}
		return len(events) > 0 && (!drag || emptied)
	})
	if emptied {
		// The server runs the emptied hull from the sink's -0.7: a block
		// down within a few ticks, where a hull left without it dips under
		// a tenth and floats back. vanilla: AbstractBoat.tickBubbleColumn
		servertest.UntilTicks(t, srv, 3, "the emptied boat sinking below 64", func() bool {
			_, y, _, _, _, ok := srv.BoatState(boatID)
			return ok && y < 64
		})
	}
	return bubbled, events, emptied
}
