package mobs

import (
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// vanilla: ShoulderRidingEntity.setEntityOnShoulder, Entity.saveWithoutId,
// ServerPlayer.addAdditionalSaveData and respawnEntityOnShoulder.
func TestShoulderCompoundInsertionHistory(t *testing.T) {
	t.Parallel()
	cfg := servertest.MobTestConfig()
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	cfg.LevelName = t.TempDir()
	ops := servertest.OpsListing(t, "Keeper")
	srv, c := servertest.Join(t, cfg, "Keeper", ops)
	servertest.HoldTicks(t, srv)
	c.MoveToGround(0.5, 64, 0.5, true)
	c.Settle()
	perchedParrot(t, srv, 4)
	check := func(c *servertest.Client, selector, path string) {
		t.Helper()
		c.Settle()
		for _, command := range []string{
			"data get entity " + selector + " " + path,
			`tellraw @s {nbt:"` + path + `",entity:"` + selector + `"}`,
			`tellraw @s {nbt:"` + path + `",entity:"` + selector + `",plain:1b}`,
		} {
			c.ChatCommand(command)
			line := servertest.FeedbackComponent(t, c)
			text := line.Plain()
			if strings.HasPrefix(command, "data ") {
				if line.Translate != "commands.data.entity.query" || len(line.With) != 2 {
					t.Fatalf("%s: %+v", command, line)
				}
				text = line.With[1].Plain()
			}
			for _, group := range []string{"Invulnerable FallFlying", "PortalCooldown AbsorptionAmount", "fall_distance Air OnGround"} {
				last := -1
				for key := range strings.FieldsSeq(group) {
					at := strings.Index(text, key+":")
					if at <= last {
						t.Errorf("%s: collision group %s out of order: %s", command, group, text)
					}
					last = at
				}
			}
		}
	}
	check(c, "@s", "ShoulderEntityLeft")
	c.Disconnect()
	srv.Shutdown()
	again, connect := servertest.Start(t, cfg, ops)
	servertest.FreezeUntilHeld(t, again)
	back := connect().Join("Keeper")
	servertest.HoldTicks(t, again)
	check(back, "@s", "ShoulderEntityLeft")
	if left, _ := again.ShoulderParrots("Keeper"); left != 4 {
		t.Fatalf("reloaded shoulder variant=%d, want 4", left)
	}
	servertest.RunTicks(t, again, 21)
	back.ChatCommand("gamemode spectator")
	servertest.AwaitSystemChatSaying(t, back, "commands.gamemode.success.self")
	if len(again.SelectedEntities(parrotType)) != 1 {
		t.Fatal("released shoulder parrot missing")
	}
	check(back, "@e[type=parrot,limit=1]", "{}")
}
