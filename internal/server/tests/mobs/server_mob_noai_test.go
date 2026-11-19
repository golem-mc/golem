package mobs

import (
	"github.com/golem-mc/golem/internal/server/servertest"

	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
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
