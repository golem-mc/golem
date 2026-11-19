package mobs

import (
	"slices"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestAHarnessedHappyGhastSeatsRidersAndFliesWhereTheFirstSteers: a plain
// click on a harnessed ghast seats the player with the goggles coming down,
// a second player takes the next seat, the first's Move Vehicle reports fly
// it, and stepping off sets the rider on top; the last one off lifts the
// goggles.
// vanilla: HappyGhast.mobInteract, addPassenger, removePassenger,
// getControllingPassenger, getDismountLocationForPassenger
func TestAHarnessedHappyGhastSeatsRidersAndFliesWhereTheFirstSteers(t *testing.T) {
	t.Parallel()
	srv, connect, c := wolfSceneWith(t, "Rider", servertest.MobTestConfig())
	id := srv.SpawnMob("minecraft:happy_ghast", 4.5, 64, 0.5)
	srv.SetMobNoAI(id)
	giveHand(t, srv, "Rider", "minecraft:blue_harness", 1)
	c.UseOn(id)
	watchWolf(t, c, srv, id, 2)
	if got := bodyItem(srv, id); got != "minecraft:blue_harness" {
		t.Fatalf("body = %q, want the blue harness", got)
	}

	c.UseOn(id)
	seen := watchWolf(t, c, srv, id, 2)
	if got := srv.MobPassengerIDs(id); !slices.Equal(got, []int32{c.PlayerEntityID}) {
		t.Fatalf("passengers = %v after the click, want the rider %d", got, c.PlayerEntityID)
	}
	if !seen.heard("minecraft:entity.happy_ghast.harness_goggles_down") {
		t.Errorf("no goggles-down sound in %v", seen.sounds)
	}

	other := connect().Join("Second")
	other.MoveToGround(2.5, 64, 0.5, true)
	other.Settle()
	other.UseOn(id)
	other.Settle()
	if got := srv.MobPassengerIDs(id); !slices.Equal(got, []int32{c.PlayerEntityID, other.PlayerEntityID}) {
		t.Fatalf("passengers = %v, want the rider then the second player", got)
	}

	c.Send(protocol.IDPlayMoveVehicleIn, func(b *protocol.Buffer) {
		b.PutF64(4.5)
		b.PutF64(68)
		b.PutF64(0.5)
		b.PutF32(0)
		b.PutF32(0)
		b.PutBool(false)
	})
	c.SettleAndCollect(func(pid int32, _ []byte) {
		if pid == protocol.IDPlayMoveVehicle {
			t.Error("the steered ghast's reported move was put back, want it accepted")
		}
	})
	if _, y, _, _ := srv.MobPos(id); y != 68 {
		t.Errorf("ghast at y=%v after the report, want 68", y)
	}
	srv.TickMobs(1)
	other.Settle()
	if pos := srv.PlayerPosition("Second"); pos.Y < 71 {
		t.Errorf("the second rider is at y=%v, want carried up onto the seat at 71.4", pos.Y)
	}

	other.SendSneak(true)
	other.Settle()
	if pos := srv.PlayerPosition("Second"); pos.Y != 72 {
		t.Errorf("the second rider stepped off to y=%v, want on top of the ghast at 72", pos.Y)
	}
	c.SendSneak(true)
	seen = watchWolf(t, c, srv, id, 2)
	if got := srv.MobPassengerIDs(id); len(got) != 0 {
		t.Errorf("passengers = %v after both stepped off", got)
	}
	if !seen.heard("minecraft:entity.happy_ghast.harness_goggles_up") {
		t.Errorf("no goggles-up sound in %v", seen.sounds)
	}
}
