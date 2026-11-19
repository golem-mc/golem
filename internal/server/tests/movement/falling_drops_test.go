package movement

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestFallingSandOnATorchDropsOnlyWithEntityDrops drops sand onto a torch,
// whose cell the sand cannot place into: the sand is given up as an item
// with entity_drops on and as nothing with it off.
func TestFallingSandOnATorchDropsOnlyWithEntityDrops(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		rule  string
		items int
	}{{"true", 1}, {"false", 0}} {
		t.Run("entity_drops_"+tc.rule, func(t *testing.T) {
			t.Parallel()
			srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Builder", servertest.OpsListing(t, "Builder"))
			// true is the default, and setting a rule to its value is refused.
			if tc.rule != "true" {
				c.ChatCommand("gamerule entity_drops " + tc.rule)
				c.ExpectCommandFeedback("commands.gamerule.set", "entity_drops", tc.rule)
			}

			const x, z = 4, 6
			servertest.Setblock(t, c, x, 64, z, "torch")
			c.Settle()
			before := srv.ItemCount()
			servertest.Setblock(t, c, x, 68, z, "sand")

			fall := c.AwaitFallingSpawn()
			c.ReadTicks(srv, 1200, func(id int32, data []byte) bool {
				if id != protocol.IDPlayRemoveEntities {
					return false
				}
				r := protocol.NewReader(data)
				found := false
				for n := r.ReadVarInt(); n > 0; n-- {
					found = found || r.ReadVarInt() == fall.EntityID
				}
				return found
			})
			c.Settle()
			if got := srv.ItemCount() - before; got != tc.items {
				t.Errorf("entity_drops %s: %d items dropped, want %d", tc.rule, got, tc.items)
			}
		})
	}
}
