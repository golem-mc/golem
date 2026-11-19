package blocks

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
)

// A burning arrow flying through powder snow is put out, and destroys the
// snow only while mob_griefing is on: an arrow is no player, so even an
// ownerless one needs it, and a mob's arrow also needs it for mayInteract.
// vanilla: PowderSnowBlock.entityInside, Projectile.mayInteract
func TestBurningArrowInPowderSnowIsPutOutAndMeltsIt(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name      string
		mob       bool
		griefing  bool
		wantBlock string
	}{
		{"ownerless, griefing on", false, true, "minecraft:air"},
		{"ownerless, griefing off", false, false, "minecraft:powder_snow"},
		{"mob's, griefing off", true, false, "minecraft:powder_snow"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, c := servertest.Join(t, servertest.MobTestConfig(), "Warden", servertest.OpsListing(t, "Warden"))
			c.Settle()
			srv.SetGameRule(world.RuleMobGriefing.String(), tc.griefing)
			cow := int32(0)
			if tc.mob {
				cow = srv.SpawnMob("minecraft:cow", 20.5, 64, 20.5)
				c.ExpectMobSpawn(protocol.EntityTypeCow)
			}
			srv.StopTick()
			c.Settle()
			if !srv.SetBlock(6, 65, 0, servertest.BlockState(t, "minecraft:powder_snow")) {
				t.Fatal("could not place the powder snow")
			}
			// Half a block a tick: the box reaches the snow cell at x 6.5,
			// on the third tick, and flies on through it.
			var id int32
			if tc.mob {
				id = srv.ShootMobArrowFrom(cow, 8.0, 65.2, 0.5, -0.5, 0, 0)
			} else {
				id = srv.ShootArrow(8.0, 65.2, 0.5, -0.5, 0, 0)
			}
			srv.IgniteArrow(id, 100)
			servertest.DriveTargetTicks(srv, 4)

			if fire := srv.ArrowFire(id); fire != 0 {
				t.Errorf("the arrow kept %d fire ticks in the powder snow, want 0", fire)
			}
			servertest.ExpectBlockNamed(t, srv, 6, 65, 0, tc.wantBlock, nil)
		})
	}
}
