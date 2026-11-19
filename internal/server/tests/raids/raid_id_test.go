package raids

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/nbt"
)

// TestARaiderSavesItsRaidId: an enrolled raider writes RaidId, and a raider
// loaded from that compound after raids.dat reloads rejoins the same raid in
// its wave.
// vanilla: Raider.addAdditionalSaveData, Raider.readAdditionalSaveData
func TestARaiderSavesItsRaidId(t *testing.T) {
	t.Parallel()
	srv, c, _ := village(t, rules.DifficultyNormal)
	startRaid(t, c, srv, 0)
	id := srv.SpawnMob("minecraft:pillager", 6.5, 64, 6.5)
	run(c, srv, 25)
	tag, ok := srv.MobSaveTag(id)
	if !ok {
		t.Fatal("no saved tag for the pillager")
	}
	raidID, ok := nbt.IntField(tag, "RaidId")
	if !ok || raidID < 1 {
		t.Fatalf("RaidId = %v, %v; want the raid's id", raidID, ok)
	}
	srv.SaveRaids()
	srv.ReloadRaids()
	loaded := srv.SpawnMobFromTag(tag, 6.5, 64, 6.5)
	run(c, srv, 21)
	if in, _ := srv.MobRaid(loaded); !in {
		t.Fatal("the reloaded raider is not in its raid")
	}
	if got := srv.Raids()[0].Alive; got != 1 {
		t.Errorf("raiders counted = %d, want 1", got)
	}
	if got := srv.Raids()[0].Progress; got != 1 {
		t.Errorf("restored health progress = %v, want 1", got)
	}
	back, _ := srv.MobSaveTag(loaded)
	if again, _ := nbt.IntField(back, "RaidId"); again != raidID {
		t.Errorf("RaidId after reload = %d, want %d", again, raidID)
	}
}
