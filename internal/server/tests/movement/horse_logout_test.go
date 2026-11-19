package movement

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/nbt"
)

// TestARiderLeavingOnAHorseRejoinsSeated: the file carries the horse and
// the rider attached, the horse leaves the world with them, and the next
// join raises it with the player aboard.
// vanilla: ServerPlayer.saveParentVehicle, PlayerList.remove
func TestARiderLeavingOnAHorseRejoinsSeated(t *testing.T) {
	t.Parallel()
	cfg := servertest.MobTestConfig()
	cfg.LevelName = t.TempDir()
	srv, connect := servertest.Start(t, cfg, servertest.OpsListing(t, "Rider"))
	c := connect()
	c.Join("Rider")
	c.ChatCommand("summon minecraft:horse 0.5 64 0.5 {Tame:1b}")
	c.Settle()
	ids := srv.MobIDsOfKind("minecraft:horse")
	if len(ids) != 1 {
		t.Fatalf("mobs = %v, want the one horse", ids)
	}
	c.UseOn(ids[0])
	c.Settle()
	if got := srv.RiddenMobIDs(); len(got) != 1 {
		t.Fatalf("ridden mobs = %v before leaving, want 1", got)
	}
	c.Disconnect()

	root := servertest.ReadPlayerFile(t, servertest.PlayerFile(cfg.LevelName, "Rider"))
	saved, ok := nbt.Get[nbt.Compound](root, "RootVehicle")
	if !ok {
		t.Fatal("no RootVehicle in the file of a rider who left")
	}
	if id, _ := nbt.Get[nbt.String](saved["Entity"].(nbt.Compound), "id"); id != "minecraft:horse" {
		t.Errorf("RootVehicle entity = %q, want the horse", id)
	}
	if got := srv.RiddenMobIDs(); len(got) != 0 {
		t.Errorf("ridden mobs = %v after leaving", got)
	}

	back := connect()
	back.Join("Rider")
	servertest.UntilTicks(t, srv, 200, "the rider seated again", func() bool { return len(srv.RiddenMobIDs()) == 1 })
	if got := srv.RiddenMobIDs(); len(got) != 1 {
		t.Fatalf("ridden mobs = %v after rejoining, want 1", got)
	}
}
