package mobs

import (
	"github.com/golem-mc/golem/internal/server/servertest"

	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
)

// TestANoAICowStandsStill summons NoAI cows: for two hundred ticks neither
// walks, turns or falls, and a punch's knockback does not move one. vanilla:
// Mob.isEffectiveAi is false, so LivingEntity.aiStep runs neither
// serverAiStep nor travel.
func TestANoAICowStandsStill(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.MobTestConfig(), "Herder", servertest.OpsListing(t, "Herder"))
	c.MoveToGround(2.5, 64, 5.5, true)
	c.Settle()
	servertest.HoldTicks(t, srv)

	c.ChatCommand("summon minecraft:cow 4.5 64 5.5 {NoAI:1b}")
	grounded := c.ExpectMobSpawn(protocol.EntityTypeCow)
	if grounded.MobFlags&protocol.EntityFlagNoAI == 0 {
		t.Errorf("the NoAI cow's mob flags = %#x, want the NoAI bit", grounded.MobFlags)
	}
	c.ChatCommand("summon minecraft:cow 8.5 67 8.5 {NoAI:true}")
	floating := c.ExpectMobSpawn(protocol.EntityTypeCow)

	before := map[int32]server.TestMob{}
	for _, id := range []int32{grounded.EntityID, floating.EntityID} {
		m, ok := servertest.MobByID(srv, id)
		if !ok || !m.NoAI {
			t.Fatalf("cow %d is %+v, want a NoAI cow in the herd", id, m)
		}
		before[id] = m
	}

	c.Attack(grounded.EntityID)
	c.ExpectMobDamageEvent(grounded.EntityID, "minecraft:player_attack", c.PlayerEntityID+1) // the wire ID, entity ID + 1

	servertest.RunTicks(t, srv, 200)
	for id, was := range before {
		now, ok := servertest.MobByID(srv, id)
		if !ok {
			t.Fatalf("cow %d is gone", id)
		}
		if now.X != was.X || now.Y != was.Y || now.Z != was.Z {
			t.Errorf("cow %d moved from (%v, %v, %v) to (%v, %v, %v)", id, was.X, was.Y, was.Z, now.X, now.Y, now.Z)
		}
		if now.Yaw != was.Yaw || now.HeadYaw != was.HeadYaw {
			t.Errorf("cow %d turned from yaw %v head %v to yaw %v head %v", id, was.Yaw, was.HeadYaw, now.Yaw, now.HeadYaw)
		}
	}
}

// TestASummonedNoAITagReadsItsLowByte summons cows whose NoAI names a
// number no boolean: the read is NumericTag.asBoolean's, byteValue() != 0,
// the number's low byte — 256 int is a nought byte and no NoAI, 1.0d a one
// and NoAI.
func TestASummonedNoAITagReadsItsLowByte(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.MobTestConfig(), "Caller", servertest.OpsListing(t, "Caller"))
	c.MoveToGround(2.5, 64, 5.5, true)
	c.Settle()

	c.ChatCommand("summon minecraft:cow 4.5 64 5.5 {NoAI:256}")
	plain := c.ExpectMobSpawn(protocol.EntityTypeCow)
	if plain.MobFlags&protocol.EntityFlagNoAI != 0 {
		t.Errorf("the {NoAI:256} cow's mob flags = %#x, want no NoAI bit", plain.MobFlags)
	}
	if m, ok := servertest.MobByID(srv, plain.EntityID); !ok || m.NoAI {
		t.Errorf("the {NoAI:256} cow is %+v, want no NoAI", m)
	}
	c.ChatCommand("summon minecraft:cow 8.5 67 8.5 {NoAI:1.0d}")
	floating := c.ExpectMobSpawn(protocol.EntityTypeCow)
	if floating.MobFlags&protocol.EntityFlagNoAI == 0 {
		t.Errorf("the {NoAI:1.0d} cow's mob flags = %#x, want the NoAI bit", floating.MobFlags)
	}
	if m, ok := servertest.MobByID(srv, floating.EntityID); !ok || !m.NoAI {
		t.Errorf("the {NoAI:1.0d} cow is %+v, want NoAI", m)
	}
}

// TestNoAISurvivesASaveAndReload saves a NoAI cow and checks the file
// carries NoAI:1b, as Mob.addAdditionalSaveData writes it, and the cow
// comes back with it.
func TestNoAISurvivesASaveAndReload(t *testing.T) {
	t.Parallel()
	cfg := servertest.MobTestConfig()
	srv, connect := servertest.Start(t, cfg, servertest.OpsListing(t, "Keeper"))
	c := connect().Join("Keeper")
	c.MoveToGround(2.5, 64, 5.5, true)
	c.Settle()

	c.ChatCommand("summon minecraft:cow 4.5 64 5.5 {NoAI:1b}")
	c.ExpectMobSpawn(protocol.EntityTypeCow)
	c.Disconnect()
	servertest.UntilTicks(t, srv, 100, "condition", func() bool { return srv.MobCount() == 0 })
	srv.FlushMobs()

	servertest.EntityFileIs(t, cfg.LevelName, world.ChunkPos{X: 0, Z: 0}, "minecraft:cow", func(compound nbt.Compound) {
		if got := compound["NoAI"]; got != nbt.Byte(1) {
			t.Errorf("saved NoAI = %v, want 1b", got)
		}
	})

	second := connect()
	second.Login("Keeper")
	second.Configure()
	back, ok := second.EnterWorldSeeingMob("Keeper", protocol.EntityTypeCow)
	if !ok {
		t.Fatal("the cow was not restored with its chunk")
	}
	if back.MobFlags&protocol.EntityFlagNoAI == 0 {
		t.Errorf("the returned cow's mob flags = %#x, want the NoAI bit", back.MobFlags)
	}
	if m, ok := servertest.MobByID(srv, back.EntityID); !ok || !m.NoAI {
		t.Errorf("the returned cow is %+v, want NoAI", m)
	}
}
