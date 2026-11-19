package commands

import (
	"bytes"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// vanilla: EntitySelector.findEntities, Entity.kill
func TestSelectorsFindShortLivedEntities(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.ViewDistance = config.MinDistance
	cfg.SimulationDistance = config.MinDistance
	srv, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))
	servertest.HoldTicks(t, srv)
	servertest.FreezeUntilHeld(t, srv)

	for _, tc := range []struct {
		kind, name string
		wide, tall bool
	}{
		{"evoker_fangs", "Evoker Fangs", true, true},
		{"eye_of_ender", "Eye of Ender", false, false},
		{"lightning_bolt", "Lightning Bolt", false, false},
	} {
		kind := tc.kind
		t.Run(kind, func(t *testing.T) {
			c.ChatCommand("summon " + kind + " 5.5 64 5.5")
			c.ExpectSystemChatPastMobs()
			c.ChatCommand("execute if entity @e[type=" + kind + "]")
			if got := c.ExpectSystemChatPastMobs(); !bytes.Equal(got, conditionalPassCountForTest(1)) {
				t.Errorf("selector missed live %s: % X", kind, got)
			}
			for _, filter := range []string{
				`name="` + tc.name + `"`,
				"x=5,y=63.5,z=5,dx=0,dy=0,dz=0",
				"nbt={Pos:[5.5d,64d,5.5d]}",
			} {
				c.ChatCommand("execute if entity @e[type=" + kind + "," + filter + "]")
				if got := c.ExpectSystemChatPastMobs(); !bytes.Equal(got, conditionalPassCountForTest(1)) {
					t.Errorf("%s did not match %s: % X", kind, filter, got)
				}
			}
			for _, box := range []struct {
				filter string
				match  bool
			}{
				{"x=5.7,y=64,z=5,dx=0,dy=0,dz=0", tc.wide},
				{"x=5,y=64.5,z=5,dx=0,dy=0,dz=0", tc.tall},
			} {
				c.ChatCommand("execute if entity @e[type=" + kind + "," + box.filter + "]")
				got := c.ExpectSystemChatPastMobs()
				if pass := bytes.Equal(got, conditionalPassCountForTest(1)); pass != box.match {
					t.Errorf("%s box %s matched = %v, want %v", kind, box.filter, pass, box.match)
				}
			}
			c.ChatCommand("execute as @e[type=" + kind + "] run tag @s add executor")
			c.Settle()
			c.ChatCommand("execute if entity @e[type=" + kind + ",tag=executor]")
			if got := c.ExpectSystemChatPastMobs(); !bytes.Equal(got, conditionalPassCountForTest(1)) {
				t.Errorf("execute as did not tag %s: % X", kind, got)
			}
			c.ChatCommand("kill @e[type=" + kind + "]")
			if got := c.ExpectSystemChatPastMobs(); !bytes.Contains(got, []byte("commands.kill.success.single")) {
				t.Errorf("kill did not find exactly one %s: % X", kind, got)
			}
			c.ChatCommand("execute if entity @e[type=" + kind + "]")
			c.ExpectCommandFailure(servertest.CommandErrorMessageForTest("commands.execute.conditional.fail"))
		})
	}
}
