package commands

import (
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
)

// vanilla: MobEffect.addAttributeModifiers, AttributeInstance.pack
func TestPlayerEffectModifiersAreListedInAttributes(t *testing.T) {
	t.Parallel()
	_, c, _ := servertest.JoinAs(t, servertest.SurvivalTestConfig(), "DJ", servertest.OpsListing(t, "DJ"))
	c.Settle()

	// query reads the movement_speed attribute path, failing on any other feedback.
	query := func(path string) string {
		t.Helper()
		c.ChatCommand(`data get entity @s attributes[{id:"minecraft:movement_speed"}]` + path)
		line := servertest.FeedbackComponent(t, c)
		if line.Translate != "commands.data.entity.query" || len(line.With) != 2 {
			t.Fatalf("query %q feedback = %+v, want commands.data.entity.query", path, line)
		}
		return line.With[1].Plain()
	}
	modifiers := func() string { t.Helper(); return query(".modifiers") }

	c.ChatCommand("effect give @s speed 30 1")
	c.Settle()
	got := modifiers()
	for _, want := range []string{`"minecraft:effect.speed"`, "0.4", "add_multiplied_total"} {
		if !strings.Contains(got, want) {
			t.Fatalf("modifiers with Speed II = %s, want it to contain %s", got, want)
		}
	}
	if strings.Count(got, "effect.speed") != 1 {
		t.Fatalf("modifiers = %s, want exactly one effect.speed", got)
	}

	c.ChatCommand("effect clear @s")
	c.Settle()
	if got := query(""); strings.Contains(got, "modifiers") {
		t.Fatalf("movement_speed after effect clear = %s, want no modifiers field", got)
	}

	// A reapplied effect goes last, after the others.
	// vanilla: LivingEntity.onEffectUpdated
	for _, cmd := range []string{"effect give @s speed infinite", "effect give @s slowness infinite", "effect give @s speed infinite 1"} {
		c.ChatCommand(cmd)
		c.Settle()
	}
	got = modifiers()
	if s, sl := strings.Index(got, "effect.speed"), strings.Index(got, "effect.slowness"); sl < 0 || s < sl {
		t.Fatalf("modifiers after reapplying speed = %s, want slowness before speed", got)
	}
}
