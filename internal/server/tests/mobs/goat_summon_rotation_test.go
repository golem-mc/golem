package mobs

import (
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// goatSpawnAngles reads the body and head yaw of the next goat Spawn Entity.
func goatSpawnAngles(t *testing.T, srv *server.Server, c *servertest.Client) (yaw, head float32) {
	t.Helper()
	goat, _ := registry.EntityTypeID("minecraft:goat")
	if !c.ReadTicks(srv, 10, func(id int32, data []byte) bool {
		if id != protocol.IDPlaySpawnEntity {
			return false
		}
		typ, _, y, h := servertest.ReadSpawnEntity(t, data)
		if typ != goat {
			return false
		}
		yaw, head = y, h
		return true
	}) {
		t.Fatal("no goat spawned")
	}
	return yaw, head
}

func goatRotationConfig() *config.Config {
	cfg := servertest.MobTestConfig()
	cfg.ViewDistance = config.MinDistance
	cfg.SimulationDistance = config.MinDistance
	return cfg
}

// vanilla: Entity.load sets the head before the body, and Goat.setYHeadRot
// clamps it against the body it had, 0 on a fresh mob; wrapDegrees(180) is
// -180, so the head ends at -15 (344.53125 once quantized to a byte angle).
func TestASummonedGoatSendsAHeadNearTheBodyItWasGiven(t *testing.T) {
	t.Parallel()
	const name = "GoatRot"
	srv, c, _ := servertest.JoinAs(t, goatRotationConfig(), name, servertest.OpsListing(t, name))
	c.MoveToGround(0.5, 64, 0.5, true)
	c.Settle()
	servertest.HoldTicks(t, srv)
	c.ChatCommand(`summon minecraft:goat 4.5 64 0.5 {NoAI:1b,Rotation:[180f,0f]}`)
	yaw, head := goatSpawnAngles(t, srv, c)
	if yaw != 180 || head != 344.53125 {
		t.Fatalf("yaw %v head %v, want 180 and 344.53125", yaw, head)
	}
}

// vanilla: EntityType.spawn seeds head and body from the random yaw before
// the egg's data loads, so a custom egg without Rotation keeps them equal.
func TestACustomGoatEggSendsMatchingBodyAndHeadYaw(t *testing.T) {
	t.Parallel()
	cfg := goatRotationConfig()
	cfg.GameMode = "creative"
	srv, c := servertest.Join(t, cfg, "GoatEgg")
	c.MoveToGround(0.5, 64, 0.5, true)
	c.Settle()
	srv.GiveSlot("GoatEgg", player.HotbarSlot,
		eggWithData(t, "minecraft:goat_spawn_egg", "minecraft:goat", nbt.Compound{"id": nbt.String("minecraft:goat"), "NoAI": nbt.Byte(1)}))
	servertest.HoldTicks(t, srv)
	c.PlaceBlock(2, 63, 2, protocol.FaceTop)
	yaw, head := goatSpawnAngles(t, srv, c)
	if yaw != head {
		t.Fatalf("yaw %v head %v, want them equal", yaw, head)
	}
}
