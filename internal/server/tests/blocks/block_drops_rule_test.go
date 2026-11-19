package blocks

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestBreakingABlockDropsNothingWithBlockDropsOff: a torch broken in survival
// drops its item while block_drops stands and nothing once it is off.
// vanilla: Block.popResource
func TestBreakingABlockDropsNothingWithBlockDropsOff(t *testing.T) {
	t.Parallel()
	for _, drops := range []bool{true, false} {
		name := "rule on"
		if !drops {
			name = "rule off"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			srv, c := servertest.Join(t, servertest.SurvivalTestConfig(), "Miner")
			c.Settle()
			srv.StopTick()
			srv.SetGameRule("block_drops", drops)
			srv.SetBlock(2, 64, 2, servertest.MustState(t, "minecraft:torch", nil))
			c.Settle()

			before := srv.ItemCount()
			c.BreakBlock(2, 64, 2)
			c.Settle()

			servertest.ExpectBlockNamed(t, srv, 2, 64, 2, "minecraft:air", nil)
			got := srv.ItemCount() - before
			if drops && got != 1 {
				t.Errorf("a broken torch dropped %d items, want 1", got)
			}
			if !drops && got != 0 {
				t.Errorf("a broken torch dropped %d items with block_drops off, want 0", got)
			}
		})
	}
}
