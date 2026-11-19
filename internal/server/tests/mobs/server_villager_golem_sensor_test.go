package mobs

import (
	"fmt"
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
)

// The golem sensor's box is the villager's grown by its follow range (16), so
// a golem whose centre is 16.5 blocks away still counts: |dx| < 0.3 + 16 + 0.7.
// vanilla: GolemSensor.checkForNearbyGolem, NearestLivingEntitySensor.doTick
func TestAVillagerSeesAnIronGolemSixteenAndAHalfBlocksAway(t *testing.T) {
	t.Parallel()
	srv, _ := villagerYard(t)
	srv.SetDayTime(2000)
	srv.LoadBlockAt(20, 63, 5)
	// The spawn bonus is pinned near zero: Mob.finalizeSpawn's roll (up to
	// -11.5%) would otherwise shrink the range below 16.5 now and then.
	const villager = `summon minecraft:villager 3.5 64 %.1f {attributes:[{id:"minecraft:movement_speed",base:0d},` +
		`{id:"minecraft:follow_range",base:16d,modifiers:[{id:"minecraft:random_spawn_bonus",amount:0.01d,operation:"add_multiplied_base"}]}],` +
		`Brain:{memories:{"minecraft:last_slept":{value:1L}}}}`
	for _, z := range []float64{4.5, 5.5, 6.5} {
		srv.RunLine(fmt.Sprintf(villager, z))
	}
	srv.RunLine(`summon minecraft:zombie 3.5 64 10.5 {NoAI:1b}`)
	srv.RunLine(`summon minecraft:iron_golem 20.0 64 5.5 {NoAI:1b}`)
	servertest.UntilTicks(t, srv, 100, "the villagers, zombie and golem to spawn", func() bool {
		return countKind(srv, "minecraft:villager") == 3 && countKind(srv, "minecraft:zombie") == 1
	})
	servertest.RunTicks(t, srv, 400)
	if n := countKind(srv, "minecraft:iron_golem"); n != 1 {
		t.Fatalf("%d iron golems after 400 ticks, want 1: the golem 16.5 blocks away should have been seen", n)
	}
}
