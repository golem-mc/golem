package mobs

import (
	"fmt"
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
)

// A mob taken off a mount ends where the mount's own dismount rule puts it:
// beside an animal or equine, on top of a happy ghast.
// vanilla: LivingEntity.dismountVehicle, AbstractHorse/Animal/HappyGhast/Strider.getDismountLocationForPassenger
func TestMobRiderDismountSpot(t *testing.T) {
	t.Parallel()
	const floor = 64
	cases := []struct {
		vehicle    string
		x, z       float64
		wx, wy, wz float64
	}{
		{"horse", 30.5, 48.5, 29.50175, floor, 48.5},
		{"donkey", 30.5, 48.5, 30.5 - 0.99825, floor, 48.5},
		{"llama", 6.5, 0.5, 5.749995, floor, 0.5},
		{"camel", 30.5, 12.5, 29.349995, floor, 12.5},
		{"hoglin", 18.5, 48.5, 17.5, floor, 48.5},
		{"happy_ghast", 12.5, 48.5, 12.5, floor + 4, 48.5},
		{"strider", 6.5, 42.5, 6.5, floor, 43.5},
	}
	srv, c := servertest.Join(t, servertest.HostileTestConfig(), "Watcher")
	servertest.HoldTicks(t, srv)
	c.MoveToGround(0.5, floor, 0.5, true)
	for _, tc := range cases {
		srv.RunLine(fmt.Sprintf(`summon %s %v %d %v {NoAI:1b,NoGravity:1b,Rotation:[0f,0f],Passengers:[{id:"zombie",NoAI:1b}]}`,
			tc.vehicle, tc.x, floor, tc.z))
		z := srv.MobIDsOfKind("minecraft:zombie")
		if len(z) != 1 {
			t.Fatalf("%s: zombies = %v", tc.vehicle, z)
		}
		srv.RunLine(`ride @e[type=zombie,limit=1] dismount`)
		x, y, zz, ok := srv.MobPos(z[0])
		if !ok {
			t.Fatalf("%s: no zombie position", tc.vehicle)
		}
		if math.Abs(x-tc.wx) > 1e-4 || math.Abs(y-tc.wy) > 1e-4 || math.Abs(zz-tc.wz) > 1e-4 {
			t.Errorf("%s: zombie at %v %v %v, want %v %v %v", tc.vehicle, x, y, zz, tc.wx, tc.wy, tc.wz)
		}
		srv.RunLine(`kill @e[type=!player]`)
	}
}
