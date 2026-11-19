package mobs

import (
	"github.com/golem-mc/golem/internal/server/servertest"

	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
)

// useHeldOnFreshMob stands a fresh player beside a mob summoned with the
// tag, hands them the item, kills the mob when asked (the tick is stopped, so
// the body lies where it fell), uses the item on it and returns the slot
// changes and sounds the use caused.
func useHeldOnFreshMob(t *testing.T, kind, item, tag string, kill bool) (slots, milkSounds int, held protocol.ItemStack) {
	t.Helper()
	srv, c := servertest.Join(t, servertest.MobTestConfig(), "Milker", servertest.OpsListing(t, "Milker"))
	c.MoveToGround(2.5, 64, 5.5, true)
	c.Settle()
	srv.StopTick()

	id := servertest.ItemIDOrDie(t, item)
	srv.GiveSlot("Milker", player.HotbarSlot, protocol.ItemStack{ItemID: id, Count: 1})
	c.Settle()

	c.ChatCommand(`summon minecraft:` + kind + ` 5.5 64 5.5 ` + tag)
	mob := c.ExpectMobSpawn(protocol.EntityTypeCow)
	if kill && !srv.KillMob(mob.EntityID) {
		t.Fatal("no live mob to kill")
	}
	c.UseOn(mob.EntityID)
	c.SettleAndCollect(func(pid int32, _ []byte) {
		switch pid {
		case protocol.IDPlayContainerSlot:
			slots++
		case protocol.IDPlaySoundEffect:
			// The milker is left out of the milking sound, so it only
			// reaches a second listener; any sound here is a stray.
			milkSounds++
		}
	})
	return slots, milkSounds, srv.InventorySlot("Milker", player.HotbarSlot)
}

func TestABucketOnABabyCowMilksNothing(t *testing.T) {
	t.Parallel()
	slots, sounds, held := useHeldOnFreshMob(t, "cow", "minecraft:bucket", `{Age:-24000}`, false)
	if slots != 0 || sounds != 0 || held.ItemID != servertest.ItemIDOrDie(t, "minecraft:bucket") {
		t.Errorf("a calf's bucket: %d slot changes, %d milk sounds, hand %+v; want a bucket and nothing sent", slots, sounds, held)
	}
}
