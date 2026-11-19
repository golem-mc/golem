package combat

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// A zombie in Soul Speed boots gets the speed modifier standing on soul sand
// and none standing on stone.
// vanilla: EnchantmentHelper.runLocationChangedEffects, soul_speed.json
func TestZombieInSoulSpeedBootsIsFastOnSoulSandOnly(t *testing.T) {
	srv, c, _ := servertest.JoinAs(t, servertest.MobTestConfig(), "Watcher")
	sand, _ := registry.FindState("minecraft:soul_sand", nil)
	srv.SetBlock(2, 63, 0, sand)
	c.MoveToGround(.5, 64, .5, true)
	servertest.NightWorld(srv, c)
	onSand := srv.SpawnMobSeeded("minecraft:zombie", 2.5, 64, .5, 4374)
	srv.SetMobNoAI(onSand)
	onStone := srv.SpawnMobSeeded("minecraft:zombie", 5.5, 64, .5, 4374)
	srv.SetMobNoAI(onStone)
	servertest.HoldTicks(t, srv)
	boots := servertest.EnchantedStack(t, "minecraft:iron_boots", "minecraft:soul_speed", 1)
	srv.SetMobWorn(onSand, 0, boots)
	srv.SetMobWorn(onStone, 0, boots)
	servertest.RunTicks(t, srv, 3)
	c.AwaitAttributes(onSand, servertest.AttrMovementSpeed, func(e servertest.AttrEntry) bool {
		return hasSuffixModifier(e, "minecraft:enchantment.soul_speed")
	})
	if srv.MobPace(onSand) <= srv.MobPace(onStone) {
		t.Errorf("pace %v on soul sand, %v on stone: Soul Speed does not speed the mob", srv.MobPace(onSand), srv.MobPace(onStone))
	}
}
