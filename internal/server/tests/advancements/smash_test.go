package advancements

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// maceSwingRecovered is the count of ticks a mace's swing takes to wind up.
const maceSwingRecovered = 34

// TestAHugeMaceSmashObtainsOveroverkill smashes an iron golem with a mace
// from a short fall and from a very long one: the first deals under 100
// damage and does not obtain overoverkill, the second does.
// vanilla: MaceItem.canSmashAttack, PlayerHurtEntityTrigger
func TestAHugeMaceSmashObtainsOveroverkill(t *testing.T) {
	t.Parallel()
	const name, adv = "Smasher", "minecraft:adventure/overoverkill"
	srv, connect := servertest.Start(t, servertest.CombatTestConfig())
	c := connect()
	c.JoinAs(name)
	srv.SetMoveChecks(false)
	c.MoveToGround(2.5, 64, 5.5, true)
	c.MoveToGround(2.5, 64, 5.5, true)
	srv.GiveSlot(name, player.HotbarSlot, protocol.ItemStack{ItemID: servertest.MustItemID(t, "minecraft:mace"), Count: 1})
	since := srv.TicksRun()
	servertest.UntilTicks(t, srv, 200, "the swing winds up", func() bool {
		return srv.TicksRun() >= since+2 && srv.AttackTicks(name) >= maceSwingRecovered
	})
	golem := srv.SpawnMob("minecraft:iron_golem", 4.5, 64, 5.5)
	srv.SetMobNoAI(golem)
	c.Settle()

	smash := func(from float64) {
		for y := from; y > 66; y -= 10 {
			c.MoveToGround(2.5, y, 5.5, false)
		}
		c.MoveToGround(2.5, 66, 5.5, false)
		c.Settle()
		c.Attack(golem)
		c.Settle()
	}
	smash(72)
	if srv.AdvancementObtained(name, adv, "overoverkill") {
		t.Fatal("a six-block smash obtained overoverkill")
	}
	if hp, ok := srv.MobHealth(golem); !ok || hp >= 100 {
		t.Fatalf("the smash did not land: health %v, alive %v", hp, ok)
	}
	srv.SetMobHealth(golem, 100)
	servertest.UntilTicks(t, srv, 200, "the swing winds up", func() bool {
		return srv.AttackTicks(name) >= maceSwingRecovered
	})
	smash(300)
	if !srv.AdvancementObtained(name, adv, "overoverkill") {
		t.Error("a smash after a fall from the sky did not obtain overoverkill")
	}
}
