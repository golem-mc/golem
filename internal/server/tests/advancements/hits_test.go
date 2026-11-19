package advancements

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// The hit triggers: used_totem, entity_hurt_player and target_hit.

// TestATotemSaveObtainsPostmortal hurts a player holding a totem of
// undying: a blow it survives does not obtain totem_of_undying, the lethal
// one the totem turns does. vanilla: LivingEntity.checkTotemDeathProtection
func TestATotemSaveObtainsPostmortal(t *testing.T) {
	t.Parallel()
	const name, totem = "Survivor", "minecraft:adventure/totem_of_undying"
	srv, _ := servertest.Join(t, servertest.SurvivalTestConfig(), name)
	srv.GiveSlot(name, player.HotbarSlot, protocol.ItemStack{ItemID: servertest.ItemIDOrDie(t, "minecraft:totem_of_undying"), Count: 1})
	srv.StopTick()
	if !srv.Hurt(name, 2) {
		t.Fatal("the first blow did not land")
	}
	if srv.AdvancementObtained(name, totem, "used_totem") {
		t.Fatal("a blow the player survived obtained totem_of_undying")
	}
	srv.OpenHurtWindow(name, 0)
	srv.Hurt(name, 40)
	if !srv.AdvancementObtained(name, totem, "used_totem") {
		t.Error("the totem's save from a lethal blow did not obtain totem_of_undying")
	}
}

// TestBlockingAnArrowObtainsNotTodayThankYou raises a shield: a zombie's
// blow it blocks does not obtain deflect_arrow, an arrow it blocks does.
// vanilla: LivingEntity.hurtServer, EntityHurtPlayerTrigger
func TestBlockingAnArrowObtainsNotTodayThankYou(t *testing.T) {
	t.Parallel()
	const name, deflect = "Blocker", "minecraft:story/deflect_arrow"
	srv, c := servertest.Join(t, servertest.SurvivalTestConfig(), name)
	c.MoveToGround(0.5, 64, 0.5, true)
	srv.GiveSlot(name, player.OffHandSlot, protocol.ItemStack{ItemID: servertest.ItemIDOrDie(t, "minecraft:shield"), Count: 1})
	c.UseItemRightClicks(protocol.HandOff)
	c.Settle()
	servertest.HoldTicks(t, srv)
	// Past the block delay: a shield just raised blocks nothing yet.
	servertest.RunTicks(t, srv, 6)

	if srv.HurtFrom(name, 3, 0.5, 3.5) {
		t.Fatal("the zombie's blow from the front landed on the raised shield")
	}
	if srv.AdvancementObtained(name, deflect, "deflected_projectile") {
		t.Fatal("a blocked zombie's blow obtained deflect_arrow")
	}
	servertest.RunTicks(t, srv, 20)
	srv.ShootArrow(0.5, 65, 3.5, 0, 0, -3)
	servertest.UntilTicks(t, srv, 40, "an arrow the shield blocked obtains deflect_arrow", func() bool {
		return srv.AdvancementObtained(name, deflect, "deflected_projectile")
	})
}

// TestHittingATargetsCentreFromAfarObtainsBullseye has the player's arrows
// hit a target block's centre: with the player 10 blocks away it does not
// obtain bullseye, with the player 40 away it does.
// vanilla: TargetBlock.onProjectileHit, TargetBlockTrigger
func TestHittingATargetsCentreFromAfarObtainsBullseye(t *testing.T) {
	t.Parallel()
	const name, bullseye = "Archer", "minecraft:adventure/bullseye"
	srv, c := servertest.Join(t, servertest.SurvivalTestConfig(), name)
	if !srv.SetBlock(6, 65, 8, servertest.BlockState(t, "minecraft:target")) {
		t.Fatal("the target's chunk is not loaded")
	}
	c.MoveToGround(16.5, 64, 8.5, true)
	c.Settle()
	servertest.HoldTicks(t, srv)
	powered := func() bool {
		_, props := servertest.StateProps(srv.BlockAt(6, 65, 8))
		return props["power"] == "15"
	}
	unpowered := func() bool { return !powered() }

	srv.ShootPlayerArrow(name, 7.5, 65.5, 8.5, -3, 0, 0)
	servertest.UntilTicks(t, srv, 40, "the near shot powers the target", powered)
	if srv.AdvancementObtained(name, bullseye, "bullseye") {
		t.Fatal("a centre hit 10 blocks from the player obtained bullseye")
	}
	servertest.UntilTicks(t, srv, 40, "the target's power resets", unpowered)

	c.MoveToGround(46.5, 64, 8.5, true)
	c.Settle()
	srv.ShootPlayerArrow(name, 7.5, 65.5, 8.5, -3, 0, 0)
	servertest.UntilTicks(t, srv, 40, "a centre hit 40 blocks from the player obtains bullseye", func() bool {
		return srv.AdvancementObtained(name, bullseye, "bullseye")
	})
}
