package combat

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestADeathOffALadderNamesTheLadder climbs a ladder and falls from it onto
// the ground: the fall is worded "fell off a ladder", not the generic fall.
// vanilla: FallLocation.getCurrentFallLocation, CombatTracker.getFallMessage
func TestADeathOffALadderNamesTheLadder(t *testing.T) {
	t.Parallel()
	srv, c, id := servertest.JoinAs(t, servertest.CombatTestConfig(), "Climber")
	c.MoveToGround(0.5, 64, 0.5, true)

	ladder := servertest.MustState(t, "minecraft:ladder", map[string]string{"facing": "north", "waterlogged": "false"})
	if !srv.SetBlock(0, 70, 0, ladder) {
		t.Fatal("the ladder was not placed")
	}
	srv.SetHealth("Climber", 1)
	c.MoveToGround(0.5, 70, 0.5, false)
	c.MoveToGround(0.5, 70, 0.5, false)
	c.MoveToGround(0.5, 64, 0.5, true)

	message := protocol.Text{
		Translate: "death.fell.accident.ladder",
		With:      []protocol.Text{servertest.PlayerDisplayComponent("Climber")},
	}
	expectFatalDeath(srv, c, id, message)
	c.Disconnect()
}

// TestAShortFallDeathIsHittingTheGround dies of a 4.5 block fall, which no
// knock-off entry words: the message is "hit the ground too hard".
// vanilla: CombatTracker.getDeathMessage
func TestAShortFallDeathIsHittingTheGround(t *testing.T) {
	t.Parallel()
	srv, c, id := servertest.JoinAs(t, servertest.CombatTestConfig(), "Faller")
	c.MoveToGround(0.5, 64, 0.5, true)
	srv.SetHealth("Faller", 1)
	c.MoveStep(0.5, 68.5, 0.5, false)
	c.MoveStep(0.5, 64, 0.5, true)

	message := protocol.Text{
		Translate: "death.attack.fall",
		With:      []protocol.Text{servertest.PlayerDisplayComponent("Faller")},
	}
	expectFatalDeath(srv, c, id, message)
	c.Disconnect()
}
