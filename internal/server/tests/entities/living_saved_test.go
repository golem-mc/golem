package entities

import (
	"fmt"
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
)

func livingSavedConfig() *config.Config {
	cfg := servertest.MobTestConfig()
	cfg.ViewDistance = config.MinDistance
	cfg.SimulationDistance = config.MinDistance
	return cfg
}

func livingData(t *testing.T, c *servertest.Client, selector, path string) string {
	t.Helper()
	c.ChatCommand(strings.TrimSpace("data get entity " + selector + " " + path))
	line, ok := protocol.ReadText(protocol.NewReader(c.NextSystemChat()))
	if !ok || line.Translate != "commands.data.entity.query" || len(line.With) != 2 {
		t.Fatalf("data query %s %s: %+v", selector, path, line)
	}
	return line.With[1].Plain()
}

// vanilla: LivingEntity.addAdditionalSaveData, TamableAnimal.addAdditionalSaveData
func TestLivingSavedDefaults(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, livingSavedConfig(), "Scribe", servertest.OpsListing(t, "Scribe"))
	servertest.HoldTicks(t, srv)
	c.Settle()
	for i, kind := range []string{"zombie", "skeleton", "cow", "villager", "creeper", "spider", "horse", "wolf", "armor_stand", "chicken", "iron_golem", "cat", "parrot", "nautilus", "zombie_nautilus"} {
		t.Run(kind, func(t *testing.T) {
			selector := fmt.Sprintf("@e[tag=body%d,limit=1]", i)
			c.ChatCommand(fmt.Sprintf(`summon %s 3.5 64 3.5 {NoAI:1b,Tags:["body%d"]}`, kind, i))
			c.Settle()
			data := livingData(t, c, selector, "")
			for _, field := range []string{"HurtTime: 0s", "DeathTime: 0s", "FallFlying: 0b", "current_impulse_context_reset_grace_time: 0", "Brain: {memories: {}}"} {
				if !strings.Contains(data, field) {
					t.Errorf("missing %s in %s", field, data)
				}
			}
			switch kind {
			case "wolf", "cat", "parrot", "nautilus", "zombie_nautilus":
				if got := livingData(t, c, selector, "Sitting"); got != "0b" {
					t.Errorf("Sitting = %s, want 0b", got)
				}
			case "armor_stand":
				if got := livingData(t, c, selector, "Pose"); got != "{}" {
					t.Errorf("Pose = %s, want {}", got)
				}
			}
		})
	}
}
