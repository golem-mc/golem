package combat

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestSweepSparesOwnPet sweeps a zombie with the fencer's own tamed wolf and
// a teammate's beside it, and a stranger's wolf behind: the zombie and the
// stranger's wolf are hurt, the two allied wolves are not.
// vanilla: Player.doSweepAttack, TamableAnimal.considersEntityAsAlly
func TestSweepSparesOwnPet(t *testing.T) {
	t.Parallel()
	srv, connect := friendlyFireServer(t)
	fencer := connect()
	fencer.JoinAs("Fencer")
	// Two moves, so the first's displacement is not still the recorded
	// movement that makes the sweep's feedback reject.
	fencer.MoveToGround(2.5, 64, 5.5, true)
	fencer.MoveToGround(2.5, 64, 5.5, true)
	mate := connect()
	mate.JoinAs("Mate")
	mate.MoveToGround(12.5, 64, 12.5, true)
	fencer.Settle()
	mate.Settle()
	servertest.HoldTicks(t, srv)
	srv.GiveSlot("Fencer", player.HotbarSlot, protocol.ItemStack{
		ItemID: servertest.MustItemID(t, "minecraft:iron_sword"), Count: 1,
	})
	srv.RunLine("team add red")
	srv.RunLine("team join red Fencer")
	srv.RunLine("team join red Mate")
	// Planted after the swing recovers, so nothing wanders before the blow.
	servertest.RunTicks(t, srv, servertest.SwingRecovered)
	zombie := srv.SpawnMob("minecraft:zombie", 4.5, 64, 5.5)
	own := srv.SpawnMob("minecraft:wolf", 4.5, 64, 6.5)
	mates := srv.SpawnMob("minecraft:wolf", 5.45, 64, 5.5)
	strangers := srv.SpawnMob("minecraft:wolf", 3.5, 64, 6.5)
	if !srv.TameMobFor(own, "Fencer") || !srv.TameMobFor(mates, "Mate") ||
		!srv.SetMobOwner(strangers, protocol.OfflineUUID("Stranger")) {
		t.Fatal("a wolf could not be tamed")
	}
	hp := func(id int32) float64 {
		h, ok := srv.MobHealth(id)
		if !ok {
			t.Fatalf("mob %d is gone", id)
		}
		return h
	}
	before := map[int32]float64{zombie: hp(zombie), own: hp(own), mates: hp(mates), strangers: hp(strangers)}

	fencer.Attack(zombie)
	fencer.Settle()
	if got := hp(zombie); got >= before[zombie] {
		t.Fatalf("zombie health = %v, want the blow to have hurt it from %v", got, before[zombie])
	}
	if got := hp(strangers); got >= before[strangers] {
		t.Errorf("a stranger's wolf health = %v, want the sweep to have hurt it from %v", got, before[strangers])
	}
	if got := hp(own); got != before[own] {
		t.Errorf("the fencer's own wolf health = %v, want it spared at %v", got, before[own])
	}
	if got := hp(mates); got != before[mates] {
		t.Errorf("a teammate's wolf health = %v, want it spared at %v", got, before[mates])
	}
}
