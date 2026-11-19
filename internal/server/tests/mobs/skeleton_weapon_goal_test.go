package mobs

import (
	"fmt"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestASkeletonWithoutABowFightsInMelee stands a skeleton that cannot walk a
// block from a survival player at midnight: holding a sword or nothing it
// strikes in melee, and once handed a bow it shoots again.
// vanilla: AbstractSkeleton.reassessWeaponGoal
func TestASkeletonWithoutABowFightsInMelee(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, equipment string
	}{
		{"iron sword", `,equipment:{mainhand:{id:"minecraft:iron_sword",count:1}}`},
		// Summoned with NBT, a skeleton skips finalizeSpawn and its bow.
		{"empty hands", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, connect := servertest.Start(t, servertest.HostileTestConfig(), servertest.OpsListing(t, "Prey"))
			srv.SetDayTime(18000) // midnight, whose dark is no daylight burn
			servertest.FreezeUntilHeld(t, srv)
			c := connect()
			c.JoinAs("Prey")
			c.MoveToGround(0.5, 64, 0.5, true)
			c.Settle()

			pos := srv.PlayerPosition("Prey")
			c.ChatCommand(fmt.Sprintf(`summon minecraft:skeleton %.2f %.2f %.2f {attributes:[{id:"minecraft:movement_speed",base:0.0d}]%s}`,
				pos.X+1, pos.Y, pos.Z, tc.equipment))
			c.ExpectMobSpawn(protocol.EntityTypeSkeleton)
			servertest.HoldTicks(t, srv)

			// Target pick, the melee goal's first search and its swing fit
			// well inside 100 ticks.
			servertest.UntilTicks(t, srv, 100, "the skeleton's blow", func() bool { return srv.PlayerHealth("Prey") < 20 })

			c.ChatCommand("item replace entity @e[type=minecraft:skeleton] weapon.mainhand with minecraft:bow")
			// The melee hunt stops, the bow goal starts and draws for its 20
			// ticks before the first arrow.
			if !servertest.ASpawnOfWithinTicks(c, srv, 100, protocol.EntityTypeArrow) {
				t.Error("the skeleton handed a bow shot no arrow within 100 ticks")
			}
		})
	}
}

// TestASkeletonDrawingAnOffHandBowSetsTheOffHandBit summons a skeleton with a
// bow only in its off hand beside a survival player at midnight: its draw
// tells the client the use is in the off hand.
// vanilla: RangedBowAttackGoal.tick, ProjectileUtil.getWeaponHoldingHand,
// LivingEntity.startUsingItem
func TestASkeletonDrawingAnOffHandBowSetsTheOffHandBit(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.HostileTestConfig(), servertest.OpsListing(t, "Prey"))
	srv.SetDayTime(18000) // midnight, whose dark is no daylight burn
	servertest.FreezeUntilHeld(t, srv)
	c := connect()
	c.JoinAs("Prey")
	c.MoveToGround(0.5, 64, 0.5, true)
	c.Settle()

	pos := srv.PlayerPosition("Prey")
	c.ChatCommand(fmt.Sprintf(`summon minecraft:skeleton %.2f %.2f %.2f {attributes:[{id:"minecraft:movement_speed",base:0.0d}],equipment:{offhand:{id:"minecraft:bow",count:1}}}`,
		pos.X+4, pos.Y, pos.Z))
	skeleton := c.ExpectMobSpawn(protocol.EntityTypeSkeleton)
	servertest.HoldTicks(t, srv)

	// Target pick and the first draw fit well inside 100 ticks.
	e, ok := c.MetaEntryWithinTicks(srv, 100, skeleton.EntityID, protocol.MetadataIndexHandState)
	if !ok {
		t.Fatal("the skeleton raised no bow within 100 ticks")
	}
	want := uint8(protocol.EntityFlagHandActive | protocol.EntityFlagActiveHandOff)
	if e.ByteVal != want {
		t.Errorf("hand state = %#02x, want %#02x (using, off hand)", e.ByteVal, want)
	}
}
