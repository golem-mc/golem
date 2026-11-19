package redstone

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestSneakingPastASculkSensorObtainsAvoidVibration: a sneaking landing the sensor
// would have heard grants avoid_vibration and leaves the sensor inactive; a
// standing landing activates it and grants nothing, and so does a sneaking
// landing out of the sensor's range.
// vanilla: VibrationSystem.Listener.handleGameEvent
func TestSneakingPastASculkSensorObtainsAvoidVibration(t *testing.T) {
	t.Parallel()
	const adv, criterion = "minecraft:adventure/avoid_vibration", "avoid_vibration"
	cases := []struct {
		name      string
		sneak     bool
		sensorX   int32
		wantGrant bool
		wantPhase string
	}{
		{"sneaking in range", true, 3, true, "inactive"},
		{"standing in range", false, 3, false, "active"},
		{"sneaking out of range", true, 20, false, "inactive"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, c := servertest.Join(t, servertest.SurvivalTestConfig(), "Sneaker")
			srv.SetBlock(tc.sensorX, 64, 0, servertest.MustState(t, "minecraft:sculk_sensor", nil))
			servertest.HoldTicks(t, srv)
			c.SendSneak(tc.sneak)
			c.Settle()
			// Three blocks: no fall damage, whose entity_damage sneaking does not hide.
			c.MoveToGround(0.5, 67, 0.5, false)
			c.MoveToGround(0.5, 64, 0.5, true)
			c.Settle()
			servertest.RunTicks(t, srv, 3)
			if got := srv.AdvancementObtained("Sneaker", adv, criterion); got != tc.wantGrant {
				t.Errorf("avoid_vibration obtained = %v, want %v", got, tc.wantGrant)
			}
			servertest.ExpectBlockNamed(t, srv, tc.sensorX, 64, 0, "minecraft:sculk_sensor",
				map[string]string{"sculk_sensor_phase": tc.wantPhase})
		})
	}
}
