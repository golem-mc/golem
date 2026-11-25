package combat

import (
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestAFireworkRocketsVelocityGoesToItsWatchersAlone flies a rocket beside
// one player while another stands past its tracking range: the velocity
// its movement ticks carry is told to the player watching it and to nobody
// else.
// vanilla: ServerEntity.sendChanges, sendToTrackingPlayers
func TestAFireworkRocketsVelocityGoesToItsWatchersAlone(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.ViewDistance = 4 // the rocket is then seen from 64 blocks
	srv, connect := servertest.Start(t, cfg)
	near := connect().Join("Near")
	far := connect().Join("Far")
	servertest.HoldTicks(t, srv)
	// 120 blocks east is out of the 64 blocks four chunks of sight reach.
	far.MoveToGround(120.5, 64, 0.5, true)
	far.ReadTicks(srv, 0, func(int32, []byte) bool { return false })

	// Flight 3: a life of 40 to 51 ticks, so the 25 cover three movement
	// ticks of a rise that speeds up.
	id := srv.SpawnFireworkStackEntity(servertest.FireworkStack(3, 0), 3.5, 65, 3.5).ID
	motionOf := servertest.PacketAbout(id, protocol.IDPlaySetEntityMotion)
	far.ExpectNoneWithinTicks(srv, 25, motionOf)

	var told int
	near.ReadTicks(srv, 0, func(id int32, data []byte) bool {
		if motionOf(id, data) {
			told++
		}
		return false
	})
	if told == 0 {
		t.Fatal("the watching player was told no velocity of the rising rocket")
	}
}

// TestAFireworkRocketsBurnEndsIsTold sets a rocket alight for four ticks and
// flies it on: the watcher is told the fire flag once, kept through tick
// three, then cleared on the fourth, off the rocket's ten-tick movement
// period, while the rocket is still flying.
// vanilla: Entity.baseTick, ServerEntity.sendChanges
func TestAFireworkRocketsBurnEndsIsTold(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	srv, c := servertest.Join(t, cfg, "Watcher")
	servertest.HoldTicks(t, srv)
	rocket := srv.SpawnFireworkAtAngle(3.5, 65, 3.5, 0, 0, 0, 200)
	srv.RunLine("data merge entity " + rocket.UUID.String() + " {Fire:4s}")

	fireFlag := func(id int32, data []byte) (on, told bool) {
		if id != protocol.IDPlayEntityMetadata {
			return false, false
		}
		r := protocol.NewReader(data)
		if r.ReadVarInt() != rocket.ID {
			return false, false
		}
		flags, ok := servertest.ReadEntityFlags(r)
		return flags&protocol.EntityFlagOnFire != 0, ok
	}
	set, cleared := 0, false
	c.ReadTicks(srv, 3, func(id int32, data []byte) bool {
		if on, told := fireFlag(id, data); told {
			if on {
				set++
			} else {
				cleared = true
			}
		}
		return false
	})
	if set != 1 || cleared {
		t.Fatalf("through tick three the fire flag was told set %d times, cleared = %v; want set once, never cleared", set, cleared)
	}
	if !c.ReadTicks(srv, 1, func(id int32, data []byte) bool {
		on, told := fireFlag(id, data)
		return told && !on
	}) {
		t.Fatal("the fire flag was not told cleared on tick four")
	}
	if _, _, ok := srv.FireworkState(rocket.UUID); !ok {
		t.Fatal("the rocket ended before its burn did")
	}
}

// TestACrossbowFlameRocketLightsOnItsFirstTick fires a crossbow loaded with a
// Flame rocket: the rocket pairs unlit and is told alight on its first tick.
// vanilla: Projectile.applyOnProjectileSpawned, Ignite, Entity.baseTick
func TestACrossbowFlameRocketLightsOnItsFirstTick(t *testing.T) {
	t.Parallel()
	cfg := servertest.BowTestConfig()
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	srv, c := servertest.Join(t, cfg, "Archer")
	servertest.HoldTicks(t, srv)
	crossbow := servertest.ItemStack(t, "minecraft:crossbow", 1)
	crossbow.SetChargedProjectiles([]protocol.ItemStack{
		servertest.EnchantedStack(t, "minecraft:firework_rocket", "minecraft:flame", 1)})
	srv.GiveSlot("Archer", 36, crossbow)
	c.MoveTo(0.5, 64, 0.5, -90, 0)
	servertest.DrawBow(c, -90, 0)

	var id int32
	paired := false
	if !c.ReadTicks(srv, 0, func(pid int32, data []byte) bool {
		if pid == protocol.IDPlaySpawnEntity {
			if entityID, kind := servertest.SpawnEntityHead(data); kind == protocol.EntityTypeFireworkRocket {
				id = entityID
			}
			return false
		}
		if id == 0 || pid != protocol.IDPlayEntityMetadata {
			return false
		}
		r := protocol.NewReader(data)
		if r.ReadVarInt() != id {
			return false
		}
		// The pairing payload carries a slot, which ReadEntityFlags cannot
		// step, after the flags entry it leads with when it has one.
		lit := r.ReadU8() == protocol.MetadataIndexEntityFlags && r.ReadVarInt() == protocol.MetadataByte &&
			r.ReadU8()&protocol.EntityFlagOnFire != 0
		paired = !lit
		return true
	}) {
		t.Fatal("the rocket was not shown before a tick ran")
	}
	if !paired {
		t.Fatal("the rocket paired alight before its first tick")
	}
	if !c.ReadTicks(srv, 1, func(pid int32, data []byte) bool {
		if pid != protocol.IDPlayEntityMetadata {
			return false
		}
		r := protocol.NewReader(data)
		if r.ReadVarInt() != id {
			return false
		}
		flags, ok := servertest.ReadEntityFlags(r)
		return ok && flags&protocol.EntityFlagOnFire != 0
	}) {
		t.Fatal("the rocket was not told alight on its first tick")
	}
}
