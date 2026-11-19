package items

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// vanilla: MapItemSavedData.hasMapInvisibilityItemEquipped
func TestBodyAndSaddleEquipmentHideMapHolders(t *testing.T) {
	t.Parallel()
	srv, viewer, wearer := holdersOfMapZero(t)
	servertest.HoldTicks(t, srv)
	x := 5.5
	check := func(t *testing.T, wantOthers int) {
		t.Helper()
		// Moving the wearer also forces a fresh list on their own map, where
		// equipping an invisibility item must preserve both player markers.
		x++
		wearer.MoveTo(x, 64, 5.5, 0, 0)
		wearer.Settle()
		viewer.Drain()
		servertest.RunTicks(t, srv, 5)
		for _, tc := range []struct {
			client *servertest.Client
			want   int
		}{
			{viewer, wantOthers},
			{wearer, 2},
		} {
			var latest servertest.MapDataUpdate
			found := false
			tc.client.ReadTicks(srv, 0, func(id int32, data []byte) bool {
				if id == protocol.IDPlayMapData {
					u := servertest.ParseMapData(t, data)
					if u.ID() == 0 && u.HasIcons {
						latest, found = u, true
					}
				}
				return false
			})
			if !found || playerMarkers(latest) != tc.want {
				t.Fatalf("map markers = %+v (received %v), want %d players", latest.Icons, found, tc.want)
			}
		}
	}
	check(t, 2)
	for _, slot := range []struct{ command, equipment string }{
		{"armor.body", "body"},
		{"saddle", "saddle"},
	} {
		t.Run(slot.equipment, func(t *testing.T) {
			srv.RunLine(`item replace entity Bystander ` + slot.command + ` with minecraft:carved_pumpkin[minecraft:equippable={slot:"` + slot.equipment + `"}]`)
			check(t, 1)
			srv.RunLine(`item replace entity Bystander ` + slot.command + ` with minecraft:air`)
			check(t, 2)
		})
	}
}
