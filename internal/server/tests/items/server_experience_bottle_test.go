package items

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
)

// TestAUsedBottleOEnchantingFliesAtItsOwnPowerWithItsThrowSound checks the
// birth of a thrown bottle: 0.7 blocks a tick along the look lifted twenty
// degrees, and the neutral throw sound at the thrower's feet.
// vanilla: ExperienceBottleItem.use
func TestAUsedBottleOEnchantingFliesAtItsOwnPowerWithItsThrowSound(t *testing.T) {
	t.Parallel()
	srv, c, _ := servertest.JoinAs(t, servertest.BowTestConfig(), "Thrower")
	srv.GiveSlot("Thrower", 36, protocol.ItemStack{ItemID: servertest.MustItem("minecraft:experience_bottle"), Count: 4})

	c.MoveTo(0.5, 64, 0.5, -90, 0)
	servertest.HoldTicks(t, srv)
	servertest.UseHeld(c, -90, 0)
	traffic := servertest.ReadThrownTraffic(c, srv, 20)
	bottle := traffic.SpawnOfType(protocol.EntityTypeExperienceBottle)
	if bottle == nil {
		t.Fatal("no bottle came into the world for the use")
	}
	// Level aim lifted 20 degrees: (cos 20, sin 20) * 0.7 = (0.658, 0.239),
	// within the throw's spread.
	if speed := math.Sqrt(bottle.Vx*bottle.Vx + bottle.Vy*bottle.Vy + bottle.Vz*bottle.Vz); speed < 0.67 || speed > 0.73 {
		t.Errorf("the bottle flies at %v, want 0.7", speed)
	}
	if bottle.Vy < 0.2 || bottle.Vy > 0.28 || bottle.Vx < 0.6 || bottle.Vx > 0.72 {
		t.Errorf("the bottle flies (%v, %v), want the look lifted twenty degrees", bottle.Vx, bottle.Vy)
	}
	var throw *servertest.ThrownSound
	for i := range traffic.Sounds {
		if traffic.Sounds[i].Name == "minecraft:entity.experience_bottle.throw" {
			throw = &traffic.Sounds[i]
		}
	}
	if throw == nil {
		t.Fatal("no bottle throw sound was played")
	}
	if throw.Category != protocol.SoundCategoryNeutral || throw.Volume != 0.5 {
		t.Errorf("throw sound category %d volume %v, want neutral at half", throw.Category, throw.Volume)
	}
	if throw.X != 0.5 || throw.Y != 64 || throw.Z != 0.5 {
		t.Errorf("throw sound at (%v %v %v), want the thrower's feet", throw.X, throw.Y, throw.Z)
	}
}
