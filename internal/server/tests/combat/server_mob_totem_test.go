package combat

import (
	"fmt"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// TestAMobHoldingATotemSurvivesALethalBlow checks that a mob with a totem of
// undying in either hand is left at one half-heart by a blow that would kill
// it, with the totem spent, the item's effects on it, and the totem
// animation (entity event 35) told to its watchers.
func TestAMobHoldingATotemSurvivesALethalBlow(t *testing.T) {
	t.Parallel()
	for _, hand := range []string{"mainhand", "offhand"} {
		t.Run(hand, func(t *testing.T) {
			t.Parallel()
			cfg := servertest.HostileTestConfig()
			cfg.GameMode = "creative"
			srv, connect := servertest.Start(t, cfg, servertest.OpsListing(t, "Prey"))
			srv.SetDayTime(18000) // midnight, whose dark keeps the daylight burn off the zombie
			c := connect()
			c.JoinAs("Prey")
			c.Settle()

			pos := srv.PlayerPosition("Prey")
			c.ChatCommand(fmt.Sprintf(
				`summon minecraft:zombie %.1f %.1f %.1f {Health:0.5f,equipment:{%s:{id:"minecraft:totem_of_undying",count:1}}}`,
				pos.X+1.0, pos.Y, pos.Z, hand))
			zombie := c.ExpectMobSpawn(protocol.EntityTypeZombie)
			c.Attack(zombie.EntityID)

			hurtSound, ok := registry.SoundEventID("minecraft:entity.zombie.hurt")
			if !ok {
				t.Fatal("no zombie hurt sound")
			}
			totem, spent, heard := false, false, false
			particles := 0
			skip := func(id int32) bool {
				return id != protocol.IDPlayEntityEquipment && servertest.HostileSkip(id)
			}
			c.ReadTicksSkipping(srv, servertest.HostileTicks, skip, func(id int32, data []byte) bool {
				switch id {
				case protocol.IDPlaySoundEffect:
					if protocol.NewReader(data).ReadVarInt() == hurtSound+1 {
						heard = true
					}
				case protocol.IDPlayEntityEvent:
					r := protocol.NewReader(data)
					if r.ReadI32() != zombie.EntityID {
						return false
					}
					switch r.ReadU8() {
					case protocol.EntityStatusTotem:
						totem = true
					case protocol.EntityStatusDeath:
						t.Error("the zombie died with a totem in its hand")
					}
				case protocol.IDPlayEntityMetadata:
					if p, ok := effectParticlesOf(c, protocol.NewReader(data), zombie.EntityID); ok {
						particles = len(p)
					}
				case protocol.IDPlayEntityEquipment:
					if protocol.NewReader(data).ReadVarInt() == zombie.EntityID {
						spent = true
					}
				}
				return totem && particles > 0 && spent
			})
			if !totem {
				t.Error("the totem animation never reached the watcher")
			}
			if particles != 2 {
				t.Errorf("the mob shows %d effect particles, want absorption and fire resistance (the undead refuse regeneration)", particles)
			}
			if heard {
				t.Error("a saved mob played its hurt sound")
			}
			if !spent {
				t.Error("the totem leaving the mob's hand was never told")
			}
		})
	}
}

// TestATotemSavedMobHitByAnArrowPlaysNoHurtSound checks that the hurt sound
// stays muted when the lethal blow is not a player's melee hit.
// vanilla: LivingEntity.hurtServer
func TestATotemSavedMobHitByAnArrowPlaysNoHurtSound(t *testing.T) {
	t.Parallel()
	cfg := servertest.HostileTestConfig()
	cfg.GameMode = "creative"
	srv, connect := servertest.Start(t, cfg, servertest.OpsListing(t, "Prey"))
	srv.SetDayTime(18000) // midnight keeps the daylight burn off the zombie
	c := connect()
	c.JoinAs("Prey")
	c.Settle()

	pos := srv.PlayerPosition("Prey")
	c.ChatCommand(fmt.Sprintf(
		`summon minecraft:zombie %.1f %.1f %.1f {Health:0.5f,equipment:{mainhand:{id:"minecraft:totem_of_undying",count:1}}}`,
		pos.X+1.0, pos.Y, pos.Z))
	zombie := c.ExpectMobSpawn(protocol.EntityTypeZombie)
	if id := srv.ShootArrow(pos.X+4, pos.Y+1, pos.Z, -3, 0, 0); id == 0 {
		t.Fatal("the arrow never flew")
	}

	hurtSound, ok := registry.SoundEventID("minecraft:entity.zombie.hurt")
	if !ok {
		t.Fatal("no zombie hurt sound")
	}
	totem, heard := false, false
	skip := func(id int32) bool { return servertest.HostileSkip(id) }
	c.ReadTicksSkipping(srv, servertest.HostileTicks, skip, func(id int32, data []byte) bool {
		switch id {
		case protocol.IDPlaySoundEffect:
			if protocol.NewReader(data).ReadVarInt() == hurtSound+1 {
				heard = true
			}
		case protocol.IDPlayEntityEvent:
			r := protocol.NewReader(data)
			if r.ReadI32() == zombie.EntityID && r.ReadU8() == protocol.EntityStatusTotem {
				totem = true
			}
		}
		return totem
	})
	if !totem {
		t.Fatal("the arrow never triggered the totem")
	}
	c.Settle()
	if heard {
		t.Error("a saved mob played its hurt sound for an arrow")
	}
}
