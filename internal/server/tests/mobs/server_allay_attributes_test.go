package mobs

import (
	"fmt"
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// vanilla: FlyingMoveControl.tick, Allay.travel, LivingEntity.travelFlying
func TestAllayFlightUsesLiveFlyingSpeed(t *testing.T) {
	t.Parallel()
	srv, c := allayWorld(t, "", 0)
	if !srv.SetBlock(12, 79, 0, registry.BlockNoteBlock.DefaultState()) {
		t.Fatal("could not place the destination note block")
	}
	// A delivery keeps the AI steering toward a known point in open air.
	srv.RunLine(`summon minecraft:allay 2.5 80 0.5 {equipment:{mainhand:{id:"minecraft:diamond",count:1}},Inventory:[{id:"minecraft:diamond",count:1}],Brain:{memories:{"minecraft:liked_noteblock":{value:{dimension:"minecraft:overworld",pos:[I;12,79,0]}},"minecraft:liked_noteblock_cooldown_ticks":{value:600}}}}`)
	id := c.ExpectMobSpawn(allayType()).EntityID
	for _, speed := range []float64{0, 0.05, 0.2, 0.4, 0, 0.1} {
		feedback := srv.RunLine(fmt.Sprintf("attribute @e[type=minecraft:allay,limit=1] minecraft:flying_speed base set %g", speed))
		if len(feedback) != 1 || feedback[0].Translate != "commands.attribute.base_value.set.success" {
			t.Fatalf("setting flying_speed to %g: %v", speed, feedback)
		}
		vx, vy, vz, ok := srv.MobVelocity(id)
		if !ok {
			t.Fatal("allay disappeared")
		}
		servertest.RunTicks(t, srv, 1)
		nx, ny, nz, ok := srv.MobVelocity(id)
		if !ok {
			t.Fatal("allay disappeared")
		}
		// Delivery's 2.25 pace sets both the control input and air acceleration;
		// combined forward/vertical input above unit length is normalized.
		// Existing momentum still coasts when the attribute becomes zero.
		controlSpeed := 2.25 * speed
		want := math.Min(controlSpeed, 1/math.Sqrt2) * controlSpeed
		horizontal := math.Hypot(nx/0.91-vx, nz/0.91-vz)
		vertical := math.Abs(ny/0.91 - vy)
		if math.Abs(horizontal-want) > 1e-6 {
			t.Errorf("flying_speed %g: horizontal acceleration %g, want %g", speed, horizontal, want)
		}
		if math.Abs(vertical-want) > 1e-6 {
			t.Errorf("flying_speed %g: vertical acceleration %g, want %g", speed, vertical, want)
		}
		if total := math.Hypot(horizontal, vertical); total > controlSpeed+1e-6 {
			t.Errorf("flying_speed %g: total acceleration %g exceeds control speed %g", speed, total, controlSpeed)
		}
	}
}
