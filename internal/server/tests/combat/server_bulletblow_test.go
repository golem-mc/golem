package combat

import (
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/server/player"
)

// TestABlowOnAShulkerBulletSweepsButWearsNothing: a full blade's blow breaks
// a bullet, sweeps the living body beside it, and a non-living target costs
// the blade no wear.
// vanilla: Player.attack, Player.doSweepAttack
func TestABlowOnAShulkerBulletSweepsButWearsNothing(t *testing.T) {
	t.Parallel()
	f := newSweepFixture(t)
	f.watcher.MoveToGround(40.5, 64, 40.5, true)
	f.watcher.Settle()
	f.ironSword(t)

	f.srv.StopTick()
	far := f.srv.SpawnMob("minecraft:cow", 20.5, 64, 5.5)
	f.srv.LaunchShulkerBullet(4.5, 64.5, 5.5, far, 3)
	var bullet int32
	for _, it := range f.srv.ThrownInDimension("minecraft:overworld") {
		if it.Kind == "minecraft:shulker_bullet" {
			bullet = it.ID
		}
	}
	if bullet == 0 {
		t.Fatal("no bullet was launched")
	}
	beside := f.srv.SpawnMob("minecraft:cow", 5.45, 64, 5.5)
	f.fencer.Settle()
	f.watcher.Settle()

	f.fencer.Attack(bullet)
	f.fencer.Settle()
	heard := f.watcher.HearBodies()
	if got := heard.Healths[beside]; math.Abs(float64(got)-9) > 0.01 {
		t.Errorf("the cow beside the broken bullet was left at %v, want the sweep's plain one off a ten", got)
	}
	if got := f.srv.InventorySlot("Fencer", player.HotbarSlot).Damage; got != 0 {
		t.Errorf("the blade took %d wear from a bullet, want none", got)
	}
}
