package entities

import (
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
)

// vanilla: LivingEntity.addAdditionalSaveData
func TestFreshMannequinEmptyAttributes(t *testing.T) {
	t.Parallel()
	cfg := mannequinConfig()
	cfg.LevelName = t.TempDir()
	ops := servertest.OpsListing(t, "Keeper")
	srv, c := servertest.Join(t, cfg, "Keeper", ops)
	servertest.HoldTicks(t, srv)
	c.ChatCommand("summon minecraft:mannequin 1.5 64 1.5")
	servertest.AwaitSystemChatSaying(t, c, "commands.summon.success")
	c.Settle()

	expect := func(t *testing.T, c *servertest.Client) {
		t.Helper()
		for _, path := range []string{"", " attributes"} {
			c.ChatCommand("data get entity @e[type=mannequin,limit=1]" + path)
			line := servertest.FeedbackComponent(t, c)
			if line.Translate != "commands.data.entity.query" || len(line.With) != 2 {
				t.Errorf("/data get%s: feedback = %+v, want entity query", path, line)
				continue
			}
			got := line.With[1].Plain()
			if path == "" {
				if !strings.Contains(got, "attributes: []") {
					t.Errorf("full entity data = %s, want attributes: []", got)
				}
			} else if got != "[]" {
				t.Errorf("attributes = %q, want []", got)
			}
		}
	}
	t.Run("fresh", func(t *testing.T) { expect(t, c) })
	srv.FlushMobs()
	c.Disconnect()
	srv.Shutdown()

	again, connect := servertest.Start(t, cfg, ops)
	servertest.FreezeUntilHeld(t, again)
	client := connect().Join("Keeper")
	servertest.HoldTicks(t, again)
	servertest.UntilTicks(t, again, 20, "saved mannequin wakes", func() bool {
		return len(again.SelectedEntities("minecraft:mannequin")) == 1
	})
	client.Settle()
	t.Run("restart", func(t *testing.T) { expect(t, client) })
}
