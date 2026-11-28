package mobs

import (
	"math"
	"strconv"
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestMobRiderSeatHeights: a mob rider stands at its vehicle's passenger
// attachment less its own vehicle attachment, including the spider rule and
// per-kind ridingOffset values.
// vanilla: Entity.positionRider, Spider.getVehicleAttachmentPoint
func TestMobRiderSeatHeights(t *testing.T) {
	t.Parallel()
	srv, _ := servertest.Join(t, servertest.HostileTestConfig(), "Rider", servertest.OpsListing(t, "Rider"))
	servertest.HoldTicks(t, srv)
	cases := []struct {
		vehicle, rider, extra string
		wantY                 float64
	}{
		{"pig", "pillager", "", -59.73125},
		{"ravager", "pillager", "", -58.3375},
		{"pig", "wither_skeleton", "", -60.00625},
		{"pig", "spider", "", -59.44375},
		{"witch", "zombie", "", -58.4375},
		{"pig", "zombie_villager", "IsBaby:1b,", -59.25625},
	}
	for i, tc := range cases {
		tag := "seat" + strconv.Itoa(i)
		srv.RunLine("summon minecraft:" + tc.vehicle + " " + strconv.Itoa(i*3) + " -60 0 {NoAI:1b,NoGravity:1b,Rotation:[0f,0f],Passengers:[{id:\"minecraft:" +
			tc.rider + "\"," + tc.extra + "NoAI:1b,NoGravity:1b,Tags:[\"" + tag + "\"]}]}")
	}
	servertest.RunTicks(t, srv, 3)
	for i, tc := range cases {
		tag := "seat" + strconv.Itoa(i)
		var got string
		for _, text := range srv.RunLine("data get entity @e[tag=" + tag + ",limit=1] Pos[1]") {
			if text.Translate == "commands.data.entity.query" && len(text.With) == 2 {
				got = text.With[1].Plain()
			}
		}
		y, err := strconv.ParseFloat(strings.TrimSuffix(got, "d"), 64)
		if err != nil || math.Abs(y-tc.wantY) > 1e-4 {
			t.Errorf("%s on %s: feet y = %q, want %v", tc.rider, tc.vehicle, got, tc.wantY)
		}
	}
}
