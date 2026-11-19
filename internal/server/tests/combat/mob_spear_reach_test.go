package combat

import (
	"fmt"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestASpearSkeletonStrikesFromSpearReach stands a wither skeleton that
// cannot walk beside a player: its melee reach with an iron spear is the
// spear's 4.5 and 2.0 halved by the mob factor, so two blocks apart (a 1.35
// gap between the boxes) only the spear lands and one block apart (0.35) only
// the bare hand's 0.83 does. A wooden spear that removes its attack_range
// reaches as the bare hand does. A zombie holding a spear runs SpearUseGoal
// ahead of its melee goal, so a melee-only mob carries the reach.
// vanilla: Mob.isWithinMeleeAttackRange
func TestASpearSkeletonStrikesFromSpearReach(t *testing.T) {
	t.Parallel()
	const rangeless = `"minecraft:wooden_spear",components:{"!minecraft:attack_range":{}}`
	for _, tc := range []struct {
		name    string
		held    string
		dx      float64
		wantHit bool
	}{
		{"spear two blocks off", `"minecraft:iron_spear"`, 2, true},
		{"bare hand two blocks off", "", 2, false},
		{"spear inside its minimum", `"minecraft:iron_spear"`, 1, false},
		{"bare hand one block off", "", 1, true},
		{"spear without attack_range two blocks off", rangeless, 2, false},
		{"spear without attack_range one block off", rangeless, 1, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, connect := servertest.Start(t, servertest.HostileTestConfig(), servertest.OpsListing(t, "Prey"))
			servertest.FreezeUntilHeld(t, srv)
			c := connect()
			c.JoinAs("Prey")
			c.MoveToGround(0.5, 64, 0.5, true)
			c.Settle()

			equipment := ""
			if tc.held != "" {
				equipment = fmt.Sprintf(`,equipment:{mainhand:{id:%s,count:1}}`, tc.held)
			}
			pos := srv.PlayerPosition("Prey")
			c.ChatCommand(fmt.Sprintf(`summon minecraft:wither_skeleton %.2f %.2f %.2f {attributes:[{id:"minecraft:movement_speed",base:0.0d}]%s}`,
				pos.X+tc.dx, pos.Y, pos.Z, equipment))
			c.ExpectMobSpawn(protocol.EntityTypeWitherSkeleton)
			servertest.HoldTicks(t, srv)

			// Target pick, the melee goal's first search and its swing fit
			// well inside 100 ticks.
			const ticks = 100
			hurt := func() bool { return srv.PlayerHealth("Prey") < 20 }
			if tc.wantHit {
				servertest.UntilTicks(t, srv, ticks, "the skeleton's blow", hurt)
				return
			}
			servertest.RunTicks(t, srv, ticks)
			if hurt() {
				t.Errorf("the skeleton struck from %v blocks, out of its reach", tc.dx)
			}
		})
	}
}
