package mobs

import (
	"bytes"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestAWitchThrowsSplashPotionsAtAPlayer stands a witch eight blocks off a
// survival player and watches the wire: the witch throws a splash potion
// it owns, born at its eyes less a tenth, and keeps throwing.
func TestAWitchThrowsSplashPotionsAtAPlayer(t *testing.T) {
	t.Parallel()
	srv, c, _ := servertest.JoinAs(t, servertest.BowTestConfig(), "Prey")

	witchID := srv.SpawnMob("minecraft:witch", 8.5, 64, 0.5)
	var potions []servertest.ThrownSpawn
	c.ReadTicks(srv, 400, func(id int32, data []byte) bool {
		if id == protocol.IDPlaySpawnEntity {
			if sp := servertest.ReadThrownSpawn(c, data); sp.EntityType == protocol.EntityTypeSplashPotion {
				potions = append(potions, sp)
			}
		}
		return len(potions) >= 2
	})
	if len(potions) < 2 {
		t.Fatalf("a witch eight blocks from a player threw %d splash potions in 400 ticks, want at least 2", len(potions))
	}
	for _, p := range potions {
		if p.OwnerID != witchID {
			t.Errorf("a potion's owner = %d, want the witch %d", p.OwnerID, witchID)
		}
	}
}

// TestADrinkingWitchResendsItsMovementSpeed hurts a witch so it drinks
// healing, and expects Update Attributes carrying the drinking modifier
// once the drink starts, after the spawn's own packet without it.
func TestADrinkingWitchResendsItsMovementSpeed(t *testing.T) {
	t.Parallel()
	srv, c, _ := servertest.JoinAs(t, servertest.BowTestConfig(), "Watcher")

	witchID := srv.SpawnMob("minecraft:witch", 8.5, 64, 0.5)
	srv.HurtMobByWorld(witchID, 1, "minecraft:generic")
	var withModifier, without int
	c.ReadTicks(srv, 600, func(id int32, data []byte) bool {
		if id != protocol.IDPlayUpdateAttributes || protocol.NewReader(data).ReadVarInt() != witchID {
			return false
		}
		if bytes.Contains(data, []byte("minecraft:drinking")) {
			withModifier++
		} else {
			without++
		}
		return withModifier > 0
	})
	if withModifier == 0 {
		t.Fatalf("a drinking witch never sent the drinking modifier (%d other attribute packets)", without)
	}
}

// TestAWitchIsUnhurtByItsOwnHarmingSplash lands a harming splash the witch
// owns on its own feet: the witch keeps its health.
func TestAWitchIsUnhurtByItsOwnHarmingSplash(t *testing.T) {
	t.Parallel()
	srv, _, _ := servertest.JoinAs(t, servertest.BowTestConfig(), "Bystander")

	witchID := srv.SpawnMob("minecraft:witch", 8.5, 64, 0.5)
	srv.SetMobNoAI(witchID)
	before, ok := srv.MobHealth(witchID)
	if !ok {
		t.Fatal("the witch is not in the herd")
	}
	srv.ThrowMobPotion("minecraft:harming", 8.5, 65, 0.5, witchID)
	srv.TickEntities(10)
	srv.TickMobs(3)
	if after, _ := srv.MobHealth(witchID); after != before {
		t.Errorf("a witch's own harming splash took its health from %v to %v, want it unchanged", before, after)
	}
}

// TestAHarmingSplashHurtsAWitchItsThrowerIsNot is the control: the same
// splash owned by another body does hurt the witch.
func TestAHarmingSplashHurtsAWitchItsThrowerIsNot(t *testing.T) {
	t.Parallel()
	srv, _, _ := servertest.JoinAs(t, servertest.BowTestConfig(), "Bystander")

	witchID := srv.SpawnMob("minecraft:witch", 8.5, 64, 0.5)
	srv.SetMobNoAI(witchID)
	before, _ := srv.MobHealth(witchID)
	srv.ThrowMobPotion("minecraft:harming", 8.5, 65, 0.5, 0)
	srv.TickEntities(10)
	srv.TickMobs(3)
	if after, _ := srv.MobHealth(witchID); after >= before {
		t.Errorf("a foreign harming splash left the witch at %v from %v, want it hurt", after, before)
	}
}

// TestAWitchLeadsAKnockedBackPlayer holds a player's server-side velocity
// along +z each tick while a witch eight blocks east throws at them: the
// potions fly toward where the player is headed, not where they stand.
func TestAWitchLeadsAKnockedBackPlayer(t *testing.T) {
	t.Parallel()
	srv, c, _ := servertest.JoinAs(t, servertest.BowTestConfig(), "Prey")
	c.MoveToGround(0.5, 64, 0.5, true)
	c.Settle()
	servertest.HoldTicks(t, srv)

	srv.SpawnMob("minecraft:witch", 8.5, 64, 0.5)
	const lead = 60.0
	var potions []servertest.ThrownSpawn
	for range 400 {
		srv.SetPlayerVelocity("Prey", 0, 0, lead)
		c.ReadTicks(srv, 1, func(id int32, data []byte) bool {
			if id == protocol.IDPlaySpawnEntity {
				if sp := servertest.ReadThrownSpawn(c, data); sp.EntityType == protocol.EntityTypeSplashPotion {
					potions = append(potions, sp)
				}
			}
			return false
		})
		if len(potions) >= 3 {
			break
		}
	}
	if len(potions) < 3 {
		t.Fatalf("the witch threw %d splash potions in 400 ticks, want at least 3", len(potions))
	}
	for i, p := range potions {
		// The spread moves each aim component by at most 0.0172275*8 of the
		// unit aim, so an unled potion's vz stays under 0.75*0.14 = 0.11,
		// while a led one, aimed 60 blocks south of a witch that stays within
		// a few blocks of z 0.5, has an aim z of over 0.9 and a vz over 0.5.
		if p.Vx >= 0 || p.Vz < 0.5 {
			t.Errorf("potion %d flew (%.3f, %.3f, %.3f), want vz >= 0.5 south toward where the player is headed", i, p.Vx, p.Vy, p.Vz)
		}
	}
}
