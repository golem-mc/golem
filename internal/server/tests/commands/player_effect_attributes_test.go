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

	modifiers := func() string {
		t.Helper()
		c.ChatCommand(`data get entity @s attributes[{id:"minecraft:movement_speed"}].modifiers`)
		line := servertest.FeedbackComponent(t, c)
		if line.Translate != "commands.data.entity.query" || len(line.With) != 2 {
			return ""
		}
		return line.With[1].Plain()
	}

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
	if got := modifiers(); strings.Contains(got, "effect.speed") {
		t.Fatalf("modifiers after effect clear = %s, want no effect.speed", got)
	}
}
