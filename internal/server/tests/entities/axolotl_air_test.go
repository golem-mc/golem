package entities

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
)

func TestAxolotlStartsWithMaxAirOnLand(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, livingSavedConfig(), "Scribe", servertest.OpsListing(t, "Scribe"))
	servertest.HoldTicks(t, srv)
	c.ChatCommand("weather clear")
	c.Settle()
	c.ChatCommand(`summon axolotl 3.5 64 3.5 {PersistenceRequired:1b,Tags:["dry"]}`)
	c.Settle()
	const dry = "@e[tag=dry,limit=1]"
	if got := livingData(t, c, dry, "Air"); got != "6000s" {
		t.Errorf("initial Air = %s, want 6000s", got)
	}
	health := livingData(t, c, dry, "Health")
	servertest.RunTicks(t, srv, 400)
	if got := livingData(t, c, dry, "Health"); got != health {
		t.Errorf("Health after 400 dry ticks = %s, want %s", got, health)
	}
	if got := livingData(t, c, dry, "Air"); got != "5600s" {
		t.Errorf("Air after 400 dry ticks = %s, want 5600s", got)
	}

	c.ChatCommand(`summon axolotl 3.5 64 3.5 {Air:1234s,NoAI:1b,Tags:["loaded"]}`)
	c.Settle()
	if got := livingData(t, c, "@e[tag=loaded,limit=1]", "Air"); got != "1234s" {
		t.Errorf("loaded Air = %s, want 1234s", got)
	}
}
