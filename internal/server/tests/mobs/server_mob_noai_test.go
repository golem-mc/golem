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

// TestNoAIMobCountdowns holds the countdowns vanilla runs in aiStep / tick
// rather than serverAiStep: glow squid dark ticks, vex limited life (including
// starvation hurt), bee stay-out and hive validity drop, and allay duplication
// cooldown all advance with NoAI, while AI-gated fields like bee
// TicksSincePollination do not.
// vanilla: GlowSquid.aiStep, Vex.tick, Bee.aiStep, Bee.customServerAiStep, Allay.aiStep
func TestNoAIMobCountdowns(t *testing.T) {
	t.Parallel()
	srv, _ := servertest.Join(t, servertest.MobTestConfig(), "Watcher", func(s *server.Server) {
		servertest.FreezeUntilHeld(t, s)
	})
	servertest.HoldTicks(t, srv)

	for _, line := range []string{
		`summon minecraft:glow_squid 2.5 64 2.5 {DarkTicksRemaining:50,NoAI:1b}`,
		`summon minecraft:vex 4.5 64 2.5 {life_ticks:500,NoAI:1b}`,
		`summon minecraft:vex 6.5 64 2.5 {life_ticks:5,NoAI:1b,Tags:["starving"]}`,
		`summon minecraft:bee 8.5 64 2.5 {CannotEnterHiveTicks:400,hive_pos:[I;0,0,0],NoAI:1b}`,
		`summon minecraft:allay 10.5 64 2.5 {DuplicationCooldown:300L,NoAI:1b}`,
	} {
		srv.RunLine(line)
	}

	servertest.RunTicks(t, srv, 30)

	query := func(selector, path string) (string, bool) {
		for _, text := range srv.RunLine("data get entity " + selector + " " + path) {
			if text.Translate == "commands.data.entity.query" && len(text.With) == 2 {
				return text.With[1].Plain(), true
			}
		}
		return "", false
	}

	for _, tc := range []struct {
		selector, path, want string
	}{
		{`@e[type=minecraft:glow_squid,limit=1]`, "DarkTicksRemaining", "20"},
		{`@e[type=minecraft:vex,tag=!starving,limit=1]`, "life_ticks", "470"},
		{`@e[type=minecraft:vex,tag=starving,limit=1]`, "life_ticks", "15"},
		{`@e[type=minecraft:vex,tag=starving,limit=1]`, "Health", "12.0f"},
		{`@e[type=minecraft:bee,limit=1]`, "CannotEnterHiveTicks", "370"},
		{`@e[type=minecraft:bee,limit=1]`, "TicksSincePollination", "0"},
		{`@e[type=minecraft:allay,limit=1]`, "DuplicationCooldown", "270L"},
	} {
		got, ok := query(tc.selector, tc.path)
		if !ok {
			t.Errorf("%s %s missing, want %s", tc.selector, tc.path, tc.want)
			continue
		}
		if got != tc.want {
			t.Errorf("%s %s = %s, want %s", tc.selector, tc.path, got, tc.want)
		}
	}
	if got, ok := query(`@e[type=minecraft:bee,limit=1]`, "hive_pos"); ok {
		t.Errorf("NoAI bee kept invalid hive_pos = %s, want dropped", got)
	}
}
