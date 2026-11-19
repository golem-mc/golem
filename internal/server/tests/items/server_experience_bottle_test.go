package items

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"math"
	"testing"
	"time"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/player"
)

// TestABottleOEnchantingBurstsIntoOrbsOnTheGround: a bottle thrown at the
// floor is gone on landing and leaves experience orbs where it fell.
// vanilla: ThrownExperienceBottle.onHit
func TestABottleOEnchantingBurstsIntoOrbsOnTheGround(t *testing.T) {
	t.Parallel()
	srv, _, _ := servertest.BoatPlayer(t, "survival", "Thrower", 0)
	if id := srv.ThrowThrown(entity.ThrownExperienceBottle, 9.5, 66, 9.5, 0, 90, 0, 7); id == 0 {
		t.Fatal("the bottle never flew")
	}
	servertest.PollUntil(t, 30*time.Second, func() bool { return srv.OrbCount() > 0 })
	if thrown := srv.ThrownInDimension("minecraft:overworld"); len(thrown) != 0 {
		t.Errorf("%d thrown bodies in the world, want the bottle gone", len(thrown))
	}
}

// TestAThrownBottleOEnchantingIsSpentAndFlies: using the item throws one
// bottle and spends it.
// vanilla: ExperienceBottleItem.use
func TestAThrownBottleOEnchantingIsSpentAndFlies(t *testing.T) {
	t.Parallel()
	srv, c, _ := servertest.BoatPlayer(t, "survival", "Thrower", 0)
	srv.GiveSlot("Thrower", player.HotbarSlot, protocol.ItemStack{ItemID: servertest.MustItemID(t, "minecraft:experience_bottle"), Count: 2})
	c.UseBoatItem(0, -45)
	servertest.PollUntil(t, 30*time.Second, func() bool { return len(srv.ThrownInDimension("minecraft:overworld")) == 1 })
	if got := srv.InventorySlot("Thrower", player.HotbarSlot).Count; got != 1 {
		t.Errorf("bottles left = %d, want 1", got)
	}
}

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

// TestABottleOEnchantingHittingACowBurstsAgainstItsFlight checks the burst
// on a body: world event 2002 with the bottle's colour, three to eleven
// experience, and the orbs placed back against the flight.
// vanilla: ThrownExperienceBottle.onHit
func TestABottleOEnchantingHittingACowBurstsAgainstItsFlight(t *testing.T) {
	t.Parallel()
	srv, c, _ := servertest.JoinAs(t, servertest.BowTestConfig(), "Thrower")
	// Sixteen blocks from the player, whom a nearer orb would fly to and
	// vanish into.
	cowID := srv.SpawnMob("minecraft:cow", 0.5, 64, 17.5)
	c.ExpectMobSpawn(protocol.EntityTypeCow)
	// Held, so the orbs are read at the tick of the burst, before they fall
	// or merge.
	servertest.HoldTicks(t, srv)
	srv.MoveMob(cowID, 0.5, 17.5)
	srv.ThrowThrown(entity.ThrownExperienceBottle, 0.5, 65, 16, 0, 20, 0, 7)

	var event, colour int32
	c.ReadTicks(srv, 100, func(id int32, data []byte) bool {
		if id != protocol.IDPlayWorldEvent {
			return false
		}
		r := protocol.NewReader(data)
		if event = r.ReadI32(); event == protocol.WorldEventPotionSplash {
			r.ReadPosition()
			colour = r.ReadI32()
			return true
		}
		return false
	})
	if event != protocol.WorldEventPotionSplash || colour != -13083194 {
		t.Fatalf("world event %d with colour %d, want 2002 with -13083194", event, colour)
	}
	servertest.UntilTicks(t, srv, 1, "orbs from the burst", func() bool { return len(srv.OrbsInWorld()) > 0 })
	xp := 0
	for _, o := range srv.OrbsInWorld() {
		xp += o.XP
		// The bottle struck the cow's near face, z=17.05; the orbs spawn a
		// quarter block back from it, against the flight.
		if o.Z >= 17.05 {
			t.Errorf("an orb stands at z=%v, want in front of the cow's face at 17.05", o.Z)
		}
	}
	if xp < 3 || xp > 11 {
		t.Errorf("the burst holds %d experience, want 3 + two rolls of 0..4", xp)
	}
}
