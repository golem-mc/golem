package commands

import (
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// vanilla: Entity, LivingEntity, Mob, AgeableMob, Animal, Pig, Bat.addAdditionalSaveData
func TestFreshMobEmptyAttributes(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.LevelName = t.TempDir()
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	srv, c := servertest.Join(t, cfg, "Alice", servertest.OpsListing(t, "Alice"))
	servertest.HoldTicks(t, srv)
	// With Tags, the bat has 25 keys: the empty attributes list must grow
	// Java's table from 32 to 64 buckets and change the root traversal.
	fixtures := []struct{ kind, keys string }{
		{"bat", "NoGravity Brain Invulnerable FallFlying PortalCooldown AbsorptionAmount DeathTime PersistenceRequired BatFlags UUID Tags Motion Health LeftHanded fall_distance Air OnGround NoAI Rotation current_impulse_context_reset_grace_time Pos Fire CanPickUpLoot attributes HurtTime"},
		{"pig", "NoGravity AgeLocked Brain Invulnerable FallFlying ForcedAge sound_variant PortalCooldown AbsorptionAmount InLove DeathTime variant PersistenceRequired UUID Tags Age Motion Health LeftHanded fall_distance Air OnGround NoAI Rotation current_impulse_context_reset_grace_time Pos Fire CanPickUpLoot attributes HurtTime"},
	}
	expect := func(t *testing.T, c *servertest.Client, kind, keys string) {
		t.Helper()
		expectNonlivingHistory(t, c, kind, keys)
		selector := "@e[type=" + kind + ",limit=1]"
		for _, cmd := range []string{
			"data get entity " + selector + " attributes",
			`tellraw @s {nbt:"attributes",entity:"` + selector + `"}`,
			`tellraw @s {nbt:"attributes",entity:"` + selector + `",plain:1b}`,
		} {
			c.ChatCommand(cmd)
			line := servertest.FeedbackComponent(t, c)
			text := line.Plain()
			if strings.HasPrefix(cmd, "data ") {
				if line.Translate != "commands.data.entity.query" || len(line.With) != 2 {
					t.Fatalf("%s: feedback = %+v", cmd, line)
				}
				text = line.With[1].Plain()
			}
			if text != "[]" {
				t.Errorf("%s: attributes = %q, want []", cmd, text)
			}
		}
	}
	for _, fixture := range fixtures {
		t.Run(fixture.kind, func(t *testing.T) {
			c.ChatCommand("summon " + fixture.kind + ` 4.5 70 4.5 {NoGravity:1b,NoAI:1b,Tags:["boundary"]}`)
			servertest.AwaitSystemChatSaying(t, c, "commands.summon.success")
			c.Settle()
			expect(t, c, fixture.kind, fixture.keys)
			c.ChatCommand("data merge entity @e[type=" + fixture.kind + `,limit=1] {Health:2f}`)
			servertest.AwaitSystemChatSaying(t, c, "commands.data.entity.modified")
			c.Settle()
			expect(t, c, fixture.kind, fixture.keys)
		})
	}
	srv.FlushMobs()
	c.Disconnect()
	srv.Shutdown()
	again, connect := servertest.Start(t, cfg, servertest.OpsListing(t, "Alice"))
	servertest.FreezeUntilHeld(t, again)
	client := connect().Join("Alice")
	servertest.HoldTicks(t, again)
	servertest.UntilTicks(t, again, 20, "saved mobs wake", func() bool {
		for _, fixture := range fixtures {
			if len(again.SelectedEntities("minecraft:"+fixture.kind)) != 1 {
				return false
			}
		}
		return true
	})
	client.Settle()
	for _, fixture := range fixtures {
		t.Run("restart/"+fixture.kind, func(t *testing.T) {
			expect(t, client, fixture.kind, fixture.keys)
		})
	}
}
