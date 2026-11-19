package commands

import (
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// vanilla: Entity.saveWithoutId and each kind's addAdditionalSaveData.
func TestSummonedEntitySaveDefaults(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.LevelName = t.TempDir()
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	srv, c := servertest.Join(t, cfg, "Alice", servertest.OpsListing(t, "Alice"))
	servertest.HoldTicks(t, srv)

	cases := []struct{ kind, want, absent string }{
		{"text_display", `{text:""}`, "NoAI"},
		{"block_display", `{}`, "NoAI"},
		{"item_display", `{}`, "NoAI"},
		{"interaction", `{}`, "NoAI"},
		{"marker", `{}`, "NoAI"},
		{"vex", `{NoGravity:1b,NoAI:1b}`, ""},
		{"camel", `{Tame:1b,NoAI:1b}`, ""},
		{"camel_husk", `{Tame:1b,NoAI:1b}`, ""},
		{"happy_ghast", `{still_timeout:0,NoAI:1b}`, ""},
		{"allay", `{listener:{event_delay:0,selector:{tick:-1L}},NoAI:1b}`, ""},
		{"pillager", `{Inventory:[],NoAI:1b}`, ""},
		{"wandering_trader", `{Inventory:[],NoAI:1b}`, ""},
		{"mannequin", `{HurtTime:0s,DeathTime:0s,FallFlying:0b,current_impulse_context_reset_grace_time:0}`, "TicksFrozen NoAI"},
	}
	for _, tc := range cases {
		tag := `{NoAI:1b}`
		if tc.absent == "NoAI" {
			tag = `{NoAI:1b,CanPickUpLoot:1b,PersistenceRequired:1b,LeftHanded:1b,drop_chances:{mainhand:1f},leash:{UUID:[I;1,2,3,4]},home_pos:[I;1,2,3],home_radius:4,DeathLootTable:"minecraft:empty",DeathLootTableSeed:1L}`
		}
		c.ChatCommand("summon " + tc.kind + " 4.5 70 4.5 " + tag)
		servertest.AwaitSystemChatSaying(t, c, "commands.summon.success")
	}
	// vanilla: Vex.tick sets NoGravity after its first superclass tick.
	servertest.RunTicks(t, srv, 1)
	check := func(t *testing.T, client *servertest.Client) {
		t.Helper()
		for _, tc := range cases {
			t.Run(tc.kind, func(t *testing.T) {
				client.ChatCommand("execute if entity @e[type=" + tc.kind + ",nbt=" + tc.want + "]")
				line := servertest.FeedbackComponent(t, client)
				if line.Translate != "commands.execute.conditional.pass_count" {
					t.Errorf("saved defaults %s: %s", tc.want, line.Plain())
				}
				absent := tc.absent
				if absent == "NoAI" {
					absent += " CanPickUpLoot PersistenceRequired LeftHanded drop_chances leash home_pos home_radius DeathLootTable DeathLootTableSeed"
				}
				for key := range strings.FieldsSeq(absent) {
					client.ChatCommand("data get entity @e[type=" + tc.kind + ",limit=1] " + key)
					line = servertest.FeedbackComponent(t, client)
					if len(line.Extra) == 1 {
						line = line.Extra[0]
					}
					if line.Translate != "arguments.nbtpath.nothing_found" {
						t.Errorf("%s should be absent: %s", key, line.Plain())
					}
					if key != "NoAI" && key != "TicksFrozen" {
						continue
					}
					value := "1b"
					if key == "TicksFrozen" {
						value = "0"
					}
					client.ChatCommand("execute unless entity @e[type=" + tc.kind + ",nbt={" + key + ":" + value + "}]")
					line = servertest.FeedbackComponent(t, client)
					if line.Translate != "commands.execute.conditional.pass" {
						t.Errorf("unexpected NBT selector match: %s", line.Plain())
					}
				}
			})
		}
	}
	t.Run("summoned", func(t *testing.T) { check(t, c) })
	srv.FlushMobs()
	c.Disconnect()
	srv.Shutdown()
	again, connect := servertest.Start(t, cfg, servertest.OpsListing(t, "Alice"))
	servertest.FreezeUntilHeld(t, again)
	client := connect().Join("Alice")
	servertest.HoldTicks(t, again)
	servertest.UntilTicks(t, again, 20, "saved entities wake", func() bool {
		for _, tc := range cases {
			if len(again.SelectedEntities("minecraft:"+tc.kind)) != 1 {
				return false
			}
		}
		return true
	})
	t.Run("reloaded", func(t *testing.T) { check(t, client) })
}
